package poo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/session"
)

// leaseFile is the on-disk shape the store's lease writes, mirrored so a
// test can put a foreign lease on disk and prove the store leaves it alone.
type leaseFile struct {
	OwnerID     string    `json:"owner_id"`
	Token       string    `json:"token"`
	PID         int       `json:"pid"`
	Host        string    `json:"host"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// The continuation: a durable session's loaded records name a run that a
// crash left unsettled, and Session.ResumeRun carries it on.
//
// Every test drives the real path: records are written to a store's file the
// way a killed process leaves them, session.Resume loads and restores them,
// and the resumed run is driven by the public API.

// resumeTool is a tool a continuation test drives.
//
// It counts its calls, so a test can tell a replayed call from one the
// recovery answered without the model.
type resumeTool struct {
	// name is the tool's name.
	name string
	// class is the tool's side-effect class.
	class poo.ToolKind
	// idempotent reports that running the tool twice is safe.
	idempotent bool
	// call runs the tool, when the test needs a known point in it.
	call func(ctx context.Context) (poo.ToolResult, error)
	// nest are the calls the tool makes through the session's nested
	// seam, when the test needs a call a tool makes of its own.
	nest []poo.NestedCall

	mu    sync.Mutex
	calls []string
}

// Calls returns the arguments the tool ran with, in order.
func (t *resumeTool) Calls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

// Spec describes the tool.
func (t *resumeTool) Spec() poo.ToolSpec {
	class := t.class
	if class == "" {
		class = poo.ClassReadOnly
	}
	return poo.ToolSpec{
		Name:        t.name,
		Description: "a tool the continuation tests drive",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"additionalProperties":false}`),
		Class:       class,
		Idempotent:  t.idempotent,
	}
}

// Prepare accepts anything.
func (t *resumeTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var args struct {
		Text string `json:"text"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
	}
	return resumePrepared{text: args.Text}, nil
}

// Call records the call and returns the result.
func (t *resumeTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p := in.(resumePrepared)
	t.mu.Lock()
	t.calls = append(t.calls, p.text)
	nest := append([]poo.NestedCall(nil), t.nest...)
	t.mu.Unlock()
	if len(nest) > 0 {
		if env.Nested == nil {
			return poo.ToolResult{Text: "no nested caller", IsError: true}, nil
		}
		if _, err := env.Nested.Call(ctx, nest); err != nil {
			return poo.ToolResult{Text: err.Error(), IsError: true}, nil
		}
	}
	if t.call != nil {
		return t.call(ctx)
	}
	return poo.ToolResult{Text: "ran " + t.name}, nil
}

// resumePrepared is a prepared call of a resume tool.
type resumePrepared struct{ text string }

// Summary describes the call.
func (p resumePrepared) Summary() string { return p.text }

// allowAsker is the asker the continuation tests answer asks with.
//
// It allows every ask, and it names itself so an answer's record says who
// answered.
type allowAsker struct {
	// asks receives every ask the policy put to it.
	asks chan perm.Ask
}

// Ask answers the ask and reports it.
func (a *allowAsker) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	if a.asks != nil {
		select {
		case a.asks <- ask:
		default:
		}
	}
	return perm.DecisionAllow, nil
}

// Available reports that the asker can answer.
func (a *allowAsker) Available() bool { return true }

// Budget is the asker's budget.
func (a *allowAsker) Budget() time.Duration { return time.Minute }

// Name names the asker, so the answer's record says who answered it.
func (a *allowAsker) Name() string { return "allow-everything" }

// resumeOptions builds the options a continuation test resumes with.
//
// The session's own settings are the ones its header recorded, so the
// restore's compatibility check passes: the profile and the model are the
// header's, and the tools are the ones this build links.
func resumeOptions(t *testing.T, tr poo.ChatTransport, tools ...*resumeTool) poo.Options {
	t.Helper()
	opts := poo.Options{
		Transport:    tr,
		Sink:         &recordingSink{},
		SessionID:    "s1",
		Tools:        map[string]poo.Tool{},
		Model:        poo.ModelInfo{Name: "fast", ModelID: "test-model", ContextLimit: 32768, Ops: []poo.Op{poo.OpChat, poo.OpCompact}},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
		Profiles:     []poo.Profile{{Name: "code"}},
		Models: map[string]poo.ModelInfo{
			"fast": {Name: "fast", ModelID: "test-model", ContextLimit: 32768, Ops: []poo.Op{poo.OpChat, poo.OpCompact}},
		},
		SettingsHash: "settings-1",
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
	}
	for _, tool := range tools {
		opts.Tools[tool.name] = tool
		opts.ToolOrder = append(opts.ToolOrder, tool.name)
	}
	return opts
}

// resumeHeader builds the header of a durable session the test writes.
func resumeHeader(t *testing.T, tools ...*resumeTool) *poo.SessionCreatedRecord {
	t.Helper()
	infos := make([]poo.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		infos = append(infos, poo.ToolInfo{
			Name:       tool.name,
			Class:      tool.Spec().Class,
			Idempotent: tool.idempotent,
		})
	}
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:     poo.FormatVersion,
			SessionID:  "s1",
			Mode:       string(session.ModeDurable),
			CreatedAt:  "2026-01-01T00:00:00Z",
			WorkingDir: "/tmp",
			Profile:    "code",
			Roles: poo.ModelRoles{Chat: &poo.ModelInfo{
				Name: "fast", ModelID: "test-model", ContextLimit: 32768,
				Ops: []poo.Op{poo.OpChat, poo.OpCompact},
			}},
			Tools:        infos,
			SettingsHash: "settings-1",
			Build:        poo.BuildInfo{Name: "poo", Version: "test"},
		},
	}
}

// startedRun returns the record of a run that started and never settled.
func startedRun(runID string) *poo.RunStartedRecord {
	return &poo.RunStartedRecord{
		RecordMeta: poo.NewRecordMeta("run.started"),
		RunID:      runID,
		StartedAt:  "2026-01-01T00:00:00Z",
	}
}

// userMessage returns a user message record.
func userMessage(runID string, turn int, text string) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginUser),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleUser,
		Content:    []poo.ContentBlock{{Kind: poo.ContentText, Text: text}},
	}
}

// assistantCall returns the assistant message that asked for calls.
func assistantCall(runID string, turn int, calls ...poo.ContentBlock) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleAssistant,
		Content:    calls,
	}
}

// callBlock returns the content block of one tool call.
func callBlock(callID, tool, args string) poo.ContentBlock {
	return poo.ContentBlock{
		Kind:       poo.ContentToolCall,
		ToolCallID: callID,
		Name:       tool,
		Arguments:  json.RawMessage(args),
	}
}

// callRecord returns the record of a call the model made.
func callRecord(runID string, turn int, handle, callID, tool, args string) *poo.ToolCallRecord {
	return &poo.ToolCallRecord{
		RecordMeta: poo.NewRecordMetaFrom("tool.call", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Handle:     handle,
		CallID:     callID,
		Tool:       tool,
		Arguments:  json.RawMessage(args),
	}
}

// startedCall returns the write-ahead record of a side-effecting call.
func startedCall(runID string, turn int, handle, callID, tool string) *poo.ToolStartedRecord {
	return &poo.ToolStartedRecord{
		RecordMeta: poo.NewRecordMeta("tool.started"),
		RunID:      runID,
		Turn:       turn,
		Handle:     handle,
		CallID:     callID,
		Tool:       tool,
	}
}

// askedRecord returns the record of a permission ask about a call.
func askedRecord(runID, askID, handle, tool, class, deadline, fallback string) *poo.PermissionAskedRecord {
	return &poo.PermissionAskedRecord{
		RecordMeta: poo.NewRecordMeta("permission.asked"),
		RunID:      runID,
		AskID:      askID,
		Handle:     handle,
		Tool:       tool,
		Summary:    "a call the policy asks about",
		Class:      poo.ToolKind(class),
		Reason:     poo.AskReasonPolicy,
		Deadline:   deadline,
		Fallback:   fallback,
	}
}

// hookRecord returns a turn hook's record.
func hookRecord(runID string, turn int, phase, status string, index int, interrupted bool) *poo.TurnHookRecord {
	return &poo.TurnHookRecord{
		RecordMeta:  poo.NewRecordMeta("turn.hook"),
		RunID:       runID,
		Turn:        turn,
		Phase:       phase,
		Status:      status,
		HookIndex:   index,
		Interrupted: interrupted,
	}
}

// queuedInput returns the record of an input the session accepted and never
// delivered.
func queuedInput(runID, inputID string, mode poo.InputMode, text string) *poo.InputQueuedRecord {
	return &poo.InputQueuedRecord{
		RecordMeta: poo.NewRecordMeta("input.queued"),
		SessionID:  "s1",
		RunID:      runID,
		InputID:    inputID,
		Mode:       mode,
		Text:       text,
	}
}

// writeResumeLog writes a durable session's records the way a killed process
// leaves them, and returns the store's directory.
func writeResumeLog(t *testing.T, recs ...poo.Record) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	defer f.Close()
	for _, rec := range recs {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("encoding the record: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("writing the record: %v", err)
		}
	}
	return dir
}

// readResumeLog reads a session's records back, without recovering them.
//
// A read through the store would recover the session again; the test reads
// the file as a reader of the log does.
func readResumeLog(t *testing.T, dir, id string) []poo.Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	codec := session.NewCodec()
	var recs []poo.Record
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rec, err := codec.Decode([]byte(line))
		if err != nil {
			t.Fatalf("decoding %s: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

// namedRecords returns the records with a record name.
func namedRecords(recs []poo.Record, name string) []poo.Record {
	var out []poo.Record
	for _, rec := range recs {
		if rec.Meta().Record == name {
			out = append(out, rec)
		}
	}
	return out
}

// resumeStore builds the store a continuation test loads from.
func resumeStore(t *testing.T, dir string, fns ...session.Option) *session.FileStore {
	t.Helper()
	store, err := session.NewStore(dir, fns...)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// resumeDurable loads a session and restores it, and registers the
// transition check the FSM's tests use.
func resumeDurable(t *testing.T, store *session.FileStore, opts poo.Options, fns ...poo.Option) *session.Resumed {
	t.Helper()
	resumed, err := session.Resume(context.Background(), store, "s1", opts, fns...)
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	t.Cleanup(func() { checkTransitions(t, resumed.Session) })
	t.Cleanup(func() { resumed.Session.Close() })
	return resumed
}

// waitResumed waits for the session's run to finish.
func waitResumed(t *testing.T, s *poo.Session) poo.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.Wait(ctx)
	if err != nil {
		t.Fatalf("waiting for the resumed run: %v", err)
	}
	return result
}

// TestDurableResumeReAsksALiveAskWithItsDeadline covers the live ask: the
// run the records left waiting asks again, with the ask id and the deadline
// the log recorded, and the wait is published until the answer lands.
func TestDurableResumeReAsksALiveAskWithItsDeadline(t *testing.T) {
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "delete the build"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"build"}`)),
		// The ask is written before its call's record, and the crash left
		// the call unrecorded: the ask names the call it waits on.
		askedRecord("r1", "ask_1", "r1", "echo", string(poo.ClassSideEffecting), deadline, "deny"),
	)
	store := resumeStore(t, dir)
	asker := &allowAsker{asks: make(chan perm.Ask, 4)}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	opts := resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), tool)
	opts.Policy = policy

	resumed := resumeDurable(t, store, opts)
	if resumed.Restored.PendingRun == nil {
		t.Fatal("the restore holds no pending run, want the run the ask holds open")
	}
	if resumed.Restored.PendingRun.Phase != poo.StateExecuting {
		t.Errorf("the run resumes at %q, want %q", resumed.Restored.PendingRun.Phase, poo.StateExecuting)
	}
	if got := resumed.Restored.PendingRun.RunID; got != "r1" {
		t.Errorf("the pending run is %q, want %q", got, "r1")
	}
	if resumed.Session.Running() {
		t.Error("the restored session runs without being asked to, want it idle")
	}
	if _, ok := resumed.Session.Awaiting(); ok {
		t.Error("the idle session publishes a wait, want none before the resumption")
	}

	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	// The plan's ask is published as the run's wait, so a client sees what
	// the resumed run is waiting on.
	if awaiting, ok := resumed.Session.Awaiting(); ok {
		if awaiting.AskID != "ask_1" {
			t.Errorf("the published wait names the ask %q, want %q", awaiting.AskID, "ask_1")
		}
		if awaiting.Kind != poo.AskKindPermission {
			t.Errorf("the published wait is of the kind %q, want %q", awaiting.Kind, poo.AskKindPermission)
		}
	}
	waitResumed(t, resumed.Session)
	if awaiting, ok := resumed.Session.Awaiting(); ok {
		t.Errorf("the answered ask is still published as %+v, want none", awaiting)
	}

	// The asker was asked about the call the log named.
	select {
	case ask := <-asker.asks:
		if ask.Tool != "echo" {
			t.Errorf("the asker was asked about the tool %q, want %q", ask.Tool, "echo")
		}
		if ask.RunID != "r1" {
			t.Errorf("the asker was asked about the run %q, want %q", ask.RunID, "r1")
		}
	default:
		t.Fatal("the asker was never asked, want the resumed run to ask again")
	}

	// The run ran the call the ask was about, and settled.
	if calls := tool.Calls(); len(calls) != 1 {
		t.Errorf("the tool ran %d time(s), want 1", len(calls))
	}
	recs := readResumeLog(t, dir, "s1")
	if got := len(namedRecords(recs, "run.resumed")); got != 1 {
		t.Fatalf("the log holds %d resumption(s), want 1", got)
	}
	asked := namedRecords(recs, "permission.asked")
	if len(asked) != 2 {
		t.Fatalf("the log holds %d ask(s), want the log's and the resumed run's", len(asked))
	}
	second, ok := asked[1].(*poo.PermissionAskedRecord)
	if !ok {
		t.Fatalf("the second ask is %T", asked[1])
	}
	// The re-ask carries the log's deadline, not a fresh one.
	if second.Deadline != deadline {
		t.Errorf("the re-ask's deadline is %q, want the recorded %q", second.Deadline, deadline)
	}
	if second.AskID != "ask_1" {
		t.Errorf("the re-ask's id is %q, want the recorded %q", second.AskID, "ask_1")
	}
	// The re-ask names its own call, so a later load pairs it exactly.
	if second.Handle != "r1" || second.CallID != "call_1" {
		t.Errorf("the re-ask names the call %q/%q, want r1/call_1", second.Handle, second.CallID)
	}
	answered := namedRecords(recs, "permission.answered")
	if len(answered) != 1 {
		t.Fatalf("the log holds %d answer(s), want 1", len(answered))
	}
	if got := answered[0].(*poo.PermissionAnsweredRecord); got.Decision != "allow" {
		t.Errorf("the answer is %q, want an allow", got.Decision)
	}
	if got := len(namedRecords(recs, "run.settled")); got != 1 {
		t.Errorf("the log holds %d settle(s), want 1", got)
	}
}

// TestDurableResumePairsAReAskWithTheCallItNames covers the resumed batch that
// holds a replayed call and a live ask for one tool: the log's ask belongs to
// the call it names, so the live ask keeps the id and deadline the client was
// shown, and the replay asks afresh.
func TestDurableResumePairsAReAskWithTheCallItNames(t *testing.T) {
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	// The ask is written before its call's record, the way the core writes
	// it, so the log holds a record for the earlier call only: that one
	// replays, and the ask waits on the call it names.
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "delete both builds"),
		assistantCall("r1", 1,
			callBlock("call_1", "echo", `{"text":"replay"}`),
			callBlock("call_2", "echo", `{"text":"live"}`),
		),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"replay"}`),
		askedRecord("r1", "ask_1", "r2", "echo", string(poo.ClassSideEffecting), deadline, "deny"),
	)
	store := resumeStore(t, dir)
	asker := &allowAsker{asks: make(chan perm.Ask, 4)}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	opts := resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), tool)
	opts.Policy = policy

	resumed := resumeDurable(t, store, opts)
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	// The replay runs first, then the call the log's ask held open. Both
	// passed the policy, so the tool ran both, in the batch's order.
	if calls := tool.Calls(); len(calls) != 2 {
		t.Fatalf("the tool ran %v, want the replay then the live ask's call", calls)
	} else if calls[0] != "replay" || calls[1] != "live" {
		t.Errorf("the tool ran %v, want the replay then the live ask's call", calls)
	}

	recs := readResumeLog(t, dir, "s1")
	asks := namedRecords(recs, "permission.asked")
	if len(asks) != 3 {
		t.Fatalf("the log holds %d ask(s), want the log's and the resumed run's two", len(asks))
	}
	var live, replay *poo.PermissionAskedRecord
	for _, rec := range asks {
		ask := rec.(*poo.PermissionAskedRecord)
		switch ask.Summary {
		case "live":
			live = ask
		case "replay":
			replay = ask
		}
	}
	if live == nil {
		t.Fatalf("the log holds no resumed ask about the live call, want one")
	}
	if replay == nil {
		t.Fatalf("the log holds no resumed ask about the replayed call, want one")
	}
	// The live ask is the one the client was shown: it keeps the log's id
	// and the deadline the ask had left.
	if live.AskID != "ask_1" {
		t.Errorf("the live ask's id is %q, want the recorded %q", live.AskID, "ask_1")
	}
	if live.Deadline != deadline {
		t.Errorf("the live ask's deadline is %q, want the recorded %q", live.Deadline, deadline)
	}
	// The re-asks name their own calls: the live one waits on the second
	// call, and the replay's ask on the first.
	if live.Handle != "r2" || live.CallID != "call_2" {
		t.Errorf("the live ask names the call %q/%q, want r2/call_2", live.Handle, live.CallID)
	}
	if replay.Handle != "r1" || replay.CallID != "call_1" {
		t.Errorf("the replay's ask names the call %q/%q, want r1/call_1", replay.Handle, replay.CallID)
	}
	// The replay is a fresh ask: it takes neither the id nor the deadline.
	if replay.AskID == "ask_1" {
		t.Errorf("the replay's ask took the id %q, want a fresh one", replay.AskID)
	}
	if replay.AskID == "" {
		t.Error("the replay's ask has no id, want a fresh one")
	}
	if replay.Deadline == deadline {
		t.Errorf("the replay's ask took the recorded deadline %q, want a fresh one", replay.Deadline)
	}
}

// TestDurableResumeDoesNotRunAnAskWhoseCallItCannotFind covers the live ask
// whose call the log never recorded: the call was one a running tool made,
// and the crash cut the run short between the ask and the nested call's own
// tool.call record. The ask is not a call of its own, so the load answers it
// by its fallback, the resumed run replays its parent, and the parent's
// nested call asks afresh.
func TestDurableResumeDoesNotRunAnAskWhoseCallItCannotFind(t *testing.T) {
	parent := &resumeTool{
		name:  "parent",
		class: poo.ClassReadOnly,
		nest:  []poo.NestedCall{{Tool: "child", Arguments: json.RawMessage(`{"text":"nested"}`)}},
	}
	child := &resumeTool{name: "child", class: poo.ClassSideEffecting}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	dir := writeResumeLog(t,
		resumeHeader(t, parent, child),
		startedRun("r1"),
		userMessage("r1", 0, "do the thing"),
		assistantCall("r1", 1, callBlock("call_1", "parent", `{"text":"parent"}`)),
		callRecord("r1", 1, "r1", "call_1", "parent", `{"text":"parent"}`),
		// The ask names the nested call the parent made. The crash came
		// between the ask and the nested call's own record, so the log
		// holds no tool.call for the handle the ask names.
		askedRecord("r1", "ask_1", "r2", "child", string(poo.ClassSideEffecting), deadline, "deny"),
	)
	store := resumeStore(t, dir)
	asker := &allowAsker{asks: make(chan perm.Ask, 4)}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	opts := resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), parent, child)
	opts.Policy = policy

	resumed := resumeDurable(t, store, opts)
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	// The replayed batch ran the parent and no call of its own for the
	// ask: the child ran once, from inside the parent, with the
	// arguments the parent gave it.
	if got := parent.Calls(); len(got) != 1 || got[0] != "parent" {
		t.Errorf("the parent tool ran %v, want the one replayed call", got)
	}
	if got := child.Calls(); len(got) != 1 || got[0] != "nested" {
		t.Errorf("the child tool ran %v, want the parent's nested call once", got)
	}

	recs := readResumeLog(t, dir, "s1")
	// The handle the ask's record carries names the nested call the
	// parent makes again, not a call of the resumed batch's own: a
	// result under it is the tool that made the call reading it back,
	// never the model.
	const askHandle = "r2"
	nestedCall := false
	for _, rec := range namedRecords(recs, "tool.call") {
		call := rec.(*poo.ToolCallRecord)
		if call.Handle != askHandle {
			continue
		}
		if call.Parent == "" {
			t.Errorf("the ask's handle %q names a call of the resumed batch's own, want the parent's nested call", askHandle)
		}
		nestedCall = true
	}
	if !nestedCall {
		t.Errorf("the log holds no nested call under the ask's handle %q, want the parent's", askHandle)
	}
	// No tool message carries the child: the model never saw the nested
	// call, and a message with an empty call id is one the provider
	// rejects.
	for _, rec := range namedRecords(recs, "message") {
		msg := rec.(*poo.MessageRecord)
		if msg.Role != poo.RoleTool {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind != poo.ContentToolResult {
				continue
			}
			if block.ToolCallID == "" {
				t.Errorf("the tool message for %q carries a result with an empty call id", block.Name)
			}
			if block.Name == "child" {
				t.Errorf("the model's history holds a result for the nested tool %q", block.Name)
			}
		}
	}
	// The ask was not run as a call of its own, and the load answered it:
	// the parent's nested call asks a fresh question beside it.
	answered := namedRecords(recs, "permission.answered")
	// One answer resolves the log's ask, and one resolves the fresh ask.
	if len(answered) != 2 {
		t.Fatalf("the log holds %d answer(s), want the log's ask's and the fresh ask's", len(answered))
	}
	var answeredAsk bool
	for _, rec := range answered {
		answer := rec.(*poo.PermissionAnsweredRecord)
		if answer.AskID != "ask_1" {
			continue
		}
		answeredAsk = true
		if answer.Decision != "deny" || answer.Provenance != poo.ProvenanceTimeout {
			t.Errorf("the answer is %q from %q, want a deny from %q", answer.Decision, answer.Provenance, poo.ProvenanceTimeout)
		}
	}
	if !answeredAsk {
		t.Error("the log holds no answer for the unplaceable ask")
	}
	asks := namedRecords(recs, "permission.asked")
	if len(asks) != 2 {
		t.Fatalf("the log holds %d ask(s), want the log's and the parent's nested call's", len(asks))
	}
	// The parent's nested call asks afresh: the answered ask's id and
	// deadline belong to the call the log named, not to this one.
	reask := asks[1].(*poo.PermissionAskedRecord)
	if reask.AskID == "ask_1" {
		t.Errorf("the re-ask took the id %q, want a fresh one", "ask_1")
	}
	if reask.Deadline == deadline {
		t.Errorf("the re-ask took the recorded deadline %q, want a fresh one", deadline)
	}
	if reask.Tool != "child" {
		t.Errorf("the re-ask is about the tool %q, want %q", reask.Tool, "child")
	}
}

// TestDurableResumeDoesNotArmAnUnplaceableAskForTheTopLevelCall covers the
// ask whose recorded call the log never saw: the model called the child
// top-level before the parent that nests it, so the resumed batch asks about
// the top-level child before the parent's nested child. The ask named the
// nested handle, so it cannot be the top-level call's: it is answered by its
// recorded fallback, and the top-level replay asks afresh.
func TestDurableResumeDoesNotArmAnUnplaceableAskForTheTopLevelCall(t *testing.T) {
	child := &resumeTool{name: "child", class: poo.ClassSideEffecting}
	parent := &resumeTool{
		name:  "parent",
		class: poo.ClassReadOnly,
		nest:  []poo.NestedCall{{Tool: "child", Arguments: json.RawMessage(`{"text":"nested"}`)}},
	}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	dir := writeResumeLog(t,
		resumeHeader(t, child, parent),
		startedRun("r1"),
		userMessage("r1", 0, "do both"),
		assistantCall("r1", 1,
			callBlock("call_1", "child", `{"text":"top"}`),
			callBlock("call_2", "parent", `{"text":"parent"}`),
		),
		callRecord("r1", 1, "r1", "call_1", "child", `{"text":"top"}`),
		callRecord("r1", 1, "r2", "call_2", "parent", `{"text":"parent"}`),
		// The nested child asked under the handle it would have taken. The
		// crash came before the nested call's own tool.call record, so the
		// scan cannot find the call the ask names.
		askedRecord("r1", "ask_1", "r3", "child", string(poo.ClassSideEffecting), deadline, "deny"),
	)
	store := resumeStore(t, dir)
	asker := &allowAsker{asks: make(chan perm.Ask, 8)}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	opts := resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), child, parent)
	opts.Policy = policy

	resumed := resumeDurable(t, store, opts)
	// The load answers the ask the scan could not place, instead of arming
	// it for whichever call asks first.
	recs := readResumeLog(t, dir, "s1")
	answered := namedRecords(recs, "permission.answered")
	if len(answered) != 1 {
		t.Fatalf("the log holds %d answer(s), want the unplaceable ask's", len(answered))
	}
	answer := answered[0].(*poo.PermissionAnsweredRecord)
	if answer.AskID != "ask_1" {
		t.Errorf("the answer resolves the ask %q, want %q", answer.AskID, "ask_1")
	}
	if answer.Decision != "deny" || answer.Provenance != poo.ProvenanceTimeout {
		t.Errorf("the answer is %q from %q, want a deny from %q", answer.Decision, answer.Provenance, poo.ProvenanceTimeout)
	}

	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	// Both child calls ran, each with its own ask: the top-level replay's
	// and the parent's nested one.
	if got := child.Calls(); len(got) != 2 || got[0] != "top" || got[1] != "nested" {
		t.Errorf("the child tool ran %v, want the top-level call then the nested one", got)
	}
	if got := parent.Calls(); len(got) != 1 || got[0] != "parent" {
		t.Errorf("the parent tool ran %v, want the one replayed call", got)
	}
	recs = readResumeLog(t, dir, "s1")
	asks := namedRecords(recs, "permission.asked")
	if len(asks) != 3 {
		t.Fatalf("the log holds %d ask(s), want the log's and the resumed run's two", len(asks))
	}
	// The log's ask is answered; every ask after it is fresh, so none
	// inherits the id or the deadline the client was shown for ask_1.
	for _, rec := range asks[1:] {
		fresh := rec.(*poo.PermissionAskedRecord)
		if fresh.AskID == "ask_1" {
			t.Errorf("the resumed ask about %q took the id %q", fresh.Summary, "ask_1")
		}
		if fresh.Deadline == deadline {
			t.Errorf("the resumed ask about %q took the recorded deadline %q", fresh.Summary, deadline)
		}
	}
	// The top-level replay asked afresh, under its own summary.
	var top bool
	for _, rec := range asks[1:] {
		fresh := rec.(*poo.PermissionAskedRecord)
		if fresh.Summary == "top" {
			top = true
		}
	}
	if !top {
		t.Errorf("the log holds no fresh ask about the top-level call, want one")
	}
}

// TestDurableResumeAnswersAnUnplaceableAskInsteadOfWaiting covers the ask
// whose recorded call the log never saw, with no call left to ask it again:
// the side-effecting parent may have run, so it is answered interrupted, and
// the ask would have waited for ever on a run that never asks it.
func TestDurableResumeAnswersAnUnplaceableAskInsteadOfWaiting(t *testing.T) {
	parent := &resumeTool{
		name:  "parent",
		class: poo.ClassSideEffecting,
		nest:  []poo.NestedCall{{Tool: "child", Arguments: json.RawMessage(`{"text":"nested"}`)}},
	}
	child := &resumeTool{name: "child", class: poo.ClassSideEffecting}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	dir := writeResumeLog(t,
		resumeHeader(t, parent, child),
		startedRun("r1"),
		userMessage("r1", 0, "do the thing"),
		assistantCall("r1", 1, callBlock("call_1", "parent", `{"text":"parent"}`)),
		callRecord("r1", 1, "r1", "call_1", "parent", `{"text":"parent"}`),
		startedCall("r1", 1, "r1", "call_1", "parent"),
		// The nested child's ask names a call whose own record the crash
		// never wrote, and the parent may already have run, so the
		// resumed run never makes the call again.
		askedRecord("r1", "ask_1", "r2", "child", string(poo.ClassSideEffecting), deadline, "deny"),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, parent, child))

	// The load answers the ask: nothing will ask it again.
	recs := readResumeLog(t, dir, "s1")
	answered := namedRecords(recs, "permission.answered")
	if len(answered) != 1 {
		t.Fatalf("the log holds %d answer(s), want the unplaceable ask's", len(answered))
	}
	answer := answered[0].(*poo.PermissionAnsweredRecord)
	if answer.AskID != "ask_1" {
		t.Errorf("the answer resolves the ask %q, want %q", answer.AskID, "ask_1")
	}
	if answer.Decision != "deny" || answer.Provenance != poo.ProvenanceTimeout {
		t.Errorf("the answer is %q from %q, want a deny from %q", answer.Decision, answer.Provenance, poo.ProvenanceTimeout)
	}

	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	// The resumed run waits on nothing: the ask it could not place is
	// answered, not published.
	if awaiting, ok := resumed.Session.Awaiting(); ok {
		t.Errorf("the resumed run waits on %+v, want nothing", awaiting)
	}
	waitResumed(t, resumed.Session)

	if got := parent.Calls(); len(got) != 0 {
		t.Errorf("the parent tool ran %v, want no repeat of an effect that may have happened", got)
	}
	recs = readResumeLog(t, dir, "s1")
	// The ask is answered once, and no second ask replaces it.
	asks := namedRecords(recs, "permission.asked")
	if len(asks) != 1 {
		t.Errorf("the log holds %d ask(s), want only the one the crash left", len(asks))
	}
	answered = namedRecords(recs, "permission.answered")
	if len(answered) != 1 {
		t.Errorf("the log holds %d answer(s), want the ask answered once", len(answered))
	}
}

// TestDurableResumeReAsksAnOldLogByToolName covers a log written before an
// ask carried its call: the ask names none, and the log holds no record for
// its call, so the resumed run matches the ask by tool name and keeps the id
// and deadline the client was shown.
func TestDurableResumeReAsksAnOldLogByToolName(t *testing.T) {
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "delete the build"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"build"}`)),
		// The old ask names no call, and the crash left the call
		// unrecorded: nothing pairs the two by position, so only the
		// tool's name can.
		askedRecord("r1", "ask_1", "", "echo", string(poo.ClassSideEffecting), deadline, "deny"),
	)
	store := resumeStore(t, dir)
	asker := &allowAsker{asks: make(chan perm.Ask, 4)}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	opts := resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), tool)
	opts.Policy = policy

	resumed := resumeDurable(t, store, opts)
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	if calls := tool.Calls(); len(calls) != 1 || calls[0] != "build" {
		t.Fatalf("the tool ran %v, want the ask's call once", calls)
	}
	recs := readResumeLog(t, dir, "s1")
	asks := namedRecords(recs, "permission.asked")
	if len(asks) != 2 {
		t.Fatalf("the log holds %d ask(s), want the old log's and the re-ask", len(asks))
	}
	reask := asks[1].(*poo.PermissionAskedRecord)
	if reask.AskID != "ask_1" {
		t.Errorf("the re-ask's id is %q, want the recorded %q", reask.AskID, "ask_1")
	}
	if reask.Deadline != deadline {
		t.Errorf("the re-ask's deadline is %q, want the recorded %q", reask.Deadline, deadline)
	}
	// The re-ask is a new record, so it carries the call it is about even
	// though the old one could not.
	if reask.Handle != "r1" {
		t.Errorf("the re-ask's handle is %q, want %q", reask.Handle, "r1")
	}
}

// TestDurableResumeResolvesAnExpiredAskAtLoad covers the ask whose deadline
// passed while the process was dead: the load answers it the way it was
// going to be answered, and the resumed run reads the refusal.
func TestDurableResumeResolvesAnExpiredAskAtLoad(t *testing.T) {
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	expired := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "delete the build"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"build"}`)),
		// The ask is written before its call's record, and the crash left
		// the call unrecorded: the ask names the call the fallback denies.
		askedRecord("r1", "ask_1", "r1", "echo", string(poo.ClassSideEffecting), expired, "deny"),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool))
	if resumed.Restored.PendingRun == nil {
		t.Fatal("the restore holds no pending run, want the run the expired ask left")
	}

	// The load answered the ask: the log holds the answer and the call's
	// denial before the run resumes.
	recs := readResumeLog(t, dir, "s1")
	answered := namedRecords(recs, "permission.answered")
	if len(answered) != 1 {
		t.Fatalf("the log holds %d answer(s), want the expired ask's", len(answered))
	}
	answer := answered[0].(*poo.PermissionAnsweredRecord)
	if answer.Decision != "deny" || answer.Provenance != poo.ProvenanceTimeout {
		t.Errorf("the answer is %q from %q, want a deny from %q", answer.Decision, answer.Provenance, poo.ProvenanceTimeout)
	}
	results := namedRecords(recs, "tool.result")
	if len(results) != 1 {
		t.Fatalf("the log holds %d result(s), want the denied call's", len(results))
	}
	if got := results[0].(*poo.ToolResultRecord); got.Outcome != poo.OutcomeDenied {
		t.Errorf("the call's outcome is %q, want %q", got.Outcome, poo.OutcomeDenied)
	}

	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)
	if calls := tool.Calls(); len(calls) != 0 {
		t.Errorf("the tool ran %d time(s), want 0: the ask's fallback refused the call", len(calls))
	}
	// The model read the refusal before its next request.
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the resumed run made %d request(s), want 1", len(requests))
	}
	if !requestHolds(requests[0], "the permission ask expired") {
		t.Error("the model's request does not carry the denied result")
	}
}

// requestHolds reports whether a request's messages hold a text.
func requestHolds(req poo.ChatRequest, text string) bool {
	for _, msg := range req.Messages {
		if strings.Contains(msg.Text(), text) {
			return true
		}
		for _, block := range msg.Content {
			if strings.Contains(block.Text, text) {
				return true
			}
		}
	}
	return false
}

// TestDurableResumeReplaysReadOnlyAndIdempotentCalls covers the executing
// phase: the calls the log makes safe to repeat run again, and the turn ends
// the way every turn does.
func TestDurableResumeReplaysReadOnlyAndIdempotentCalls(t *testing.T) {
	read := &resumeTool{name: "echo", class: poo.ClassReadOnly}
	write := &resumeTool{name: "write", class: poo.ClassSideEffecting, idempotent: true}
	dir := writeResumeLog(t,
		resumeHeader(t, read, write),
		startedRun("r1"),
		userMessage("r1", 0, "look and write"),
		assistantCall("r1", 1,
			callBlock("call_1", "echo", `{"text":"a"}`),
			callBlock("call_2", "write", `{"text":"b"}`),
		),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
		callRecord("r1", 1, "r2", "call_2", "write", `{"text":"b"}`),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, read, write))
	if resumed.Restored.PendingRun == nil {
		t.Fatal("the restore holds no pending run")
	}
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	if got := read.Calls(); len(got) != 1 || got[0] != "a" {
		t.Errorf("the read-only tool ran %v, want one call of \"a\"", got)
	}
	if got := write.Calls(); len(got) != 1 || got[0] != "b" {
		t.Errorf("the idempotent tool ran %v, want one call of \"b\"", got)
	}
	recs := readResumeLog(t, dir, "s1")
	results := namedRecords(recs, "tool.result")
	if len(results) != 2 {
		t.Fatalf("the log holds %d result(s), want the two replayed calls'", len(results))
	}
	// The replayed calls keep the handles the crashed run gave them, so the
	// results pair with the calls the model made.
	handles := map[string]bool{}
	for _, rec := range results {
		handles[rec.(*poo.ToolResultRecord).Handle] = true
	}
	for _, want := range []string{"r1", "r2"} {
		if !handles[want] {
			t.Errorf("the log holds no result for the handle %q, want one", want)
		}
	}
	// The calls were recorded once: a replayed call writes no second
	// tool.call.
	if got := len(namedRecords(recs, "tool.call")); got != 2 {
		t.Errorf("the log holds %d call(s), want the two the crashed run made", got)
	}
	settled := namedRecords(recs, "run.settled")
	if len(settled) != 1 {
		t.Fatalf("the log holds %d settle(s), want 1", len(settled))
	}
	if got := settled[0].(*poo.RunSettledRecord).Reason; got != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want %q", got, poo.ReasonEndTurn)
	}
}

// TestDurableResumeNeverRepeatsAStartedSideEffect covers the effect the log
// says may have happened: the call is answered interrupted, and the tool
// never runs again.
func TestDurableResumeNeverRepeatsAStartedSideEffect(t *testing.T) {
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "publish it"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"release"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"release"}`),
		startedCall("r1", 1, "r1", "call_1", "echo"),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool))
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	if calls := tool.Calls(); len(calls) != 0 {
		t.Errorf("the tool ran %v, want no replay of an effect that may have happened", calls)
	}
	recs := readResumeLog(t, dir, "s1")
	results := namedRecords(recs, "tool.result")
	if len(results) != 1 {
		t.Fatalf("the log holds %d result(s), want the interrupted call's", len(results))
	}
	result := results[0].(*poo.ToolResultRecord)
	if result.Outcome != poo.OutcomeInterrupted {
		t.Errorf("the call's outcome is %q, want %q", result.Outcome, poo.OutcomeInterrupted)
	}
	if !strings.Contains(result.Text, "outcome unknown") {
		t.Errorf("the interrupted result says %q, want it to say the outcome is unknown", result.Text)
	}
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the resumed run made %d request(s), want 1", len(requests))
	}
	if !requestHolds(requests[0], "outcome unknown") {
		t.Error("the model's request does not carry the interrupted result")
	}
}

// resumeHook is an AfterTurn hook a continuation test drives.
//
// It records the turn of every call, so a test can tell the turn the
// resumption re-entered from the turn that follows it.
type resumeHook struct {
	// name names the hook.
	name string
	// idempotent reports that running it twice is safe.
	idempotent bool

	mu    sync.Mutex
	turns []int
}

// Idempotent reports whether the hook may be run again.
func (h *resumeHook) Idempotent() bool { return h.idempotent }

// AfterTurn records the turn and appends nothing.
func (h *resumeHook) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.mu.Lock()
	h.turns = append(h.turns, ctx.Turn)
	h.mu.Unlock()
	return poo.TurnDecision{}, nil
}

// Calls returns the turns the hook ran in, in order.
func (h *resumeHook) Calls() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.turns...)
}

// ranIn reports whether the hook ran in a turn.
func (h *resumeHook) ranIn(turn int) bool {
	for _, got := range h.Calls() {
		if got == turn {
			return true
		}
	}
	return false
}

// TestDurableResumeRunsIdempotentHooksFromTheRecordedIndex covers the hook
// chain: the hooks before the index ran already, an idempotent hook from the
// index runs again, and one that is not idempotent is skipped and noted.
func TestDurableResumeRunsIdempotentHooksFromTheRecordedIndex(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	first := &resumeHook{name: "first", idempotent: true}
	second := &resumeHook{name: "second", idempotent: true}
	third := &resumeHook{name: "third", idempotent: false}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "answer"),
		assistantCall("r1", 1, poo.ContentBlock{Kind: poo.ContentText, Text: "here"}),
		hookRecord("r1", 1, "after_turn", "started", 0, false),
		hookRecord("r1", 1, "after_turn", "ended", 0, false),
		hookRecord("r1", 1, "after_turn", "started", 1, false),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := resumeOptions(t, tr, tool)
	opts.AfterTurn = []poo.AfterTurnHook{first, second, third}
	resumed := resumeDurable(t, store, opts)
	if resumed.Restored.PendingRun == nil {
		t.Fatal("the restore holds no pending run")
	}
	if got := resumed.Restored.PendingRun.Phase; got != poo.StateExecuting {
		t.Errorf("the run resumes at %q, want %q", got, poo.StateExecuting)
	}
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	// Hook 0 ran before the crash and does not run again; hook 1 is where
	// the chain stopped, and it is idempotent, so it runs; hook 2 is skipped.
	// The turn the resumption re-entered is turn 1, and the turn that
	// follows it is turn 2: only turn 1 says what the resumption ran.
	if first.ranIn(1) {
		t.Error("the hook before the index ran in the resumed turn, want it left alone")
	}
	if !second.ranIn(1) {
		t.Error("the idempotent hook at the index did not run in the resumed turn, want it re-run")
	}
	if third.ranIn(1) {
		t.Error("the hook that is not idempotent ran in the resumed turn, want it skipped")
	}
	recs := readResumeLog(t, dir, "s1")
	var noted bool
	for _, rec := range namedRecords(recs, "turn.hook") {
		hook := rec.(*poo.TurnHookRecord)
		if hook.Status == "skipped" && hook.HookIndex == 2 {
			noted = true
		}
	}
	if !noted {
		t.Error("the log does not note the skipped hook, want a skipped record for index 2")
	}
	if got := len(namedRecords(recs, "run.settled")); got != 1 {
		t.Errorf("the log holds %d settle(s), want 1", got)
	}
}

// TestDurableResumeTakesAnInterruptedTurnToTheGate covers the turn a crash
// caught while it was being interrupted: the resumed run notes the
// interruption and reads the queued message at the gate.
func TestDurableResumeTakesAnInterruptedTurnToTheGate(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	hook := &resumeHook{name: "first", idempotent: true}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "answer"),
		assistantCall("r1", 1, poo.ContentBlock{Kind: poo.ContentText, Text: "here"}),
		queuedInput("r1", "in-1", poo.Steer, "stop and read this"),
		hookRecord("r1", 1, "after_turn", "started", 0, true),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := resumeOptions(t, tr, tool)
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	resumed := resumeDurable(t, store, opts)
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	recs := readResumeLog(t, dir, "s1")
	if got := len(namedRecords(recs, "turn.interrupted")); got != 1 {
		t.Errorf("the log holds %d interruption(s), want 1", got)
	}
	if hook.ranIn(1) {
		t.Error("the hook of an interrupted turn ran in that turn, want the interrupted path to skip it")
	}
	// The message the interrupt carried reaches the model at the gate.
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the resumed run made %d request(s), want 1", len(requests))
	}
	if !requestHolds(requests[0], "stop and read this") {
		t.Error("the model's request does not carry the queued interrupt message")
	}
}

// TestDurableResumeDeliversQueuedInputs covers the input a crash left
// queued: it is the user's message, so it is delivered and recorded as
// delivered, not dropped.
func TestDurableResumeDeliversQueuedInputs(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "start"),
		assistantCall("r1", 1, poo.ContentBlock{Kind: poo.ContentText, Text: "here"}),
		queuedInput("r1", "in-1", poo.Steer, "and also this"),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool))
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)

	recs := readResumeLog(t, dir, "s1")
	delivered := namedRecords(recs, "input.delivered")
	if len(delivered) != 1 {
		t.Fatalf("the log holds %d delivery(s), want the queued input's", len(delivered))
	}
	if got := delivered[0].(*poo.InputDeliveredRecord); got.InputID != "in-1" {
		t.Errorf("the delivery names the input %q, want %q", got.InputID, "in-1")
	}
	if got := len(namedRecords(recs, "input.dropped")); got != 0 {
		t.Errorf("the log holds %d drop(s), want 0", got)
	}
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the resumed run made %d request(s), want 1", len(requests))
	}
	if !requestHolds(requests[0], "and also this") {
		t.Error("the model's request does not carry the queued input")
	}
}

// TestDurableResumeRestoresFeeds covers the subscription a crash left
// running: the resumed run's store takes the instance on again.
func TestDurableResumeRestoresFeeds(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "watch the inbox"),
		assistantCall("r1", 1, poo.ContentBlock{Kind: poo.ContentText, Text: "here"}),
		&poo.FeedSubscribedRecord{
			RecordMeta: poo.NewRecordMetaFrom("feed.subscribed", poo.FeedOrigin("inbox")),
			Feed:       "inbox",
			Instance:   "inbox",
		},
	)
	store := resumeStore(t, dir)
	ran := make(chan struct{})
	var once sync.Once
	spec := poo.FeedSpec{
		Name:     "inbox",
		Optional: true,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			once.Do(func() { close(ran) })
			<-ctx.Done()
			return nil
		},
	}
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool), poo.WithFeeds(spec))
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the restored feed never ran, want its supervisor started again")
	}
	waitResumed(t, resumed.Session)
	recs := readResumeLog(t, dir, "s1")
	if got := len(namedRecords(recs, "feed.started")); got == 0 {
		t.Error("the log holds no feed attempt, want the restored feed's supervisor to have started one")
	}
}

// TestDurableResumeWakesTheRunOnTheJobsRecoveredResult covers the run a job
// held open: the load ends the job with its outcome unknown, the resumed run
// waits for the queued result, and the model reads it before its next
// request.
func TestDurableResumeWakesTheRunOnTheJobsRecoveredResult(t *testing.T) {
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "start the long job"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"run in background"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"run in background"}`),
		&poo.ToolResultRecord{
			RecordMeta: poo.NewRecordMeta("tool.result"),
			RunID:      "r1",
			Turn:       1,
			Handle:     "r1",
			CallID:     "call_1",
			Tool:       "echo",
			Outcome:    poo.OutcomeStarted,
			Text:       "started",
		},
		&poo.JobStartedRecord{
			RecordMeta: poo.NewRecordMeta("job.started"),
			SessionID:  "s1",
			JobID:      "j1",
			Handle:     "r2",
			Tool:       "echo",
			RunID:      "r1",
		},
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool))
	if resumed.Restored.PendingRun == nil {
		t.Fatal("the restore holds no pending run")
	}
	if got := resumed.Restored.PendingRun.Phase; got != poo.StateWaiting {
		t.Errorf("the run resumes at %q, want %q", got, poo.StateWaiting)
	}
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)
	// The call the log already answered is not replayed: the tool ran zero
	// times in the resumed process, so a mutant that replays it fails.
	if got := len(tool.Calls()); got != 0 {
		t.Errorf("the resumed process ran the tool %d time(s), want 0 (the call already had its result)", got)
	}

	// The job the load ended is on the log as an interrupted result, and the
	// model read it: the run woke on it rather than waiting for ever.
	recs := readResumeLog(t, dir, "s1")
	ended := namedRecords(recs, "job.ended")
	if len(ended) != 1 {
		t.Fatalf("the log holds %d job end(s), want 1", len(ended))
	}
	if got := ended[0].(*poo.JobEndedRecord).Outcome; got != poo.OutcomeInterrupted {
		t.Errorf("the job ended %q, want %q", got, poo.OutcomeInterrupted)
	}
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the resumed run made %d request(s), want 1", len(requests))
	}
	if !requestHolds(requests[0], session.InterruptedJobText) {
		t.Errorf("the model's request does not carry the recovered job result:\n%+v", requests[0].Messages)
	}
}

// TestDurableResumeCountsACrashDuringTheResumption covers the count a crash
// during a resumed run leaves: the next load spends it against the run's
// budget.
func TestDurableResumeCountsACrashDuringTheResumption(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
	)
	store := resumeStore(t, dir)
	resumed := resumeDurable(t, store, resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), tool))
	if got := resumed.Restored.PendingRun.Resumptions; got != 0 {
		t.Errorf("the plan counts %d resumption(s), want 0", got)
	}
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	waitResumed(t, resumed.Session)
	recs := readResumeLog(t, dir, "s1")
	resumptions := namedRecords(recs, "run.resumed")
	if len(resumptions) != 1 {
		t.Fatalf("the log holds %d resumption(s), want 1", len(resumptions))
	}
	if got := resumptions[0].(*poo.RunResumedRecord).ResumptionAttempt; got != 1 {
		t.Errorf("the resumption is attempt %d, want 1", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// The crash lands after the resumption's record and before anything
	// else: the log is the one the resumption left, with the run unended.
	crashed := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
		&poo.RunResumedRecord{
			RecordMeta:        poo.NewRecordMeta("run.resumed"),
			RunID:             "r1",
			ResumptionAttempt: 1,
			FromPhase:         poo.StateExecuting,
			Turn:              1,
		},
	)
	again := resumeStore(t, crashed, session.WithDurableOptions(session.DurableOptions{MaxResumptions: 2}))
	reloaded := resumeDurable(t, again, resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), tool))
	if reloaded.Restored.PendingRun == nil {
		t.Fatal("the reload holds no pending run, want the run the crash left")
	}
	if got := reloaded.Restored.PendingRun.Resumptions; got != 1 {
		t.Errorf("the reload counts %d resumption(s), want the crash's 1", got)
	}
}

// TestDurableResumeSettlesAtTheResumptionLimit covers the budget: a run
// resumed as often as it may be is settled interrupted, and the detail names
// the limit.
func TestDurableResumeSettlesAtTheResumptionLimit(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	recs := []poo.Record{
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
	}
	for attempt := 1; attempt <= session.DefaultMaxResumptions; attempt++ {
		recs = append(recs, &poo.RunResumedRecord{
			RecordMeta:        poo.NewRecordMeta("run.resumed"),
			RunID:             "r1",
			ResumptionAttempt: attempt,
			FromPhase:         poo.StateExecuting,
			Turn:              1,
		})
	}
	dir := writeResumeLog(t, recs...)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool))
	if resumed.Restored.PendingRun != nil {
		t.Errorf("the restore holds the pending run %+v, want none at the limit", resumed.Restored.PendingRun)
	}
	if _, err := resumed.Session.ResumeRun(context.Background()); !errors.Is(err, poo.ErrNoPendingRun) {
		t.Fatalf("resuming returned %v, want %v", err, poo.ErrNoPendingRun)
	}
	read := readResumeLog(t, dir, "s1")
	settled := namedRecords(read, "run.settled")
	if len(settled) != 1 {
		t.Fatalf("the log holds %d settle(s), want 1", len(settled))
	}
	record := settled[0].(*poo.RunSettledRecord)
	if record.Reason != poo.ReasonInterrupted {
		t.Errorf("the run settled %q, want %q", record.Reason, poo.ReasonInterrupted)
	}
	if !strings.Contains(record.Detail.Text, "resumption_limit") {
		t.Errorf("the settle detail is %q, want it to name the resumption limit", record.Detail.Text)
	}
	if len(tr.Requests()) != 0 {
		t.Error("the limited run reached the model, want it settled without a request")
	}
}

// TestDurableResumeStopsAtALeaseLoss covers the lease-loss boundary: the run
// stops, no settle is written, and both closes succeed without touching the
// new owner's lease.
func TestDurableResumeStopsAtALeaseLoss(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(entered)
		select {
		case <-release:
			return poo.ToolResult{Text: "ran"}, nil
		case <-ctx.Done():
			return poo.ToolResult{Text: "cancelled"}, ctx.Err()
		}
	}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
	)
	t.Cleanup(func() { close(release) })
	store, err := session.NewStore(dir, session.WithLeaseOptions(session.LeaseOptions{
		TTL:               200 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
		OwnerID:           "owner-1",
	}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed, err := session.Resume(context.Background(), store, "s1", resumeOptions(t, tr, tool))
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	t.Cleanup(func() { checkTransitions(t, resumed.Session) })
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the replayed call never started")
	}

	// The lease changes hands: another owner's lease is on disk, so the
	// session's next heartbeat finds a token that is not its own. The
	// foreign lease is written in the on-disk shape the store writes, so
	// the close's release finds a token that is not its own and leaves
	// it alone.
	leasePath := session.LeasePath(dir, "s1")
	now := time.Now()
	foreign := leaseFile{
		OwnerID:     "owner-2",
		Token:       "foreign-token",
		PID:         os.Getpid() + 1,
		Host:        "foreign-host",
		AcquiredAt:  now,
		HeartbeatAt: now,
		ExpiresAt:   now.Add(time.Hour),
	}
	foreignBytes, err := json.Marshal(foreign)
	if err != nil {
		t.Fatalf("encoding the foreign lease: %v", err)
	}
	if err := os.WriteFile(leasePath, foreignBytes, 0o600); err != nil {
		t.Fatalf("writing the foreign lease: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !resumed.Session.Broken() {
		if time.Now().After(deadline) {
			t.Fatal("the session did not notice the lost lease within 5s")
		}
		// The store's heartbeat may renew the lease over the foreign one;
		// re-write it so the next tick finds a token that is not its own.
		if err := os.WriteFile(leasePath, foreignBytes, 0o600); err != nil {
			t.Fatalf("re-writing the foreign lease: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The record count is the boundary: a lost lease withholds every
	// record that follows, so the count is frozen at the loss.
	count := len(readResumeLog(t, dir, "s1"))
	if _, err := resumed.Session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the stopped run: %v", err)
	}
	// The run ended in memory, and only its records were withheld: the
	// boundary is the log, not the process.
	if _, ok := resumed.Session.Result("r1"); !ok {
		t.Error("the run has no result, want the stopped run to have ended")
	}
	// No record follows a lost lease: the log belongs to the new owner,
	// and a settle beside its records is a run it never saw.
	if got := len(readResumeLog(t, dir, "s1")); got != count {
		t.Errorf("the log holds %d records after the run's wait, want %d (unchanged after a lost lease)", got, count)
	}
	if err := resumed.Session.Close(); err != nil {
		t.Errorf("closing the session: %v, want a clean close", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("closing the store: %v, want a clean close", err)
	}
	// The close's release never touches the new owner's lease: no record
	// followed the loss, and the foreign lease's bytes are unchanged.
	if got := len(readResumeLog(t, dir, "s1")); got != count {
		t.Errorf("the log holds %d records after the closes, want %d (unchanged after a lost lease)", got, count)
	}
	gotLease, err := os.ReadFile(leasePath)
	if err != nil {
		t.Fatalf("reading the foreign lease: %v", err)
	}
	if !bytes.Equal(gotLease, foreignBytes) {
		t.Errorf("the foreign lease's bytes changed after the closes:\ngot  %s\nwant %s", gotLease, foreignBytes)
	}
}

// TestALoggedRunStillSettlesInterruptedOnLoad covers the regression the mode
// flip could make: a logged session's unsettled run is settled interrupted
// by the load, and nothing resumes it.
func TestALoggedRunStillSettlesInterruptedOnLoad(t *testing.T) {
	header := resumeHeader(t)
	header.Mode = string(session.ModeLogged)
	tool := &resumeTool{name: "echo"}
	dir := writeResumeLog(t,
		header,
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool), poo.WithAutoResume(true))

	if resumed.Loaded.Continuation != nil {
		t.Errorf("the load planned %+v for a logged session, want nothing", resumed.Loaded.Continuation)
	}
	if resumed.Restored.PendingRun != nil {
		t.Errorf("the restore holds the pending run %+v, want none", resumed.Restored.PendingRun)
	}
	if len(resumed.Restored.Interrupted) != 1 || resumed.Restored.Interrupted[0] != "r1" {
		t.Errorf("the restore reports %v interrupted, want the run", resumed.Restored.Interrupted)
	}
	if _, err := resumed.Session.ResumeRun(context.Background()); !errors.Is(err, poo.ErrNoPendingRun) {
		t.Fatalf("resuming returned %v, want %v", err, poo.ErrNoPendingRun)
	}
	recs := readResumeLog(t, dir, "s1")
	settled := namedRecords(recs, "run.settled")
	if len(settled) != 1 {
		t.Fatalf("the log holds %d settle(s), want 1", len(settled))
	}
	if got := settled[0].(*poo.RunSettledRecord).Reason; got != poo.ReasonInterrupted {
		t.Errorf("the run settled %q, want %q", got, poo.ReasonInterrupted)
	}
	if got := len(namedRecords(recs, "run.resumed")); got != 0 {
		t.Errorf("the log holds %d resumption(s), want 0", got)
	}
	if len(tr.Requests()) != 0 {
		t.Error("the logged session reached the model, want it idle after the load")
	}
}

// TestDurableResumeRefusesAPromptWhileItRuns covers the busy boundary: a
// session with a pending run takes a resumption, and a prompt while it runs
// is refused.
func TestDurableResumeRefusesAPromptWhileItRuns(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	tool := &resumeTool{name: "echo", class: poo.ClassSideEffecting}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(entered)
		select {
		case <-release:
			return poo.ToolResult{Text: "ran"}, nil
		case <-ctx.Done():
			return poo.ToolResult{}, ctx.Err()
		}
	}
	var releaseOnce sync.Once
	releaseIt := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseIt)
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
	)
	store := resumeStore(t, dir)
	resumed := resumeDurable(t, store, resumeOptions(t, scripted([]fakeResponse{{text: "done"}}), tool))
	if _, err := resumed.Session.ResumeRun(context.Background()); err != nil {
		t.Fatalf("resuming the run: %v", err)
	}
	<-entered
	if _, err := resumed.Session.ResumeRun(context.Background()); !errors.Is(err, poo.ErrBusy) {
		t.Errorf("a second resumption returned %v, want %v", err, poo.ErrBusy)
	}
	if _, err := resumed.Session.Prompt(context.Background(), "hello"); !errors.Is(err, poo.ErrBusy) {
		t.Errorf("a prompt while the run resumed returned %v, want %v", err, poo.ErrBusy)
	}
	releaseIt()
	result := waitResumed(t, resumed.Session)
	if result.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want %q", result.Reason, poo.ReasonEndTurn)
	}
	if got := len(namedRecords(readResumeLog(t, dir, "s1"), "run.settled")); got != 1 {
		t.Errorf("the log holds %d settle(s), want 1", got)
	}
}

// TestDurableResumeResumesBeforeRestoreReturns covers auto-resume: a caller
// that asks for it finds the run already carrying on.
func TestDurableResumeResumesBeforeRestoreReturns(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
	)
	store := resumeStore(t, dir)
	tr := scripted([]fakeResponse{{text: "done"}})
	resumed := resumeDurable(t, store, resumeOptions(t, tr, tool), poo.WithAutoResume(true))
	if resumed.Session.PendingRun() != nil {
		t.Error("the restored session still holds the plan, want it claimed by the auto-resume")
	}
	result := waitResumed(t, resumed.Session)
	if result.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want %q", result.Reason, poo.ReasonEndTurn)
	}
	if got := tool.Calls(); len(got) != 1 {
		t.Errorf("the replayed call ran %d time(s), want 1", len(got))
	}
	if got := len(namedRecords(readResumeLog(t, dir, "s1"), "run.resumed")); got != 1 {
		t.Errorf("the log holds %d resumption(s), want 1", got)
	}
}

// TestDurableResumeReportsThePlanOnTheWire covers the shape a client reads:
// the pending run's id, turn, phase and resumption count.
func TestDurableResumeReportsThePlanOnTheWire(t *testing.T) {
	tool := &resumeTool{name: "echo"}
	dir := writeResumeLog(t,
		resumeHeader(t, tool),
		startedRun("r1"),
		userMessage("r1", 0, "go"),
		assistantCall("r1", 1, callBlock("call_1", "echo", `{"text":"a"}`)),
		callRecord("r1", 1, "r1", "call_1", "echo", `{"text":"a"}`),
		&poo.RunResumedRecord{
			RecordMeta:        poo.NewRecordMeta("run.resumed"),
			RunID:             "r1",
			ResumptionAttempt: 1,
			FromPhase:         poo.StateExecuting,
			Turn:              1,
		},
	)
	store := resumeStore(t, dir)
	resumed := resumeDurable(t, store, resumeOptions(t, scripted(nil), tool))
	info := resumed.Session.PendingRun()
	if info == nil {
		t.Fatal("the session holds no pending run")
	}
	if info.RunID != "r1" || info.Turn != 1 || info.Phase != poo.StateExecuting || info.Resumptions != 1 {
		t.Errorf("the pending run is %+v, want r1 at turn 1 executing after 1 resumption", info)
	}
	if resumed.Restored.PendingRun == nil || *resumed.Restored.PendingRun != *info {
		t.Errorf("the restore reports %+v, want the session's %+v", resumed.Restored.PendingRun, info)
	}
	// The session is idle and inspectable until it is resumed.
	if got := resumed.Session.State(); got != poo.StateIdle {
		t.Errorf("the session is %q, want %q", got, poo.StateIdle)
	}
	if resumed.Session.Running() {
		t.Error("the session runs without being asked to")
	}
	if _, ok := resumed.Session.Awaiting(); ok {
		t.Error("an idle session publishes a wait")
	}
	assertNoTransitions(t, resumed.Session)
}

// assertNoTransitions fails for a session that moved before it ran.
func assertNoTransitions(t *testing.T, s *poo.Session) {
	t.Helper()
	if got := s.Transitions(); len(got) != 0 {
		t.Errorf("the idle session moved through %v, want no move", got)
	}
}
