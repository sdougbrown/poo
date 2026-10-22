// Package grep implements the grep tool: a search over the files of a
// tree.
//
// The tool picks its own files, so its behavior does not depend on
// whether ripgrep is installed: inside a git work tree it asks git for
// the files, so the ignore rules hold, and outside one it walks the tree.
package grep

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/protect"
)

// Defaults for the grep tool.
const (
	// DefaultMaxMatches is the number of results the tool shows.
	DefaultMaxMatches = 250
	// MaxLineChars is the number of characters a single reported line
	// may hold. A longer line is cut, so a minified file cannot flood
	// the context.
	MaxLineChars = 2000
	// BinarySniffBytes is how much of a file is read to decide whether
	// it is binary.
	BinarySniffBytes = 8 << 10
)

// The modes the tool searches in.
const (
	// ModeContent shows the lines that match.
	ModeContent = "content"
	// ModeFilesWithMatches shows the files that hold a match.
	ModeFilesWithMatches = "files_with_matches"
	// ModeCount shows how many matches each file holds.
	ModeCount = "count"
)

// Tool is the grep tool.
type Tool struct {
	// MaxMatches is the number of results the tool shows. Zero means
	// DefaultMaxMatches.
	MaxMatches int
	// ObservationCap is the cap on the output, in characters. Zero means
	// the cap comes from the run's settings.
	ObservationCap int
}

// New returns a grep tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "grep",
		Description: "Search the files of a tree for a regular expression.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "The RE2 regular expression to search for."},
    "path": {"type": "string", "description": "The directory to search; the working directory by default."},
    "glob": {"type": "string", "description": "A filter, matched against the path relative to the search root, e.g. *.go"},
    "mode": {"type": "string", "description": "content (default), files_with_matches or count"}
  },
  "required": ["pattern"]
}`),
		Class:      poo.ClassReadOnly,
		PrimaryArg: "pattern",
	}
}

// args are the tool's arguments.
type args struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Glob    string `json:"glob,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// prepared is a validated grep call.
type prepared struct {
	// pattern is the compiled pattern, nil when the pattern is empty.
	pattern *regexp.Regexp
	// source is the pattern as it was written.
	source string
	// root is the directory the search covers.
	root string
	// glob filters the paths, matched against the path relative to the
	// root.
	glob string
	// mode is the mode the search runs in.
	mode string
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string {
	if p.source == "" {
		return fmt.Sprintf("files in %s", p.root)
	}
	return fmt.Sprintf("%q in %s", p.source, p.root)
}

// Prepare validates the arguments.
//
// The pattern is compiled here, so an invalid regular expression is a
// validation failure and not a failure of the search.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("grep: %v", err)
	}
	mode := a.Mode
	if mode == "" {
		mode = ModeContent
	}
	switch mode {
	case ModeContent, ModeFilesWithMatches, ModeCount:
	default:
		return nil, poo.InvalidArgsf("grep: mode must be %s, %s or %s, not %q",
			ModeContent, ModeFilesWithMatches, ModeCount, a.Mode)
	}
	// An empty pattern lists the files, which is the one thing the
	// files mode does with it.
	if a.Pattern == "" && mode != ModeFilesWithMatches {
		return nil, poo.InvalidArgsf("grep: pattern must not be empty in the %s mode", mode)
	}
	var re *regexp.Regexp
	if a.Pattern != "" {
		compiled, err := regexp.Compile(a.Pattern)
		if err != nil {
			return nil, poo.InvalidArgsf("grep: %v", err)
		}
		re = compiled
	}
	if a.Glob != "" {
		if _, err := path.Match(a.Glob, ""); err != nil {
			return nil, poo.InvalidArgsf("grep: the glob %q is malformed: %v", a.Glob, err)
		}
	}
	root := a.Path
	if strings.TrimSpace(root) == "" {
		root = env.WorkingDir
	}
	if root == "" {
		root = "."
	}
	// A relative root is taken from the working directory of the run.
	if !filepath.IsAbs(root) && env.WorkingDir != "" {
		root = filepath.Join(env.WorkingDir, root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, poo.InvalidArgsf("grep: %v", err)
	}
	return &prepared{
		pattern: re,
		source:  a.Pattern,
		root:    abs,
		glob:    a.Glob,
		mode:    mode,
	}, nil
}

// decodeStrict decodes one JSON value and nothing else.
//
// A second value, or anything after the first, is a malformed call: the
// model sent more than it meant to, and taking the first object would
// silently run the wrong thing.
func decodeStrict(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("the arguments hold more than one JSON value")
		}
		return err
	}
	return nil
}

// Call runs the search.
//
// The context is honoured: a cancelled run stops paying for the search.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("grep: %v", err), IsError: true}, nil
	}
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("grep: prepared value is not a grep call")
	}

	protected, err := protect.Compile(env.ProtectedPaths)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("grep: %v", err), IsError: true}, nil
	}

	files, err := selectFiles(p.root)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("grep: %v", err), IsError: true}, nil
	}

	maxMatches := t.MaxMatches
	if maxMatches <= 0 {
		maxMatches = DefaultMaxMatches
	}
	cap := t.ObservationCap
	if cap <= 0 {
		cap = env.Settings.ObservationCapChars
	}
	if cap <= 0 {
		cap = bash.ObservationCapMax
	}

	s := &search{mode: p.mode, max: maxMatches}
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return poo.ToolResult{Text: fmt.Sprintf("grep: %v", err), IsError: true}, nil
		}
		abs := filepath.Join(p.root, filepath.FromSlash(rel))
		// The glob filter narrows the search before anything is read.
		if p.glob != "" {
			ok, err := path.Match(p.glob, rel)
			if err != nil || !ok {
				continue
			}
		}
		// A protected file is skipped, and the count is reported.
		if protected.Match(abs) {
			s.protected++
			continue
		}
		// A symlink is not followed, and a directory is not read.
		info, err := os.Lstat(abs)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		binary, err := isBinary(abs)
		if err != nil || binary {
			continue
		}
		if p.pattern == nil {
			// The empty pattern of the files mode lists the files.
			s.files++
			s.add(rel)
			continue
		}
		matched, err := s.scan(ctx, abs, rel, p.pattern)
		if err != nil {
			return poo.ToolResult{Text: fmt.Sprintf("grep: %v", err), IsError: true}, nil
		}
		if matched {
			s.files++
		}
	}
	return poo.ToolResult{Text: s.render(cap)}, nil
}

// search holds what a search found.
type search struct {
	// mode is the mode the search ran in.
	mode string
	// max is the number of results that are shown.
	max int
	// entries are the lines the model sees.
	entries []string
	// matches is the number of results that were found.
	matches int
	// files is the number of files that hold a result.
	files int
	// elided is the number of results that were not shown.
	elided int
	// protected is the number of protected files that were skipped.
	protected int
	// chars is the number of characters of the shown results.
	chars int
}

// add records a result.
//
// A result that does not fit under the cap on the results is counted and
// left out, so the elision line can say how many were left out.
func (s *search) add(line string) {
	s.matches++
	if len(s.entries) >= s.max {
		s.elided++
		return
	}
	s.entries = append(s.entries, line)
	s.chars += len(line) + 1
}

// scan reads a file and records what the mode shows.
//
// It reports whether the file held a match.
func (s *search) scan(ctx context.Context, abs, rel string, re *regexp.Regexp) (bool, error) {
	f, err := os.Open(abs)
	if err != nil {
		return false, nil
	}
	defer f.Close()
	br := bufio.NewReader(f)
	number := 0
	matches := 0
	for {
		if err := ctx.Err(); err != nil {
			return matches > 0, err
		}
		text, err := br.ReadString('\n')
		if len(text) > 0 {
			number++
			line := strings.TrimSuffix(text, "\n")
			line = strings.TrimSuffix(line, "\r")
			if re.MatchString(line) {
				matches++
				if s.mode == ModeContent {
					s.add(fmt.Sprintf("%s:%d:%s", rel, number, truncateLine(line)))
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return matches > 0, err
		}
	}
	switch s.mode {
	case ModeCount:
		if matches > 0 {
			s.add(fmt.Sprintf("%s:%d", rel, matches))
		}
	case ModeFilesWithMatches:
		if matches > 0 {
			s.add(rel)
		}
	}
	return matches > 0, nil
}

// render builds the output the model reads.
//
// The output names what the search found, and the last line says what
// was left out.
func (s *search) render(cap int) string {
	for {
		lines := s.lines()
		text := strings.Join(lines, "\n")
		if len(text) <= cap || len(s.entries) == 0 {
			return text
		}
		// The output does not fit under the cap: the last result goes,
		// and the elision count grows.
		s.entries = s.entries[:len(s.entries)-1]
		s.elided++
	}
}

// lines returns the lines of the output.
func (s *search) lines() []string {
	lines := make([]string, 0, len(s.entries)+2)
	lines = append(lines, s.header())
	lines = append(lines, s.entries...)
	if line := s.elision(); line != "" {
		lines = append(lines, line)
	}
	return lines
}

// header names what the search found.
func (s *search) header() string {
	switch s.mode {
	case ModeCount:
		return fmt.Sprintf("[grep · %d files with matches]", s.files)
	case ModeFilesWithMatches:
		return fmt.Sprintf("[grep · %d files]", s.files)
	default:
		return fmt.Sprintf("[grep · %d matches in %d files]", s.matches, s.files)
	}
}

// elision names what the search left out.
//
// The line is there when something was left out: the results past the
// cap, or the protected files that were skipped.
func (s *search) elision() string {
	var parts []string
	if s.elided > 0 {
		switch s.mode {
		case ModeContent:
			parts = append(parts, fmt.Sprintf("%d more matches elided", s.elided))
		default:
			parts = append(parts, fmt.Sprintf("%d more files elided", s.elided))
		}
	}
	if s.protected > 0 {
		parts = append(parts, fmt.Sprintf("%d protected files skipped", s.protected))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[grep · " + strings.Join(parts, " · ") + "]"
}

// selectFiles returns the files under root that the search covers.
//
// Inside a git work tree the files come from git, so the ignore rules
// hold, the nested ones included. Outside one the tree is walked, and
// the dot-directories are skipped.
//
// The paths come back sorted, and relative to the root, in slash form.
func selectFiles(root string) ([]string, error) {
	if inWorkTree(root) {
		files, err := gitFiles(root)
		if err == nil {
			slices.Sort(files)
			return files, nil
		}
	}
	files, err := walkFiles(root)
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

// inWorkTree reports whether a directory is inside a git work tree.
func inWorkTree(root string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// gitFiles returns the files of a work tree.
//
// The paths are the ones git knows: the tracked files and the untracked
// ones the ignore rules do not hide.
func gitFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(stdout.String(), "\x00") {
		if name == "" {
			continue
		}
		files = append(files, filepath.ToSlash(name))
	}
	return files, nil
}

// walkFiles returns the files of a tree.
//
// The dot-directories are skipped, and no ignore file is applied: the
// tree is searched as it stands.
func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An entry that cannot be read is skipped: the walk is a
			// search, and one unreadable directory does not end it.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// isBinary reports whether a file holds a NUL byte in its first bytes.
func isBinary(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, BinarySniffBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return bytes.IndexByte(buf[:n], 0) >= 0, nil
}

// truncateLine cuts a line to the line cap.
//
// The cut is in characters, and a rune is never split.
func truncateLine(line string) string {
	count := 0
	for i := range line {
		if count == MaxLineChars {
			return line[:i] + "…"
		}
		count++
	}
	return line
}

// section is the guidance the tool brings to the system prompt.
//
// The tool shows where it searched and what it left out, so the model
// can narrow the search and carry on.
const section = "Grep:\n" +
	"- The pattern is an RE2 regular expression, matched line by line, and the output is \"path:line:text\".\n" +
	"- Use the files_with_matches mode to find the files that hold a pattern, and the count mode to see how often it occurs.\n" +
	"- The glob filter is matched against the path relative to the search root, e.g. \"*.go\".\n" +
	"- An empty pattern in the files_with_matches mode lists the files.\n" +
	"- Inside a git work tree the ignore rules hold, and outside one every file under the root is searched.\n" +
	"- The last line says how many results were left out, and how many protected files were skipped."

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section
}
