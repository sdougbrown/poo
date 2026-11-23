package poo_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// A registered CLI takes the background path the way a bash command
// does: a call with background true returns at once with a started
// outcome, the CLI runs as a job, and its result arrives as a message.

// sayManifest writes a one-CLI registry whose say CLI appends to the
// marker and greets, and loads it.
func sayManifest(t *testing.T, marker string) *cliregistry.Catalog {
	t.Helper()
	say := filepath.Join(t.TempDir(), "say")
	script := "#!/bin/sh\necho ran >> " + marker + "\necho hello \"$1\"\n"
	if err := os.WriteFile(say, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the CLI: %v", err)
	}
	body, err := json.Marshal(map[string]any{"version": 1, "tools": map[string]any{
		"say": map[string]any{
			"description": "Say hello to a name",
			"command":     []string{say},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"name": map[string]any{"type": "string"}},
				"additionalProperties": false,
			},
			"bindings": []any{
				map[string]any{"property": "name", "style": "positional"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	catalog, err := cliregistry.Load(dir)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	return catalog
}

// callCliCall is a call of the say CLI through call_tool.
func callCliCall(background bool) poo.ToolCall {
	args := map[string]any{"name": "say", "args": map[string]any{"name": "ada"}}
	if background {
		args["background"] = true
	}
	raw, _ := json.Marshal(args)
	return poo.ToolCall{CallID: "call_say", Name: "call_tool", Arguments: raw}
}

// TestACallCliBackgroundCallBecomesAJob covers the rule: the model asks
// for a registered CLI to run in the background, the call returns at
// once with a started outcome, and the CLI runs.
func TestACallCliBackgroundCallBecomesAJob(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{callCliCall(true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	catalog := sayManifest(t, marker)
	tools := map[string]poo.Tool{"call_tool": cliregistry.NewCall(catalog)}
	opts := poo.Options{
		Transport:  tr,
		Tools:      tools,
		ToolOrder:  []string{"call_tool"},
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "say hello off the critical path"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}

	// The call's own result is the started one.
	found := false
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Outcome == poo.OutcomeStarted {
			found = true
			if !strings.Contains(r.Text, "[started job j1 · call_tool;") {
				t.Errorf("the started result is %q, want the started text", r.Text)
			}
		}
	}
	if !found {
		t.Error("the log holds no started result")
	}

	// The CLI runs, its result arrives, and the completion names the
	// CLI's output.
	waitFor(t, "the CLI to run", func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && strings.Contains(string(data), "ran")
	})
	var end *poo.JobEndedRecord
	waitFor(t, "the job's completed result", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.JobEndedRecord); ok && strings.Contains(r.Text, "hello ada") {
				end = r
				return true
			}
		}
		return false
	})
	if end.Outcome != poo.OutcomeOK {
		t.Errorf("the job ended %q, want ok", end.Outcome)
	}
}
