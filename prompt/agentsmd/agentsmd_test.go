package agentsmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sdougbrown/poo/prompt/agentsmd"
)

// The loader reads one file from one directory: AGENTS.md, else
// CLAUDE.md, and never a parent's file.

// writeFile writes a file in a directory and returns its path.
func writeFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// TestTheAgentsFileWins covers the precedence: AGENTS.md is read when
// the directory holds one, whatever CLAUDE.md says.
func TestTheAgentsFileWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "the agents file")
	writeFile(t, dir, "CLAUDE.md", "the claude file")

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != "the agents file" {
		t.Errorf("the text is %q, want the agents file", got)
	}
}

// TestTheClaudeFileIsReadWhenThereIsNoAgentsFile covers the second
// name of the lookup.
func TestTheClaudeFileIsReadWhenThereIsNoAgentsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CLAUDE.md", "the claude file")

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != "the claude file" {
		t.Errorf("the text is %q, want the claude file", got)
	}
}

// TestADirectoryWithNoFileHoldsNoInstructions covers the case that
// there is nothing to load: the text is empty and there is no error.
func TestADirectoryWithNoFileHoldsNoInstructions(t *testing.T) {
	dir := t.TempDir()

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != "" {
		t.Errorf("the text is %q, want it empty", got)
	}
}

// TestTheParentIsNotWalked covers the rule that the loader reads the
// directory it is given and never a parent's file.
func TestTheParentIsNotWalked(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, parent, "AGENTS.md", "the parent's file")
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("making the child directory: %v", err)
	}

	got, err := agentsmd.Load(child)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != "" {
		t.Errorf("the text is %q, want it empty", got)
	}
}

// TestTheFileAtTheCapIsNotCut covers the cap at its limit: a file of
// exactly the cap is shown whole, and nothing says it was cut.
func TestTheFileAtTheCapIsNotCut(t *testing.T) {
	dir := t.TempDir()
	text := strings.Repeat("a", agentsmd.MaxBytes)
	writeFile(t, dir, "AGENTS.md", text)

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != text {
		t.Errorf("the text is %d bytes, want the file's %d bytes", len(got), len(text))
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("the text says it was cut: %q", got[len(got)-80:])
	}
}

// TestTheFileJustUnderTheCapIsNotCut covers the cap from below.
func TestTheFileJustUnderTheCapIsNotCut(t *testing.T) {
	dir := t.TempDir()
	text := strings.Repeat("a", agentsmd.MaxBytes-1)
	writeFile(t, dir, "AGENTS.md", text)

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != text {
		t.Errorf("the text is %d bytes, want the file's %d bytes", len(got), len(text))
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("the text says it was cut: %q", got[len(got)-80:])
	}
}

// TestTheFileOverTheCapIsCutWithANotice covers the cap from above: the
// text is cut and the notice says so.
func TestTheFileOverTheCapIsCutWithANotice(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", strings.Repeat("a", agentsmd.MaxBytes+100))

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if !strings.Contains(got, "[AGENTS.md truncated: only the first 65536 bytes are shown]") {
		t.Errorf("the text holds no notice: %q", got[len(got)-80:])
	}
	shown, _, _ := strings.Cut(got, "\n\n[AGENTS.md truncated")
	if len(shown) != agentsmd.MaxBytes {
		t.Errorf("the text shows %d bytes, want %d", len(shown), agentsmd.MaxBytes)
	}
}

// TestTheCutNeverSplitsARune covers the rule that the cut is at a
// rune boundary: the text stays valid UTF-8 and holds no half rune.
func TestTheCutNeverSplitsARune(t *testing.T) {
	dir := t.TempDir()
	// The last byte of the cap falls in the middle of the three-byte
	// rune that starts one byte before it.
	text := strings.Repeat("a", agentsmd.MaxBytes-1) + "€" + strings.Repeat("b", 100)
	writeFile(t, dir, "AGENTS.md", text)

	got, err := agentsmd.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if !utf8.ValidString(got) {
		t.Error("the text is not valid UTF-8")
	}
	if strings.ContainsRune(got, '€') {
		t.Error("the text holds the rune the cut fell in")
	}
	shown, _, _ := strings.Cut(got, "\n\n[AGENTS.md truncated")
	if len(shown) != agentsmd.MaxBytes-1 {
		t.Errorf("the text shows %d bytes, want %d", len(shown), agentsmd.MaxBytes-1)
	}
	if !strings.HasPrefix(text, shown) {
		t.Error("the text shown is not the head of the file")
	}
}

// TestAPathThatIsNotAFileIsAnError covers the case that the name the
// loader found is a directory: the file cannot be read, and the
// caller is told so.
func TestAPathThatIsNotAFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755); err != nil {
		t.Fatalf("making the directory: %v", err)
	}

	_, err := agentsmd.Load(dir)
	if err == nil {
		t.Fatal("loading the directory succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "AGENTS.md") {
		t.Errorf("the error is %q, want it to name the file", err)
	}
}

// TestAnUnreadableFileIsAnError covers the case that the file is
// there and cannot be read: the caller is told, and the loader does
// not fall through to the second name.
func TestAnUnreadableFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "AGENTS.md", "the agents file")
	writeFile(t, dir, "CLAUDE.md", "the claude file")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("changing the file's mode: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if os.Geteuid() == 0 {
		t.Skip("the root user reads every file")
	}

	_, err := agentsmd.Load(dir)
	if err == nil {
		t.Fatal("loading the directory succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "AGENTS.md") {
		t.Errorf("the error is %q, want it to name the file", err)
	}
}

// TestAMissingDirectoryHoldsNoInstructions covers the case that the
// directory itself is not there: there is nothing to load.
func TestAMissingDirectoryHoldsNoInstructions(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")

	got, err := agentsmd.Load(missing)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if got != "" {
		t.Errorf("the text is %q, want it empty", got)
	}
}
