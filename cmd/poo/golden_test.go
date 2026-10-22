package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestThePromptIsGoldenForEachProfile covers the golden prompts: the
// prompt of each profile is the one the golden file holds.
//
// The prompt is what the model is told, so a change in it is a change
// in what the model does. The goldens are the profiles' prompts, and
// the search profile's prompt says nothing about bash: a read-only
// build's prompt never mentions the tool it does not link.
func TestThePromptIsGoldenForEachProfile(t *testing.T) {
	for _, name := range []string{"base", "code", "search"} {
		t.Run(name, func(t *testing.T) {
			got := promptFor(t, name)
			golden := filepath.Join("testdata", "golden", "prompt-"+name+".txt")
			if os.Getenv("POO_GOLDEN") != "" {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatalf("creating the golden directory: %v", err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("writing the golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading the golden: %v", err)
			}
			if got != string(want) {
				t.Errorf("the prompt is not the golden one:\n%s", diff(got, string(want)))
			}
		})
	}
}

// TestTheSearchPromptHasNoBashGuidance covers the omission thesis: the
// read-only profile's prompt says nothing about the shell.
func TestTheSearchPromptHasNoBashGuidance(t *testing.T) {
	got := promptFor(t, "search")
	for _, needle := range []string{"Bash:", "fresh shell", "background"} {
		if strings.Contains(got, needle) {
			t.Errorf("the search prompt mentions %q", needle)
		}
	}
	if !strings.Contains(got, "Read:") {
		t.Error("the search prompt does not mention the read tool")
	}
}

// promptFor returns the prompt of a profile.
func promptFor(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	body := "version: 1\nendpoints:\n  test:\n    base_url: http://localhost:4000/v1\nmodels:\n  test-model:\n    endpoint: test\n    model_id: test-model\n    api: chat_completions\n    tools_mode: native\n    ops: [chat]\n    context_limit: 32768\nroles:\n  chat: test-model\n"
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	var out bytes.Buffer
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = out.ReadFrom(r)
		close(done)
	}()
	code := run([]string{"prompt", "--profile", name, "--settings", settings})
	w.Close()
	<-done
	os.Stdout = old
	r.Close()
	if code != 0 {
		t.Fatalf("poo prompt exited with %d", code)
	}
	return out.String()
}

// diff returns the lines the two texts differ on.
func diff(got, want string) string {
	var b strings.Builder
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			b.WriteString("- " + g + "\n")
			b.WriteString("+ " + w + "\n")
		}
	}
	return b.String()
}
