package cliregistry

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	registry "github.com/sdougbrown/cli-registry"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/internal/schematext"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
)

// CallTool runs one registered tool.
//
// The call is side-effecting: an arbitrary command can change anything,
// so the permission policy decides about it the way it decides about
// bash.
type CallTool struct {
	// catalog is the set of tools the call may run.
	catalog *Catalog
	// search reports that the session offers find_tool over the
	// catalog, so an unknown name can point at the search.
	search bool
	// sessionTools are the names of the tools the session offers. A
	// refusal that says "call it directly" is only true for a name
	// this list holds; nil keeps the binary-scoped refusal for a tool
	// built without the session's names.
	sessionTools []string
	// executor runs the tool. Nil means the local executor, the way it
	// does for bash.
	executor exec.Executor
}

// NewCall returns the tool that runs a catalog entry.
func NewCall(catalog *Catalog) *CallTool {
	return &CallTool{catalog: catalog}
}

// WithExecutor gives the tool the executor its commands run through.
//
// It is the executor bash uses, so a registered tool runs in the same
// place and under the same process rules as a shell command.
func (t *CallTool) WithExecutor(e exec.Executor) *CallTool {
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

// WithSessionTools gives the tool the names of the tools the session
// offers beside it.
//
// The built-in refusal tells the model to call a tool directly, which
// only works when the session offers it: a name the settings disabled,
// or one a profile did not include, gets the catalog's hint instead.
func (t *CallTool) WithSessionTools(names []string) *CallTool {
	t.sessionTools = names
	return t
}

// callDescription is what the tool says it is for.
//
// The name alone read as a dispatcher, so the description says what the call
// cannot do: a tool the session offers beside it, the call itself, and a
// shell are all called directly, never through it.
const callDescription = CallToolName + " runs one of the commands registered in the catalog, and only those. " +
	"It is not a general dispatcher: the tools this session offers beside it are called directly, and " +
	CallToolName + " cannot run them, itself, a shell, or anything the catalog does not list."

// CanBackgroundRunningCall reports that a call's running command can
// finish under a job without restarting: the process runs detached from
// the run, the way a bash command does.
func (*CallTool) CanBackgroundRunningCall() bool { return true }

// Spec describes the tool.
func (t *CallTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        CallToolName,
		Description: callDescription,
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "One line of the catalog: the registered command's name."},
    "args": {"type": "object", "additionalProperties": true, "description": "The command's arguments, as the catalog line describes them."}
  },
  "required": ["command"]
,"additionalProperties":false}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "command",
		// A registered tool runs where bash runs, so it leaves the turn
		// the same way. Background true turns the call into a job, and
		// the result arrives as a message.
		Background: poo.BackgroundAllowed,
	}
}

// The prompt section the tool brings.
//
// The catalog has a section of its own, which the session renders; the
// tool adds what the catalog cannot say: what a call does when it fails,
// and where its output goes.
//
//go:embed call.md
var callSectionText string

var callSection = prompt.MustCompile(CallToolName, callSectionText, nil)

// PromptSection renders the tool's guidance.
func (t *CallTool) PromptSection(env prompt.Env) string { return callSection.Render(env) }

// callArgs are the tool's arguments.
type callArgs struct {
	// Command is the registered command to run.
	Command string `json:"command"`
	// Args are the command's arguments, as a JSON object.
	Args json.RawMessage `json:"args,omitempty"`
}

// builtinToolNames are the tools poo offers beside the catalog.
//
// A model that read the catalog's tool as a dispatcher reached for it with
// one of these, so the refusal names the mistake rather than the catalog's
// size. cmd/poo's test pins the list to the tools the binary builds.
var builtinToolNames = []string{
	"bash", "read", "grep", "history", "edit", "write", "fetch", "codemode",
	"jobs", "cancel_job", "find_tool", "call_mcp", "subscribe_feed", "finish",
}

// preparedCall is a validated call_cli call.
type preparedCall struct {
	// command is the registered command.
	command string
	// args are the command's arguments.
	args map[string]any
	// plan is the command the call runs.
	plan *registry.Plan
	// key is the call's canonical input.
	key json.RawMessage
}

// Summary describes the call for a human reader.
func (p *preparedCall) Summary() string {
	return strings.TrimSpace(p.command + " " + string(marshal(p.args)))
}

// CanonicalKey returns the call's prepared input.
func (p *preparedCall) CanonicalKey() json.RawMessage { return p.key }

// EvidenceArguments returns the arguments a decision model may see.
func (p *preparedCall) EvidenceArguments() json.RawMessage { return p.key }

// Prepare validates the arguments and plans the command.
//
// The command has to be in the catalog, and the arguments have to fit the
// command's schema, including the library's rule about a value a
// separate-value binding would put where the command parses options.
// Both are refused here, before a process exists, so a malformed call
// never reaches a command and the model sees the reason instead of the
// command's failure. The plan is the command and nothing else: no
// environment is read and no process is started until Call.
func (t *CallTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	if t.catalog == nil {
		return nil, poo.InvalidArgsf("%s: no CLI registry is configured", CallToolName)
	}
	var a callArgs
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("%s: %v", CallToolName, err)
	}
	entry, ok := t.catalog.Lookup(a.Command)
	if !ok {
		return nil, t.unknownCommand(a.Command)
	}
	args := map[string]any{}
	if len(a.Args) > 0 {
		// An omitted args is an empty object, because a command that takes
		// no arguments is called with the command alone. An explicit null
		// is not the same thing: the property's type is an object.
		if string(a.Args) == "null" {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object, not null", CallToolName, a.Command)
		}
		if err := json.Unmarshal(a.Args, &args); err != nil {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object: %v", CallToolName, a.Command, err)
		}
	}
	plan, err := t.catalog.Plan(a.Command, args)
	if err != nil {
		return nil, planError(CallToolName, a.Command, entry.Schema, err)
	}
	key := marshal(struct {
		Command string         `json:"command"`
		Args    map[string]any `json:"args"`
	}{a.Command, args})
	return &preparedCall{command: a.Command, args: args, plan: plan, key: key}, nil
}

// unknownCommand answers a command the catalog does not hold.
//
// Two misreadings cost a model a round trip each, so they are answered
// with the correction: call_cli nested inside itself, and a tool the
// session offers directly passed to call_cli as if it were registered.
// Everything else is told what the catalog does hold, as far as the
// prompt lists it.
func (t *CallTool) unknownCommand(command string) error {
	switch {
	case command == CallToolName || command == "call_tool":
		return poo.InvalidArgsf("%s: %q is this tool; pass a registered command's name as \"command\", "+
			"not %s itself", CallToolName, command, CallToolName)
	case t.isRefusableBuiltin(command):
		return poo.InvalidArgsf("%s: %q is a built-in tool; call it directly instead of through %s",
			CallToolName, command, CallToolName)
	}
	if t.catalog.Len() == 0 {
		return poo.InvalidArgsf("%s: no tool is registered", CallToolName)
	}
	return poo.InvalidArgsf("%s: no tool named %q; %s",
		CallToolName, command, t.catalog.NamesHint(t.search))
}

// isRefusableBuiltin reports whether the refusal may name the command
// a built-in tool: the command is one of the binary's tools, and the
// session offers it too.
func (t *CallTool) isRefusableBuiltin(command string) bool {
	if !slices.Contains(builtinToolNames, command) {
		return false
	}
	return t.sessionTools == nil || slices.Contains(t.sessionTools, command)
}

// planError reports a call the tool's own definition refused.
//
// A validation failure is the model's to correct, so it comes back as
// an invalid call: the schema, and a value a separate-value binding
// would put where the command reads an option, are both the library's
// checks and both land here. Anything else is the manifest being
// unusable — a binding that cannot be built, or a working directory that
// does not resolve — and it is reported as it is. tool is the tool over the
// catalog, and name the entry whose schema refused the arguments; a direct
// tool is named after its entry, so it names it once.
func planError(tool, name string, schema map[string]any, err error) error {
	var invalid *registry.ValidationError
	if errors.As(err, &invalid) {
		// The refusal is the model's to correct, so it renders in the
		// schema's own vocabulary: the shared renderer, aimed at the
		// tool's schema.
		return poo.InvalidArgsf("%s: %s", subject(tool, name), schematext.IssuesText(name, schema, invalid.Issues))
	}
	return fmt.Errorf("%s: %w", subject(tool, name), err)
}

// Call runs the planned command through the session's executor.
func (t *CallTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return runCall(ctx, CallToolName, t.executor, in, env)
}

// runCall runs a prepared value the tool named tool made.
//
// Both tools over the catalog run their plan through the same runner, so an
// entry offered directly runs exactly as the call over the catalog runs it,
// and neither can drift from the other. The tool's name is the prefix a
// failure carries: a call names itself and the command it runs, and a direct
// tool is already named after its command.
func runCall(ctx context.Context, tool string, executor exec.Executor, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*preparedCall)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("%s: prepared value is not a %s call", tool, tool)
	}
	if p.plan == nil {
		return poo.ToolResult{}, fmt.Errorf("%s: the call has no plan", subject(tool, p.command))
	}
	return runner{executor: executor}.run(ctx, tool, p, env)
}

// subject names what a failure is about.
//
// A call is the tool and the command inside it; a direct tool is the command
// alone, so it says its name once.
func subject(tool, command string) string {
	if tool == command {
		return tool
	}
	return tool + ": " + command
}

// runner runs a planned command through a session's executor.
type runner struct {
	// executor runs the command. Nil means the local executor, the way it
	// does for bash.
	executor exec.Executor
}

// run runs one planned command.
//
// The command runs where bash runs and under the same rules: the
// session's executor (the sandbox, when the settings name one), the
// session's working directory unless the manifest fixes one, an
// environment looked up in the session's scrubbed one with a manifest's
// session:id resolving to the session's own ID, and the run's
// context, so a cancelled run stops the tool and everything it started.
//
// The output is the tool's stdout and stderr with its exit status, in the
// envelope bash's observation uses. An exit outside the tool's success
// codes and a timeout are recoverable failures: the model sees what the
// command said and may try something else.
func (r runner) run(ctx context.Context, tool string, p *preparedCall, env poo.ToolEnv) (poo.ToolResult, error) {
	// A manifest's env:NAME is resolved against the session's scrubbed
	// environment, never the host's: a name the scrub removed or the
	// passthrough list left out is a reference the tool cannot have. A
	// manifest's session:id resolves to this call's session, at the
	// call, never when the settings loaded.
	environ, err := p.plan.ResolveEnvScoped(exec.Lookup(env.Env), exec.SessionScope(env.SessionID))
	if err != nil {
		return poo.ToolResult{}, fmt.Errorf("%s: %w", subject(tool, p.command), err)
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
	if res, ok := stoppedResult(ctx.Err(), registry.Output{}, cap); ok {
		return res, nil
	}

	executor := r.executor
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
	proc, err := executor.Start(runCtx, exec.Command{
		Argv:   p.plan.Argv,
		Dir:    dir,
		Env:    environ,
		Stdin:  p.plan.Stdin,
		Stderr: stderr,
	}, stdout)
	if err != nil {
		return poo.ToolResult{}, fmt.Errorf("%s: %w", subject(tool, p.command), err)
	}
	code, waitErr := proc.Wait()

	out := registry.Output{
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

	// A failed tool's exit and diagnostics take precedence over an incomplete write.
	res, err := p.plan.Finish(out)
	if err != nil {
		return failed(tool, err, cap)
	}
	if errors.Is(waitErr, exec.ErrStdinIncomplete) {
		return poo.ToolResult{}, fmt.Errorf("%s: %w", subject(tool, p.command), exec.ErrStdinIncomplete)
	}
	// A tool whose manifest decodes JSON returns that JSON as the
	// result: the decoded value is the output, and the bash
	// exit-code envelope is bypassed. Two edges fall through to the
	// envelope (both pinned): a decoded null, and a decoded result
	// whose marshalled form is longer than the observation cap,
	// counted in characters the way the cap is sized. A successful
	// call's stderr is diagnostics the model still needs, so it rides
	// on the decoded text after a newline, and a cut capture says so
	// the way the envelope does.
	if res.Decoded != nil {
		// The encoder keeps the bytes a Marshal would HTML-escape, so
		// a value that holds & or < comes back as the tool printed
		// it; it sorts map keys the way Marshal does.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(res.Decoded); err != nil {
			return failed(tool, err, cap)
		}
		// The encoder appends a newline; drop it.
		buf.Truncate(buf.Len() - 1)
		raw := buf.Bytes()
		if caps.RuneCount(string(raw)) <= cap {
			text := string(raw)
			if len(res.Stderr) > 0 {
				text += "\n" + string(res.Stderr)
			}
			if res.Truncated {
				text += "\ntruncated: " + bash.TruncationHint
			}
			return poo.ToolResult{Text: text, Structured: raw}, nil
		}
		// An over-budget decoded result falls through to the envelope
		// below, which elides the middle and keeps the exit code.
	}
	returncode := res.ExitCode
	text, structured := bash.Envelope(bash.Observation{
		ExitCode:  &returncode,
		Output:    string(res.Stdout),
		Stderr:    string(res.Stderr),
		Cap:       cap,
		Truncated: res.Truncated,
	})
	return poo.ToolResult{Text: text, Structured: structured}, nil
}

// stoppedResult renders a call the run's context ended, and reports
// whether it did.
//
// Whatever the command wrote before it was stopped is kept: it is the
// output the model needs to see why the call did not finish. A deadline
// is a timeout; a cancellation is a failure without one.
func stoppedResult(err error, out registry.Output, cap int) (poo.ToolResult, bool) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		text, structured := bash.Envelope(bash.Observation{
			TimedOut:  true,
			Output:    string(out.Stdout),
			Stderr:    string(out.Stderr),
			Cap:       cap,
			Truncated: out.Truncated,
		})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, true
	case errors.Is(err, context.Canceled):
		text, structured := bash.Envelope(bash.Observation{
			Output:    string(out.Stdout),
			Stderr:    string(out.Stderr),
			Cap:       cap,
			Truncated: out.Truncated,
		})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, true
	}
	return poo.ToolResult{}, false
}

// failed renders a completed run outside the tool's success codes as an
// observation.
//
// Such a run is recoverable: the model sees what the command said, on
// both streams, and may try something else. Anything else is the
// environment being unusable, and it goes back as an error, named after the
// tool that ran it.
func failed(tool string, err error, cap int) (poo.ToolResult, error) {
	var exited *registry.ExecError
	if errors.As(err, &exited) {
		// A process that died by a signal has no exit status, so the
		// field is null, as it is in bash's envelope.
		var code *int
		if exited.ExitCode >= 0 {
			code = &exited.ExitCode
		}
		text, structured := bash.Envelope(bash.Observation{
			ExitCode:  code,
			Output:    string(exited.Stdout),
			Stderr:    string(exited.Stderr),
			Cap:       cap,
			Truncated: exited.Truncated,
		})
		return poo.ToolResult{Text: text, Structured: structured, IsError: true}, nil
	}
	return poo.ToolResult{}, fmt.Errorf("%s: %w", tool, err)
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
