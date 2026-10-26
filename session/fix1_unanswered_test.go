package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// fix1Bash is a side-effecting tool named the way the fixtures name
// theirs, so a restored session's compatibility check passes.
type fix1Bash struct{}

// Spec describes the tool.
func (fix1Bash) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "bash",
		Description: "the fixture tool",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassSideEffecting,
	}
}

// Prepare accepts the call.
func (fix1Bash) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return blockingArgs{}, nil
}

// Call returns at once.
func (fix1Bash) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "done"}, nil
}

// fix1Read is the read-only tool the fixture header names.
type fix1Read struct{ fix1Bash }

// Spec describes the tool.
func (fix1Read) Spec() poo.ToolSpec {
	spec := fix1Bash{}.Spec()
	spec.Name = "read"
	spec.Class = poo.ClassReadOnly
	return spec
}

// fix1AssistantCall returns the assistant message that carries a tool call.
func fix1AssistantCall(runID string, turn int, callID, tool string) *poo.MessageRecord {
	return &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      runID,
		Turn:       turn,
		Role:       poo.RoleAssistant,
		MessageID:  "m1",
		Content: []poo.ContentBlock{{
			Kind:       poo.ContentToolCall,
			ToolCallID: callID,
			Name:       tool,
			Arguments:  json.RawMessage(`{}`),
		}},
	}
}

// fix1Options returns the options a restore of the fixture session
// takes.
func fix1Options(id string, tool poo.Tool, transport poo.ChatTransport) poo.Options {
	return poo.Options{
		Transport: transport,
		Tools: map[string]poo.Tool{
			tool.Spec().Name: tool,
			"read":           fix1Read{},
		},
		ToolOrder:    []string{tool.Spec().Name, "read"},
		SessionID:    id,
		Model:        poo.ModelInfo{ModelID: "m", ContextLimit: 32768},
		Sink:         nil,
		SystemPrompt: "the prompt",
	}
}

// TestARecoveredCallIsAnsweredInTheNextRequest is the
// consequence of the recovery's two-record write: the result lands, its
// message does not, and the next request carries a call the provider
// cannot match.
func TestARecoveredCallIsAnsweredInTheNextRequest(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	tool := fix1Bash{}
	writeSession(t, dir, "s1",
		createdRecord("s1"),
		runStarted("r1"),
		fix1AssistantCall("r1", 1, "call_1", "bash"),
		toolCallWithID("r1", 1, "r1", "bash", "call_1"),
	)

	// The result is appended, the message that carries it fails.
	store.appendFault = 2
	if _, err := store.Load(ctx, "s1"); err == nil {
		t.Fatal("the load succeeded, want the injected failure")
	}
	store.appendFault = 0

	transport := pootest.NewTransport(pootest.Turn{Text: "ok", Finish: "stop"})
	resumed, err := Resume(ctx, store, "s1", fix1Options("s1", tool, transport))
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	t.Cleanup(func() { resumed.Session.Close() })
	if _, err := resumed.Session.Prompt(ctx, "carry on"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := resumed.Session.Wait(ctx); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}
	requests := transport.Requests()
	if len(requests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(requests))
	}
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, msg := range requests[0].Messages {
		for _, block := range msg.Content {
			switch block.Kind {
			case poo.ContentToolCall:
				calls[block.ToolCallID] = true
			case poo.ContentToolResult:
				results[block.ToolCallID] = true
			}
		}
	}
	var unanswered []string
	for id := range calls {
		if !results[id] {
			unanswered = append(unanswered, id)
		}
	}
	if len(unanswered) > 0 {
		t.Errorf("the next request carries the calls %s with no result", strings.Join(unanswered, ","))
	}
}
