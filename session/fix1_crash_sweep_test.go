package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// fix1EchoTool is the read-only tool the crash sweep's session calls.
type fix1EchoTool struct{}

// Spec describes the tool.
func (fix1EchoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "the crash sweep's echo",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (fix1EchoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return blockingArgs{}, nil
}

// Call answers at once.
func (fix1EchoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "echoed"}, nil
}

// fix1DenyPolicy denies the echo call whose arguments say the word.
type fix1DenyPolicy struct{}

// BeforeTool denies one call and allows the rest.
func (fix1DenyPolicy) BeforeTool(_ poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if info.Spec.Name == "echo" && strings.Contains(string(info.Arguments), "deny") {
		return poo.Deny("the crash sweep denies this call"), nil
	}
	return poo.Allow(), nil
}

// fix1CrashSession returns the tools and header a crash sweep session is
// built with.
func fix1CrashSession(t *testing.T) (poo.Tool, poo.Tool, *poo.SessionCreated) {
	t.Helper()
	echo := fix1EchoTool{}
	job := pootest.NewJobTool("job", poo.BackgroundAlways)
	job.SetResult("the job's output")
	model := testModel()
	header := &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    "s1",
		Mode:         string(ModeLogged),
		CreatedAt:    "2026-10-20T00:00:00Z",
		WorkingDir:   "/tmp",
		Profile:      testProfile,
		Roles:        poo.ModelRoles{Chat: &model},
		Tools:        []poo.ToolInfo{testToolInfo(echo), testToolInfo(job)},
		SettingsHash: testSettingsHash,
		Model:        poo.SessionModel{ID: model.ModelID, BaseURL: model.Endpoint},
		ContextLimit: model.ContextLimit,
		ToolsMode:    "native",
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
	}
	return echo, job, header
}

// fix1CrashOptions returns the options a restore of the crash sweep's
// session takes.
func fix1CrashOptions(echo, job poo.Tool, transport poo.ChatTransport) poo.Options {
	model := testModel()
	return poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{"echo": echo, "job": job},
		ToolOrder:    []string{"echo", "job"},
		SessionID:    "s1",
		Model:        model,
		Models:       map[string]poo.ModelInfo{model.Name: model},
		Profiles:     []poo.Profile{{Name: testProfile, Tools: []poo.ToolInfo{testToolInfo(echo), testToolInfo(job)}}},
		SettingsHash: testSettingsHash,
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
		SystemPrompt: "the prompt",
		Jobs:         poo.JobOptions{OnIdle: poo.OnIdleHold},
	}
}

// fix1CrashLog drives one session through tool calls, a parallel batch,
// a denied call, a job and an ask, and returns its log's lines.
//
// The ask belongs to a run that started and never settled, the state a
// crash while asking leaves.
func fix1CrashLog(t *testing.T) [][]byte {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	echo, jobTool, header := fix1CrashSession(t)
	if _, err := store.Create(context.Background(), header); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	transport := pootest.NewTransport(
		pootest.Turn{
			Finish: "tool_calls",
			Calls: []pootest.Call{
				{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`), ID: "c1"},
				{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`), ID: "c2"},
				{Name: "echo", Arguments: json.RawMessage(`{"text":"deny"}`), ID: "c3"},
				{Name: "job", Arguments: json.RawMessage(`{"text":"work"}`), ID: "c4"},
			},
		},
		pootest.Turn{Text: "done", Finish: "stop"},
	)
	model := testModel()
	sess, err := poo.New(poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{"echo": echo, "job": jobTool},
		ToolOrder:    []string{"echo", "job"},
		SessionID:    "s1",
		Model:        model,
		Models:       map[string]poo.ModelInfo{model.Name: model},
		Profiles:     []poo.Profile{{Name: testProfile, Tools: []poo.ToolInfo{testToolInfo(echo), testToolInfo(jobTool)}}},
		SettingsHash: testSettingsHash,
		Build:        poo.BuildInfo{Name: "poo", Version: "test"},
		SystemPrompt: "the prompt",
		Sink:         store,
		Policy:       fix1DenyPolicy{},
		Jobs:         poo.JobOptions{OnIdle: poo.OnIdleHold},
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	// The job's gate is open before the run starts, so the job finishes
	// while the run is still around to hear about it.
	jobTool.(*pootest.JobTool).ReleaseAll()
	if _, err := sess.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// The run that crashed while asking, and a job it started and never
	// ended. Both are the session's, so the recovery resolves them even
	// though an earlier run settled.
	path := filepath.Join(dir, "s1.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	for _, rec := range []poo.Record{
		runStarted("r2"),
		&poo.PermissionAskedRecord{
			RecordMeta: poo.NewRecordMeta("permission.asked"),
			RunID:      "r2",
			AskID:      "a1",
			Tool:       "echo",
			Reason:     "policy",
		},
		jobStarted("j9", "r9", "job", "r2"),
	} {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("encoding the record: %v", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("writing the record: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing the log: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	lines := bytesLines(data)
	// The log must hold what the sweep says it drives: calls, a parallel
	// batch, a denied call, a job and an ask.
	calls := 0
	batch := 0
	denied := false
	jobs := 0
	ask := false
	for _, line := range lines {
		rec, err := NewCodec().Decode(line)
		if err != nil {
			t.Fatalf("decoding the log: %v", err)
		}
		switch r := rec.(type) {
		case *poo.ToolCallRecord:
			calls++
		case *poo.ToolResultRecord:
			if r.Outcome == poo.OutcomeDenied {
				denied = true
			}
		case *poo.MessageRecord:
			callsInMessage := 0
			for _, block := range r.Content {
				if block.Kind == poo.ContentToolCall {
					callsInMessage++
				}
			}
			if callsInMessage > batch {
				batch = callsInMessage
			}
		case *poo.JobStarted:
			jobs++
		case *poo.PermissionAskedRecord:
			ask = true
		}
	}
	if calls < 4 {
		t.Fatalf("the log holds %d tool calls, want the batch, the denied call and the job", calls)
	}
	if batch < 2 {
		t.Fatalf("the log's largest batch holds %d calls, want a parallel batch", batch)
	}
	if !denied {
		t.Fatal("the log holds no denied call")
	}
	if jobs < 1 {
		t.Fatal("the log holds no job")
	}
	if !ask {
		t.Fatal("the log holds no ask")
	}
	return lines
}

// fix1Unanswered returns the first call id an assistant message carries
// with no result message, or the empty string when every call is paired.
func fix1Unanswered(recs []poo.Record) string {
	var calls []string
	results := map[string]bool{}
	for _, rec := range recs {
		m, ok := rec.(*poo.MessageRecord)
		if !ok {
			continue
		}
		for _, block := range m.Content {
			switch block.Kind {
			case poo.ContentToolCall:
				calls = append(calls, block.ToolCallID)
			case poo.ContentToolResult:
				results[block.ToolCallID] = true
			}
		}
	}
	for _, id := range calls {
		if id != "" && !results[id] {
			return id
		}
	}
	return ""
}

// TestEveryCrashBoundaryRecoversAPairedRequest sweeps the log's record
// boundaries: at each one the log is truncated, recovered twice,
// restored, and asked for a request. Every call is paired, the second
// recovery adds nothing, and the request the provider receives pairs
// every call with its result.
func TestEveryCrashBoundaryRecoversAPairedRequest(t *testing.T) {
	lines := fix1CrashLog(t)
	for boundary := 1; boundary <= len(lines); boundary++ {
		t.Run("boundary", func(t *testing.T) {
			dir := t.TempDir()
			var data []byte
			for _, line := range lines[:boundary] {
				data = append(data, line...)
				data = append(data, '\n')
			}
			if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), data, 0o600); err != nil {
				t.Fatalf("writing the log: %v", err)
			}
			fix1CheckBoundary(t, dir)
		})
	}
	// Once in the middle of a line: a process killed during a write.
	t.Run("mid-line", func(t *testing.T) {
		dir := t.TempDir()
		var data []byte
		for _, line := range lines {
			data = append(data, line...)
			data = append(data, '\n')
		}
		data = data[:len(data)-len(lines[len(lines)-1])/2]
		if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), data, 0o600); err != nil {
			t.Fatalf("writing the log: %v", err)
		}
		fix1CheckBoundary(t, dir)
	})
}

// fix1CheckBoundary recovers and restores one truncated log and checks
// the invariant.
func fix1CheckBoundary(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("the first load: %v", err)
	}
	if id := fix1Unanswered(loaded.Records); id != "" {
		t.Fatalf("the recovered session leaves the call %q unanswered", id)
	}
	before := len(loaded.Records)
	again, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("the second load: %v", err)
	}
	if len(again.Records) != before {
		t.Fatalf("the second recovery added %d records, want none", len(again.Records)-before)
	}
	if id := fix1Unanswered(again.Records); id != "" {
		t.Fatalf("the session leaves the call %q unanswered", id)
	}

	echo, jobTool, _ := fix1CrashSession(t)
	transport := pootest.NewTransport(
		pootest.Turn{Text: "one", Finish: "stop"},
		pootest.Turn{Text: "two", Finish: "stop"},
		pootest.Turn{Text: "three", Finish: "stop"},
	)
	sess, _, err := poo.Restore(fix1CrashOptions(echo, jobTool, transport), again.Records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer sess.Close()
	if _, err := sess.Prompt(ctx, "carry on"); err != nil {
		t.Fatalf("prompting the restored session: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("waiting for the restored session: %v", err)
	}
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the restored session made no request")
	}
	for _, req := range requests {
		calls := map[string]bool{}
		results := map[string]bool{}
		for _, msg := range req.Messages {
			for _, block := range msg.Content {
				switch block.Kind {
				case poo.ContentToolCall:
					calls[block.ToolCallID] = true
				case poo.ContentToolResult:
					results[block.ToolCallID] = true
				}
			}
		}
		for id := range calls {
			if !results[id] {
				t.Errorf("the request carries the call %q with no result", id)
			}
		}
	}
}
