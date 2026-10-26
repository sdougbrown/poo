// Package edit implements the edit tool: an exact-string replacement in
// a file.
//
// The tool replaces a string the model wrote out, and it fails loudly
// when that string appears nowhere or more than once: a change that is
// not the one the model meant is worse than no change at all.
package edit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/protect"
)

// Defaults for the edit tool.
const (
	// FileMode is the mode a created file gets.
	FileMode = 0o644
	// DirMode is the mode a created directory gets.
	DirMode = 0o755
)

// Tool is the edit tool.
type Tool struct{}

// New returns an edit tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "edit",
		Description: "Replace an exact string in a file.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "The path of the file to change."},
    "old": {"type": "string", "description": "The exact text to replace; empty to create a missing file."},
    "new": {"type": "string", "description": "The text that replaces it."},
    "replace_all": {"type": "boolean", "description": "Replace every occurrence instead of exactly one."}
  },
  "required": ["path", "old", "new"]
}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "path",
	}
}

// args are the tool's arguments.
type args struct {
	Path       string `json:"path"`
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// prepared is a validated edit call.
type prepared struct {
	// path is the path as the model wrote it.
	path string
	// abs is the path the call works on.
	abs        string
	old        string
	new        string
	replaceAll bool
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string {
	if p.old == "" {
		return fmt.Sprintf("%s (create)", p.path)
	}
	return fmt.Sprintf("%s (replace %q)", p.path, p.old)
}

// CanonicalKey returns the call's prepared input.
func (p *prepared) CanonicalKey() json.RawMessage {
	key, _ := json.Marshal(struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}{p.abs, p.old, p.new, p.replaceAll})
	return key
}

// EvidenceArguments returns the arguments a decision model may see.
func (p *prepared) EvidenceArguments() json.RawMessage {
	args, _ := json.Marshal(struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}{p.path, p.old, p.new, p.replaceAll})
	return args
}

// Prepare validates the arguments.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("edit: %v", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return nil, poo.InvalidArgsf("edit: path must not be empty")
	}
	abs := protect.Resolve(absolutePath(a.Path, env.WorkingDir))
	if env.WorkingDir != "" && !protect.Within(protect.Resolve(env.WorkingDir), abs) {
		return nil, poo.InvalidArgsf("edit: path is outside the working directory: %s", a.Path)
	}
	// A protected path is refused before the file is touched, so the
	// model never changes what it holds.
	protected, err := protect.Guarded(env.ProtectedPaths, abs)
	if err != nil {
		return nil, poo.InvalidArgsf("edit: %v", err)
	}
	if protected {
		return nil, poo.InvalidArgsf("edit: path is protected by configuration: %s", a.Path)
	}
	return &prepared{
		path:       a.Path,
		abs:        abs,
		old:        a.Old,
		new:        a.New,
		replaceAll: a.ReplaceAll,
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

// Call makes the change.
//
// The context is honoured: a cancelled run stops paying for the edit.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("edit: %v", err), IsError: true}, nil
	}
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("edit: prepared value is not an edit call")
	}

	content, mode, exists, err := read(p.abs)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("edit: %v", err), IsError: true}, nil
	}
	if !exists {
		if p.old != "" {
			return poo.ToolResult{
				Text:    fmt.Sprintf("edit: %s does not exist, and an edit with an empty old text creates it", p.path),
				IsError: true,
			}, nil
		}
		if err := create(p.abs, p.new); err != nil {
			return poo.ToolResult{Text: fmt.Sprintf("edit: %v", err), IsError: true}, nil
		}
		return poo.ToolResult{Text: fmt.Sprintf("created %s", p.path)}, nil
	}
	if p.old == "" {
		return poo.ToolResult{
			Text:    fmt.Sprintf("edit: %s exists, so the old text must not be empty", p.path),
			IsError: true,
		}, nil
	}

	found := strings.Count(content, p.old)
	switch {
	case found == 0:
		return poo.ToolResult{
			Text:    fmt.Sprintf("edit: the old text was not found in %s", p.path),
			IsError: true,
		}, nil
	case found > 1 && !p.replaceAll:
		return poo.ToolResult{
			Text: fmt.Sprintf("edit: the old text appears %d times in %s; set replace_all to replace all of them",
				found, p.path),
			IsError: true,
		}, nil
	}

	replaced := strings.ReplaceAll(content, p.old, p.new)
	if !p.replaceAll {
		replaced = strings.Replace(content, p.old, p.new, 1)
	}
	if err := writeAtomic(p.abs, replaced, mode); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("edit: %v", err), IsError: true}, nil
	}
	if p.replaceAll {
		return poo.ToolResult{Text: fmt.Sprintf("edited %s: %d replacements", p.path, found)}, nil
	}
	return poo.ToolResult{Text: fmt.Sprintf("edited %s: 1 replacement", p.path)}, nil
}

// read reads a file and reports whether it is there.
//
// The mode of an existing file comes back with it, so the change keeps
// the mode the file had.
func read(path string) (string, os.FileMode, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	if info.IsDir() {
		return "", 0, false, fmt.Errorf("%s is a directory", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false, err
	}
	return string(content), info.Mode().Perm(), true, nil
}

// create writes a new file, making the directories it needs.
func create(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return err
	}
	return writeAtomic(path, content, FileMode)
}

// writeAtomic writes a file through a temporary file and a rename in the
// same directory, so a crash or a full disk leaves the old file in place
// rather than a partial one. The mode is the file's own.
func writeAtomic(path, content string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".poo-edit-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	cleanup := func() {
		f.Close()
		os.Remove(temp)
	}
	if _, err := f.WriteString(content); err != nil {
		cleanup()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}

// absolutePath returns the path a call works on.
//
// A relative path is taken from the working directory of the run.
func absolutePath(path, workingDir string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workingDir, path)
}

// section is the guidance the tool brings to the system prompt.
//
// The tool changes a file only when the text it was given is in the
// file, so the model is told how to write that text.
const section = "Edit:\n" +
	"- The edit replaces an exact string, so the old text must be in the file exactly as you wrote it, spacing included.\n" +
	"- The old text must appear once, unless replace_all is set: the edit fails loudly when it appears nowhere or more than once.\n" +
	"- An edit with an empty old text on a path that does not exist creates the file.\n" +
	"- After an edit, run the tests once. When they pass, end your turn with a short summary."

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section
}
