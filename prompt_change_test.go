package poo_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/eval"
	"github.com/sdougbrown/poo/prompt"
)

// The prompt of a session may be changed between its runs.

// TestThePromptChangeIsInTheNextRun covers the change a consumer
// makes: the prompt a run carries is rendered at the start of the run,
// so a change made between the runs is in the next run's prompt and not
// in the first one's.
func TestThePromptChangeIsInTheNextRun(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}, {text: "the second answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = ""
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if err := s.SetPrompt(func(b *prompt.Builder) {
		b.Set("house", "the house style")
	}); err != nil {
		t.Fatalf("changing the prompt: %v", err)
	}
	runSession(t, s, "do the other thing")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	first := systemPromptOf(t, reqs[0])
	second := systemPromptOf(t, reqs[1])
	if strings.Contains(first, "the house style") {
		t.Errorf("the first run's prompt is %q, want the change to come after it", first)
	}
	if !strings.Contains(second, "the house style") {
		t.Errorf("the second run's prompt is %q, want the change in it", second)
	}
}

// TestAPromptReplacementDoesNotReachTheNextRequest covers the rule
// for a run-local prompt: a replacement stays in its own run's request,
// while the earlier run's system message stays in the record.
func TestAPromptReplacementDoesNotReachTheNextRequest(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}, {text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	var n int
	opts.BeforeRun = []poo.BeforeRunHook{beforeRunFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
		n++
		text := "only instructions for run 1"
		if n == 2 {
			text = "only instructions for run 2"
		}
		return poo.StartDecision{SystemPrompt: func(s prompt.Sections) { s.Replace(text) }}, nil
	})}
	s := newTestSession(t, opts)
	runSession(t, s, "one")
	runSession(t, s, "two")

	var systems []string
	for _, m := range tr.Requests()[1].Messages {
		if m.Role == poo.RoleSystem {
			systems = append(systems, m.Text())
		}
	}
	if len(systems) != 1 || systems[0] != "only instructions for run 2" {
		t.Errorf("the second request's system instructions are %q, want only run 2", systems)
	}
}

// TestReplayUsesTheRecordedPrompt covers the rule: replay uses the
// recorded system prompt and never re-renders it, so a prompt change
// between the recording and the replay does not change the replay.
func TestReplayUsesTheRecordedPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)}}}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SystemPrompt = "x"
	opts.Model.ContextLimit = 530
	opts.MaxTurns = 1
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	if len(tr.Requests()) != 1 {
		t.Fatal("the original run did not make its request")
	}
	var data []byte
	for _, rec := range sink.allRecords() {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	path := filepath.Join(t.TempDir(), "recording.jsonl")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := eval.Replay(path, eval.ReplayOptions{Context: context.Background(), ContextLimit: 530})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Match {
		t.Errorf("the replay of a one-request recorded prompt diverged: %v", result.Differences)
	}
}
