package mine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/mine"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/history"
)

// The mine strategy: a fork names what the history must preserve, a
// fresh request reconstructs the work from the covered transcript, and
// the reconstruction replaces the oldest turns while the store keeps
// them.

// echoTool answers with the text it was called with.
type echoTool struct{}

// Spec describes the tool.
func (t *echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (t *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return echoPrepared{text: args.Text}, nil
}

// Call answers.
func (t *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "echoed " + in.(echoPrepared).text}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct {
	text string
}

// Summary describes the call.
func (p echoPrepared) Summary() string { return "echo " + p.text }

// pathTool is a tool that names a path, so the replacement's file list
// has something to read.
type pathTool struct {
	// name is the tool's name.
	name string
}

// Spec describes the tool.
func (t *pathTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        t.name,
		Description: "touch a path",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (t *pathTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return pathPrepared{}, nil
}

// Call answers.
func (t *pathTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "done"}, nil
}

// pathPrepared is a prepared path call.
type pathPrepared struct{}

// Summary describes the call.
func (p pathPrepared) Summary() string { return "the call" }

// session builds a session that mines with the given options.
func session(t *testing.T, tr *pootest.Transport, opts mine.Options, tools map[string]poo.Tool, order []string) (*poo.Session, *pootest.Sink) {
	t.Helper()
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        tools,
		ToolOrder:    order,
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 20000},
		SystemPrompt: "the system prompt",
		Compactor:    mine.New(opts),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, sink
}

// long returns a text of the given size with a sentinel at its front.
func long(sentinel string) string {
	return sentinel + " " + strings.Repeat("x", 3500)
}

// compaction returns the log's last mine compaction.
func compaction(t *testing.T, sink *pootest.Sink) *poo.CompactionRecord {
	t.Helper()
	var found *poo.CompactionRecord
	for _, rec := range sink.Records() {
		cr, ok := rec.(*poo.CompactionRecord)
		if !ok {
			continue
		}
		if cr.Strategy != mine.Strategy {
			t.Fatalf("the compaction's strategy is %q, want %q", cr.Strategy, mine.Strategy)
		}
		found = cr
	}
	return found
}

// replacementText returns the message a replacement wrote.
func replacementText(t *testing.T, sink *pootest.Sink) string {
	t.Helper()
	cr := compaction(t, sink)
	if cr == nil {
		t.Fatal("the log holds no mine compaction")
	}
	if cr.Form != "replace" {
		t.Fatalf("the compaction's form is %q, want replace", cr.Form)
	}
	if len(cr.Replacement) != 1 {
		t.Fatalf("the compaction carries %d replacement messages, want 1", len(cr.Replacement))
	}
	return cr.Replacement[0].Text()
}

// askRequests returns the requests a strategy made for itself.
//
// A strategy's own request carries no tools, which is what tells it
// apart from the run's requests and from the fork, which re-sends the
// run's own request.
func askRequests(reqs []poo.ChatRequest) []poo.ChatRequest {
	var out []poo.ChatRequest
	for _, req := range reqs {
		if len(req.Tools) == 0 {
			out = append(out, req)
		}
	}
	return out
}

// requestText renders the messages a request carries, so a test can
// read the view the model saw.
func requestText(req poo.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Text())
		b.WriteString("\n")
	}
	return b.String()
}

// run prompts, waits, and requires the run to answer.
func run(t *testing.T, s *poo.Session) poo.Result {
	t.Helper()
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	return res
}

// TestTheRunMinesAndCarriesOn covers the strategy's whole job: a fork
// names what to preserve, a reconstruction replaces the oldest turns,
// the newest turn stays verbatim, the replacement points at what the
// store retains, and the run answers.
func TestTheRunMinesAndCarriesOn(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{
			{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
			{Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
		}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task\n\n## State\nmain.go is edited\n\n## Next Steps\n1. answer"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: "- the file main.go was edited\n- the error in the log"})
	s, sink := session(t, tr, mine.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}, "read": &pathTool{name: "read"}}, []string{"echo", "read"})

	run(t, s)

	// The fork appended the pointer prompt, and the pass ran under the
	// pointer output limit.
	forks := tr.Forks()
	if len(forks) != 1 {
		t.Fatalf("the transport holds %d forks, want 1", len(forks))
	}
	if len(forks[0].Appended) != 1 {
		t.Fatalf("the fork appends %d messages, want 1", len(forks[0].Appended))
	}
	if appended := forks[0].Appended[0].Text(); !strings.Contains(appended, "reconstructed working context") {
		t.Errorf("the fork appends %q, want the pointer prompt", appended)
	}
	if forks[0].MaxTokens != mine.New(mine.Options{}).PointerMaxTokens() {
		t.Errorf("the fork's output limit is %d, want the pointer limit", forks[0].MaxTokens)
	}

	// The mining request reads the transcript and the pointers, and it
	// is the strategy's own: it carries no tools.
	asks := askRequests(tr.Requests())
	if len(asks) != 1 {
		t.Fatalf("the transport holds %d strategy requests, want 1", len(asks))
	}
	ask := asks[0].Messages[1].Text()
	for _, want := range []string{"<transcript>", "TURN-ONE", "<pointers>", "the error in the log"} {
		if !strings.Contains(ask, want) {
			t.Errorf("the mining request is %q, want it to carry %q", ask, want)
		}
	}

	// The record's side report says what the pass cost: a fork whose
	// reply arrived whole and whose pointers were read.
	fork := compaction(t, sink).Compaction.Side.Fork
	if fork == nil {
		t.Fatal("the compaction carries no fork side report")
	}
	if fork.Bytes != len("- the file main.go was edited\n- the error in the log") {
		t.Errorf("the fork's side report counts %d bytes, want the pointers' length", fork.Bytes)
	}
	if fork.Truncated || fork.ToolCall {
		t.Errorf("the fork's side report says truncated=%v toolcall=%v, want both false", fork.Truncated, fork.ToolCall)
	}

	// The model reads the reconstruction where the covered turns were,
	// with the file list and the retention pointer after it. The
	// session offers no history tool, so the pointer names the retained
	// records without pointing at a call the model cannot make.
	text := replacementText(t, sink)
	for _, want := range []string{
		"[Context compacted: turns 1-1 reconstructed below]",
		"finish the task",
		"Files read:\n- main.go",
		"retained in this session's store of session s1 (turns 1-1)",
		"nothing was deleted",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the replacement is %q, want it to carry %q", text, want)
		}
	}

	// The last request is the run's: the reconstruction is in it, the
	// covered turn is out of it, and the newest turn stays verbatim.
	requests := tr.Requests()
	last := requests[len(requests)-1]
	if len(last.Tools) == 0 {
		t.Fatalf("the last request carries no tools, so it is a strategy request")
	}
	view := requestText(last)
	if !strings.Contains(view, "finish the task") {
		t.Errorf("the last request is %q, want the reconstruction in it", view)
	}
	if strings.Contains(view, "TURN-ONE") {
		t.Errorf("the last request is %q, want the reconstructed turn gone from it", view)
	}
	if !strings.Contains(view, "TURN-TWO") {
		t.Errorf("the last request is %q, want the newest turn kept verbatim", view)
	}

	// The reconstruction is a compaction, not history: the model reads
	// it because the replacement is in the view, not because the reply
	// was recorded.
	for _, m := range sink.Messages() {
		for _, c := range m.Content {
			if strings.Contains(c.Text, "finish the task") {
				t.Errorf("the mining reply was recorded as a message: %q", c.Text)
			}
		}
	}
}

// TestTheCarryForwardUpdatesThePriorContext covers the second
// compaction: it reads the first reconstruction as previous context and
// updates it instead of reconstructing that work again from nothing.
func TestTheCarryForwardUpdatesThePriorContext(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfirst context\n\n## Next Steps\n1. keep going"},
		pootest.Turn{Text: long("TURN-THREE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"three"}`)}}},
		pootest.Turn{Text: "## Goal\nupdated context\n\n## Next Steps\n1. finish"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(
		pootest.Turn{Text: "- the first pointers"},
		pootest.Turn{Text: "- the second pointers"},
	)
	s, sink := session(t, tr, mine.Options{Ratio: 0.02, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

	run(t, s)

	records := 0
	for _, rec := range sink.Records() {
		if _, ok := rec.(*poo.CompactionRecord); ok {
			records++
		}
	}
	if records != 2 {
		t.Fatalf("the log holds %d compactions, want 2", records)
	}

	// The second mining request carries the first reconstruction as
	// previous context, with the update prompt after it.
	asks := askRequests(tr.Requests())
	if len(asks) != 2 {
		t.Fatalf("the transport holds %d strategy requests, want 2", len(asks))
	}
	second := asks[1].Messages[1].Text()
	for _, want := range []string{"<previous-context>", "first context", "Update the existing working context", "<pointers>", "the second pointers"} {
		if !strings.Contains(second, want) {
			t.Errorf("the second mining request is %q, want it to carry %q", second, want)
		}
	}

	// The reconstruction the second compaction wrote replaces the turns
	// the first one left: the first replacement covers turn 1, so the
	// second starts at turn 2.
	if text := replacementText(t, sink); !strings.Contains(text, "turns 2-2 reconstructed below") {
		t.Errorf("the second replacement is %q, want it to name turns 2-2", text)
	}
}

// TestAMinedReconstructionRunsWhenTheForkFails covers the fallback: a
// pointer pass that fails mines without pointers instead of failing the
// compaction, because the transcript holds what the pass would have
// named.
func TestAMinedReconstructionRunsWhenTheForkFails(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task\n\n## Next Steps\n1. answer"},
		pootest.Turn{Text: "the final answer"},
	)
	// No fork turns: the pointer pass fails with an exhausted script.
	s, sink := session(t, tr, mine.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

	run(t, s)

	text := replacementText(t, sink)
	if !strings.Contains(text, "finish the task") {
		t.Errorf("the replacement is %q, want the reconstruction in it", text)
	}
	asks := askRequests(tr.Requests())
	if len(asks) != 1 {
		t.Fatalf("the transport holds %d strategy requests, want 1", len(asks))
	}
	if ask := asks[0].Messages[1].Text(); strings.Contains(ask, "</pointers>") {
		t.Errorf("the mining request is %q, want it to carry no pointers", ask)
	}

	// The record's side report carries the pass's failure: the
	// diagnostic a consumer of the compaction record reads.
	fork := compaction(t, sink).Compaction.Side.Fork
	if fork == nil || fork.Error == "" {
		t.Error("the compaction carries no fork failure in its side report")
	}
}

// TestTheMiningRequestUsesTheSideModel covers the compaction role: a
// session with a side transport asks the mining request there, while
// the fork stays on the session's own transport, because a fork
// re-sends the last main request and only that transport can stand in
// for it.
func TestTheMiningRequestUsesTheSideModel(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: "- the pointers"})
	side := pootest.NewTransport(
		pootest.Turn{Text: "## Goal\nfinish the task\n\n## Next Steps\n1. answer"},
	)
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:     tr,
		SideTransport: side,
		SideModel:     poo.ModelInfo{ModelID: "side-model", ContextLimit: 20000},
		Tools:         map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:     []string{"echo"},
		Sink:          sink,
		SessionID:     "s1",
		Model:         poo.ModelInfo{ModelID: "test-model", ContextLimit: 20000},
		SystemPrompt:  "the system prompt",
		Compactor:     mine.New(mine.Options{Ratio: 0.05, KeepRecentTokens: 1}),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	run(t, s)

	// The mining request went to the side model.
	sideReqs := side.Requests()
	if len(sideReqs) != 1 {
		t.Fatalf("the side transport holds %d requests, want 1", len(sideReqs))
	}
	if sideReqs[0].Model != "side-model" {
		t.Errorf("the side request names the model %q, want side-model", sideReqs[0].Model)
	}
	if sideReqs[0].SessionID != "s1-compaction" {
		t.Errorf("the side request names the session %q, want s1-compaction", sideReqs[0].SessionID)
	}
	if ask := sideReqs[0].Messages[1].Text(); !strings.Contains(ask, "<pointers>") {
		t.Errorf("the mining request is %q, want it to carry the pointers", ask)
	}

	// The fork stayed on the session's own transport.
	if forks := tr.Forks(); len(forks) != 1 {
		t.Fatalf("the main transport holds %d forks, want 1", len(forks))
	}
	if side.Forks() != nil && len(side.Forks()) != 0 {
		t.Errorf("the side transport holds %d forks, want none", len(side.Forks()))
	}

	// The reconstruction the side model wrote reached the run's view.
	if text := replacementText(t, sink); !strings.Contains(text, "finish the task") {
		t.Errorf("the replacement is %q, want the reconstruction in it", text)
	}
}

// TestAMinedReconstructionTheLimitCutShortIsRefused covers the guard:
// a reconstruction the output limit cut short is not a context, so the
// compaction is refused and the run carries on uncompacted.
func TestAMinedReconstructionTheLimitCutShortIsRefused(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task", Finish: "length"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: "- the pointers"})
	s, sink := session(t, tr, mine.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

	run(t, s)

	if cr := compaction(t, sink); cr != nil {
		t.Fatalf("the log holds a compaction (%s), want none", cr.Strategy)
	}

	// The run carries on over the whole history it still has.
	requests := tr.Requests()
	last := requests[len(requests)-1]
	view := requestText(last)
	if !strings.Contains(view, "TURN-ONE") {
		t.Errorf("the last request is %q, want the uncompacted turn still in it", view)
	}
}

// TestThePointerNamesTheToolTheSessionOffers covers the recovery
// pointer's wording: a session that offers the history tool gets the
// pointer that names it, so the model can read the turns back.
func TestThePointerNamesTheToolTheSessionOffers(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task\n\n## Next Steps\n1. answer"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: "- the pointers"})
	s, sink := session(t, tr, mine.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}, "history": history.New()}, []string{"echo", "history"})

	run(t, s)

	text := replacementText(t, sink)
	if !strings.Contains(text, "The history tool reads them") {
		t.Errorf("the pointer does not name the history tool the session offers: %q", text)
	}
}
