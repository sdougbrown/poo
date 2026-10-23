package cliregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	cli "github.com/sdougbrown/cli-registry"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
)

// CallTool runs one registered CLI.
//
// The call is side-effecting: an arbitrary command can change anything,
// so the permission policy decides about it the way it decides about
// bash.
type CallTool struct {
	// catalog is the set of CLIs the tool may call.
	catalog *Catalog
	// search reports that the session offers find_tool over the
	// catalog, so an unknown name can point at the search.
	search bool
}

// NewCall returns the tool that runs a registered CLI.
func NewCall(catalog *Catalog) *CallTool {
	return &CallTool{catalog: catalog}
}

// WithSearch says whether the session offers a search over the catalog.
//
// The hint an unknown name gets depends on it: a session that cannot
// search is not pointed at a search.
func (t *CallTool) WithSearch(search bool) *CallTool {
	t.search = search
	return t
}

// Spec describes the tool.
func (t *CallTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        CallToolName,
		Description: "Run one of the registered command-line tools and return its output.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "The registered tool's name."},
    "args": {"type": "object", "description": "The tool's arguments, as its schema describes them."}
  },
  "required": ["name"]
}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "name",
	}
}

// The prompt section the tool brings.
//
// The catalog has a section of its own, which the session renders; the
// tool adds what the catalog cannot say: what a call does when it fails,
// and where its output goes.
const callSection = "Calling a registered CLI:\n" +
	"- \"name\" is one line of the catalog, and \"args\" is an object of that CLI's arguments.\n" +
	"- Arguments that do not fit the CLI's schema are refused before anything runs, with the reason.\n" +
	"- The result is the command's output and its exit status, elided in the middle when it is long."

// PromptSection renders the tool's guidance.
func (t *CallTool) PromptSection(prompt.Env) string { return callSection }

// callArgs are the tool's arguments.
type callArgs struct {
	// Name is the registered CLI to run.
	Name string `json:"name"`
	// Args are the CLI's arguments, as a JSON object.
	Args json.RawMessage `json:"args,omitempty"`
}

// preparedCall is a validated call_cli call.
type preparedCall struct {
	// name is the registered CLI.
	name string
	// args are the CLI's arguments.
	args map[string]any
	// key is the call's canonical input.
	key json.RawMessage
}

// Summary describes the call for a human reader.
func (p *preparedCall) Summary() string {
	return strings.TrimSpace(p.name + " " + string(marshal(p.args)))
}

// CanonicalKey returns the call's prepared input.
func (p *preparedCall) CanonicalKey() json.RawMessage { return p.key }

// EvidenceArguments returns the arguments a decision model may see.
func (p *preparedCall) EvidenceArguments() json.RawMessage { return p.key }

// Prepare validates the arguments.
//
// The name has to be in the catalog, and the arguments have to fit the
// CLI's schema. Both are refused here, before a process exists, so a
// malformed call never reaches a command and the model sees the reason
// instead of the command's failure.
func (t *CallTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	if t.catalog == nil {
		return nil, poo.InvalidArgsf("%s: no CLI registry is configured", CallToolName)
	}
	var a callArgs
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("%s: %v", CallToolName, err)
	}
	entry, ok := t.catalog.Lookup(a.Name)
	if !ok {
		if t.catalog.Len() == 0 {
			return nil, poo.InvalidArgsf("%s: no CLI is registered", CallToolName)
		}
		return nil, poo.InvalidArgsf("%s: no CLI named %q; %s",
			CallToolName, a.Name, t.catalog.NamesHint(t.search))
	}
	args := map[string]any{}
	if len(a.Args) > 0 {
		// An omitted args is an empty object, because a CLI that takes
		// no arguments is called with the name alone. An explicit null
		// is not the same thing: the property's type is an object.
		if string(a.Args) == "null" {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object, not null", CallToolName, entry.Name)
		}
		if err := json.Unmarshal(a.Args, &args); err != nil {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object: %v", CallToolName, entry.Name, err)
		}
	}
	if issues := t.catalog.Validate(entry.Name, args); len(issues) > 0 {
		return nil, poo.InvalidArgsf("%s: %s: %s", CallToolName, entry.Name, strings.Join(issues, "; "))
	}
	key := marshal(struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}{entry.Name, args})
	return &preparedCall{name: entry.Name, args: args, key: key}, nil
}

// Call runs the CLI.
//
// The output is the CLI's stdout and stderr with its exit status, in the
// envelope bash's observation uses. An exit outside the CLI's success
// codes is a recoverable failure: the model sees what the command said
// and may try something else.
func (t *CallTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*preparedCall)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("%s: prepared value is not a %s call", CallToolName, CallToolName)
	}
	cap := env.Settings.ObservationCapChars
	if cap <= 0 {
		cap = caps.ObservationMax
	}

	res, err := t.catalog.Run(ctx, p.name, p.args)
	if err != nil {
		return failed(err, cap)
	}
	code := res.ExitCode
	text, structured := bash.Envelope(bash.Observation{
		Returncode: &code,
		Output:     string(res.Stdout),
		Stderr:     string(res.Stderr),
		Cap:        cap,
	})
	return poo.ToolResult{Text: text, Structured: structured}, nil
}

// failed renders a call that did not produce a result as an observation.
//
// A timeout and an exit outside the success codes are recoverable: the
// model sees what the command said and may try something else. Anything
// else is the environment being unusable, and it goes back as an error.
func failed(err error, cap int) (poo.ToolResult, error) {
	var timedOut *cli.TimeoutError
	if errors.As(err, &timedOut) {
		text, structured := bash.Envelope(bash.Observation{TimedOut: true, Cap: cap})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, nil
	}
	var exited *cli.ExecError
	if errors.As(err, &exited) {
		// A process that died by a signal has no exit status, so the
		// field is null, as it is in bash's envelope.
		var code *int
		if exited.ExitCode >= 0 {
			code = &exited.ExitCode
		}
		// cli-registry reports the stderr of a failed call and not its
		// stdout, so the envelope carries what is left.
		text, structured := bash.Envelope(bash.Observation{
			Returncode: code,
			Stderr:     string(exited.Stderr),
			Cap:        cap,
		})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, nil
	}
	return poo.ToolResult{}, fmt.Errorf("%s: %w", CallToolName, err)
}

// decodeStrict decodes one JSON value and nothing else.
//
// A second value, or anything after the first, is a malformed call: the
// model sent more than it meant to, and taking the first object would
// silently run something else.
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

// marshal encodes a value, falling back to an empty object.
func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
