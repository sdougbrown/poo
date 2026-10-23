package wire_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// TestACloseRacingAPromptLeavesNoRun proves the close ordering over
// the wire: a session/close that races an agent/prompt either refuses
// the prompt or finalizes its run, so the session has no run after the
// close returns.
func TestACloseRacingAPromptLeavesNoRun(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport {
		return pootest.NewTransport(pootest.Turn{Text: "hello", Finish: "stop"})
	})
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	for i := 0; i < 300; i++ {
		sessionID := createSession(t, h)
		prompt := h.callAsync(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"})
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
