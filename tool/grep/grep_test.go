package grep_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/grep"
)

// call runs a grep through the tool and returns the result.
func call(t *testing.T, tool *grep.Tool, args map[string]any, dir string) poo.ToolResult {
	t.Helper()
	return callWith(t, tool, args, dir, nil)
}

// callWith runs a grep with protected paths and returns the result.
func callWith(t *testing.T, tool *grep.Tool, args map[string]any, dir string, protected []string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	env := poo.PrepareEnv{WorkingDir: dir, ProtectedPaths: protected}
	prepared, err := tool.Prepare(raw, env)
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{PrepareEnv: env})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	return res
}

// writeFile writes a file, making the directories it needs.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// initRepo makes a directory a git work tree.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no git here: %v: %s", err, out)
	}
}

// outsideAGitWorkTree reports whether a directory is inside a work tree.
func outsideAGitWorkTree(t *testing.T, dir string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "true"
}

// TestTheIgnoreRulesHoldInsideAWorkTree covers the file selection of a
// work tree: the nested ignore rules hold, and an untracked file that
// they do not hide is searched.
func TestTheIgnoreRulesHoldInsideAWorkTree(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, dir, ".gitignore", "ignored.txt\nsub/*.log\n")
	writeFile(t, dir, "sub/.gitignore", "nested.txt\n")
	writeFile(t, dir, "found.txt", "a needle here\n")
	writeFile(t, dir, "ignored.txt", "a needle here\n")
	writeFile(t, dir, "sub/also.txt", "a needle here\n")
	writeFile(t, dir, "sub/nested.txt", "a needle here\n")
	writeFile(t, dir, "sub/out.log", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "found.txt:1:") {
		t.Errorf("the output is %q, want the untracked file", res.Text)
	}
	if !strings.Contains(res.Text, "sub/also.txt:1:") {
		t.Errorf("the output is %q, want the file in the subdirectory", res.Text)
	}
	for _, hidden := range []string{"ignored.txt", "sub/nested.txt", "sub/out.log"} {
		if strings.Contains(res.Text, hidden) {
			t.Errorf("the output holds the ignored file %s: %q", hidden, res.Text)
		}
	}
}

// TestTheDotDirectoriesAreSkippedOutsideAWorkTree covers the walk of a
// tree that is not a work tree.
func TestTheDotDirectoriesAreSkippedOutsideAWorkTree(t *testing.T) {
	dir := t.TempDir()
	if !outsideAGitWorkTree(t, dir) {
		t.Skip("the temporary directory is inside a work tree")
	}
	writeFile(t, dir, "visible.txt", "a needle here\n")
	writeFile(t, dir, ".hidden/secret.txt", "a needle here\n")
	writeFile(t, dir, ".dotfile.txt", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "visible.txt:1:") {
		t.Errorf("the output is %q, want the visible file", res.Text)
	}
	if !strings.Contains(res.Text, ".dotfile.txt:1:") {
		t.Errorf("the output is %q, want the dot file", res.Text)
	}
	if strings.Contains(res.Text, "secret.txt") {
		t.Errorf("the output holds the file under the dot directory: %q", res.Text)
	}
}

// TestNoIgnoreFileIsAppliedOutsideAWorkTree covers the walk of a tree
// that is not a work tree: an ignore file is just a file.
func TestNoIgnoreFileIsAppliedOutsideAWorkTree(t *testing.T) {
	dir := t.TempDir()
	if !outsideAGitWorkTree(t, dir) {
		t.Skip("the temporary directory is inside a work tree")
	}
	writeFile(t, dir, ".gitignore", "hidden.txt\n")
	writeFile(t, dir, "hidden.txt", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "hidden.txt:1:") {
		t.Errorf("the output is %q, want the file the ignore file names", res.Text)
	}
}

// TestTheSymlinksAreNotFollowed covers the symlinks: a link is not read.
func TestTheSymlinksAreNotFollowed(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "target.txt", "a needle here\n")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	writeFile(t, dir, "plain.txt", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "plain.txt:1:") {
		t.Errorf("the output is %q, want the plain file", res.Text)
	}
	if strings.Contains(res.Text, "link.txt") {
		t.Errorf("the output holds the symlink: %q", res.Text)
	}
}

// TestTheBinaryFilesAreSkipped covers the binary files: they are not
// searched.
func TestTheBinaryFilesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "binary.bin", "a needle here\x00 and a needle there\n")
	writeFile(t, dir, "plain.txt", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "plain.txt:1:") {
		t.Errorf("the output is %q, want the plain file", res.Text)
	}
	if strings.Contains(res.Text, "binary.bin") {
		t.Errorf("the output holds the binary file: %q", res.Text)
	}
}

// TestTheOutputIsSorted covers the order of the results.
func TestTheOutputIsSorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "c.txt", "a needle here\n")
	writeFile(t, dir, "a.txt", "a needle here\n")
	writeFile(t, dir, "b.txt", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	lines := strings.Split(res.Text, "\n")
	var files []string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		files = append(files, strings.SplitN(line, ":", 2)[0])
	}
	want := []string{"a.txt", "b.txt", "c.txt"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("the files are %v, want %v", files, want)
	}
}

// TestTheGlobFilterHolds covers the filter on the relative path.
func TestTheGlobFilterHolds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "a needle here\n")
	writeFile(t, dir, "main.txt", "a needle here\n")
	writeFile(t, dir, "cmd/main.go", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle", "glob": "*.go"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "main.go:1:") {
		t.Errorf("the output is %q, want the go file", res.Text)
	}
	if strings.Contains(res.Text, "main.txt") {
		t.Errorf("the output holds the file the filter excludes: %q", res.Text)
	}
	// The filter is matched against the relative path, and a "*" does
	// not cross a separator.
	if strings.Contains(res.Text, "cmd/main.go") {
		t.Errorf("the output holds the file in the subdirectory: %q", res.Text)
	}
}

// TestTheEmptyPatternListsTheFiles covers the listing of the files mode.
func TestTheEmptyPatternListsTheFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "nothing here\n")
	writeFile(t, dir, "b.txt", "nothing here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "", "mode": "files_with_matches"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	lines := strings.Split(res.Text, "\n")
	if !strings.Contains(lines[0], "2 files") {
		t.Errorf("the header is %q, want the count of the files", lines[0])
	}
	if !strings.Contains(res.Text, "a.txt") || !strings.Contains(res.Text, "b.txt") {
		t.Errorf("the output is %q, want the files", res.Text)
	}
}

// TestTheFilesModeShowsTheFilesThatHoldAMatch covers the files mode.
func TestTheFilesModeShowsTheFilesThatHoldAMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a needle here\n")
	writeFile(t, dir, "b.txt", "nothing here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle", "mode": "files_with_matches"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "a.txt") {
		t.Errorf("the output is %q, want the file that holds the match", res.Text)
	}
	if strings.Contains(res.Text, "b.txt") {
		t.Errorf("the output holds the file without a match: %q", res.Text)
	}
}

// TestTheCountModeCountsTheMatchesOfEachFile covers the count mode.
func TestTheCountModeCountsTheMatchesOfEachFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a needle here\nanother needle\n")
	writeFile(t, dir, "b.txt", "nothing here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle", "mode": "count"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "a.txt:2") {
		t.Errorf("the output is %q, want the count of the file", res.Text)
	}
	if strings.Contains(res.Text, "b.txt") {
		t.Errorf("the output holds the file without a match: %q", res.Text)
	}
}

// TestTheResultCapHoldsAndTheElidedCountIsReported covers the cap on the
// results.
func TestTheResultCapHoldsAndTheElidedCountIsReported(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("a needle here\n")
	}
	writeFile(t, dir, "many.txt", b.String())

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	lines := strings.Split(res.Text, "\n")
	var shown int
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "[grep") {
			continue
		}
		shown++
	}
	if shown != grep.DefaultMaxMatches {
		t.Errorf("the output holds %d results, want %d", shown, grep.DefaultMaxMatches)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "50 more matches elided") {
		t.Errorf("the last line is %q, want the elided count", last)
	}
}

// TestTheObservationCapHolds covers the cap on the characters.
func TestTheObservationCapHolds(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("a needle here, and a few more words to make the line long\n")
	}
	writeFile(t, dir, "many.txt", b.String())

	tool := &grep.Tool{ObservationCap: 500}
	res := call(t, tool, map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if len(res.Text) > 500 {
		t.Errorf("the output is %d chars, want at most 500", len(res.Text))
	}
	if !strings.Contains(res.Text, "elided") {
		t.Errorf("the output is %q, want the elision line", res.Text)
	}
}

// TestAnInvalidPatternIsAValidationError covers the pattern that does
// not compile.
func TestAnInvalidPatternIsAValidationError(t *testing.T) {
	tool := grep.New()
	_, err := tool.Prepare(json.RawMessage(`{"pattern":"a("}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the invalid pattern was accepted")
	}
	if _, ok := err.(*poo.ErrInvalidArgs); !ok {
		t.Errorf("the error is %T, want an ErrInvalidArgs", err)
	}
}

// TestAnUnknownModeIsAValidationError covers the mode that does not
// exist.
func TestAnUnknownModeIsAValidationError(t *testing.T) {
	tool := grep.New()
	if _, err := tool.Prepare(json.RawMessage(`{"pattern":"a","mode":"lines"}`), poo.PrepareEnv{}); err == nil {
		t.Fatal("the unknown mode was accepted")
	}
}

// TestTheProtectedFilesAreSkippedAndCounted covers the protected paths:
// the file is skipped, and the count is reported.
func TestTheProtectedFilesAreSkippedAndCounted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "open.txt", "a needle here\n")
	secret := writeFile(t, dir, "secret.txt", "a needle here\n")

	res := callWith(t, grep.New(), map[string]any{"pattern": "needle"}, dir, []string{secret})
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "open.txt:1:") {
		t.Errorf("the output is %q, want the file that is not protected", res.Text)
	}
	if strings.Contains(res.Text, "secret.txt") {
		t.Errorf("the output holds the protected file: %q", res.Text)
	}
	if !strings.Contains(res.Text, "1 protected files skipped") {
		t.Errorf("the output is %q, want the count of the protected files", res.Text)
	}
}

// TestTheSearchRootIsHonoured covers the path argument.
func TestTheSearchRootIsHonoured(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sub/inside.txt", "a needle here\n")
	writeFile(t, dir, "outside.txt", "a needle here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle", "path": "sub"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "inside.txt:1:") {
		t.Errorf("the output is %q, want the file under the root", res.Text)
	}
	if strings.Contains(res.Text, "outside.txt") {
		t.Errorf("the output holds the file outside the root: %q", res.Text)
	}
}

// TestAMissingRootIsAnError covers the root that is not there.
func TestAMissingRootIsAnError(t *testing.T) {
	dir := t.TempDir()
	res := call(t, grep.New(), map[string]any{"pattern": "needle", "path": "missing"}, dir)
	if !res.IsError {
		t.Fatalf("the search succeeded: %s", res.Text)
	}
}

// TestARootThatIsAFileIsAnError covers the root that is not a
// directory.
func TestARootThatIsAFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a needle here\n")
	res := call(t, grep.New(), map[string]any{"pattern": "needle", "path": "a.txt"}, dir)
	if !res.IsError {
		t.Fatalf("the search succeeded: %s", res.Text)
	}
}

// TestNoMatchesIsAnEmptyResult covers the search that finds nothing.
func TestNoMatchesIsAnEmptyResult(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "nothing here\n")

	res := call(t, grep.New(), map[string]any{"pattern": "needle"}, dir)
	if res.IsError {
		t.Fatalf("the search failed: %s", res.Text)
	}
	if !strings.Contains(res.Text, "0 matches in 0 files") {
		t.Errorf("the output is %q, want the empty result", res.Text)
	}
}
