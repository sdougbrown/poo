package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The after-tool chain runs before anything about the result is
// recorded or emitted, so the record, the event and the model all see
// the same sanitized result.

// recordingAfterTool is an after-tool hook that records what it was
// asked and answers with a fixed decision.
type recordingAfterTool struct {
	mu sync.Mutex
	// calls counts the calls the hook saw.
	calls int
	// decision is what the hook answers.
	decision poo.ResultDecision
	// seen holds the results the hook was given, in order.
	seen []poo.ToolResult
	// names are the names of the hooks, in the order they ran.
	names *[]string
	// name is the hook's name.
	name string
	// runIDs holds the run ids the hook was given, in order.
	runIDs []string
	// jobs holds the job ids the hook was given, in order.
	jobs []string
	// running holds whether the session had a run when the hook ran.
	running []bool
	// session is the session the hook asks about, when it is set.
	session *poo.Session
}

// AfterTool records the result and answers.
func (h *recordingAfterTool) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	h.mu.Lock()
	h.calls++
	h.seen = append(h.seen, result)
	decision := h.decision
	h.runIDs = append(h.runIDs, ctx.RunID)
	h.jobs = append(h.jobs, ctx.JobID)
	session := h.session
	h.mu.Unlock()
	if session != nil {
		h.mu.Lock()
		h.running = append(h.running, session.Running())
		h.mu.Unlock()
	}
	if h.names != nil {
		*h.names = append(*h.names, h.name)
	}
	return decision, nil
}

// Calls returns how many results the hook saw.
func (h *recordingAfterTool) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// RunIDs returns the run ids the hook was given, in order.
func (h *recordingAfterTool) RunIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.runIDs...)
}

// Jobs returns the job ids the hook was given, in order.
func (h *recordingAfterTool) Jobs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.jobs...)
}

// Running holds whether the session had a run when the hook ran.
func (h *recordingAfterTool) Running() []bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]bool(nil), h.running...)
}

// Seen returns the results the hook was given, in order.
func (h *recordingAfterTool) Seen() []poo.ToolResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]poo.ToolResult(nil), h.seen...)
}

// TestAnAnnotationKeepsTheOriginalInTheRecord covers the rule:
// an annotation changes what the model reads, and the record keeps the
// original.
func TestAnAnnotationKeepsTheOriginalInTheRecord(t *testing.T) {
	tool := &echoTool{result: "the original"}
	hook := &recordingAfterTool{decision: poo.Annotate("and the note")}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if result := lastToolResult(t, tr); !strings.Contains(result, "the original") || !strings.Contains(result, "and the note") {
		t.Errorf("the model saw %q, want the original and the note", result)
	}
	rec := toolResultRecord(t, sink)
	if !strings.Contains(rec.Text, "the original") {
		t.Errorf("the record's text is %q, want the original in it", rec.Text)
	}
	if len(rec.Hooks) != 1 || rec.Hooks[0].Action != "annotate" {
		t.Fatalf("the record's hook decisions are %+v, want one annotation", rec.Hooks)
	}
	if rec.Hooks[0].Text != "and the note" {
		t.Errorf("the record's decision text is %q, want the note", rec.Hooks[0].Text)
	}
	if rec.Original != nil {
		t.Errorf("the record holds an original %+v, want none: the text keeps it", rec.Original)
	}
}

// TestAReplacementKeepsTheOriginalInTheRecord covers the rule:
// the model has not seen the result yet, so the record keeps what the
// tool really said.
func TestAReplacementKeepsTheOriginalInTheRecord(t *testing.T) {
	tool := &echoTool{result: "the original"}
	hook := &recordingAfterTool{decision: poo.Replace(poo.ToolResult{Text: "the replacement"})}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if want := poo.HandleLine("r1", "echo") + "\n" + "the replacement"; lastToolResult(t, tr) != want {
		t.Errorf("the model saw %q, want %q", lastToolResult(t, tr), want)
	}
	rec := toolResultRecord(t, sink)
	if want := poo.HandleLine("r1", "echo") + "\n" + "the replacement"; rec.Text != want {
		t.Errorf("the record's text is %q, want %q", rec.Text, want)
	}
	if rec.Original == nil {
		t.Fatal("the record holds no original, want the tool's own result")
	}
	if rec.Original.Text != "the original" {
		t.Errorf("the record's original is %q, want the tool's own result", rec.Original.Text)
	}
	if len(rec.Hooks) != 1 || rec.Hooks[0].Action != "replace" {
		t.Fatalf("the record's hook decisions are %+v, want one replacement", rec.Hooks)
	}
}

// TestARedactionNeverWritesTheOriginal covers the rule: the
// original of a redacted result never reaches the record, the events or
// the store.
func TestARedactionNeverWritesTheOriginal(t *testing.T) {
	const secret = "s3cr3t-token"
	tool := &echoTool{result: "the output with " + secret}
	hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if result := lastToolResult(t, tr); strings.Contains(result, secret) {
		t.Errorf("the model saw the secret in %q, want it redacted", result)
	}
	rec := toolResultRecord(t, sink)
	if !rec.Redacted {
		t.Error("the record does not say the result was redacted")
	}
	if rec.RedactedBy == "" {
		t.Error("the record does not name the hook that redacted the result")
	}
	if rec.Original != nil {
		t.Errorf("the record holds an original %+v, want none", rec.Original)
	}
	if strings.Contains(rec.Text, secret) {
		t.Errorf("the record's text is %q, want no secret in it", rec.Text)
	}
	for _, decision := range rec.Hooks {
		if decision.Text != "" {
			t.Errorf("the record's decision %+v holds contents, want none in a redacted chain", decision)
		}
	}
	if strings.Contains(recordsText(sink), secret) {
		t.Error("the secret reached the store, want it redacted")
	}
}

// TestTheChainRunsInRegistrationOrder covers the rule: each
// replacement gets the previous hook's output.
func TestTheChainRunsInRegistrationOrder(t *testing.T) {
	names := &[]string{}
	first := &recordingAfterTool{name: "first", names: names, decision: poo.Replace(poo.ToolResult{Text: "the first replacement"})}
	second := &recordingAfterTool{name: "second", names: names, decision: poo.Annotate("the second note")}
	tool := &echoTool{result: "the original"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{first, second}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if len(*names) != 2 || (*names)[0] != "first" || (*names)[1] != "second" {
		t.Fatalf("the hooks ran in the order %v, want first then second", *names)
	}
	seen := second.Seen()
	if len(seen) != 1 {
		t.Fatalf("the second hook saw %d results, want 1", len(seen))
	}
	if seen[0].Text != "the first replacement" {
		t.Errorf("the second hook saw %q, want the first hook's output", seen[0].Text)
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + "the first replacement\nthe second note"; lastToolResult(t, tr) != want {
		t.Errorf("the model saw %q, want %q", lastToolResult(t, tr), want)
	}
}

// TestARedactionWinsOverAReplacement covers the rule: a
// redaction wins over a replacement in the same decision, and the
// original never reaches the record.
func TestARedactionWinsOverAReplacement(t *testing.T) {
	const secret = "s3cr3t-token"
	tool := &echoTool{result: "the output with " + secret}
	hook := &recordingAfterTool{decision: poo.ResultDecision{
		Redacted: true,
		Replaced: &poo.ToolResult{Text: "the replacement"},
	}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	rec := toolResultRecord(t, sink)
	if !rec.Redacted {
		t.Error("the record does not say the result was redacted")
	}
	if rec.Original != nil {
		t.Errorf("the record holds an original %+v, want none", rec.Original)
	}
	if strings.Contains(recordsText(sink), secret) {
		t.Error("the secret reached the store, want it redacted")
	}
}

// toolResultRecord returns the tool result record the sink kept.
func toolResultRecord(t *testing.T, sink *recordingSink) *poo.ToolResultRecord {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			return r
		}
	}
	t.Fatal("the store holds no tool result record")
	return nil
}

// recordsText renders every record the sink kept, so a test can look
// for a secret that must not be there.
func recordsText(sink *recordingSink) string {
	var b strings.Builder
	for _, rec := range sink.allRecords() {
		b.WriteString(rec.Meta().Record)
		b.WriteString(" ")
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			b.WriteString(r.Text)
			if r.Original != nil {
				b.WriteString(r.Original.Text)
			}
		}
		if m, ok := rec.(*poo.MessageRecord); ok {
			for _, c := range m.Content {
				b.WriteString(c.Text)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestAStoppedSyncCallStillRunsItsAfterToolChain covers the rule:
// the after-tool chain runs for every call whose tool ran, on a context
// of its own, so an interrupt or a cancellation does not bypass a
// redaction.
func TestAStoppedSyncCallStillRunsItsAfterToolChain(t *testing.T) {
	const secret = "s3cr3t-in-a-call"
	for _, mode := range []string{"interrupt", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{backgroundCall("slow", "one")}},
				{text: "done"},
			})
			sink := &recordingSink{}
			tool := newHoldTool("slow", "the output with "+secret)
			tool.block = true
			opts := oneToolOptions(t, tr, tool, sink)
			hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
			opts.AfterTool = []poo.AfterToolHook{hook}
			s := newTestSession(t, opts)

			if _, err := s.Prompt(context.Background(), "go"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			<-tool.started
			switch mode {
			case "interrupt":
				if _, err := s.Send("stop", poo.Interrupt); err != nil {
					t.Fatalf("interrupting the run: %v", err)
				}
			case "cancel":
				s.Cancel("cancel:test")
			}
			waitFor(t, "the run to settle", func() bool { return sink.Settled() != nil })

			if got := hook.Calls(); got != 1 {
				t.Errorf("the chain ran %d time(s), want 1 at the call's completion", got)
			}
			rec := toolResultRecord(t, sink)
			if strings.Contains(rec.Text, secret) {
				t.Errorf("the tool.result record holds the secret: %q", rec.Text)
			}
			for _, ev := range s.Events() {
				if ev.Name != poo.EventToolResult {
					continue
				}
				text, _ := ev.Payload["text"].(string)
				if strings.Contains(text, secret) {
					t.Errorf("the tool.result event holds the secret: %q", text)
				}
			}
			if store := sinkJSON(sink, secret); strings.Contains(store, secret) {
				t.Errorf("the secret reached the store:\n%s", store)
			}
		})
	}
}

// TestARedactionSurvivesALaterHookFailure covers the rule: any
// Redact makes the whole chain redacted, so a hook that fails afterwards
// must not expose text an earlier decision carried.
func TestARedactionSurvivesALaterHookFailure(t *testing.T) {
	const secret = "REVIEW_SECRET_4AB"
	for _, mode := range []string{"error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)}}},
				{text: "done"},
			})
			sink := &recordingSink{}
			opts := testOptions(t, tr, &echoTool{result: secret}, sink)
			opts.HookTimeout = 10 * time.Millisecond
			opts.AfterTool = []poo.AfterToolHook{
				afterToolFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
					return poo.Replace(result), nil
				}),
				afterToolFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
					return poo.Redact(poo.ToolResult{Text: "safe"}), nil
				}),
				afterToolFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
					if mode == "timeout" {
						<-ctx.Done()
						return poo.Keep(), ctx.Err()
					}
					return poo.Keep(), context.Canceled
				}),
			}
			s := newTestSession(t, opts)
			runSession(t, s, "start")
			if store := sinkJSON(sink, secret); strings.Contains(store, secret) {
				t.Errorf("the redacted secret persisted after %s: %s", mode, store)
			}
		})
	}
}

// TestAFatalErrorsTextIsTheHookProcessedResult covers the rule:
// only the hook-processed result is recorded or emitted. A fatal tool
// error keeps its tool_fatal classification, but its published text is
// the result the chain returned.
func TestAFatalErrorsTextIsTheHookProcessedResult(t *testing.T) {
	const secret = "REVIEW_FATAL_SECRET"
	tool := &echoTool{call: func(context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{}, poo.Fatalf("failed with token %s", secret)
	}}
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)}}}})
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{afterToolFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
		return poo.Redact(poo.ToolResult{Text: "safe failure", IsError: true}), nil
	})}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	if store := sinkJSON(sink, secret); strings.Contains(store, secret) {
		t.Errorf("the fatal error's secret reached the store: %s", store)
	}
	events, _ := json.Marshal(s.Events())
	if strings.Contains(string(events), secret) {
		t.Error("the fatal error's secret reached the events")
	}
	if got := sink.Settled(); got == nil || got.Reason != poo.ReasonToolFatal {
		t.Fatalf("the run settled %v, want tool_fatal", got)
	}
	if got := sink.Settled().Detail.Text; strings.Contains(got, secret) {
		t.Errorf("the settle detail holds the secret: %q", got)
	}
}
