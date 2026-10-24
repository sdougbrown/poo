package poo_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestTheDefaultCodePromptIsTheRecordedOne covers the jobs section's
// bytes in a real request: the section carries the newline its own text
// ends with, so the blank line before the environment is the one the
// recorded prompt has.
func TestTheDefaultCodePromptIsTheRecordedOne(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "done"})
	s, err := poo.New(poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"job": pootest.NewJobTool("job", poo.BackgroundAllowed)},
		ToolOrder:  []string{"job"},
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: "/fixed/work",
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()
	runID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.WaitRun(context.Background(), runID); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(requests))
	}
	got := systemPromptOf(t, requests[0])

	wantBytes, err := os.ReadFile(filepath.Join("testdata", "systemprompt", "jobs-code.txt"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	date := regexp.MustCompile(`(?m)^- Date: .*$`)
	want := date.ReplaceAllString(string(wantBytes), "- Date: <DATE>")
	got = date.ReplaceAllString(got, "- Date: <DATE>")
	if got != want {
		t.Errorf("the prompt is not the recorded one:\nfirst difference at byte %d\n got %q\nwant %q",
			firstDifference(got, want), excerptAt(got, firstDifference(got, want)), excerptAt(want, firstDifference(got, want)))
	}
}

// firstDifference returns the index of the first byte where two strings
// differ, or the shorter string's length when one is a prefix.
func firstDifference(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// excerptAt returns a short window around a byte offset.
func excerptAt(text string, at int) string {
	const window = 80
	start := max(0, at-window)
	end := min(len(text), at+window)
	return text[start:end]
}
