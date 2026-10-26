package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// copyTree copies a directory tree.
//
// The tree's own .git directory is skipped: the runner measures the
// working dir against a pristine copy of itself, and a checked-in .git would
// only add noise to the diff.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			// A symlink or a device file in a fixture is not something
			// the runner copies.
			return nil
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

// copyFile copies one file, making the target's directory as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// treeFiles returns the regular files of a tree, keyed by their relative
// path.
//
// The .git directory is skipped, and so are the paths a tool generates:
// the bytecode a python run leaves behind and the cache a test run
// leaves behind are not the model's changes. The paths are sorted.
func treeFiles(dir string) (map[string]string, error) {
	return walkFiles(dir, false)
}

// allFiles is treeFiles without the generated files: it hashes every
// regular file copyTree would copy.
func allFiles(dir string) (map[string]string, error) {
	return walkFiles(dir, true)
}

func walkFiles(dir string, all bool) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || (!all && generatedDir(d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !all && generatedFile(rel) {
			return nil
		}
		hash, err := hashFile(path)
		if err != nil {
			return err
		}
		files[rel] = hash
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("eval: reading %s: %w", dir, err)
	}
	return files, nil
}

// generatedDir reports whether a directory holds files a tool generates.
//
// The bytecode cache and the test cache are not the model's changes, and
// a read-only task's check rejects any change at all.
func generatedDir(name string) bool {
	switch name {
	case "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache":
		return true
	}
	return false
}

// generatedFile reports whether a path names a file a tool generates.
//
// The check is on the whole relative path, so a bytecode file inside a
// package's cache is caught as well as the cache directory itself.
func generatedFile(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts {
		if generatedDir(p) {
			return true
		}
	}
	base := parts[len(parts)-1]
	return strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".pyo")
}

// restoreTests puts the fixture's own tests back into the working dir.
//
// A coding task's check runs the fixture's tests, and a model that
// rewrote them would make the check pass without fixing anything. The
// pristine copy is the one the runner took before the run, so the
// check always runs the tests the task shipped with.
//
// The tests directory is only restored when there is one: a task with
// no tests has nothing to restore.
func restoreTests(pristine, workdir string) error {
	src := filepath.Join(pristine, "tests")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil
	}
	dst := filepath.Join(workdir, "tests")
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("eval: clearing %s: %w", dst, err)
	}
	return copyTree(src, dst)
}

// measureDiff reports what a run changed, comparing the working dir against a
// pristine copy of it.
//
// The comparison does not need a repository: a fixture is a plain
// directory, and the runner has to work on one.
//
// The scratch the diff needs lives under the run's own work root, never
// in /tmp's shared space.
func measureDiff(workdir, pristine string) (Diff, string, error) {
	newFiles, err := treeFiles(workdir)
	if err != nil {
		return Diff{}, "", err
	}
	oldFiles, err := treeFiles(pristine)
	if err != nil {
		return Diff{}, "", err
	}

	var changed []string
	for path, hash := range newFiles {
		if old, ok := oldFiles[path]; !ok || old != hash {
			changed = append(changed, path)
		}
	}
	for path := range oldFiles {
		if _, ok := newFiles[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)

	lines := 0
	var out strings.Builder
	for _, rel := range changed {
		text, count, err := diffOne(workdir, pristine, rel)
		if err != nil {
			return Diff{Files: changed}, out.String(), err
		}
		lines += count
		out.WriteString(text)
	}
	return Diff{Files: changed, Lines: lines}, out.String(), nil
}

// diffOne returns the unified diff of one file and its changed-line
// count.
func diffOne(workdir, pristine, rel string) (string, int, error) {
	oldPath := filepath.Join(pristine, rel)
	newPath := filepath.Join(workdir, rel)

	oldData, oldErr := os.ReadFile(oldPath)
	newData, newErr := os.ReadFile(newPath)
	oldExists := oldErr == nil
	newExists := newErr == nil
	if oldErr != nil && !os.IsNotExist(oldErr) {
		return "", 0, fmt.Errorf("eval: reading %s: %w", oldPath, oldErr)
	}
	if newErr != nil && !os.IsNotExist(newErr) {
		return "", 0, fmt.Errorf("eval: reading %s: %w", newPath, newErr)
	}

	switch {
	case oldExists && !newExists:
		return fmt.Sprintf("--- a/%s\n+++ /dev/null\n@@ deleted @@\n", rel), countLines(oldData), nil
	case !oldExists && newExists:
		return fmt.Sprintf("--- /dev/null\n+++ b/%s\n@@ added @@\n", rel), countLines(newData), nil
	case !oldExists && !newExists:
		return "", 0, nil
	}
	return diffBytesIn(filepath.Dir(pristine), rel, oldData, newData)
}

// diffBytesIn returns the unified diff of two files and the number of
// changed lines.
//
// The scratch the diff needs is made under the given base directory, so
// it lives with the run's own scratch and never in /tmp.
func diffBytesIn(base, rel string, oldData, newData []byte) (string, int, error) {
	dir, err := os.MkdirTemp(base, "poo-eval-diff")
	if err != nil {
		return "", 0, fmt.Errorf("eval: making a diff directory: %w", err)
	}
	defer os.RemoveAll(dir)

	oldFile := filepath.Join(dir, "old")
	newFile := filepath.Join(dir, "new")
	if err := os.WriteFile(oldFile, oldData, 0o644); err != nil {
		return "", 0, fmt.Errorf("eval: writing the diff input: %w", err)
	}
	if err := os.WriteFile(newFile, newData, 0o644); err != nil {
		return "", 0, fmt.Errorf("eval: writing the diff input: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), "diff", "-u", "--label", "a/"+rel, "--label", "b/"+rel, oldFile, newFile)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	// diff exits 1 when the files differ, which is the normal case.
	_ = cmd.Run()

	text := out.String()
	return text, countChangedLines(text), nil
}

// countChangedLines counts the added and removed lines of a unified diff.
func countChangedLines(diff string) int {
	count := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			count++
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			count++
		}
	}
	return count
}

// countLines counts the lines of a file's content.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte("\n"))
	if !bytes.HasSuffix(data, []byte("\n")) {
		n++
	}
	return n
}

// hashFile returns the hex SHA-256 of a file's content.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("eval: opening %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("eval: reading %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
