package wire_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/session"
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

// The handshake names the default profile, and a session whose params
// name no profile runs under it.
func TestInitializeNamesTheDefaultProfile(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})

	result := h.initialize()
	if result.DefaultProfile == "" {
		t.Fatal("the handshake names no default profile")
	}
	offered := false
	for _, profile := range result.Profiles {
		if profile.Name == result.DefaultProfile {
			offered = true
			break
		}
	}
	if !offered {
		t.Errorf("the default profile %q is not one the handshake offers", result.DefaultProfile)
	}

	// The header a session writes names the profile it runs under, so
	// it is the record of what session/create fell back to.
	raw, rerr := h.call(protocol.MethodSessionCreate, protocol.CreateParams{WorkingDir: factory.dir})
	if rerr != nil {
		t.Fatalf("the session was refused: %v", rerr)
	}
	var info protocol.SessionInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatalf("decoding the session: %v", err)
	}
	meta, err := session.MetadataOf(factory.dir, info.SessionID)
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	if meta.Profile != result.DefaultProfile {
		t.Errorf("the session runs under the profile %q, want the handshake's %q", meta.Profile, result.DefaultProfile)
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
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{
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

// The settled event carries the run's usage total.
func TestTheSettledEventCarriesTheUsageTotal(t *testing.T) {
	cached := 5
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{
			Text: "hello", Finish: "stop",
			InputTokens: 12, OutputTokens: 3, CacheReadTokens: &cached,
		})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	settled := h.event("run.settled")
	b, err := json.Marshal(settled.Fields["detail"])
	if err != nil {
		t.Fatalf("marshaling the detail: %v", err)
	}
	var detail protocol.SettleDetail
	if err := json.Unmarshal(b, &detail); err != nil {
		t.Fatalf("decoding the detail: %v", err)
	}
	if detail.Usage == nil {
		t.Fatal("the settled event carries no usage total")
	}
	if detail.Usage.InputTokens != 12 || detail.Usage.OutputTokens != 3 || detail.Usage.CacheReadTokens != 5 {
		t.Errorf("the usage total is %+v, want 12/3 with 5 cache-read", detail.Usage)
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	transport.waitStarted(t)

	_, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "again"})
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
	_, rerr := h.call(protocol.MethodRunMessage, protocol.MessageParams{SessionID: sessionID, Text: "hi"})
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
// never asked, and it reads one warning with its session that says why
// its asks fall back.
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

	// The session's warning names the cause and the capability that
	// fixes it.
	sessionInfo := createdSessionInfo(t, h)
	sessionID := sessionInfo.SessionID
	if len(sessionInfo.Warnings) != 1 {
		t.Fatalf("the session carried %d warnings (%v), want one", len(sessionInfo.Warnings), sessionInfo.Warnings)
	}
	if !strings.Contains(sessionInfo.Warnings[0], "permission_answers") {
		t.Errorf("the warning is %q, want it to name the capability", sessionInfo.Warnings[0])
	}
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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

// TestTheSessionIsSilentWhenTheClientDeclaredTheCapability covers the
// other side: a client that declared the capability reads no warning
// with its session.
func TestTheSessionIsSilentWhenTheClientDeclaredTheCapability(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	info := createdSessionInfo(t, h)
	if len(info.Warnings) != 0 {
		t.Errorf("the session carried %d warnings (%v), want none", len(info.Warnings), info.Warnings)
	}
}

// createdSessionInfo creates a session and returns what the client
// learned about it.
func createdSessionInfo(t *testing.T, h *harness) protocol.SessionInfo {
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
	return info
}

// A client that answers a permission ask with a message has the
// message shown to the model and kept in the answered record.
func TestPermissionAskDeniedWithAMessage(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	ask := theAsk(t, h)
	h.answerWith(&protocol.Request{ID: json.RawMessage(`"` + ask.AskID + `"`)}, map[string]any{
		"decision": protocol.DecisionDeny,
		"message":  "not while I am watching",
	})

	settled := h.event("run.settled")
	if settled.Reason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	result := h.event("tool.result")
	if result.Fields["outcome"] != "denied" {
		t.Errorf("the tool's outcome = %v, want denied", result.Fields["outcome"])
	}
	if text, _ := result.Fields["text"].(string); !strings.Contains(text, "not while I am watching") {
		t.Errorf("the denied result says %q, want the client's message", text)
	}
	answered := h.event("permission.answered")
	if answered.Fields["message"] != "not while I am watching" {
		t.Errorf("the answer's message = %v, want the client's", answered.Fields["message"])
	}
	// The record in the log keeps the message too.
	line := theAnsweredLine(t, factory, sessionID)
	if !strings.Contains(line, `"message":"not while I am watching"`) {
		t.Errorf("the answered record says %s, want the client's message", line)
	}
}

// A client that answers an allowance with a message has the tool run,
// and the message kept in the answered record: a message longer than
// the bound is cut at a rune boundary, in the event and in the record.
func TestPermissionAskAllowedWithAMessage(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	ask := theAsk(t, h)
	message := "mind the working tree" + strings.Repeat("🌱", 2500)
	bounded := "mind the working tree" + strings.Repeat("🌱", 1979)
	h.answerWith(&protocol.Request{ID: json.RawMessage(`"` + ask.AskID + `"`)}, map[string]any{
		"decision": protocol.DecisionAllow,
		"message":  message,
	})

	settled := h.event("run.settled")
	if settled.Reason != "end_turn" {
		t.Errorf("the run settled %q, want end_turn", settled.Reason)
	}
	result := h.event("tool.result")
	if result.Fields["outcome"] != "ok" {
		t.Errorf("the tool's outcome = %v, want ok", result.Fields["outcome"])
	}
	answered := h.event("permission.answered")
	if answered.Fields["message"] != bounded {
		t.Errorf("the answer's message = %v, want the bounded message", answered.Fields["message"])
	}
	// The record in the log keeps the bounded message too.
	line := theAnsweredLine(t, factory, sessionID)
	if !strings.Contains(line, `"message":"`+bounded+`"`) {
		t.Errorf("the answered record says %s, want the bounded message", line)
	}
	if strings.Contains(line, `"message":"`+message+`"`) {
		t.Errorf("the answered record carries the whole message, want the bound: %s", line)
	}
}

// A client that answers without a message writes the records the older
// core wrote: the log line carries no message field.
func TestPermissionAskWithoutAMessageKeepsTheRecords(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	ask := theAsk(t, h)
	h.answer(&protocol.Request{ID: json.RawMessage(`"` + ask.AskID + `"`)}, protocol.DecisionDeny)

	h.event("run.settled")
	answered := h.event("permission.answered")
	if _, ok := answered.Fields["message"]; ok {
		t.Errorf("the answer's event carries a message: %v", answered.Fields)
	}
	line := theAnsweredLine(t, factory, sessionID)
	if strings.Contains(line, `"message"`) {
		t.Errorf("the answered record carries a message field: %s", line)
	}
}

// A message longer than the bound is cut at the bound, at a rune
// boundary: the model sees a bounded note, whatever the client sent.
func TestPermissionAskMessageIsBounded(t *testing.T) {
	factory := permissionFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	ask := theAsk(t, h)
	message := strings.Repeat("x", 1999) + strings.Repeat("🎯", 500)
	h.answerWith(&protocol.Request{ID: json.RawMessage(`"` + ask.AskID + `"`)}, map[string]any{
		"decision": protocol.DecisionDeny,
		"message":  message,
	})

	h.event("run.settled")
	result := h.event("tool.result")
	text, _ := result.Fields["text"].(string)
	want := strings.Repeat("x", 1999) + "🎯"
	if !strings.Contains(text, want) {
		t.Error("the denied result does not carry the bounded message")
	}
	if strings.Contains(text, message) {
		t.Error("the denied result carries the whole message, want the bound")
	}
}

// theAnsweredLine returns the permission.answered line of a session's
// log.
func theAnsweredLine(t *testing.T, factory *testFactory, sessionID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(factory.dir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("reading the session log: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"permission.answered"`) {
			return line
		}
	}
	t.Fatal("the log holds no answered record")
	return ""
}

// The inspect pages through the events.
func TestInspectPagesTheEvents(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	h.event("run.settled")
	// The settled event reaches the client before the run retires: the
	// state flips to settled later, in wakeWaiters. Wait for the state
	// before reading it back.
	h.awaitState(t, sessionID, "settled")

	result, rerr := h.call(protocol.MethodRunInspect, protocol.InspectParams{SessionID: sessionID})
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
	result, rerr = h.call(protocol.MethodRunInspect, protocol.InspectParams{SessionID: sessionID, AfterSeq: after})
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
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	transport.waitStarted(t)

	result, rerr = h.call(protocol.MethodRunInspect, protocol.InspectParams{
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
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
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
		raw, rerr := h.call(protocol.MethodRunInspect, protocol.InspectParams{
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
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
		raw, rerr := h.call(protocol.MethodRunInspect, protocol.InspectParams{
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
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
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
		_, rerr := h.call(protocol.MethodRunInspect, protocol.InspectParams{
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
	result, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
	if rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	var prompted protocol.PromptResult
	if err := json.Unmarshal(result, &prompted); err != nil {
		t.Fatalf("decoding the prompt result: %v", err)
	}
	transport.waitStarted(t)

	if _, rerr := h.call(protocol.MethodRunCancel, protocol.CancelParams{SessionID: sessionID, RunID: prompted.RunID}); rerr != nil {
		t.Fatalf("the cancel was refused: %v", rerr)
	}
	settled := h.event("run.settled")
	if settled.Reason != "cancelled" {
		t.Errorf("the run settled %q, want cancelled", settled.Reason)
	}
	// The run has settled, so the cancel again is answered.
	if _, rerr := h.call(protocol.MethodRunCancel, protocol.CancelParams{SessionID: sessionID, RunID: prompted.RunID}); rerr != nil {
		t.Fatalf("the second cancel was refused: %v", rerr)
	}
	// A run the session never had is not found.
	_, rerr = h.call(protocol.MethodRunCancel, protocol.CancelParams{SessionID: sessionID, RunID: "nope"})
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	h.send(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
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
	h.send(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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

// The history carries the call's summary, which is what a transcript shows.
func TestTheHistoryCarriesAToolCallsSummary(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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
	for _, item := range page.Items {
		if item.Type != "tool_call" {
			continue
		}
		if item.Summary != "the test tool" {
			t.Errorf("the tool call's summary is %q, want the tool's summary", item.Summary)
		}
		return
	}
	t.Fatal("the history holds no tool call")
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
	if got.Prompt.Preview {
		t.Error("the prompt is rendered when the session is built, so it is no preview")
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

// A prompt file that does not render refuses the session, and no request
// reaches the endpoint.
func TestCreateWithAPromptThatDoesNotRender(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"nested call", `{{define "part"}}{{.Model}}{{end}}{{template "part" .Model}}`},
		{"builtin result", `{{(index .Tools 0).Bad}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := prompt.Load(fstest.MapFS{"base.md": {Data: []byte(tc.text)}}, prompt.Sources{"base": nil})
			if err != nil {
				t.Fatalf("loading the prompt file: %v", err)
			}
			transport := pootest.NewTransport()
			factory := newTestFactory(t, func() poo.ChatTransport { return transport })
			factory.prompt = []func(*prompt.Builder){dir.Apply}
			h := newHarness(t, factory, wire.Bounds{})
			h.initialize()

			_, rerr := h.call(protocol.MethodSessionCreate, protocol.CreateParams{WorkingDir: factory.dir})
			if rerr == nil {
				t.Fatal("the create with a prompt that does not render was answered")
			}
			if rerr.Data == nil || rerr.Data.Code != protocol.DataInvalidSettings {
				t.Fatalf("the data code = %+v, want %q", rerr.Data, protocol.DataInvalidSettings)
			}
			if !strings.Contains(rerr.Data.Detail, "base.md") {
				t.Errorf("the failure %q does not name the file", rerr.Data.Detail)
			}
			if got := len(transport.Requests()); got != 0 {
				t.Errorf("the endpoint got %d requests, want none", got)
			}
		})
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
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
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

// TestUncodableResultAnswersAnError covers a result the server cannot
// encode: the request is still answered, with the failure, so the
// client does not wait for an answer that never comes.
func TestUncodableResultAnswersAnError(t *testing.T) {
	factory := newTestFactory(t, nil)
	factory.settings = func() (map[string]any, error) {
		return map[string]any{"broken": make(chan int)}, nil
	}
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	_, rerr := h.call(protocol.MethodSettingsGet, nil)
	if rerr == nil {
		t.Fatal("the server answered the settings it could not encode with a result")
	}
	if rerr.Code != protocol.CodeInternalError || rerr.Data == nil || rerr.Data.Code != protocol.DataInternalError {
		t.Errorf("the failure is %d/%s, want %d/%s", rerr.Code, rerr.Data, protocol.CodeInternalError, protocol.DataInternalError)
	}
}

// TestACloseRacingAPromptLeavesNoRun proves the close ordering over
// the wire: a session/close that races an run/prompt either refuses
// the prompt or finalizes its run, so the session has no run after the
// close returns.
func TestACloseRacingAPromptLeavesNoRun(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	for i := 0; i < 50; i++ {
		sessionID := createSession(t, h)
		prompt := h.callAsync(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
		closeAnswer := h.callAsync(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID})
		<-prompt
		<-closeAnswer
		sess := factory.lastSession()
		if sess == nil {
			t.Fatal("the factory built no session")
		}
		if sess.Running() {
			t.Fatalf("iteration %d: the close returned and the session still runs", i)
		}
		if sess.Broken() {
			t.Fatalf("iteration %d: the session is broken after the close", i)
		}
	}
}

// TestAnExplicitNullFieldIsInvalidParams covers the contract's rule:
// an explicit null is invalid, at every depth the shapes define.
func TestAnExplicitNullFieldIsInvalidParams(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	for _, raw := range []string{
		`{"session_id":null}`,
		`{"working_dir":".","models":null}`,
		`{"working_dir":".","models":{"chat":null}}`,
		`{"working_dir":".","prompt_overrides":{"sections":{"house":null}}}`,
	} {
		method := protocol.MethodSessionGet
		if raw != `{"session_id":null}` {
			method = protocol.MethodSessionCreate
		}
		_, rerr := h.callRaw(method, raw)
		if rerr == nil {
			t.Errorf("%s accepted %s", method, raw)
			continue
		}
		if rerr.Data == nil || rerr.Data.Code != protocol.DataInvalidParams {
			t.Errorf("%s refused %s with %q, want %q", method, raw, dataCode(rerr), protocol.DataInvalidParams)
		}
	}
}

// TestARequestWithAnotherJSONRPCVersionIsInvalid covers the line's
// jsonrpc field: the contract's requests say "2.0".
func TestARequestWithAnotherJSONRPCVersionIsInvalid(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	h.mu.Lock()
	id := "wrong-version"
	ch := make(chan *protocol.Response, 1)
	h.pending[id] = ch
	h.mu.Unlock()
	h.write(map[string]any{"jsonrpc": "1.0", "id": id, "method": protocol.MethodSettingsGet})
	resp := <-ch
	if resp.Error == nil {
		t.Fatal("the request with jsonrpc 1.0 was served")
	}
	if resp.Error.Data == nil || resp.Error.Data.Code != protocol.DataInvalidRequest {
		t.Errorf("the data code = %q, want %q", dataCode(resp.Error), protocol.DataInvalidRequest)
	}
}

// TestModelsListReturnsTheCatalog covers models/list: the effective
// catalog is what the factory offers.
func TestModelsListReturnsTheCatalog(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	raw, rerr := h.call(protocol.MethodModelsList, protocol.ModelsParams{})
	if rerr != nil {
		t.Fatalf("models/list was refused: %v", rerr)
	}
	var result protocol.ModelsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decoding the models: %v", err)
	}
	if len(result.Models) != 1 || result.Models[0].Name != "test-model" {
		t.Errorf("the models are %+v, want the factory's own", result.Models)
	}
}

// TestConfigureDuringARunIsBusy covers session/configure: a session
// that is running refuses the change.
func TestConfigureDuringARunIsBusy(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	transport.waitStarted(t)

	_, rerr := h.call(protocol.MethodSessionConfigure, protocol.ConfigureParams{SessionID: sessionID})
	if rerr == nil {
		t.Fatal("the configure during a run was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataBusy {
		t.Errorf("the data code = %q, want %q", dataCode(rerr), protocol.DataBusy)
	}
	transport.release()
}

// TestCloseReportsAFlushError covers session/close: the flush failure
// reaches the client instead of a successful empty result.
func TestCloseReportsAFlushError(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	factory.flushErr = errors.New("the flush failed")
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	_, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID})
	if rerr == nil {
		t.Fatal("the close whose flush failed was answered with a result")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataStoreError {
		t.Errorf("the data code = %q, want %q", dataCode(rerr), protocol.DataStoreError)
	}
}

// dataCode returns the data code an error carries.
func dataCode(rerr *protocol.Error) string {
	if rerr == nil || rerr.Data == nil {
		return ""
	}
	return rerr.Data.Code
}

// TestLoadWithAnInvalidSessionIDIsInvalidParams covers the id rule at
// the wire: a client cannot make the server read outside the session
// directory.
func TestLoadWithAnInvalidSessionIDIsInvalidParams(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	for _, id := range []string{"../x", "a/b", "..", `a\b`} {
		_, rerr := h.call(protocol.MethodSessionLoad, protocol.LoadParams{SessionID: id})
		if rerr == nil {
			t.Errorf("the load of %q was answered", id)
			continue
		}
		if rerr.Data == nil || rerr.Data.Code != protocol.DataInvalidParams {
			t.Errorf("the load of %q failed with %q, want %q", id, dataCode(rerr), protocol.DataInvalidParams)
		}
	}
}

// TestTheInspectPageHoldsItsByteBudget covers the event page's byte
// budget: a page holds the events that fit and says that more exist,
// the pages together cover every event in order, and no page carries
// more than the budget's bytes — except the page that holds one event
// over the budget, which still comes back, so a client paging forward
// always moves.
func TestTheInspectPageHoldsItsByteBudget(t *testing.T) {
	big := strings.Repeat("x", 3000)
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: big, Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{EventPageBytes: 1024})
	h.initialize()

	sessionID := createSession(t, h)
	for i := 0; i < 6; i++ {
		if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
			t.Fatalf("the prompt was refused: %v", rerr)
		}
		h.event("run.settled")
		h.awaitState(t, sessionID, "settled")
	}

	seen := 0
	pages := 0
	oversized := false
	after := int64(0)
	for {
		pages++
		if pages > 100 {
			t.Fatal("the pages did not end")
		}
		result, rerr := h.call(protocol.MethodRunInspect, protocol.InspectParams{SessionID: sessionID, AfterSeq: after})
		if rerr != nil {
			t.Fatalf("the inspect was refused: %v", rerr)
		}
		var inspected protocol.InspectResult
		if err := json.Unmarshal(result, &inspected); err != nil {
			t.Fatalf("decoding the inspect result: %v", err)
		}
		// The event's own data is spread flat into the wire object, so
		// the size that meets the budget is the raw object's, not a
		// re-marshaled struct's: Payload does not decode.
		var raw struct {
			Events []json.RawMessage `json:"events"`
		}
		if err := json.Unmarshal(result, &raw); err != nil {
			t.Fatalf("decoding the raw inspect result: %v", err)
		}
		if len(inspected.Events) == 0 {
			t.Fatal("the page holds no events")
		}
		bytes := 0
		for i, evt := range inspected.Events {
			if evt.Seq != int64(seen+1) {
				t.Fatalf("the page holds the event %d, want %d", evt.Seq, seen+1)
			}
			seen++
			bytes += len(raw.Events[i]) + 1
		}
		if bytes > 1024 && len(inspected.Events) != 1 {
			t.Errorf("the page carries %d bytes of events over the budget, want the oversize alone", bytes)
		}
		if len(inspected.Events) == 1 && bytes > 1024 {
			oversized = true
		}
		if inspected.NextSeq != inspected.Events[len(inspected.Events)-1].Seq {
			t.Errorf("next_seq = %d, want the last event's %d", inspected.NextSeq, inspected.Events[len(inspected.Events)-1].Seq)
		}
		if !inspected.HasMore {
			break
		}
		after = inspected.NextSeq
	}
	if pages < 3 {
		t.Errorf("the run came back in %d pages, want more: the budget cut nothing", pages)
	}
	if !oversized {
		t.Error("no page holds one event over the budget, want the oversized event to come back alone")
	}
}

// TestThePageBudgetFollowsTheQueueBytes covers the clamp: a deployment
// that lowers the queue's byte budget lowers the page budgets with it,
// so a page is never a line the queue refuses.
func TestThePageBudgetFollowsTheQueueBytes(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: strings.Repeat("x", 3000), Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{QueueBytes: 4096})
	h.initialize()

	sessionID := createSession(t, h)
	for i := 0; i < 4; i++ {
		if _, rerr := h.call(protocol.MethodRunPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
			t.Fatalf("the prompt was refused: %v", rerr)
		}
		h.event("run.settled")
		h.awaitState(t, sessionID, "settled")
	}

	seen := 0
	pages := 0
	after := int64(0)
	for {
		pages++
		if pages > 100 {
			t.Fatal("the pages did not end")
		}
		result, rerr := h.call(protocol.MethodRunInspect, protocol.InspectParams{SessionID: sessionID, AfterSeq: after})
		if rerr != nil {
			t.Fatalf("the inspect was refused: %v", rerr)
		}
		var inspected protocol.InspectResult
		if err := json.Unmarshal(result, &inspected); err != nil {
			t.Fatalf("decoding the inspect result: %v", err)
		}
		var raw struct {
			Events []json.RawMessage `json:"events"`
		}
		if err := json.Unmarshal(result, &raw); err != nil {
			t.Fatalf("decoding the raw inspect result: %v", err)
		}
		if len(inspected.Events) == 0 {
			t.Fatal("the page holds no events")
		}
		bytes := 0
		first := 0
		for i, evt := range inspected.Events {
			if evt.Seq != int64(seen+1) {
				t.Fatalf("the page holds the event %d, want %d", evt.Seq, seen+1)
			}
			seen++
			if i == 0 {
				first = len(raw.Events[i]) + 1
			}
			bytes += len(raw.Events[i]) + 1
		}
		// The events are far over the budget, so every page holds one of
		// them alone: the clamp keeps the page at the queue's half.
		if bytes-first > 2048 {
			t.Errorf("the page carries %d bytes of events past its first, over the queue's half", bytes-first)
		}
		if !inspected.HasMore {
			break
		}
		after = inspected.NextSeq
	}
	if pages < 2 {
		t.Errorf("the run came back in %d pages, want more: the budget cut nothing", pages)
	}
}
