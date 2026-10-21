package read_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/read"
)

// call runs a read through the tool and returns the result.
func call(t *testing.T, tool *read.Tool, args map[string]any, dir string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	return res
}

// writeFile writes a file and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestTheHeaderNamesTheVisibleRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\nfour\n")

	res := call(t, read.New(), map[string]any{"path": "a.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	if !strings.HasPrefix(lines[0], "[a.txt · lines 1–4 of 4]") {
		t.Errorf("the header is %q, want it to name the range", lines[0])
	}
	if !strings.HasSuffix(lines[0], "]") {
		t.Errorf("the header %q does not end the bracket", lines[0])
	}
}

func TestTheOffsetAndLimitChooseTheRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\nfour\nfive\n")

	res := call(t, read.New(), map[string]any{
		"path":   "a.txt",
		"offset": 2,
		"limit":  2,
	}, dir)

	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	if !strings.HasPrefix(lines[0], "[a.txt · lines 2–3 of 5]") {
		t.Errorf("the header is %q, want lines 2–3", lines[0])
	}
	if lines[1] != "two\nthree\n" {
		t.Errorf("the body is %q, want the second and third lines", lines[1])
	}
}

func TestTheDefaultLimitIsTwoThousandLines(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 2500; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	writeFile(t, dir, "a.txt", b.String())

	res := call(t, read.New(), map[string]any{"path": "a.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	if !strings.HasPrefix(lines[0], "[a.txt · lines 1–2000 of 2500]") {
		t.Errorf("the header is %q, want the default 2000-line limit", lines[0])
	}
}

func TestALongLineIsTruncatedAtTwoThousandChars(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", strings.Repeat("x", 5000)+"\n")

	res := call(t, read.New(), map[string]any{"path": "a.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	body := lines[1]
	if len(body) != read.MaxLineChars+1 {
		t.Errorf("the body is %d chars, want %d plus the newline", len(body), read.MaxLineChars)
	}
}

func TestTheOutputCapEndsTheReadAndTheHeaderSaysSo(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&b, "line-%04d-%s\n", i, strings.Repeat("y", 100))
	}
	writeFile(t, dir, "a.txt", b.String())

	// A cap of 1000 chars ends the read well before the file does.
	tool := &read.Tool{OutputCap: 1000}
	res := call(t, tool, map[string]any{"path": "a.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	if !strings.HasSuffix(lines[0], " · capped]") {
		t.Errorf("the header is %q, want the capped marker", lines[0])
	}
	if !strings.HasPrefix(lines[0], "[a.txt · lines 1–") {
		t.Errorf("the header is %q, want it to name the range", lines[0])
	}
	if !strings.Contains(lines[0], "of 500") {
		t.Errorf("the header is %q, want it to name the total", lines[0])
	}
	// The body holds the lines that fit under the cap.
	if len(lines[1]) > 1000 {
		t.Errorf("the body is %d chars, want at most the 1000-char cap", len(lines[1]))
	}
}

func TestABinaryFileIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	content := append([]byte("abc"), 0x00)
	content = append(content, []byte("defghij")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	res := call(t, read.New(), map[string]any{"path": "bin.dat"}, dir)
	if !res.IsError {
		t.Fatalf("the read of a binary file succeeded: %s", res.Text)
	}
	if !strings.Contains(res.Text, "binary") {
		t.Errorf("the error is %q, want it to say the file is binary", res.Text)
	}
	// The error names the file size.
	if !strings.Contains(res.Text, fmt.Sprintf("%d bytes", len(content))) {
		t.Errorf("the error is %q, want it to name the file size", res.Text)
	}
}

func TestAMissingFileIsARecoverableError(t *testing.T) {
	dir := t.TempDir()
	res := call(t, read.New(), map[string]any{"path": "nope.txt"}, dir)
	if !res.IsError {
		t.Fatalf("the read of a missing file succeeded: %s", res.Text)
	}
}

func TestPrepareRejectsBadArguments(t *testing.T) {
	tool := read.New()
	for _, raw := range []string{
		`{"path":"a.txt","extra":1}`,
		`{"path":""}`,
		`{"path":"a.txt","offset":-1}`,
		`{"path":"a.txt","limit":-1}`,
		`not json`,
	} {
		if _, err := tool.Prepare(json.RawMessage(raw), poo.PrepareEnv{}); err == nil {
			t.Errorf("Prepare accepted %s", raw)
		}
	}
}

// TestARangePastTheEndOfTheFileIsEmpty covers a read whose offset is
// beyond the file: the body is empty, and the header names the total
// rather than claiming a range that does not exist.
func TestARangePastTheEndOfTheFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\ntwo\n")

	res := call(t, read.New(), map[string]any{
		"path":   "a.txt",
		"offset": 50,
	}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	if !strings.Contains(lines[0], "of 2") {
		t.Errorf("the header is %q, want it to name the total", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("the body is %q, want it empty", lines[1])
	}
	// The range is empty, so the header says so rather than naming a
	// line that does not exist.
	if strings.Contains(lines[0], "50–49") {
		t.Errorf("the header is %q, want it to name an empty range", lines[0])
	}
}

func TestTheOutputCapIsContextRelative(t *testing.T) {
	// The plan: min(70000, floor(0.10 × context_limit × 3.5)).
	if got := read.OutputCap(32768); got != 11468 {
		t.Errorf("OutputCap(32768) is %d, want 11468", got)
	}
	if got := read.OutputCap(262144); got != 70000 {
		t.Errorf("OutputCap(262144) is %d, want 70000", got)
	}
}

// TestACancelledReadIsAnErrorBeforeItScans covers the context contract:
// a read whose context is already done fails without paying for the
// scan, the same way the bash tool honours its context.
func TestACancelledReadIsAnErrorBeforeItScans(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\ntwo\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	raw, err := json.Marshal(map[string]any{"path": "a.txt"})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	prepared, err := read.New().Prepare(json.RawMessage(raw), poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := read.New().Call(ctx, prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if !res.IsError {
		t.Errorf("the cancelled read is not an error: %s", res.Text)
	}
}
