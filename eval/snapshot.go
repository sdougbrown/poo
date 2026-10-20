package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// sourceGuard holds what an invocation knows about its inputs.
//
// A run copies its workdir from a snapshot of the fixture taken before the
// first run, so a model that edits the repository's fixture cannot change
// the start of a later run. The hashes taken at the same moment let the
// harness tell that a run edited the source anyway.
type sourceGuard struct {
	// snapshots maps a fixture's source directory to its snapshot.
	snapshots map[string]string
	// hashes maps a source directory to its content hash at the start.
	hashes map[string]string
}

// newSourceGuard snapshots the fixtures the tasks use under root and
// hashes the fixtures and the task directories they come from.
func newSourceGuard(tasks []Task, taskRoot, root string) (*sourceGuard, error) {
	g := &sourceGuard{snapshots: map[string]string{}, hashes: map[string]string{}}
	for _, t := range tasks {
		if t.Dir != "" {
			if err := g.record(t.Dir); err != nil {
				return nil, err
			}
		}
		if !strings.HasPrefix(t.Repo, "fixture:") {
			continue
		}
		src, err := t.FixtureDir(taskRoot)
		if err != nil {
			return nil, err
		}
		src = filepath.Clean(src)
		if _, done := g.snapshots[src]; done {
			continue
		}
		if err := g.record(src); err != nil {
			return nil, err
		}
		dst := filepath.Join(root, "fixtures", strings.TrimPrefix(t.Repo, "fixture:"))
		if err := copyTree(src, dst); err != nil {
			return nil, fmt.Errorf("eval: snapshotting fixture %s: %w", src, err)
		}
		g.snapshots[src] = dst
	}
	return g, nil
}

func (g *sourceGuard) record(dir string) error {
	if _, ok := g.hashes[dir]; ok {
		return nil
	}
	h, err := hashTree(dir)
	if err != nil {
		return err
	}
	g.hashes[dir] = h
	return nil
}

// snapshot returns the snapshot of a task's fixture.
func (g *sourceGuard) snapshot(t Task, taskRoot string) (string, error) {
	src, err := t.FixtureDir(taskRoot)
	if err != nil {
		return "", err
	}
	dst, ok := g.snapshots[filepath.Clean(src)]
	if !ok {
		return "", fmt.Errorf("eval: no snapshot of fixture %s", src)
	}
	return dst, nil
}

// changed returns the source directories of a task whose content differs
// from what the invocation started with.
func (g *sourceGuard) changed(t Task, taskRoot string) []string {
	dirs := []string{t.Dir}
	if src, err := t.FixtureDir(taskRoot); err == nil {
		dirs = append(dirs, filepath.Clean(src))
	}
	var out []string
	for _, d := range dirs {
		want, ok := g.hashes[d]
		if !ok {
			continue
		}
		if got, err := hashTree(d); err != nil || got != want {
			out = append(out, d)
		}
	}
	return out
}

// hashTree returns a content hash of a tree, over the files treeFiles
// reports.
func hashTree(dir string) (string, error) {
	files, err := treeFiles(dir)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%s\n", p, files[p])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SourceChanged returns the number of runs invalidated because the task
// set's source changed while they ran.
func (r *Report) SourceChanged() int {
	n := 0
	for _, res := range r.Results {
		if res.SourceChanged {
			n++
		}
	}
	return n
}
