//go:build linux

package repl

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

type backgroundFactory struct {
	wire.Factory
	dir       string
	transport *pootest.Transport
	model     poo.ModelInfo
	sink      *pootest.Sink
}

func (f backgroundFactory) Profiles() []protocol.ProfileInfo    { return nil }
func (f backgroundFactory) DefaultProfile() string              { return "test" }
func (f backgroundFactory) Capabilities() protocol.Capabilities { return protocol.Capabilities{} }
func (f backgroundFactory) Roles() protocol.ModelRoles {
	return protocol.ModelRoles{Chat: f.model.ModelID}
}
func (f backgroundFactory) Models(context.Context, bool) ([]protocol.ModelInfo, error) {
	return nil, nil
}
func (f backgroundFactory) Create(_ context.Context, _ protocol.CreateParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
	s, err := poo.New(poo.Options{
		SessionID: "handoff", WorkingDir: f.dir, Transport: f.transport, Model: f.model, Sink: f.sink,
		Tools: map[string]poo.Tool{"bash": bash.New()}, Observers: []poo.Observer{env.Observer},
	})
	return s, &wire.Info{Mode: "ephemeral", Models: f.Roles()}, err
}

func TestBackgroundMovesTheSameProcessAcrossRunCancellation(t *testing.T) {
	dir := t.TempDir()
	sink := pootest.NewSink()
	transport := pootest.NewTransportFromScript(pootest.Script{Turns: []pootest.Turn{
		{Calls: []pootest.Call{{Name: "bash", Arguments: []byte(`{"description":"background probe","command":"echo $$ > started; while [[ ! -f release ]]; do /bin/sleep 0.02; done; echo process-$$"}`)}}},
		{Text: "waiting for the result"},
		{Text: "job output received"},
	}})
	term, code := startTerminalClient(t, func([]string) (io.WriteCloser, io.ReadCloser, func() error, error) {
		serverIn, clientOut := io.Pipe()
		clientIn, serverOut := io.Pipe()
		done := make(chan error, 1)
		go func() {
			defer serverOut.Close()
			defer serverIn.Close()
			done <- wire.Serve(wire.Options{In: serverIn, Out: serverOut, Factory: backgroundFactory{
				dir: dir, transport: transport, model: poo.ModelInfo{ModelID: "test", ContextLimit: 32768}, sink: sink,
			}})
		}()
		return clientOut, clientIn, func() error { return <-done }, nil
	})
	waitFor(t, term, promptMark)
	term.press("run probe\r")
	waitFor(t, term, "background probe")
	var pid string
	for end := time.Now().Add(5 * time.Second); pid == ""; {
		if data, err := os.ReadFile(filepath.Join(dir, "started")); err == nil && strings.HasSuffix(string(data), "\n") {
			pid = strings.TrimSpace(string(data))
		} else if time.Now().After(end) {
			t.Fatal("Bash did not write its PID")
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	term.press("/bg\r")
	waitFor(t, term, "started job j1")
	for end := time.Now().Add(5 * time.Second); len(transport.Requests()) < 2; {
		if time.Now().After(end) {
			t.Fatal("the foreground run did not continue after the handoff")
		}
		time.Sleep(10 * time.Millisecond)
	}
	term.press("\x1b")
	waitFor(t, term, "✓ cancelled")
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Check delivery to the model even when the REPL does not render the wake.
	deadline := time.Now().Add(5 * time.Second)
	for {
		requests := transport.Requests()
		if len(requests) >= 3 {
			var receipts int
			for _, msg := range requests[2].Messages {
				receipts += strings.Count(msg.Text(), "process-"+pid)
			}
			if receipts != 1 {
				t.Fatalf("model received the original process output %d times, want once", receipts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the job did not wake the session with its result")
		}
		time.Sleep(10 * time.Millisecond)
	}
	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("REPL exited %d", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the REPL did not exit")
	}
	var starts, receipts, ends int
	for _, rec := range sink.Records() {
		switch r := rec.(type) {
		case *poo.JobStartedRecord:
			if r.JobID == "j1" && r.Handle == "r1" {
				starts++
			}
		case *poo.ToolResultRecord:
			if r.Handle == "r1" {
				if r.Outcome != poo.OutcomeStarted || starts != 1 {
					t.Fatalf("the foreground got %q before job admission", r.Outcome)
				}
				receipts++
			}
		case *poo.JobEndedRecord:
			if r.JobID == "j1" && r.Handle == "r1" {
				if r.Outcome != poo.OutcomeOK || receipts != 1 {
					t.Fatalf("the job ended with %q before its foreground receipt", r.Outcome)
				}
				ends++
			}
		}
	}
	if starts != 1 || receipts != 1 || ends != 1 {
		t.Fatalf("job starts = %d, foreground receipts = %d, job ends = %d; want one each", starts, receipts, ends)
	}
}
