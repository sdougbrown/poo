package session

// Review scratch tests: crash windows that leave a tool call the model
// saw without a result, which the recovery never repairs.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// fix1AssistantCallMessage returns the assistant message record that carries
// one tool call, the way the core records a model response with calls.
func fix1AssistantCallMessage(runID string, turn int, callID, handle, tool string) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleAssistant,
		Content: []poo.ContentBlock{{
			Kind:       poo.ContentToolCall,
			ToolCallID: callID,
			Name:       tool,
			Arguments:  json.RawMessage(`{}`),
		}},
	}
}

// the log holds the assistant message and the tool.call record, but the
// process was killed before the tool.result record: the recovery answers
// the call.
func TestRecoveryAnswersACallWithNoResult(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		fix1AssistantCallMessage("r1", 1, "call-1", "r1", "bash"),
		toolCall("r1", 1, "r1", "bash"),
	)
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	hasResult := false
	hasMessage := false
	for _, rec := range loaded.Records {
		switch r := rec.(type) {
		case *poo.ToolResultRecord:
			if r.Handle == "r1" {
				hasResult = true
			}
		case *poo.MessageRecord:
			for _, b := range r.Content {
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					hasMessage = true
				}
			}
		}
	}
	if !hasResult || !hasMessage {
		t.Fatalf("control: the recovery wrote result=%v message=%v, want both", hasResult, hasMessage)
	}
}

// A kill between the tool.result record and the message record that
// carries it leaves the call unanswered for good: the recovery keys on
// the result record, so the next load writes no message, and the
// restored session sends a request whose tool call has no result.
func TestAKillBetweenTheResultAndItsMessageIsRepaired(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := newBlockingTool()
	writeSession(t, dir, "s1",
		testHeader("s1", tool),
		runStarted("r1"),
		fix1AssistantCallMessage("r1", 1, "call-1", "r1", "blocker"),
		toolCallWithID("r1", 1, "r1", "blocker", "call-1"),
	)

	// The recovery's second write (the message) fails: the state a kill
	// leaves after the tool.result record landed.
	store.appendFault = 2
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the load succeeded, want the injected failure")
	}
	store.appendFault = 0
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	hasResult := false
	hasMessage := false
	for _, rec := range loaded.Records {
		switch r := rec.(type) {
		case *poo.ToolResultRecord:
			if r.Handle == "r1" {
				hasResult = true
			}
		case *poo.MessageRecord:
			for _, b := range r.Content {
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					hasMessage = true
				}
			}
		}
	}
	if !hasResult {
		t.Fatal("the log holds no tool.result record, want one")
	}
	if !hasMessage {
		t.Fatal("the log holds no message for the result, want it repaired")
	}

	transport := pootest.NewTransport(pootest.Turn{Text: "ok"})
	opts := testOptions("s1", tool, transport)
	res, err := Resume(ctx, store, "s1", opts)
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	sess := res.Session
	if _, err := sess.Prompt(ctx, "continue"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the resumed session made no request")
	}
	unpaired := false
	for _, req := range requests {
		for _, msg := range req.Messages {
			for _, b := range msg.Content {
				if b.Kind == poo.ContentToolCall && b.ToolCallID == "call-1" {
					unpaired = true
				}
			}
		}
	}
	if !unpaired {
		t.Fatal("control failed: the request has no tool call")
	}
	// The call is in the request. Does any message answer it?
	answered := false
	for _, req := range requests {
		for _, msg := range req.Messages {
			for _, b := range msg.Content {
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					answered = true
				}
			}
		}
	}
	if !answered {
		t.Errorf("the resumed request carries the call call-1 with no result")
	}
}

// A kill between the assistant message record and the tool.call record
// leaves a call the model saw with no record at all: the recovery sees
// no call, settles the run, and the restored session sends the call
// with no result.
func TestAKillBeforeTheCallRecordIsRepaired(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := newBlockingTool()
	writeSession(t, dir, "s1",
		testHeader("s1", tool),
		runStarted("r1"),
		fix1AssistantCallMessage("r1", 1, "call-1", "r1", "blocker"),
	)
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// The recovery wrote the result the model never saw a record for.
	hasResult := false
	for _, rec := range loaded.Records {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			hasResult = true
			if r.CallID != "call-1" {
				t.Errorf("the recovered result carries the call id %q, want call-1", r.CallID)
			}
		}
	}
	if !hasResult {
		t.Fatal("the recovery wrote no result record, want one")
	}

	transport := pootest.NewTransport(pootest.Turn{Text: "ok"})
	opts := testOptions("s1", tool, transport)
	res, err := Resume(ctx, store, "s1", opts)
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	sess := res.Session
	if _, err := sess.Prompt(ctx, "continue"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	sawCall := false
	answered := false
	for _, req := range transport.Requests() {
		for _, msg := range req.Messages {
			for _, b := range msg.Content {
				if b.Kind == poo.ContentToolCall && b.ToolCallID == "call-1" {
					sawCall = true
				}
				if b.Kind == poo.ContentToolResult && b.ToolCallID == "call-1" {
					answered = true
				}
			}
		}
	}
	if sawCall && !answered {
		t.Errorf("the resumed request carries the call call-1 with no result")
	}
	if !sawCall {
		t.Fatal("control failed: the request has no tool call")
	}
}
