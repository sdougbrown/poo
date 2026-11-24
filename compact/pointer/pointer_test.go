package pointer_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/pointer"
	"github.com/sdougbrown/poo/pootest"
)

// The pointer strategy: the chat model's fork names queries, the
// retained history answers them, and the compaction model composes the
// working context from the digest — never from the transcript whole.

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

// unreadableSink is a sink whose read side always fails: the strategy
// composes from the seed and the notes alone.
type unreadableSink struct {
	*pootest.Sink
}

// ReadRecords always fails.
func (u unreadableSink) ReadRecords(context.Context) ([]poo.Record, error) {
	return nil, errors.New("the store is closed")
}

// readSink is a test sink that also reads its records back, so the
// strategy's queries run over the session's own records.
type readSink struct {
	*pootest.Sink
}

// ReadRecords returns the sink's records.
func (r readSink) ReadRecords(context.Context) ([]poo.Record, error) {
	return r.Sink.Records(), nil
}

// session builds a pointer session whose sink reads back.
func session(t *testing.T, tr *pootest.Transport, opts pointer.Options) (*poo.Session, *pootest.Sink) {
	t.Helper()
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         readSink{Sink: sink},
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 20000},
		SystemPrompt: "the system prompt",
		Compactor:    pointer.New(opts),
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

// askRequests returns the requests a strategy made for itself.
func askRequests(reqs []poo.ChatRequest) []poo.ChatRequest {
	var out []poo.ChatRequest
	for _, req := range reqs {
		if len(req.Tools) == 0 {
			out = append(out, req)
		}
	}
	return out
}

// requestText renders the messages a request carries.
func requestText(req poo.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Text())
		b.WriteString("\n")
	}
	return b.String()
}

// TestTheRunReconstructsFromTheQueries covers the strategy's whole job:
// the fork names queries, the retained history answers them, and the
// compose ask reads the digest — not the transcript whole.
func TestTheRunReconstructsFromTheQueries(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task\n\n## Next Steps\n1. answer"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: `{"queries": ["TURN-ONE", "the deploy"], "notes": "the deploy is pending"}`})
	s, sink := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1})

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

	// The fork was asked for JSON.
	forks := tr.Forks()
	if !forks[0].JSON {
		t.Error("the fork was not asked for a JSON reply")
	}

	// The compose request carries the orientation seed, the matches the
	// queries hit, and the fork's notes — and none of the transcript
	// the queries did not name.
	asks := askRequests(tr.Requests())
	if len(asks) != 1 {
		t.Fatalf("the transport holds %d strategy requests, want 1", len(asks))
	}
	ask := asks[0].Messages[1].Text()
	for _, want := range []string{
		"<task>", "do the thing",
		"<matches>", "TURN-ONE",
		"<notes>", "the deploy is pending",
	} {
		if !strings.Contains(ask, want) {
			t.Errorf("the compose request is %q, want it to carry %q", ask, want)
		}
	}
	// The queries named turn 1 alone: turn 2's text stays out of the
	// compose request.
	if strings.Contains(ask, "TURN-TWO") {
		t.Errorf("the compose request carries the transcript the queries did not name: %q", ask)
	}

	// The replacement carries the reconstruction and the retention
	// pointer.
	var compaction *poo.CompactionRecord
	for _, rec := range sink.Records() {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			compaction = cr
		}
	}
	if compaction == nil {
		t.Fatal("the log holds no compaction")
	}
	if compaction.Strategy != pointer.Strategy {
		t.Fatalf("the compaction's strategy is %q, want %q", compaction.Strategy, pointer.Strategy)
	}
	text := compaction.Replacement[0].Text()
	for _, want := range []string{
		"[Context compacted: turns 1-1 reconstructed below]",
		"finish the task",
		"retained in this session's store of session s1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the replacement is %q, want it to carry %q", text, want)
		}
	}

	// The run's last request carries the reconstruction, not the
	// replaced turn.
	last := tr.Requests()[len(tr.Requests())-1]
	view := requestText(last)
	if !strings.Contains(view, "finish the task") {
		t.Errorf("the last request is %q, want the reconstruction in it", view)
	}
	if strings.Contains(view, "TURN-ONE") {
		t.Errorf("the last request is %q, want the replaced turn gone from it", view)
	}
}

// TestTheForkRetrySurvivesARefusedJSONAsk covers the response-format
// fallback: an endpoint that refuses the JSON ask gets one more fork
// without it, and the pass carries on with what that reply said.
func TestTheForkRetrySurvivesARefusedJSONAsk(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(
		pootest.Turn{Err: errors.New("response_format is not supported")},
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": "the notes"}`},
	)
	s, sink := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1})
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

	forks := tr.Forks()
	if len(forks) != 2 {
		t.Fatalf("the transport holds %d forks, want the refused ask and the retry", len(forks))
	}
	if !forks[0].JSON {
		t.Error("the first fork was not asked for a JSON reply")
	}
	if forks[1].JSON {
		t.Error("the retry asked for a JSON reply again")
	}
	compacted := false
	for _, rec := range sink.Records() {
		if cr, ok := rec.(*poo.CompactionRecord); ok && cr.Strategy == "pointer" {
			compacted = true
			if cr.Compaction.Side == nil || cr.Compaction.Side.Fork == nil || cr.Compaction.Side.Fork.Error != "" {
				t.Errorf("the pointer compaction's fork side is %+v, want the retry's success on it", cr.Compaction.Side)
			}
		}
	}
	if !compacted {
		t.Error("the run holds no pointer compaction")
	}
}

// TestAMalformedPointerReplyIsReformed covers the recovery layer: a
// fork reply that does not parse goes back to the compaction role for
// one cheap reformat, and the queries the reform returns drive the
// extraction.
func TestAMalformedPointerReplyIsReformed(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: "- I was working on TURN-ONE; next: the deploy"})
	s, sink := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The reform ask carries the malformed reply, small and alone.
	asks := askRequests(tr.Requests())
	if len(asks) != 2 {
		t.Fatalf("the transport holds %d strategy requests, want the reform and the compose", len(asks))
	}
	// The reform reuses the pointer pass's limit, not the mining one.
	if got := asks[0].MaxTokens; got != 16384 {
		t.Errorf("the reform asked for %d tokens, want the pointer pass's 16384", got)
	}
	reform := asks[0].Messages[len(asks[0].Messages)-1].Text()
	if !strings.Contains(reform, "TURN-ONE") || !strings.Contains(reform, "the deploy") {
		t.Errorf("the reform request is %q, want the malformed reply in it", reform)
	}

	// The queries the reform returned drove the extraction.
	ask := asks[1].Messages[1].Text()
	if !strings.Contains(ask, "TURN-ONE") {
		t.Errorf("the compose request is %q, want the matched line in it", ask)
	}

	// The record says the reply was reformed.
	for _, rec := range sink.Records() {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			if cr.Compaction.Side == nil || cr.Compaction.Side.Fork == nil || !cr.Compaction.Side.Fork.Reformed {
				t.Errorf("the compaction's side is %+v, want the reform on it", cr.Compaction.Side)
			}
		}
	}
}

// TestAReformThatDoesNotParseKeepsTheNotes covers the reform's own
// lenient parse: a reformat that does not come back as JSON leaves the
// original reply as the notes.
func TestAReformThatDoesNotParseKeepsTheNotes(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "still not json"},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: "- the deploy is pending"})
	s, _ := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	asks := askRequests(tr.Requests())
	if len(asks) != 2 {
		t.Fatalf("the transport holds %d strategy requests, want 2", len(asks))
	}
	ask := asks[1].Messages[1].Text()
	for _, want := range []string{"<notes>", "the deploy is pending"} {
		if !strings.Contains(ask, want) {
			t.Errorf("the compose request is %q, want it to carry %q", ask, want)
		}
	}
	if strings.Contains(ask, "TURN-ONE") {
		t.Errorf("the compose request is %q, want no extracted content from an unreformable reply", ask)
	}
}

// TestAReconstructionThatCannotCompleteFallsBackToTheMine covers the
// fallback: a compose ask the output limit cut short hands the run to
// the full-span strategy, whose compaction is the one on the record.
func TestAReconstructionThatCannotCompleteFallsBackToTheMine(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"x"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"x"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task", Finish: "length"},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(
		pootest.Turn{Text: `{"queries": ["TURN-ONE"]}`},
		pootest.Turn{Text: "- the fallback pointers"},
	)
	s, sink := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1, PointerMaxTokens: 512})

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

	var strategy string
	for _, rec := range sink.Records() {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			strategy = cr.Strategy
		}
	}
	if strategy != "mine" {
		t.Fatalf("the fallback's compaction strategy is %q, want mine", strategy)
	}
	// The full-span rung made its own pointer pass: two forks on the
	// record.
	if got := len(tr.Forks()); got != 2 {
		t.Errorf("the transport holds %d forks, want the selective pass and the fallback's", got)
	}
	// The fallback inherits the pointer pass's limit: the mine
	// strategy's own default would ask for the window-scaled 8192.
	if got := tr.Forks()[1].MaxTokens; got != 512 {
		t.Errorf("the fallback's fork asked for %d tokens, want the inherited 512", got)
	}
}

// TestAStoreThatCannotBeReadStillComposes covers the seam's absence: a
// session whose sink cannot read its records back composes from the
// seed and the notes alone.
func TestAStoreThatCannotBeReadStillComposes(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": "the notes"}`})
	sink := unreadableSink{pootest.NewSink()}
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 20000},
		SystemPrompt: "the system prompt",
		Compactor:    pointer.New(pointer.Options{Ratio: 0.05, KeepRecentTokens: 1}),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })

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

	asks := askRequests(tr.Requests())
	if len(asks) != 1 {
		t.Fatalf("the transport holds %d strategy requests, want 1", len(asks))
	}
	ask := asks[0].Messages[1].Text()
	if strings.Contains(ask, "TURN-ONE") {
		t.Errorf("the compose request is %q, want no extracted content from an unreadable store", ask)
	}
	if !strings.Contains(ask, "the notes") || !strings.Contains(ask, "do the thing") {
		t.Errorf("the compose request is %q, want the notes and the seed in it", ask)
	}
}

// TestASessionThatKeepsCompactingEscalatesToTheFallback covers the
// thrash guard: a run whose window keeps refilling compacts under the
// selective pass a bounded number of times, and the compactions after
// that are the full-span fallback's, whose reconstruction carries what
// the selective pass kept dropping.
func TestASessionThatKeepsCompactingEscalatesToTheFallback(t *testing.T) {
	// The turns are a kilobyte-token each: big enough to cross the
	// ratio trigger, small enough that the window never exhausts
	// between compactions.
	big := strings.Repeat("x", 3500)
	tr := pootest.NewTransport(
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: big},
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"three"}`)}}},
		pootest.Turn{Text: strings.Repeat("y", 3500)},
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"four"}`)}}},
		pootest.Turn{Text: strings.Repeat("y", 3500)},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
	)
	s, sink := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1, EscalateAfter: 2})

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

	kinds := map[string]int{}
	for _, rec := range sink.Records() {
		kinds[rec.Meta().Record]++
	}
	t.Logf("sink records: %v, settle %q source %q", kinds, res.Reason, res.Detail.Source)
	var strategies []string
	for _, rec := range sink.Records() {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			strategies = append(strategies, cr.Strategy)
		}
	}
	if len(strategies) != 3 {
		t.Fatalf("the run compacted %d times (%v), want 3", len(strategies), strategies)
	}
	for i, want := range []string{"pointer", "pointer", "mine"} {
		if strategies[i] != want {
			t.Errorf("compaction %d ran under %q, want %q", i+1, strategies[i], want)
		}
	}
}

// TestTheEscalationStaysOffUntilTheSettingsNameIt covers the guard's
// default: a session that keeps refilling its window keeps compacting
// selectively for as long as it runs, because a run the settings
// pointed at the pointer strategy must never silently spend its
// compactions under another one.
func TestTheEscalationStaysOffUntilTheSettingsNameIt(t *testing.T) {
	big := strings.Repeat("x", 3500)
	tr := pootest.NewTransport(
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: big},
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"three"}`)}}},
		pootest.Turn{Text: big},
		pootest.Turn{Text: big, Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"four"}`)}}},
		pootest.Turn{Text: big},
		pootest.Turn{Text: "the final answer"},
	)
	tr.SetForkTurns(
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
		pootest.Turn{Text: `{"queries": ["TURN-ONE"], "notes": ""}`},
	)
	s, sink := session(t, tr, pointer.Options{Ratio: 0.05, KeepRecentTokens: 1})

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
	var strategies []string
	for _, rec := range sink.Records() {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			strategies = append(strategies, cr.Strategy)
		}
	}
	for i, got := range strategies {
		if got != "pointer" {
			t.Errorf("compaction %d ran under %q, want pointer: the guard is opt-in and the settings named it not", i+1, got)
		}
	}
	if len(strategies) < 3 {
		t.Fatalf("the run compacted %d times, want the refilling run to compact repeatedly", len(strategies))
	}
}
