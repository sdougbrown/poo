package bash_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestTheObservationCapCountsCharacters covers a cap in characters, not
// bytes: a multi-byte rune does not eat three of them.
func TestTheObservationCapCountsCharacters(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{ObservationCap: 99, Timeout: 5 * time.Second}

	// 100 euro signs: 100 characters, 300 bytes. The cap is 99, so the
	// output must elide: the head and the tail each hold 49 characters
	// (half the cap) and the middle holds the other two.
	res := call(t, tool, "for i in $(seq 1 100); do printf '€'; done", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})

	obs := structured(t, res)
	// The elision happened: the full output is not in the observation.
	if _, ok := obs["output"]; ok {
		t.Errorf("the observation holds the plain output, want the elided form")
	}
	head, _ := obs["output_head"].(string)
	tail, _ := obs["output_tail"].(string)
	if got := utf8.RuneCountInString(head); got != 49 {
		t.Errorf("the head holds %d characters, want 49", got)
	}
	if got := utf8.RuneCountInString(tail); got != 49 {
		t.Errorf("the tail holds %d characters, want 49", got)
	}
	// The elided count is in characters, not bytes.
	if elided, _ := obs["elided_chars"].(float64); elided != 2 {
		t.Errorf("elided_chars is %v, want 2", elided)
	}
	// Every half is valid UTF-8: a cut never splits a rune.
	if !utf8.ValidString(head) || !utf8.ValidString(tail) {
		t.Errorf("the elided observation split a rune: head %q tail %q", head, tail)
	}
}

// TestTheElidedCountIsInCharacters covers a long output whose middle is
// elided: the count is in characters.
func TestTheElidedCountIsInCharacters(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{ObservationCap: 10, Timeout: 5 * time.Second}

	// 100 euro signs: 100 characters, 300 bytes.
	res := call(t, tool, "for i in $(seq 1 100); do printf '€'; done", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})

	obs := structured(t, res)
	elided, _ := obs["elided_chars"].(float64)
	if elided != 90 {
		t.Errorf("elided_chars is %v, want 90 characters", elided)
	}
}

// TestPrepareRejectsTrailingData covers arguments that carry something
// after the object: the first object is not silently the whole call.
func TestPrepareRejectsTrailingData(t *testing.T) {
	tool := bash.New()
	for _, raw := range []string{
		`{"command":"ls"} garbage`,
		`{"command":"ls"}{"command":"rm -rf /"}`,
		`{"command":"ls"} {"command":"rm -rf /"}`,
	} {
		if _, err := tool.Prepare(json.RawMessage(raw), poo.PrepareEnv{}); err == nil {
			t.Errorf("Prepare accepted %s", raw)
		}
	}
}
