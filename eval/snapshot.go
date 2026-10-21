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
func newSourceGuard(tasks []Task, fixtureRoot, root string) (*sourceGuard, error) {
	g := &sourceGuard{snapshots: map[string]string{}, hashes: map[string]string{}}
	for _, t := range tasks {
		if t.Dir != "" {
			if err := g.record(t.Dir, false); err != nil {
				return nil, err
			}
		}
		if !strings.HasPrefix(t.Repo, "fixture:") {
			continue
		}
		src, err := t.FixtureDir(fixtureRoot)
		if err != nil {
			return nil, err
		}
		src = filepath.Clean(src)
		if _, done := g.snapshots[src]; done {
			continue
		}
		if err := g.record(src, false); err != nil {
			return nil, err
		}
		rel := strings.TrimPrefix(t.Repo, "fixture:")
		// The snapshot destination is checked against the work root: a
		// relative repo path cannot reach outside it. (FixtureDir has
		// already refused an escaping repo, so this is belt and braces.)
		rel = filepath.Clean(rel)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("eval: task %q: the fixture repo %q escapes the work root", t.ID, t.Repo)
		}
		dst := filepath.Join(root, "fixtures", rel)
		if err := copyTree(src, dst); err != nil {
			return nil, fmt.Errorf("eval: snapshotting fixture %s: %w", src, err)
		}
		if err := g.record(dst, true); err != nil {
			return nil, err
		}
		g.snapshots[src] = dst
	}
	return g, nil
}

func (g *sourceGuard) record(dir string, all bool) error {
	if _, ok := g.hashes[dir]; ok {
		return nil
	}
	h, err := hashTree(dir, all)
	if err != nil {
		return err
	}
	g.hashes[dir] = h
	return nil
}

// snapshot returns the snapshot of a task's fixture.
func (g *sourceGuard) snapshot(t Task, fixtureRoot string) (string, error) {
	src, err := t.FixtureDir(fixtureRoot)
	if err != nil {
		return "", err
	}
	dst, ok := g.snapshots[filepath.Clean(src)]
	if !ok {
		return "", fmt.Errorf("eval: no snapshot of fixture %s", src)
	}
	return dst, nil
}

// changed returns the directories of a task whose content differs from
// what the invocation started with: the task directory, the fixture and
// the fixture's snapshot. The names are relative to the task set's root,
// so they hold no machine's paths.
func (g *sourceGuard) changed(t Task, fixtureRoot string) []string {
	type dir struct {
		path, name string
		all        bool
	}
	setRoot := filepath.Dir(fixtureRoot)
	name := func(d string) string {
		if rel, err := filepath.Rel(setRoot, d); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
		return filepath.Base(d)
	}
	var dirs []dir
	if t.Dir != "" {
		dirs = append(dirs, dir{t.Dir, name(t.Dir), false})
	}
	if src, err := t.FixtureDir(fixtureRoot); err == nil {
		src = filepath.Clean(src)
		dirs = append(dirs, dir{src, name(src), false})
		if snap, ok := g.snapshots[src]; ok {
			dirs = append(dirs, dir{snap, "the snapshot of " + name(src), true})
		}
	}
	var out []string
	for _, d := range dirs {
		want, ok := g.hashes[d.path]
		if !ok {
			continue
		}
		if got, err := hashTree(d.path, d.all); err != nil || got != want {
			out = append(out, d.name)
		}
	}
	return out
}

// hashTree returns a content hash of a tree, over the files treeFiles
// reports.
func hashTree(dir string, all bool) (string, error) {
	walk := treeFiles
	if all {
		walk = allFiles
	}
	files, err := walk(dir)
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
