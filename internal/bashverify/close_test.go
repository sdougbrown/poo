package bashverify

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestSessionCloseLeavesNoDescendant covers the plan's job table: a
// session that closes stops the jobs it owns, and nothing survives.
func TestSessionCloseLeavesNoDescendant(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := bash.New(bash.WithExecutor(c.build(t)))
			tr := pootest.NewTransport(
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "bash",
					Arguments: json.RawMessage(`{"command":"` + jobCommand + `","background":true}`),
				}}},
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "bash",
					Arguments: json.RawMessage(`{"command":"sleep 30"}`),
				}}},
			)
			sink := pootest.NewSink()
			s, err := poo.New(poo.Options{
				Transport:  tr,
				Tools:      map[string]poo.Tool{"bash": tool},
				ToolOrder:  []string{"bash"},
				Sink:       sink,
				SessionID:  "s1",
				Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
				WorkingDir: dir,
				Env:        os.Environ(),
			})
			if err != nil {
				t.Fatalf("building the session: %v", err)
			}
			if _, err := s.Prompt(context.Background(), "start the long job"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			waitFor(t, "the job to start", func() bool { return resultTexts(sink, "started job j1") > 0 })
			waitFor(t, "the job's process to start", func() bool { return c.alive(t, dir) })

			// The session closes with the job still running.
			if err := s.Close(); err != nil {
				t.Fatalf("closing the session: %v", err)
			}
			waitFor(t, "the job's process to die", func() bool { return !c.alive(t, dir) })
			if survivors := c.survivors(t, dir); len(survivors) > 0 {
				t.Errorf("the closed session left %v behind", survivors)
			}
		})
	}
}
