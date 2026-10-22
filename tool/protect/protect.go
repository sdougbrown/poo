// Package protect matches the paths a run must not touch.
//
// The paths come from the operator's settings as globs. A glob is matched
// against the resolved absolute path of a file, so a symlink into a
// protected place is protected as well, and "**" crosses the directory
// separators.
//
// The package imports the standard library only, so the tools and a
// sandboxing executor can share it.
//
// The bash tool is not covered by it: with the local executor a command
// reaches anything the process can. A contained executor masks the
// protected paths instead, and the file tools keep their own check.
package protect

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Set is a set of protected-path globs.
//
// The zero set matches nothing: a run with no protected paths configured
// protects nothing.
type Set struct {
	// globs holds the patterns as they were configured, with the home
	// directory expanded.
	globs []string
	// patterns holds the patterns, split into path segments.
	patterns [][]string
}

// Compile builds a set from the configured globs.
//
// A glob is absolute, or starts with "~", which stands for the user's home
// directory. A glob that is neither is an error: a relative glob would be
// matched against an absolute path and would protect nothing, which is
// worse than a refusal.
//
// A trailing separator is normalized away, so "/etc/" and "/etc" protect
// the same paths. The root itself is kept.
//
// A malformed glob is an error too, and so is a glob whose home directory
// cannot be found.
//
// An empty glob is skipped, and a set of nothing matches nothing.
func Compile(globs []string) (*Set, error) {
	s := &Set{}
	for _, glob := range globs {
		trimmed := strings.TrimSpace(glob)
		if trimmed == "" {
			continue
		}
		expanded, err := expandHome(trimmed)
		if err != nil {
			return nil, err
		}
		expanded = filepath.Clean(expanded)
		if !filepath.IsAbs(expanded) {
			return nil, fmt.Errorf("protect: the glob %q must be absolute or start with ~", glob)
		}
		segments := splitPath(expanded)
		hasWildcard := false
		for _, segment := range segments {
			if segment == "**" {
				hasWildcard = true
				continue
			}
			if _, err := path.Match(segment, ""); err != nil {
				return nil, fmt.Errorf("protect: the glob %q is malformed: %w", glob, err)
			}
			if strings.ContainsAny(segment, "*?[") {
				hasWildcard = true
			}
		}
		// A literal path protects what it names and everything under it,
		// so "/etc" is the same setting as "/etc/**".
		if !hasWildcard {
			segments = append(segments, "**")
		}
		s.globs = append(s.globs, expanded)
		s.patterns = append(s.patterns, segments)
	}
	return s, nil
}

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(glob string) (string, error) {
	if glob != "~" && !strings.HasPrefix(glob, "~/") {
		return glob, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("protect: the glob %q needs a home directory: %w", glob, err)
	}
	if glob == "~" {
		return home, nil
	}
	return filepath.Join(home, glob[2:]), nil
}

// Match reports whether a path is protected.
//
// The path is resolved first: a symlink whose target is protected is
// protected as well. The path is matched as it was given too, so a glob
// written with a symlinked prefix still matches.
func (s *Set) Match(target string) bool {
	if s == nil || len(s.patterns) == 0 {
		return false
	}
	abs := absPath(target)
	resolved := resolvePath(abs)
	for _, pattern := range s.patterns {
		if matchSegments(pattern, splitPath(resolved)) {
			return true
		}
		if matchSegments(pattern, splitPath(abs)) {
			return true
		}
	}
	return false
}

// Expand returns the paths at or under root that are protected.
//
// A root that matches a glob is returned as the topmost match, without a
// walk: masking the root hides everything below it. Otherwise the walk
// starts at the glob's fixed prefix when that prefix is under the root, so
// expanding one bind directory does not walk another. A glob whose prefix
// is outside the root cannot match inside it, and is skipped.
//
// The walk does not follow symlinks: a symlinked directory is not entered,
// and a path is matched as it stands.
//
// The paths come back sorted, so a caller that masks them does the same
// thing every time.
func (s *Set) Expand(root string) ([]string, error) {
	if s == nil || len(s.patterns) == 0 {
		return nil, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("protect: resolving %s: %w", root, err)
	}
	// A root that is a symlink is walked through, so the directory
	// behind it is seen.
	if info, err := os.Lstat(abs); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
	}
	// A root that is itself protected is the topmost match: hiding it
	// hides every path below it, and the walk has nothing to add.
	if s.matchOwn(abs) {
		return []string{abs}, nil
	}

	roots := s.walkRoots(abs)
	var found []string
	for _, walkRoot := range roots {
		err = filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// An entry that cannot be read is skipped: the walk is a
				// search, and one unreadable directory does not end it.
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if p == abs && walkRoot == abs {
				return nil
			}
			if s.matchOwn(p) {
				found = append(found, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("protect: walking %s: %w", walkRoot, err)
		}
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

// walkRoots returns the directories the walk of root has to cover.
//
// A glob can only match inside root when its fixed prefix and root contain
// one another. The walk starts at the deeper of the two, so a glob for a
// small directory does not walk the whole bind it happens to be under, and
// a glob whose prefix is outside root costs nothing.
func (s *Set) walkRoots(root string) []string {
	var out []string
	for _, pattern := range s.patterns {
		prefix := fixedPrefix(pattern)
		var walkRoot string
		switch {
		case within(root, prefix):
			// The prefix is at or under the root, so the walk can
			// start there.
			walkRoot = prefix
		case within(prefix, root):
			// The root is under the prefix, so the matches can be
			// anywhere under the root.
			walkRoot = root
		default:
			continue
		}
		if info, err := os.Stat(walkRoot); err != nil || !info.IsDir() {
			continue
		}
		if !slices.Contains(out, walkRoot) {
			out = append(out, walkRoot)
		}
	}
	return out
}

// fixedPrefix returns the part of a pattern before its first wildcard.
//
// The prefix is a real path: every segment in it is a literal. The prefix
// of an absolute pattern is itself absolute.
func fixedPrefix(pattern []string) string {
	literal := make([]string, 0, len(pattern))
	for _, segment := range pattern {
		if segment == "**" {
			break
		}
		if _, err := path.Match(segment, ""); err != nil {
			break
		}
		if strings.ContainsAny(segment, "*?[") {
			break
		}
		literal = append(literal, segment)
	}
	if len(literal) == 0 {
		return "/"
	}
	prefix := strings.Join(literal, "/")
	if prefix == "" {
		return "/"
	}
	return prefix
}

// within reports whether a path is at or under a directory.
func within(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Resolve returns the absolute form of a path with its symlinks resolved.
//
// A path that does not exist is resolved as far as it goes, so a tool can
// decide what a path means before it creates the last element.
func Resolve(path string) string {
	return resolvePath(absPath(path))
}

// Within reports whether a path is at or under a directory.
//
// Both paths are compared as they are. A caller that needs symlinks
// resolved resolves them first, with Resolve.
func Within(dir, path string) bool {
	return within(absPath(dir), absPath(path))
}

// Guarded reports whether a path is protected by the given globs.
//
// A malformed glob is an error: a caller that cannot compile the globs
// must not run the call unprotected.
func Guarded(globs []string, target string) (bool, error) {
	set, err := Compile(globs)
	if err != nil {
		return false, err
	}
	return set.Match(target), nil
}

// Len returns the number of globs in the set.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.patterns)
}

// Globs returns the globs of the set, with the home directory expanded.
func (s *Set) Globs() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.globs...)
}

// matchOwn reports whether a path matches a glob, as the path stands.
//
// The path is not resolved: the walk that calls this does not follow
// symlinks, and a path that is a symlink is protected or not by the name
// it has.
func (s *Set) matchOwn(target string) bool {
	segments := splitPath(target)
	for _, pattern := range s.patterns {
		if matchSegments(pattern, segments) {
			return true
		}
	}
	return false
}

// matchSegments reports whether a path matches a pattern.
//
// A "**" segment matches any number of path segments, none included, so
// "/etc/**" holds "/etc" and everything under it. Every other segment is
// matched with the shell's rules, so a "*" does not cross a separator.
func matchSegments(pattern, segments []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(segments); i++ {
				if matchSegments(pattern[1:], segments[i:]) {
					return true
				}
			}
			return false
		}
		if len(segments) == 0 {
			return false
		}
		ok, err := path.Match(pattern[0], segments[0])
		if err != nil || !ok {
			return false
		}
		pattern, segments = pattern[1:], segments[1:]
	}
	return len(segments) == 0
}

// splitPath splits a path into its segments.
//
// The path is in slash form, and the empty first segment of an absolute
// path is kept, so a pattern and a path split the same way.
func splitPath(p string) []string {
	return strings.Split(filepath.ToSlash(p), "/")
}

// absPath returns the absolute form of a path.
func absPath(target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return filepath.Clean(target)
	}
	return abs
}

// resolvePath returns the path with its symlinks resolved.
//
// A path that does not exist is resolved as far as it goes: the directory
// that holds it is resolved, and a symlink is followed to the path it
// points at, which may not exist either.
func resolvePath(target string) string {
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		return resolved
	}
	dir, base := filepath.Split(target)
	if dir == "" {
		return target
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return target
	}
	joined := filepath.Join(resolvedDir, base)
	// The last element may be a symlink whose target does not exist: the
	// target is what the path means.
	if link, err := os.Readlink(joined); err == nil {
		if !filepath.IsAbs(link) {
			link = filepath.Join(resolvedDir, link)
		}
		if resolved, err := filepath.EvalSymlinks(link); err == nil {
			return resolved
		}
		return filepath.Clean(link)
	}
	return joined
}
