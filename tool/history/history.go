// Package history implements the history tool: a read over the turns
// the session's store retains.
//
// A compaction changes the model's view, not the store, so the text a
// strategy replaced is still there. The tool is how the model gets it
// back: it reads a turn whole, or searches the retained turns for the
// detail the reconstruction left out.
package history

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/prompt"
)

// Defaults for the tool.
const (
	// DefaultMaxMatches is the number of search results the tool shows.
	DefaultMaxMatches = 250
	// MaxLineChars is the number of characters a single reported line
	// may hold. A longer line is cut, so a giant tool result cannot
	// flood the context on a search; a turn read is where the full text
	// goes.
	MaxLineChars = 2000
	// DefaultMaxChars is the most text the tool returns when the
	// settings carry no observation cap.
	DefaultMaxChars = 30000
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
var section = prompt.MustCompile("history", sectionText, nil)

// Tool is the history tool.
type Tool struct {
	// MaxMatches is the number of search results the tool shows. Zero
	// means DefaultMaxMatches.
	MaxMatches int
}

// New returns the history tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "history",
		Description: "Read or search the turns this session's store retains, including the turns a compaction replaced.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "turn": {"type": "integer", "description": "The turn to read whole."},
    "pattern": {"type": "string", "description": "The RE2 regular expression to search the retained turns for."}
  },
  "additionalProperties": false
}`),
		Class:      poo.ClassReadOnly,
		PrimaryArg: "pattern",
	}
}

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}

// args are the tool's arguments.
type args struct {
	Turn    *int   `json:"turn"`
	Pattern string `json:"pattern"`
}

// prepared is a prepared history call.
type prepared struct {
	// turn is the turn to read, when the call reads one.
	turn *int
	// pattern is the pattern to search, when the call searches.
	pattern string
}

// Summary describes the call.
func (p prepared) Summary() string {
	if p.turn != nil {
		return fmt.Sprintf("history turn %d", *p.turn)
	}
	return "history search " + p.pattern
}

// Prepare accepts the call.
//
// A call reads a turn or searches, and never both: one question per
// call keeps the answers apart, so a model that wants both asks twice.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, poo.InvalidArgsf("history: %v", err)
		}
	}
	if a.Turn != nil && a.Pattern != "" {
		return nil, poo.InvalidArgsf("history: a call reads a turn or searches, not both")
	}
	if a.Turn == nil && a.Pattern == "" {
		return nil, poo.InvalidArgsf("history: a call names a turn or a pattern")
	}
	if a.Turn != nil && *a.Turn < 1 {
		return nil, poo.InvalidArgsf("history: turn must be at least 1, not %d", *a.Turn)
	}
	if a.Turn != nil {
		return &prepared{turn: a.Turn}, nil
	}
	if _, err := compile(a.Pattern); err != nil {
		return nil, poo.InvalidArgsf("history: %v", err)
	}
	return &prepared{pattern: a.Pattern}, nil
}

// Call answers.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("history: %v", err), IsError: true}, nil
	}
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("history: prepared value is not a history call")
	}
	if env.History == nil {
		return poo.ToolResult{Text: "history: the session's store cannot be read back", IsError: true}, nil
	}

	maxMatches := t.MaxMatches
	if maxMatches <= 0 {
		maxMatches = DefaultMaxMatches
	}

	cap := env.Settings.ObservationCapChars
	if cap <= 0 {
		cap = DefaultMaxChars
	}

	if p.turn != nil {
		text, err := env.History.Turn(*p.turn)
		if err != nil {
			return poo.ToolResult{Text: fmt.Sprintf("history: %v", err), IsError: true}, nil
		}
		if text == "" {
			return poo.ToolResult{Text: fmt.Sprintf("history: turn %d holds no message", *p.turn)}, nil
		}
		return poo.ToolResult{Text: cut(fmt.Sprintf("turn %d\n%s", *p.turn, text), cap)}, nil
	}

	matches, err := env.History.Search(p.pattern)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("history: %v", err), IsError: true}, nil
	}
	if len(matches) == 0 {
		return poo.ToolResult{Text: "history: no retained line matches"}, nil
	}
	shown := matches
	left := 0
	if len(shown) > maxMatches {
		left = len(shown) - maxMatches
		shown = shown[:maxMatches]
	}
	var b strings.Builder
	for _, m := range shown {
		line := fmt.Sprintf("turn %d: %s\n", m.Turn, cut(m.Text, MaxLineChars))
		if b.Len()+len(line) > cap {
			b.WriteString("(output truncated at the observation cap; narrow the pattern)")
			return poo.ToolResult{Text: strings.TrimRight(b.String(), "\n")}, nil
		}
		b.WriteString(line)
	}
	if left > 0 {
		b.WriteString(fmt.Sprintf("(%d more results not shown; narrow the pattern)", left))
	}
	return poo.ToolResult{Text: strings.TrimRight(b.String(), "\n")}, nil
}

// cut cuts a line at MaxLineChars.
func cut(line string, max int) string {
	if max <= 0 || len(line) <= max {
		return line
	}
	// The prefix is cut on a character boundary: a byte slice would
	// split a multi-byte character and put invalid UTF-8 in the result.
	return caps.RunePrefix(line, max) + "…"
}

// compile compiles the pattern, so the error names the tool.
func compile(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("the pattern is not a valid RE2 expression: %v", err)
	}
	return re, nil
}
