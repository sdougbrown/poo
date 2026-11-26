// Package write implements the write tool: the creation of a new file.
//
// It is the creation half of the file tools, under the name pre-trained
// models reach for when they mean to create a file. A path that already
// exists is refused: an overwrite is a change the model may not have
// meant, and poo's file tools fail loudly rather than half do one.
package write

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/fileio"
	"github.com/sdougbrown/poo/protect"
)

// Tool is the write tool.
type Tool struct{}

// New returns a write tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name: "write",
		Description: "Create a file with content, making directories as needed. " +
			"The path must not exist: to change an existing file, use edit.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "The path of the file to create."},
    "content": {"type": "string", "description": "The content the new file gets."}
  },
  "required": ["path", "content"]
,"additionalProperties":false}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "path",
	}
}

// args are the tool's arguments.
type args struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// prepared is a validated write call.
type prepared struct {
	// path is the path as the model wrote it.
	path string
	// abs is the path the call works on.
	abs     string
	content string
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string {
	return fmt.Sprintf("%s (create)", p.path)
}

// CanonicalKey returns the call's prepared input.
func (p *prepared) CanonicalKey() json.RawMessage {
	key, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{p.abs, p.content})
	return key
}

// EvidenceArguments returns the arguments a decision model may see.
func (p *prepared) EvidenceArguments() json.RawMessage {
	args, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{p.path, p.content})
	return args
}

// Prepare validates the arguments.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, poo.InvalidArgsf("write: %v", err)
		}
	}
	if strings.TrimSpace(a.Path) == "" {
		return nil, poo.InvalidArgsf("write: path must not be empty")
	}
	abs := protect.Resolve(fileio.Resolve(a.Path, env.WorkingDir))
	if env.RestrictWrite && env.WorkingDir != "" && !protect.Within(protect.Resolve(env.WorkingDir), abs) {
		return nil, poo.InvalidArgsf("write: path is outside the working directory: %s", a.Path)
	}
	// A protected path is refused before the file is touched, so the
	// model never changes what it holds.
	protected, err := protect.Guarded(env.ProtectedPaths, abs)
	if err != nil {
		return nil, poo.InvalidArgsf("write: %v", err)
	}
	if protected {
		return nil, poo.InvalidArgsf("write: path is protected by configuration: %s", a.Path)
	}
	return &prepared{
		path:    a.Path,
		abs:     abs,
		content: a.Content,
	}, nil
}

// Call creates the file.
//
// The context is honoured: a cancelled run stops paying for the write.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("write: %v", err), IsError: true}, nil
	}
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("write: prepared value is not a write call")
	}

	if !fileio.Reserve(p.abs) {
		return poo.ToolResult{
			Text:       fmt.Sprintf("write: another write is in progress for %s; re-read the file before retrying", p.path),
			Structured: json.RawMessage(`{"code":"edit_conflict"}`),
			IsError:    true,
		}, nil
	}
	defer fileio.Release(p.abs)

	if _, err := os.Stat(p.abs); err == nil {
		return poo.ToolResult{
			Text:    fmt.Sprintf("write: %s already exists; to change an existing file, use edit", p.path),
			IsError: true,
		}, nil
	}
	if err := fileio.Write(p.abs, p.content); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("write: %v", err), IsError: true}, nil
	}
	return poo.ToolResult{Text: fmt.Sprintf("created %s", p.path)}, nil
}
