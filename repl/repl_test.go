package repl

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/wire/protocol"
)

// startRepl starts the repl against a fresh fake server. The script
// reads the input the person types and returns what the repl printed.
func startRepl(t *testing.T, input string) (string, *fakeServer) {
	return startReplServer(t, input, &fakeServer{t: t})
}

// startReplServer starts the repl against a server the test prepared.
func startReplServer(t *testing.T, input string, server *fakeServer) (string, *fakeServer) {
	t.Helper()
	stdin, stdout := pipeUp(t, server)

	var out bytes.Buffer
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         strings.NewReader(input),
		Out:        &out,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	code := Run(opts)
	if code != 0 {
		t.Fatalf("the repl exited %d, want 0", code)
	}
	return out.String(), server
}

// TestARunRendersTheReplyTheToolsAndTheSettle covers one prompt's
// whole ride: the deltas render as markdown, the tool call and its
// result decorate the transcript, and the settle names the outcome
// and the accounting.
func TestARunRendersTheReplyTheToolsAndTheSettle(t *testing.T) {
	out, server := startRepl(t, "say hello\n/quit\n")

	for _, want := range []string{
		"❯ say hello",
		"· thinking ·",
		"hmm",
		"Hello, world.",
		"⚙ bash",
		"file one",
		"file two",
		"end_turn",
		"2 turn(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	// The statusline names the model and the window the usage filled.
	if !strings.Contains(out, "gemma4") {
		t.Errorf("the statusline names no model:\n%s", out)
	}
	if !strings.Contains(out, "12.3k/262.1k") {
		t.Errorf("the statusline shows no context fill:\n%s", out)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.prompts) != 1 || server.prompts[0] != "say hello" {
		t.Errorf("the server got the prompts %v, want one \"say hello\"", server.prompts)
	}
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestAnAskWaitsForTheDecisionItWasGiven covers the permission ask:
// the client answers it on the wire, and the answer is what the person
// typed.
func TestAnAskWaitsForTheDecisionItWasGiven(t *testing.T) {
	out, server := startRepl(t, "rm the lot\ny\n/quit\n")

	if !strings.Contains(out, "permission") || !strings.Contains(out, "allow bash?") {
		t.Errorf("the transcript shows no ask:\n%s", out)
	}
	if !strings.Contains(out, "→ allow") {
		t.Errorf("the transcript shows no decision:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.decisions) != 1 || server.decisions[0] != protocol.DecisionAllow {
		t.Errorf("the server got the decisions %v, want one allow", server.decisions)
	}
}

// TestAnUnknownAnswerDeniesTheAsk covers the safe side of the ask:
// anything that is not a yes refuses the call.
func TestAnUnknownAnswerDeniesTheAsk(t *testing.T) {
	out, server := startRepl(t, "rm the lot\nno idea\n/quit\n")

	if !strings.Contains(out, "→ deny") {
		t.Errorf("the transcript shows no denial:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.decisions) != 1 || server.decisions[0] != protocol.DecisionDeny {
		t.Errorf("the server got the decisions %v, want one deny", server.decisions)
	}
}

// TestTheCommandsDoWhatTheirHelpSays covers the slash commands the
// session answers without the server: the help lists them, and an
// unknown one is a prompt, not a failure.
func TestTheCommandsDoWhatTheirHelpSays(t *testing.T) {
	out, _ := startRepl(t, "/help\n/quit\n")

	for _, want := range []string{"/quit", "/less", "/model", "/cancel", "/help"} {
		if !strings.Contains(out, want) {
			t.Errorf("the help lacks %q:\n%s", want, out)
		}
	}
}

// TestAnUnknownCommandIsAPromptNotAFailure covers a line the repl does
// not answer to: it goes to the session like any other prompt, and the
// failure the server makes of it reads without ending the session.
func TestAnUnknownCommandIsAPromptNotAFailure(t *testing.T) {
	out, server := startRepl(t, "/frobnicate\n/quit\n")

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.prompts) != 1 || server.prompts[0] != "/frobnicate" {
		t.Errorf("the server got the prompts %v, want one \"/frobnicate\"", server.prompts)
	}
	if strings.Contains(out, "✗") {
		t.Errorf("the unknown command failed the session:\n%s", out)
	}
}

// TestACancelKeepsTheSession covers /cancel with nothing to cancel:
// the call goes out, its refusal is the repl's to ignore, and the
// session stays open.
func TestACancelKeepsTheSession(t *testing.T) {
	out, server := startRepl(t, "/cancel\n/quit\n")

	if strings.Contains(out, "✗") {
		t.Errorf("the cancel failed the session:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.cancels != 1 {
		t.Errorf("the server got %d cancel calls, want 1", server.cancels)
	}
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestAnInputThatEndsWithoutAQuitStillExitsCleanly covers the EOF path:
// a session whose input ends ends cleanly, with no failure read.
func TestAnInputThatEndsWithoutAQuitStillExitsCleanly(t *testing.T) {
	out, _ := startRepl(t, "say hello\n")

	if strings.Contains(out, "✗") {
		t.Errorf("the end of input read as a failure:\n%s", out)
	}
}

// TestARefusedPromptFailsTheLineNotTheSession covers a prompt the
// server refused: the refusal is a failure line in the transcript, and
// the repl keeps going.
func TestARefusedPromptFailsTheLineNotTheSession(t *testing.T) {
	out, server := startReplServer(t, "say hello\n/quit\n", &fakeServer{t: t, refusePrompt: true})

	if !strings.Contains(out, "✗") || !strings.Contains(out, "busy") {
		t.Errorf("the refusal did not read in the transcript:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestARefusedCallReadsInTheTranscript covers the server's refusals:
// a method the server refused is a line in the transcript, and the
// repl keeps going.
func TestARefusedCallReadsInTheTranscript(t *testing.T) {
	out, _ := startRepl(t, "/model\n/quit\n")
	if !strings.Contains(out, "gemma4 ← · gemma4") {
		t.Errorf("the catalog did not read back:\n%s", out)
	}
}
