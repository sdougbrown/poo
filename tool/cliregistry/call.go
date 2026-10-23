package cliregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cli "github.com/sdougbrown/cli-registry"
	"github.com/sdougbrown/cli-registry/schema"

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
	// executor runs the CLI. Nil means the local executor, the way it
	// does for bash.
	executor bash.Executor
}

// NewCall returns the tool that runs a registered CLI.
func NewCall(catalog *Catalog) *CallTool {
	return &CallTool{catalog: catalog}
}

// WithExecutor gives the tool the executor its commands run through.
//
// It is the executor bash uses, so a registered CLI runs in the same
// place and under the same process rules as a shell command.
func (t *CallTool) WithExecutor(e bash.Executor) *CallTool {
	t.executor = e
	return t
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
	// plan is the command the call runs.
	plan *cli.Plan
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

// Prepare validates the arguments and plans the command.
//
// The name has to be in the catalog, and the arguments have to fit the
// CLI's schema. Both are refused here, before a process exists, so a
// malformed call never reaches a command and the model sees the reason
// instead of the command's failure. The plan is the command and nothing
// else: no environment is read and no process is started until Call.
func (t *CallTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	if t.catalog == nil {
		return nil, poo.InvalidArgsf("%s: no CLI registry is configured", CallToolName)
	}
	var a callArgs
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("%s: %v", CallToolName, err)
	}
	if _, ok := t.catalog.Lookup(a.Name); !ok {
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
			return nil, poo.InvalidArgsf("%s: %s: args must be an object, not null", CallToolName, a.Name)
		}
		if err := json.Unmarshal(a.Args, &args); err != nil {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object: %v", CallToolName, a.Name, err)
		}
	}
	plan, err := t.catalog.Plan(a.Name, args)
	if err != nil {
		return nil, planError(a.Name, err)
	}
	if err := checkArgv(a.Name, args, plan.Argv); err != nil {
		return nil, err
	}
	key := marshal(struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}{a.Name, args})
	return &preparedCall{name: a.Name, args: args, plan: plan, key: key}, nil
}

// planError reports a call the CLI's own definition refused.
//
// A validation failure is the model's to correct, so it comes back as
// an invalid call. Anything else is the manifest being unusable — a
// binding that cannot be built, or a working directory that does not
// resolve — and it is reported as it is.
func planError(name string, err error) error {
	var invalid *cli.ValidationError
	if errors.As(err, &invalid) {
		return poo.InvalidArgsf("%s: %s: %s", CallToolName, name, issuesText(invalid.Issues))
	}
	return fmt.Errorf("%s: %s: %w", CallToolName, name, err)
}

// Call runs the planned command through the session's executor.
//
// The command runs where bash runs and under the same rules: the
// session's executor (the sandbox, when the settings name one), the
// session's working directory unless the manifest fixes one, an
// environment looked up in the session's scrubbed one, and the run's
// context, so a cancelled run stops the CLI and everything it started.
//
// The output is the CLI's stdout and stderr with its exit status, in the
// envelope bash's observation uses. An exit outside the CLI's success
// codes and a timeout are recoverable failures: the model sees what the
// command said and may try something else.
func (t *CallTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*preparedCall)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("%s: prepared value is not a %s call", CallToolName, CallToolName)
	}
	if p.plan == nil {
		return poo.ToolResult{}, fmt.Errorf("%s: %s: the call has no plan", CallToolName, p.name)
	}

	// A manifest's env:NAME is resolved against the session's scrubbed
	// environment, never the host's: a name the scrub removed or the
	// passthrough list left out is a reference the CLI cannot have.
	environ, err := p.plan.ResolveEnv(envLookup(env.Env))
	if err != nil {
		return poo.ToolResult{}, fmt.Errorf("%s: %s: %w", CallToolName, p.name, err)
	}

	dir := p.plan.Dir
	if dir == "" {
		dir = env.WorkingDir
	}

	cap := env.Settings.ObservationCapChars
	if cap <= 0 {
		cap = caps.ObservationMax
	}

	// A context that has already ended runs nothing: starting the
	// command only for the executor to stop it gives a fast command
	// time for its side effects. The call is classified the way a
	// stopped one is.
	if res, ok := stoppedResult(ctx.Err(), cli.Output{}, cap); ok {
		return res, nil
	}

	executor := t.executor
	if executor == nil {
		executor = bash.NewLocal()
	}

	runCtx := ctx
	if p.plan.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.plan.Timeout)
		defer cancel()
	}

	stdout := bash.NewCappedBuffer(bash.CaptureCap)
	stderr := bash.NewCappedBuffer(bash.CaptureCap)

	start := time.Now()
	proc, err := executor.Start(runCtx, bash.Command{
		Argv:   p.plan.Argv,
		Dir:    dir,
		Env:    environ,
		Stderr: stderr,
	}, stdout)
	if err != nil {
		return poo.ToolResult{}, fmt.Errorf("%s: %s: %w", CallToolName, p.name, err)
	}
	code, _ := proc.Wait()

	out := cli.Output{
		Stdout:    []byte(stdout.String()),
		Stderr:    []byte(stderr.String()),
		Truncated: stdout.Truncated() || stderr.Truncated(),
		ExitCode:  code,
		Duration:  time.Since(start),
	}

	// A command the executor stopped was stopped by the run's context,
	// so what it wrote before is all there is. A command that exited on
	// its own keeps its own outcome, however long the teardown of what
	// it left behind takes.
	if proc.Stopped() {
		if res, ok := stoppedResult(runCtx.Err(), out, cap); ok {
			return res, nil
		}
	}

	res, err := p.plan.Finish(out)
	if err != nil {
		return failed(err, cap)
	}
	returncode := res.ExitCode
	text, structured := bash.Envelope(bash.Observation{
		Returncode: &returncode,
		Output:     string(res.Stdout),
		Stderr:     string(res.Stderr),
		Cap:        cap,
	})
	return poo.ToolResult{Text: text, Structured: structured}, nil
}

// stoppedResult renders a call the run's context ended, and reports
// whether it did.
//
// Whatever the command wrote before it was stopped is kept: it is the
// output the model needs to see why the call did not finish. A deadline
// is a timeout; a cancellation is a failure without one.
func stoppedResult(err error, out cli.Output, cap int) (poo.ToolResult, bool) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		text, structured := bash.Envelope(bash.Observation{
			TimedOut: true,
			Output:   string(out.Stdout),
			Stderr:   string(out.Stderr),
			Cap:      cap,
		})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, true
	case errors.Is(err, context.Canceled):
		text, structured := bash.Envelope(bash.Observation{
			Output: string(out.Stdout),
			Stderr: string(out.Stderr),
			Cap:    cap,
		})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, true
	}
	return poo.ToolResult{}, false
}

// checkArgv refuses a value the command may read as a flag.
//
// cli-registry escapes a positional value that begins with a dash by
// putting "--" before it, and a value bound with the equals or short
// style is part of one argv element. A value bound with the space style
// is an element of its own after its flag, where a command whose flag
// takes no value reads it as another flag. poo cannot tell one such
// command from another, so it refuses the call rather than run a command
// with an option the model supplied but the manifest does not declare.
func checkArgv(name string, args map[string]any, argv []string) error {
	values := dashLeadingValues(args)
	if len(values) == 0 {
		return nil
	}
	// Everything after the first "--" is an operand, whatever it looks
	// like; before it, an element equal to a value the model supplied
	// is a value the command parses as an option.
	operands := len(argv)
	for i, element := range argv {
		if element == "--" {
			operands = i
			break
		}
	}
	for _, value := range values {
		// The command itself is argv[0], never a value the model
		// supplied.
		for i := 1; i < len(argv); i++ {
			if argv[i] != value || i >= operands {
				continue
			}
			return poo.InvalidArgsf("%s: %s: %q begins with a dash, and the command may read it as a flag; "+
				"pass a value it cannot mistake for one", CallToolName, name, value)
		}
	}
	return nil
}

// dashLeadingValues returns the string arguments that could be a flag.
//
// The values are the ones the model wrote, anywhere in the argument
// object: a binding reads a top-level property, and a positional array
// passes its elements as they are. Only a string can name another flag;
// a number encodes to digits and a sign.
func dashLeadingValues(v any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case string:
			if strings.HasPrefix(value, "-") && value != "-" {
				out = append(out, value)
			}
		case []any:
			for _, element := range value {
				walk(element)
			}
		case map[string]any:
			for _, element := range value {
				walk(element)
			}
		}
	}
	walk(v)
	return out
}

// envLookup returns the lookup a plan resolves its env references with.
//
// The environment is the session's, already scrubbed, so a name the
// scrub removed or the passthrough list left out is not there to read.
// A nil environment means the host's own, the way it does for bash.
func envLookup(environ []string) func(string) (string, bool) {
	if environ == nil {
		environ = os.Environ()
	}
	values := make(map[string]string, len(environ))
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		values[name] = value
	}
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

// failed renders a completed run outside the CLI's success codes as an
// observation.
//
// Such a run is recoverable: the model sees what the command said, on
// both streams, and may try something else. Anything else is the
// environment being unusable, and it goes back as an error.
func failed(err error, cap int) (poo.ToolResult, error) {
	var exited *cli.ExecError
	if errors.As(err, &exited) {
		// A process that died by a signal has no exit status, so the
		// field is null, as it is in bash's envelope.
		var code *int
		if exited.ExitCode >= 0 {
			code = &exited.ExitCode
		}
		text, structured := bash.Envelope(bash.Observation{
			Returncode: code,
			Output:     string(exited.Stdout),
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

// issuesText renders schema issues for the model.
//
// A path is an RFC 6901 pointer into the arguments, so the model can see
// which argument it got wrong.
func issuesText(issues []schema.StructuralIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		path := issue.Path
		if path == "" {
			path = "arguments"
		}
		parts = append(parts, path+": "+issue.Message)
	}
	return strings.Join(parts, "; ")
}

// marshal encodes a value, falling back to an empty object.
func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
