package summary_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/summary"
	"github.com/sdougbrown/poo/pootest"
)

// The summary strategy: the oldest turns become a model-written summary,
// the newest turn stays verbatim, and the run carries on.

// echoTool answers with the text it was called with.
//
// The strategy needs turns that keep the run going, so that the gate
// sees the view more than once.
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

// pathTool is a tool that names a path, so the strategy's file list has
// something to read.
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

// session builds a session that summarizes with the given options.
func session(t *testing.T, tr *pootest.Transport, opts summary.Options, tools map[string]poo.Tool, order []string) (*poo.Session, *pootest.Sink) {
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
		Compactor:    summary.New(opts),
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

// askRequest returns the requests a strategy made for itself.
//
// A strategy's own request carries no tools, which is what tells it
// apart from the run's requests.
func askRequest(req poo.ChatRequest) bool { return len(req.Tools) == 0 }

// summaryText returns the replacement a compaction wrote.
func summaryText(t *testing.T, sink *pootest.Sink) string {
	t.Helper()
	for _, rec := range sink.Records() {
		cr, ok := rec.(*poo.CompactionRecord)
		if !ok {
			continue
		}
		if cr.Strategy != summary.Strategy {
			t.Fatalf("the compaction's strategy is %q, want %q", cr.Strategy, summary.Strategy)
		}
		if cr.Form != "replace" {
			t.Fatalf("the compaction's form is %q, want replace", cr.Form)
		}
		if len(cr.Replacement) != 1 {
			t.Fatalf("the compaction carries %d replacement messages, want 1", len(cr.Replacement))
		}
		return cr.Replacement[0].Text()
	}
	t.Fatal("the log holds no summary compaction")
	return ""
}

// TestTheRunSummarizesAndCarriesOn covers the strategy's whole job: the
// oldest turn leaves the view, the model reads a summary in its place,
// the newest turn stays verbatim, and the run answers.
func TestTheRunSummarizesAndCarriesOn(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task\n\n## Next Steps\n1. answer"},
		pootest.Turn{Text: "the final answer"},
	)
	s, sink := session(t, tr, summary.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

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

	// The model reads the summary where the summarized turn was.
	text := summaryText(t, sink)
	for _, want := range []string{"[Context compacted: turns 1-1 summarized below]", "finish the task"} {
		if !strings.Contains(text, want) {
			t.Errorf("the replacement is %q, want it to carry %q", text, want)
		}
	}

	// The last request is the run's: the strategy's own request carries
	// no tools, and the run's does.
	requests := tr.Requests()
	last := requests[len(requests)-1]
	if len(last.Tools) == 0 {
		t.Fatalf("the last request carries no tools, so it is a strategy request")
	}
	view := requestText(last)
	if !strings.Contains(view, "finish the task") {
		t.Errorf("the last request is %q, want the summary in it", view)
	}
	if !strings.Contains(view, "TURN-TWO") {
		t.Errorf("the last request is %q, want the newest turn kept verbatim", view)
	}
	if strings.Contains(view, "TURN-ONE") {
		t.Errorf("the last request is %q, want the summarized turn gone from it", view)
	}
}

// TestTheSummaryRequestReadsTheConversation covers the shape of the
// strategy's request: it carries the covered turns as text, not as a
// conversation the model would answer, and it leaves their reasoning out.
func TestTheSummaryRequestReadsTheConversation(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{
			Text:     long("TURN-ONE"),
			Thinking: "THINKING-ONE",
			Calls:    []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}},
		},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	s, _ := session(t, tr, summary.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	var ask *poo.ChatRequest
	for _, req := range tr.Requests() {
		if askRequest(req) {
			r := req
			ask = &r
			break
		}
	}
	if ask == nil {
		t.Fatal("the strategy made no request of its own")
	}
	if len(ask.Messages) != 2 {
		t.Fatalf("the strategy's request carries %d messages, want a system prompt and the transcript", len(ask.Messages))
	}
	if ask.Messages[0].Role != poo.RoleSystem {
		t.Errorf("the first message's role is %q, want system", ask.Messages[0].Role)
	}
	transcript := ask.Messages[1].Text()
	for _, want := range []string{"<conversation>", "[Assistant]: TURN-ONE", "[Assistant tool calls]: echo", "[Tool result]:", "</conversation>"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("the transcript is %q, want it to carry %q", transcript, want)
		}
	}
	if strings.Contains(transcript, "THINKING-ONE") {
		t.Errorf("the transcript is %q, want the reasoning left out", transcript)
	}
}

// TestATruncatedSummaryIsRefused covers the rule that a summary the
// output limit cut short never reaches the view: below the hard limit
// the run carries on without it.
func TestATruncatedSummaryIsRefused(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nhalf a summ", Finish: "length"},
		pootest.Turn{Text: "the final answer"},
	)
	// A window whose hard limit is far above the trigger, so the refusal
	// leaves the run alive.
	s, sink := session(t, tr, summary.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

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
	for _, rec := range sink.Records() {
		if _, ok := rec.(*poo.CompactionRecord); ok {
			t.Fatal("the truncated summary was recorded as a compaction")
		}
	}
	requests := tr.Requests()
	view := requestText(requests[len(requests)-1])
	if strings.Contains(view, "half a summ") {
		t.Errorf("the last request is %q, want no truncated summary in it", view)
	}
}

// TestEachCompactionSupersedesTheLast covers repeated compaction: the
// view holds one summary, not a stack of them, and the request that
// writes the new one carries the previous summary forward.
func TestEachCompactionSupersedesTheLast(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nFIRST-SUMMARY"},
		pootest.Turn{Text: long("TURN-THREE"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"three"}`)}}},
		pootest.Turn{Text: "## Goal\nSECOND-SUMMARY"},
		pootest.Turn{Text: "the final answer"},
	)
	s, sink := session(t, tr, summary.Options{Ratio: 0.05, KeepRecentTokens: 1},
		map[string]poo.Tool{"echo": &echoTool{}}, []string{"echo"})

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	compactions := 0
	for _, rec := range sink.Records() {
		if _, ok := rec.(*poo.CompactionRecord); ok {
			compactions++
		}
	}
	if compactions < 2 {
		t.Fatalf("the log holds %d compactions, want at least 2", compactions)
	}

	view := requestText(tr.Requests()[len(tr.Requests())-1])
	if got := strings.Count(view, "[Context compacted:"); got != 1 {
		t.Errorf("the view carries %d summaries, want 1: %q", got, view)
	}
	if !strings.Contains(view, "SECOND-SUMMARY") {
		t.Errorf("the view is %q, want the newest summary in it", view)
	}

	// The second request carries the first summary, so the checkpoint
	// keeps the goal and the decisions the older turns held.
	var asks []poo.ChatRequest
	for _, req := range tr.Requests() {
		if askRequest(req) {
			asks = append(asks, req)
		}
	}
	if len(asks) < 2 {
		t.Fatalf("the strategy made %d requests, want at least 2", len(asks))
	}
	if strings.Contains(asks[0].Messages[1].Text(), "<previous-summary>") {
		t.Errorf("the first request already carries a previous summary")
	}
	if !strings.Contains(asks[1].Messages[1].Text(), "<previous-summary>") {
		t.Errorf("the second request is %q, want the previous summary in it", asks[1].Messages[1].Text())
	}
}

// TestTheSummaryNamesTheFilesTheWorkTouched covers the file list: the
// files the summarized turns read and changed are stated, so the model
// does not read them again to find out.
func TestTheSummaryNamesTheFilesTheWorkTouched(t *testing.T) {
	read := json.RawMessage(`{"path":"internal/a.go"}`)
	edited := json.RawMessage(`{"path":"internal/b.go"}`)
	tr := pootest.NewTransport(
		pootest.Turn{Text: long("TURN-ONE"), Calls: []pootest.Call{
			{Name: "read", Arguments: read},
			{Name: "edit", Arguments: edited},
		}},
		pootest.Turn{Text: long("TURN-TWO"), Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		pootest.Turn{Text: "## Goal\nfinish the task"},
		pootest.Turn{Text: "the final answer"},
	)
	tools := map[string]poo.Tool{
		"read": &pathTool{name: "read"},
		"edit": &pathTool{name: "edit"},
		"echo": &echoTool{},
	}
	order := []string{"read", "edit", "echo"}
	s, sink := session(t, tr, summary.Options{Ratio: 0.05, KeepRecentTokens: 1}, tools, order)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	text := summaryText(t, sink)
	for _, want := range []string{"Files read:\n- internal/a.go", "Files modified:\n- internal/b.go"} {
		if !strings.Contains(text, want) {
			t.Errorf("the replacement is %q, want it to carry %q", text, want)
		}
	}
}

// requestText renders a request's messages as one string.
func requestText(req poo.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Text())
		b.WriteString("\n")
	}
	return b.String()
}
