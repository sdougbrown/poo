package wire_test

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The handshake tells the client what this binary can do.
func TestInitializeReportsTheBinary(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})

	result := h.initialize()
	if result.ProtocolVersion != protocol.Version {
		t.Errorf("protocol version = %d, want %d", result.ProtocolVersion, protocol.Version)
	}
	if result.Build.Name != "poo-test" {
		t.Errorf("build name = %q, want poo-test", result.Build.Name)
	}
	if len(result.Profiles) != 1 || result.Profiles[0].Name != "code" {
		t.Fatalf("profiles = %+v, want one named code", result.Profiles)
	}
	if len(result.Profiles[0].Tools) != 1 || result.Profiles[0].Tools[0].Name != "test" {
		t.Errorf("the profile's tools = %+v, want the test tool", result.Profiles[0].Tools)
	}
	if result.Models.Chat != "test-model" {
		t.Errorf("the chat model = %q, want test-model", result.Models.Chat)
	}
	if len(result.Capabilities.Persistence) != 2 {
		t.Errorf("the persistence modes = %v, want two", result.Capabilities.Persistence)
	}
	if !result.Capabilities.Compaction {
		t.Error("the binary compacts, so the capability should say so")
	}
}

// A method before the handshake is refused.
func TestMethodBeforeTheHandshakeIsRefused(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})

	_, rerr := h.call(protocol.MethodSessionGet, protocol.GetParams{SessionID: "nope"})
	if rerr == nil {
		t.Fatal("the call before the handshake was answered")
	}
	if rerr.Code != protocol.CodeNotInitialized {
		t.Errorf("the code = %d, want %d", rerr.Code, protocol.CodeNotInitialized)
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataNotInitialized {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataNotInitialized)
	}
}

// A client that speaks no common version is refused.
func TestUnsupportedProtocolVersion(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})

	_, rerr := h.call(protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersions: []int{protocol.Version + 1, protocol.Version + 2},
		Client:           protocol.Client{Name: "test", Version: "test"},
	})
	if rerr == nil {
		t.Fatal("the handshake with no shared version was answered")
	}
	if rerr.Code != protocol.CodeVersionUnsupported {
		t.Errorf("the code = %d, want %d", rerr.Code, protocol.CodeVersionUnsupported)
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataProtocolVersionUnsupported {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataProtocolVersionUnsupported)
	}
}

// A line that is not JSON is a parse error.
func TestMalformedJSONIsAParseError(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})

	h.writeRaw("{this is not json")
	select {
	case line := <-h.stray:
		var resp protocol.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("decoding the answer: %v", err)
		}
		if resp.Error == nil {
			t.Fatal("the malformed line was answered with a result")
		}
		if resp.Error.Code != protocol.CodeParseError {
			t.Errorf("the code = %d, want %d", resp.Error.Code, protocol.CodeParseError)
		}
		if resp.Error.Data == nil || resp.Error.Data.Code != protocol.DataParseError {
			t.Errorf("the data code = %+v, want %q", resp.Error.Data, protocol.DataParseError)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not answer the malformed line")
	}
}

// A params object with a field the method does not know is refused.
func TestUnknownFieldIsRefused(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	_, rerr := h.callRaw(protocol.MethodSessionGet, `{"session_id":"x","extra":true}`)
	if rerr == nil {
		t.Fatal("the unknown field was answered")
	}
	if rerr.Code != protocol.CodeInvalidParams {
		t.Errorf("the code = %d, want %d", rerr.Code, protocol.CodeInvalidParams)
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataUnknownField {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataUnknownField)
	}
}

// A params object that is null is refused.
func TestExplicitNullIsRefused(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	_, rerr := h.callRaw(protocol.MethodSessionGet, `null`)
	if rerr == nil {
		t.Fatal("the null params were answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataInvalidParams {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataInvalidParams)
	}
}

// A method the server does not have is refused.
func TestUnknownMethodIsRefused(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	_, rerr := h.call("session/nope", map[string]any{})
	if rerr == nil {
		t.Fatal("the unknown method was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataMethodNotFound {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataMethodNotFound)
	}
}

// The run of a session goes on while the client watches it.
func TestPromptRunsAndSettles(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{
		SessionID: sessionID,
		Text:      "hi",
	})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	if prompted.RunID == "" {
		t.Fatal("the prompt returned no run id")
	}
	settled := h.event("run.settled")
	if settled.Reason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
}

// The events arrive in sequence order.
func TestEventsArriveInOrder(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	h.event("run.settled")

	h.mu.Lock()
	events := append([]event(nil), h.all...)
	h.mu.Unlock()
	var last int64
	for _, evt := range events {
		if evt.Seq <= last {
			t.Fatalf("the event %d came after %d", evt.Seq, last)
		}
		last = evt.Seq
	}
	if last == 0 {
		t.Fatal("the server sent no events")
	}
}

// A second prompt while the run is going is refused as busy.
func TestSecondPromptWhileRunningIsBusy(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	transport.waitStarted(t)

	_, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "again"})
	if rerr == nil {
		t.Fatal("the second prompt was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataBusy {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataBusy)
	}
	transport.release()
}

// A message for a session with no run is refused as not_running.
func TestMessageWithoutARunIsNotRunning(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	_, rerr := h.call(protocol.MethodAgentMessage, protocol.MessageParams{SessionID: sessionID, Text: "hi"})
	if rerr == nil {
		t.Fatal("the message was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataNotRunning {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataNotRunning)
	}
}

// A client that answers a permission ask allows the call.
func TestPermissionAskAllowed(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	ask := theAsk(t, h)
	if ask.Tool != "test" {
		t.Errorf("the ask names the tool %q, want test", ask.Tool)
	}
	if ask.Class != string(poo.ClassSideEffecting) {
		t.Errorf("the ask names the class %q, want side_effecting", ask.Class)
	}
	h.answer(&protocol.Request{ID: json.RawMessage(`"` + ask.AskID + `"`)}, protocol.DecisionAllow)

	settled := h.event("run.settled")
	if settled.Reason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	// The tool ran: the model's second turn saw its result.
	result := h.event("tool.result")
	if result.Fields["outcome"] != "ok" {
		t.Errorf("the tool's outcome = %v, want ok", result.Fields["outcome"])
	}
}

// A client that answers a permission ask denies the call.
func TestPermissionAskDenied(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	ask := theAsk(t, h)
	h.answer(&protocol.Request{ID: json.RawMessage(`"` + ask.AskID + `"`)}, protocol.DecisionDeny)

	settled := h.event("run.settled")
	if settled.Reason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	result := h.event("tool.result")
	if result.Fields["outcome"] != "denied" {
		t.Errorf("the tool's outcome = %v, want denied", result.Fields["outcome"])
	}
}

// A client that never answers a permission ask defers it, and the
// chain's fallback decides.
func TestPermissionAskNeverAnswered(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{AskBudget: 100 * time.Millisecond})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	theAsk(t, h)

	settled := h.event("run.settled")
	if settled.Reason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	// The fallback denied the call: the chain was exhausted.
	result := h.event("tool.result")
	if result.Fields["outcome"] != "denied" {
		t.Errorf("the tool's outcome = %v, want denied", result.Fields["outcome"])
	}
}

// A client that did not declare the permission_answers capability is
// never asked.
func TestPermissionAskNeedsTheCapability(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{AskBudget: 100 * time.Millisecond})
	if _, rerr := h.call(protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersions: []int{protocol.Version},
		Client:           protocol.Client{Name: "test", Version: "test"},
	}); rerr != nil {
		t.Fatalf("the handshake failed: %v", rerr)
	}

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	select {
	case req := <-h.asks:
		t.Fatalf("the server asked the client: %s", req.Method)
	case <-time.After(500 * time.Millisecond):
	}
	// The fallback denied the call, because nobody answered.
	h.event("tool.result")
}

// The inspect pages through the events.
func TestInspectPagesTheEvents(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	h.event("run.settled")

	result, rerr := h.call(protocol.MethodAgentInspect, protocol.InspectParams{SessionID: sessionID})
	if rerr != nil {
		t.Fatalf("the inspect was refused: %v", rerr)
	}
	var inspected protocol.InspectResult
	if err := json.Unmarshal(result, &inspected); err != nil {
		t.Fatalf("decoding the inspect result: %v", err)
	}
	if len(inspected.Events) < 3 {
		t.Fatalf("the inspect returned %d events, want the run's own", len(inspected.Events))
	}
	if inspected.NextSeq != inspected.Events[len(inspected.Events)-1].Seq {
		t.Errorf("next_seq = %d, want the last event's %d", inspected.NextSeq, inspected.Events[len(inspected.Events)-1].Seq)
	}
	if inspected.State != "settled" {
		t.Errorf("the state = %q, want settled", inspected.State)
	}

	// The page after the first event holds the rest.
	after := inspected.Events[0].Seq
	result, rerr = h.call(protocol.MethodAgentInspect, protocol.InspectParams{SessionID: sessionID, AfterSeq: after})
	if rerr != nil {
		t.Fatalf("the second inspect was refused: %v", rerr)
	}
	var second protocol.InspectResult
	if err := json.Unmarshal(result, &second); err != nil {
		t.Fatalf("decoding the second inspect result: %v", err)
	}
	if len(second.Events) != len(inspected.Events)-1 {
		t.Errorf("the second page holds %d events, want %d", len(second.Events), len(inspected.Events)-1)
	}
	for _, evt := range second.Events {
		if evt.Seq <= after {
			t.Errorf("the page holds the event %d, which is not after %d", evt.Seq, after)
		}
	}
}

// The inspect waits for a run to settle, and gives up with a timeout.
func TestInspectTimeout(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	transport.waitStarted(t)

	result, rerr = h.call(protocol.MethodAgentInspect, protocol.InspectParams{
		SessionID: sessionID,
		Wait: &protocol.InspectWait{
			Until:     "settled",
			RunID:     prompted.RunID,
			TimeoutMS: 100,
		},
	})
	if rerr != nil {
		t.Fatalf("the inspect was refused: %v", rerr)
	}
	var inspected protocol.InspectResult
	if err := json.Unmarshal(result, &inspected); err != nil {
		t.Fatalf("decoding the inspect result: %v", err)
	}
	if !inspected.TimedOut {
		t.Error("the inspect did not report the timeout")
	}
	if inspected.Settled != nil {
		t.Errorf("the inspect reported a settle: %+v", inspected.Settled)
	}
	transport.release()
}

// The inspect waits for a run to settle, and answers when it does.
func TestInspectWaitsForTheSettle(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	transport.waitStarted(t)

	answers := make(chan protocol.InspectResult, 1)
	go func() {
		raw, rerr := h.call(protocol.MethodAgentInspect, protocol.InspectParams{
			SessionID: sessionID,
			Wait: &protocol.InspectWait{
				Until:     "settled",
				RunID:     prompted.RunID,
				TimeoutMS: 10000,
			},
		})
		if rerr != nil {
			t.Errorf("the inspect was refused: %v", rerr)
			answers <- protocol.InspectResult{}
			return
		}
		var inspected protocol.InspectResult
		if err := json.Unmarshal(raw, &inspected); err != nil {
			t.Errorf("decoding the inspect result: %v", err)
		}
		answers <- inspected
	}()

	// The run settles while the client waits.
	time.Sleep(100 * time.Millisecond)
	transport.release()

	select {
	case inspected := <-answers:
		if inspected.TimedOut {
			t.Error("the inspect reported a timeout")
		}
		if inspected.Settled == nil {
			t.Fatal("the inspect reported no settle")
		}
		if inspected.Settled.RunID != prompted.RunID {
			t.Errorf("the settle names the run %q, want %q", inspected.Settled.RunID, prompted.RunID)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the inspect did not answer")
	}
}

// A close ends the run and releases the session's lock.
func TestCloseEndsTheRun(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	transport.waitStarted(t)

	if _, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID}); rerr != nil {
		t.Fatalf("the close was refused: %v", rerr)
	}
	settled := h.event("run.settled")
	if settled.Reason != "cancelled" {
		t.Errorf("the run settled %q, want cancelled", settled.Reason)
	}
	detail, _ := settled.Fields["detail"].(map[string]any)
	if detail == nil || detail["source"] != "cancel:close" {
		t.Errorf("the settle's detail = %+v, want the source cancel:close", settled.Fields["detail"])
	}
	if got := factory.releasedSessions(); got != 1 {
		t.Errorf("the factory released %d sessions, want 1", got)
	}
	// The session is gone: a request for it is not found.
	_, rerr := h.call(protocol.MethodSessionGet, protocol.GetParams{SessionID: sessionID})
	if rerr == nil {
		t.Fatal("the closed session was still there")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataSessionNotFound {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataSessionNotFound)
	}
}

// A close that races a settling run gives the waiter the run's result.
func TestCloseRacingASettlingRun(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}

	// The client waits for the settle, and the close races it.
	answers := make(chan protocol.InspectResult, 1)
	go func() {
		raw, rerr := h.call(protocol.MethodAgentInspect, protocol.InspectParams{
			SessionID: sessionID,
			Wait: &protocol.InspectWait{
				Until:     "settled",
				RunID:     prompted.RunID,
				TimeoutMS: 10000,
			},
		})
		if rerr != nil {
			answers <- protocol.InspectResult{}
			return
		}
		var inspected protocol.InspectResult
		_ = json.Unmarshal(raw, &inspected)
		answers <- inspected
	}()
	time.Sleep(50 * time.Millisecond)
	if _, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID}); rerr != nil {
		t.Fatalf("the close was refused: %v", rerr)
	}

	select {
	case inspected := <-answers:
		if inspected.Settled == nil {
			t.Fatal("the waiter got no settle")
		}
		if inspected.Settled.Reason != "end_turn" {
			t.Errorf("the waiter got the settle %q, want the run's own end_turn", inspected.Settled.Reason)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the waiter did not answer")
	}
}

// A close that races a waiting inspect tells the waiter the session is
// closed.
func TestCloseTellsAWaitingInspect(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	transport.waitStarted(t)

	// The waiter waits for a run the session never had: the close
	// tells it the session is closed.
	answers := make(chan *protocol.Error, 1)
	go func() {
		_, rerr := h.call(protocol.MethodAgentInspect, protocol.InspectParams{
			SessionID: sessionID,
			Wait: &protocol.InspectWait{
				Until:     "settled",
				RunID:     "nope",
				TimeoutMS: 10000,
			},
		})
		answers <- rerr
	}()
	time.Sleep(100 * time.Millisecond)
	if _, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID}); rerr != nil {
		t.Fatalf("the close was refused: %v", rerr)
	}

	select {
	case rerr := <-answers:
		if rerr == nil {
			t.Fatal("the waiter was answered with a result")
		}
		if rerr.Data == nil || rerr.Data.Code != protocol.DataSessionClosed {
			t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataSessionClosed)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the waiter did not answer")
	}
}

// A cancel ends the run, and a cancel of a run that settled is
// answered too.
func TestCancelIsIdempotent(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	transport.waitStarted(t)

	if _, rerr := h.call(protocol.MethodAgentCancel, protocol.CancelParams{SessionID: sessionID, RunID: prompted.RunID}); rerr != nil {
		t.Fatalf("the cancel was refused: %v", rerr)
	}
	settled := h.event("run.settled")
	if settled.Reason != "cancelled" {
		t.Errorf("the run settled %q, want cancelled", settled.Reason)
	}
	// The run has settled, so the cancel again is answered.
	if _, rerr := h.call(protocol.MethodAgentCancel, protocol.CancelParams{SessionID: sessionID, RunID: prompted.RunID}); rerr != nil {
		t.Fatalf("the second cancel was refused: %v", rerr)
	}
	// A run the session never had is not found.
	_, rerr = h.call(protocol.MethodAgentCancel, protocol.CancelParams{SessionID: sessionID, RunID: "nope"})
	if rerr == nil {
		t.Fatal("the cancel of an unknown run was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataRunNotFound {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataRunNotFound)
	}
}

// The client's stream ending tears the server down, and the server
// reports no failure.
func TestEOFTeardownEndsTheRun(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})

	h.initialize()
	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	transport.waitStarted(t)

	h.closeInput()
	if err := h.wait(); err != nil {
		t.Errorf("the server reported %v, want no failure", err)
	}
	if got := factory.releasedSessions(); got != 1 {
		t.Errorf("the factory released %d sessions, want 1", got)
	}
}

// The client's stream ending with a pending ask tears the server down.
func TestEOFTeardownWithAPendingAsk(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
		)
	})
	h := newHarness(t, factory, wire.Bounds{AskBudget: 30 * time.Second})

	h.initialize()
	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	theAsk(t, h)

	h.closeInput()
	if err := h.wait(); err != nil {
		t.Errorf("the server reported %v, want no failure", err)
	}
	if got := factory.releasedSessions(); got != 1 {
		t.Errorf("the factory released %d sessions, want 1", got)
	}
}

// A client that stopped reading fills the queue, and the transport
// fails.
func TestQueueOverflowFailsTheTransport(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{QueueMessages: 1, QueueBytes: 1 << 20})

	h.initialize()
	sessionID := createSession(t, h)
	h.pause()

	// The client stopped reading, so the answer never arrives: the
	// server fails the transport instead.
	h.send(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	err := h.wait()
	if err == nil {
		t.Fatal("the server reported no failure, want the transport failure")
	}
	if got := factory.releasedSessions(); got != 1 {
		t.Errorf("the factory released %d sessions, want 1", got)
	}
	// The run the transport ended says so in its settle record.
	sess := factory.lastSession()
	if sess == nil {
		t.Fatal("the factory built no session")
	}
	source, reason := settleOf(sess)
	if reason != "cancelled" {
		t.Errorf("the run settled %q, want cancelled", reason)
	}
	if source != "transport_failed" {
		t.Errorf("the settle's source = %q, want transport_failed", source)
	}
}

// A client that stopped reading and left the queue room stalls the
// write, and the watchdog fails the transport.
func TestWriteStallFailsTheTransport(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{QueueMessages: 1000, WriteStall: 100 * time.Millisecond})

	h.initialize()
	sessionID := createSession(t, h)
	h.pause()

	// The client stopped reading, so the answer never arrives: the
	// server fails the transport instead.
	h.send(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	err := h.wait()
	if err == nil {
		t.Fatal("the server reported no failure, want the transport failure")
	}
	if got := factory.releasedSessions(); got != 1 {
		t.Errorf("the factory released %d sessions, want 1", got)
	}
}

// The history pages backwards through the projection.
func TestHistoryPagesBackwards(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	h.event("run.settled")

	result, rerr := h.call(protocol.MethodSessionHistory, protocol.HistoryParams{SessionID: sessionID})
	if rerr != nil {
		t.Fatalf("the history was refused: %v", rerr)
	}
	var page protocol.HistoryResult
	if err := json.Unmarshal(result, &page); err != nil {
		t.Fatalf("decoding the history: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("the history is empty")
	}
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i].Seq <= page.Items[i-1].Seq {
			t.Fatalf("the history is not in order: %d after %d", page.Items[i].Seq, page.Items[i-1].Seq)
		}
	}
	// A page before the first item holds the older ones.
	result, rerr = h.call(protocol.MethodSessionHistory, protocol.HistoryParams{
		SessionID: sessionID,
		BeforeSeq: page.Items[0].Seq,
	})
	if rerr != nil {
		t.Fatalf("the second page was refused: %v", rerr)
	}
	var older protocol.HistoryResult
	if err := json.Unmarshal(result, &older); err != nil {
		t.Fatalf("decoding the second page: %v", err)
	}
	if len(older.Items) != 0 {
		t.Errorf("the older page holds %d items, want none", len(older.Items))
	}
}

// The session's state is reported.
func TestSessionGetReportsTheState(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	result, rerr := h.call(protocol.MethodSessionGet, protocol.GetParams{SessionID: sessionID})
	if rerr != nil {
		t.Fatalf("the get was refused: %v", rerr)
	}
	var got protocol.GetResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("decoding the result: %v", err)
	}
	if got.State != "idle" {
		t.Errorf("the state = %q, want idle", got.State)
	}
	if got.Broken {
		t.Error("the session is broken")
	}
	if got.Prompt.Preview != true {
		t.Error("the prompt should be a preview before the first run")
	}
	if got.Prompt.Text == "" {
		t.Error("the prompt holds no text")
	}
}

// settleOf returns the source and the reason of the run that settled
// last.
func settleOf(sess *poo.Session) (string, string) {
	var source, reason string
	for _, evt := range sess.Events() {
		if evt.Name != poo.EventRunSettled {
			continue
		}
		reason, _ = evt.Payload["reason"].(string)
		if detail, ok := evt.Payload["detail"].(poo.SettleDetail); ok {
			source = detail.Source
		}
	}
	return source, reason
}

// createSession starts a session and returns its id.
func createSession(t *testing.T, h *harness) string {
	t.Helper()
	result, rerr := h.call(protocol.MethodSessionCreate, protocol.CreateParams{
		WorkingDir: h.factory.dir,
	})
	if rerr != nil {
		t.Fatalf("the session could not be created: %v", rerr)
	}
	var info protocol.SessionInfo
	if err := json.Unmarshal(result, &info); err != nil {
		t.Fatalf("decoding the session info: %v", err)
	}
	if info.SessionID == "" {
		t.Fatal("the session has no id")
	}
	return info.SessionID
}

// permissionFactory returns a factory whose policy asks about every
// side-effecting call.
func permissionFactory(t *testing.T, transport func() poo.ChatTransport) *testFactory {
	t.Helper()
	factory := newTestFactory(t, transport)
	factory.unmatched = perm.ActionAsk
	return factory
}

// theAsk reads the permission ask the server sent.
func theAsk(t *testing.T, h *harness) protocol.AskParams {
	t.Helper()
	req := h.ask()
	if req.Method != protocol.MethodPermissionAsk {
		t.Fatalf("the server sent a %q request, want %q", req.Method, protocol.MethodPermissionAsk)
	}
	var params protocol.AskParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decoding the ask: %v", err)
	}
	if params.AskID == "" {
		t.Fatal("the ask has no id")
	}
	return params
}

// blockingTransport holds every request until the test releases it.
type blockingTransport struct {
	// gate ends the wait.
	gate chan struct{}
	// started is signalled when a request arrives.
	started chan struct{}
	// mu guards the count.
	mu sync.Mutex
	// count is how many requests arrived.
	count int
}

// newBlockingTransport returns a transport that holds every request.
func newBlockingTransport() *blockingTransport {
	return &blockingTransport{
		gate:    make(chan struct{}),
		started: make(chan struct{}, 8),
	}
}

// Stream holds the request until the test releases it.
func (b *blockingTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	b.mu.Lock()
	b.count++
	b.mu.Unlock()
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.gate:
		return nil, poo.Rendered{}, errors.New("the test released the transport")
	case <-ctx.Done():
		return nil, poo.Rendered{}, ctx.Err()
	}
}

// Fork is not used by the tests.
func (b *blockingTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errors.New("the test transport does not fork")
}

// waitStarted waits until a request reached the transport.
func (b *blockingTransport) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the request never reached the transport")
	}
}

// release lets the held requests go.
func (b *blockingTransport) release() {
	close(b.gate)
}

// A session the store does not hold is not found.
func TestLoadOfAnUnknownSession(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	_, rerr := h.call(protocol.MethodSessionLoad, protocol.LoadParams{SessionID: "nope"})
	if rerr == nil {
		t.Fatal("the load of an unknown session was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataSessionNotFound {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataSessionNotFound)
	}
}

// A session another process holds is locked.
func TestLoadOfALockedSession(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	// The session is open, so its lock is held: a second load of it is
	// refused.
	_, rerr := h.call(protocol.MethodSessionLoad, protocol.LoadParams{SessionID: sessionID})
	if rerr == nil {
		t.Fatal("the load of a locked session was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataSessionLocked {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataSessionLocked)
	}
}

// A mode this process does not serve is refused.
func TestCreateInAnUnsupportedMode(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	_, rerr := h.call(protocol.MethodSessionCreate, protocol.CreateParams{
		WorkingDir: factory.dir,
		Mode:       "durable",
	})
	if rerr == nil {
		t.Fatal("the create in an unsupported mode was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataUnsupportedMode {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataUnsupportedMode)
	}
}

// A model the settings do not name is refused.
func TestConfigureWithAnUnknownModel(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	_, rerr := h.call(protocol.MethodSessionConfigure, protocol.ConfigureParams{
		SessionID: sessionID,
		Models:    &protocol.ModelRoles{Chat: "no-such-model"},
	})
	if rerr == nil {
		t.Fatal("the configure with an unknown model was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataUnknownModel {
		t.Errorf("the data code = %+v, want %q", rerr.Data, protocol.DataUnknownModel)
	}
}

// The server leaves no goroutine behind.
func TestNoGoroutineIsLeftBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()
	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	h.event("run.settled")
	if _, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID}); rerr != nil {
		t.Fatalf("the close was refused: %v", rerr)
	}
	h.closeInput()
	if err := h.wait(); err != nil {
		t.Errorf("the server reported %v, want no failure", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the server left %d goroutines behind", runtime.NumGoroutine()-before)
}
