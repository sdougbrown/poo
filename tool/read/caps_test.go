package read_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/read"
)

// TestALineTruncationCountsCharacters covers the 2000-character line
// cap: it counts characters, and a cut never splits a rune.
func TestALineTruncationCountsCharacters(t *testing.T) {
	dir := t.TempDir()
	// 1999 characters, then a three-byte rune that straddles the cap.
	writeFile(t, dir, "a.txt", strings.Repeat("a", 1999)+"€tail\n")

	res := call(t, read.New(), map[string]any{"path": "a.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	body := strings.SplitN(res.Text, "\n", 2)[1]
	if !utf8.ValidString(body) {
		t.Errorf("the body is not valid UTF-8: %q", body)
	}
	// The cut is at the cap in characters, so the euro sign is whole.
	if !strings.Contains(body, "€") {
		t.Errorf("the body lost the rune at the cap: %q", body[max(0, len(body)-20):])
	}
	if got := utf8.RuneCountInString(strings.TrimSuffix(body, "\n")); got != read.MaxLineChars {
		t.Errorf("the line holds %d characters, want %d", got, read.MaxLineChars)
	}
}

// TestALineLongerThanTheScannerCapIsReadable covers a minified file with
// one enormous line: the read must not fail.
func TestALineLongerThanTheScannerCapIsReadable(t *testing.T) {
	dir := t.TempDir()
	// 17 MiB on one line, past the scanner's token ceiling.
	line := strings.Repeat("x", 17<<20)
	if err := os.WriteFile(filepath.Join(dir, "min.js"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	res := call(t, read.New(), map[string]any{"path": "min.js"}, dir)
	if res.IsError {
		t.Fatalf("the read of a huge line failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	if !strings.Contains(lines[0], "lines 1–1 of 1") {
		t.Errorf("the header is %q, want the file's one line", lines[0])
	}
	// The line is truncated at the cap, not the whole 17 MiB.
	if got := utf8.RuneCountInString(strings.TrimSuffix(lines[1], "\n")); got != read.MaxLineChars {
		t.Errorf("the body holds %d characters, want %d", got, read.MaxLineChars)
	}
}

// TestAnEmptyFileIsNamed covers a file with no lines at all.
func TestAnEmptyFileIsNamed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "empty.txt", "")

	res := call(t, read.New(), map[string]any{"path": "empty.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	header := strings.SplitN(res.Text, "\n", 2)[0]
	if !strings.Contains(header, "of 0") {
		t.Errorf("the header is %q, want it to name the total", header)
	}
	if strings.Contains(header, "1–0") {
		t.Errorf("the header is %q, want it to name an empty range", header)
	}
}

// TestAFirstLineBiggerThanTheCapIsStillShown covers a read whose first
// line does not fit under the cap: the model has to see part of it, or
// it can never make progress.
func TestAFirstLineBiggerThanTheCapIsStillShown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "big.txt", strings.Repeat("x", 2000)+"\nsecond\n")

	tool := &read.Tool{OutputCap: 1000}
	res := call(t, tool, map[string]any{"path": "big.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	header := lines[0]
	if !strings.Contains(header, "capped") {
		t.Errorf("the header is %q, want the capped marker", header)
	}
	// The body holds the first line, truncated to fit.
	body := lines[1]
	if body == "" {
		t.Fatal("the body is empty, want the first line")
	}
	if got := utf8.RuneCountInString(strings.TrimSuffix(body, "\n")); got != 1000 {
		t.Errorf("the body holds %d characters, want the cap", got)
	}
}

// TestPrepareRejectsTrailingData covers arguments that carry something
// after the object.
func TestPrepareRejectsTrailingData(t *testing.T) {
	tool := read.New()
	for _, raw := range []string{
		`{"path":"a.txt"} garbage`,
		`{"path":"a.txt"}{"path":"b.txt"}`,
		`{"path":"a.txt"} {"path":"b.txt"}`,
	} {
		if _, err := tool.Prepare(json.RawMessage(raw), poo.PrepareEnv{}); err == nil {
			t.Errorf("Prepare accepted %s", raw)
		}
	}
}
