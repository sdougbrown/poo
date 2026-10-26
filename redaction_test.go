package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The redaction: a secret a hook redacted never reaches the record,
// the model, the event, the projection or a restored session.

// theSecret is the sentinel the redaction tests look for.
const theSecret = "s3cr3t-token-value"

// secretTool returns the secret, in the text and in the structured
// payload.
type secretTool struct {
	// wait makes the call wait for its context before it returns.
	wait bool
	// started is closed when the call starts.
	started chan struct{}
}

// Spec describes the tool.
func (t *secretTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "a tool that leaks a secret",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts anything.
func (t *secretTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{text: string(raw)}, nil
}

// Call returns the secret.
func (t *secretTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if t.started != nil {
		select {
		case <-t.started:
		default:
			close(t.started)
		}
	}
	if t.wait {
		<-ctx.Done()
	}
	return poo.ToolResult{
		Text:       "the token is " + theSecret,
		Structured: json.RawMessage(`{"token":"` + theSecret + `"}`),
	}, nil
}

// redactingHook redacts every result it sees.
type redactingHook struct {
	// result is what the model reads instead of the secret.
	result poo.ToolResult
}

// AfterTool redacts the result.
func (h redactingHook) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	return poo.Redact(h.result), nil
}

// replacingHook replaces every result it sees.
type replacingHook struct {
	// result is what the model reads instead.
	result poo.ToolResult
}

// AfterTool replaces the result.
func (h replacingHook) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	return poo.Replace(h.result), nil
}

// secretEverywhere looks for the secret in everything a session
// produced: the records, the requests, the events and the projection.
func secretEverywhere(t *testing.T, s *poo.Session, records []poo.Record, transport *pootest.Transport) {
	t.Helper()
	for _, rec := range records {
		raw, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("encoding a record: %v", err)
		}
		if strings.Contains(string(raw), theSecret) {
			t.Errorf("the record %s holds the secret: %s", rec.Meta().Record, raw)
		}
	}
	for i, req := range transport.Requests() {
		raw, err := json.Marshal(req.Messages)
		if err != nil {
			t.Fatalf("encoding a request: %v", err)
		}
		if strings.Contains(string(raw), theSecret) {
			t.Errorf("the request %d holds the secret: %s", i, raw)
		}
	}
	for _, ev := range s.Events() {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("encoding an event: %v", err)
		}
		if strings.Contains(string(raw), theSecret) {
			t.Errorf("the event %s holds the secret: %s", ev.Name, raw)
		}
	}
	page := poo.ProjectHistory(records, 0, 0)
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("encoding the projection: %v", err)
	}
	if strings.Contains(string(raw), theSecret) {
		t.Errorf("the projection holds the secret: %s", raw)
	}
}

// secretOptions builds the options of a session with a secret tool.
func secretOptions(t *testing.T, tr poo.ChatTransport, tool poo.Tool, sink poo.RecordSink) poo.Options {
	t.Helper()
	return poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": tool},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}
}

// secretScript is the script a run with a secret tool follows.
func secretScript() *pootest.Transport {
	return scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "done"},
	})
}

// TestARedactedResultKeepsTheSecretOutOfEverything covers the
// redaction: the secret is in the tool's result, and a hook redacts it.
// The secret reaches neither the record, nor the model, nor the event,
// nor the projection, nor a restored session.
func TestARedactedResultKeepsTheSecretOutOfEverything(t *testing.T) {
	tr := secretScript()
	sink := &recordingSink{}
	opts := secretOptions(t, tr, &secretTool{}, sink)
	opts.AfterTool = []poo.AfterToolHook{redactingHook{result: poo.ToolResult{Text: "[the result was redacted]"}}}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	secretEverywhere(t, s, sink.allRecords(), tr)

	// The result the model reads is the redaction.
	for _, rec := range sink.allRecords() {
		result, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if !result.Redacted {
			t.Error("the result record does not say it was redacted")
		}
		if result.Original != nil {
			t.Errorf("the result record holds the original: %+v", result.Original)
		}
		if result.Structured != nil {
			t.Errorf("the result record holds a structured payload: %s", result.Structured)
		}
	}

	// A restored session holds no secret either.
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	session, _, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	for _, m := range poo.Messages(session) {
		if strings.Contains(m.Text(), theSecret) {
			t.Errorf("the restored view holds the secret: %q", m.Text())
		}
	}
}

// TestARedactionHoldsWithTwoHooksInBothOrders covers the chain: a
// redaction wins over the whole chain, whatever order the hooks ran in.
func TestARedactionHoldsWithTwoHooksInBothOrders(t *testing.T) {
	cases := []struct {
		name  string
		hooks []poo.AfterToolHook
	}{
		{
			name: "replace then redact",
			hooks: []poo.AfterToolHook{
				replacingHook{result: poo.ToolResult{Text: "a replacement"}},
				redactingHook{result: poo.ToolResult{Text: "[the result was redacted]"}},
			},
		},
		{
			name: "redact then replace",
			hooks: []poo.AfterToolHook{
				redactingHook{result: poo.ToolResult{Text: "[the result was redacted]"}},
				replacingHook{result: poo.ToolResult{Text: "a replacement"}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := secretScript()
			sink := &recordingSink{}
			opts := secretOptions(t, tr, &secretTool{}, sink)
			opts.AfterTool = c.hooks
			s := newTestSession(t, opts)
			runSession(t, s, "start")

			secretEverywhere(t, s, sink.allRecords(), tr)
			for _, rec := range sink.allRecords() {
				result, ok := rec.(*poo.ToolResultRecord)
				if !ok {
					continue
				}
				if !result.Redacted {
					t.Error("the result record does not say it was redacted")
				}
				if result.Original != nil {
					t.Errorf("the result record holds the original: %+v", result.Original)
				}
				for _, decision := range result.Hooks {
					if decision.Text != "" {
						t.Errorf("the decision of %q holds text: %q", decision.Hook, decision.Text)
					}
				}
			}
		})
	}
}

// TestARedactionHoldsWhenTheRunIsCancelledMidBatch covers the
// cancellation: a batch that is cut short leaves no secret behind.
func TestARedactionHoldsWhenTheRunIsCancelledMidBatch(t *testing.T) {
	tr := secretScript()
	sink := &recordingSink{}
	tool := &secretTool{wait: true, started: make(chan struct{})}
	opts := secretOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{redactingHook{result: poo.ToolResult{Text: "[the result was redacted]"}}}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.started
	// The run is cancelled while the call is running.
	s.Cancel("the test cancelled the run")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	secretEverywhere(t, s, sink.allRecords(), tr)
}
