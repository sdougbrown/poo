package cliregistry

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
)

// Tools returns one tool per catalog entry, in name order.
//
// A catalog short enough to show is offered entry by entry instead of behind
// a call: the model sees each entry's own name, description and argument
// schema in the tool list, so a call that names the command inside its
// arguments would say the same thing twice. The tools share this tool's
// catalog and executor, so an entry offered this way runs exactly where and
// how the call over the catalog runs it, and reaches exactly the entries the
// call can reach.
func (t *CallTool) Tools() []poo.Tool {
	entries := t.catalog.Entries()
	if len(entries) == 0 {
		return nil
	}
	tools := make([]poo.Tool, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, &entryTool{catalog: t.catalog, entry: entry, executor: t.executor})
	}
	return tools
}

// entryTool is one catalog entry offered as a tool of its own.
//
// The entry's schema is the tool's schema: the model passes the command's
// arguments directly, with nothing wrapping them. Everything the entry means
// beyond that — its argv, its bindings, its timeout, its environment and the
// shape of its observation — is the catalog's, so this tool is the call over
// the catalog with a shorter argument path.
type entryTool struct {
	// catalog is the set of tools the entry belongs to.
	catalog *Catalog
	// entry is the registered tool this one offers.
	entry Entry
	// executor runs the entry. Nil means the local executor, the way it
	// does for bash.
	executor exec.Executor
}

// Spec describes the tool.
func (t *entryTool) Spec() poo.ToolSpec {
	schema := entrySchema(t.entry.Schema)
	spec := poo.ToolSpec{
		Name:        t.entry.Name,
		Description: t.entry.Description,
		Schema:      schema,
		Class:       poo.ClassSideEffecting,
		// A registered tool runs where bash runs, so it leaves the turn
		// the same way: background true turns the call into a job.
		Background: poo.BackgroundAllowed,
	}
	// poo adds its own background argument to a tool that may run in the
	// background and refuses a tool whose schema already defines it. A
	// manifest that declares the name keeps its own argument, and the entry
	// runs in the turn rather than failing the session.
	if poo.SchemaDefinesBackground(schema) {
		spec.Background = poo.BackgroundNever
	}
	return spec
}

// Prepare validates the arguments and plans the command.
//
// The arguments are the entry's own object, not a command name wrapping them:
// a direct tool has no command to name. The plan is the command and nothing
// else, and a value that does not fit the entry's schema is refused here,
// before a process exists.
func (t *entryTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	if t.catalog == nil {
		return nil, poo.InvalidArgsf("%s: no CLI registry is configured", t.entry.Name)
	}
	args, err := entryArgs(t.entry.Name, raw)
	if err != nil {
		return nil, err
	}
	plan, err := t.catalog.Plan(t.entry.Name, args)
	if err != nil {
		return nil, planError(t.entry.Name, t.entry.Name, t.entry.Schema, err)
	}
	key := marshal(struct {
		Command string         `json:"command"`
		Args    map[string]any `json:"args"`
	}{t.entry.Name, args})
	return &preparedCall{command: t.entry.Name, args: args, plan: plan, key: key}, nil
}

// Call runs the planned command through the session's executor.
func (t *entryTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return runCall(ctx, t.entry.Name, t.executor, in, env)
}

// entryArgs decodes a direct tool's arguments.
//
// The whole object is the command's arguments. An omitted object is the empty
// one, because a command that takes no arguments is called with nothing to
// say; an explicit null is not the same thing, because the schema's type is an
// object.
func entryArgs(name string, raw json.RawMessage) (map[string]any, error) {
	args := map[string]any{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return args, nil
	}
	if string(trimmed) == "null" {
		return nil, poo.InvalidArgsf("%s: the arguments must be an object, not null", name)
	}
	if err := json.Unmarshal(trimmed, &args); err != nil {
		return nil, poo.InvalidArgsf("%s: the arguments must be an object: %v", name, err)
	}
	return args, nil
}

// entrySchema returns the JSON schema the model reads for an entry's arguments.
//
// A manifest that declares no schema takes the empty object schema, so the
// tool list never carries a tool whose arguments are unconstrained and
// undocumented.
func entrySchema(schema map[string]any) json.RawMessage {
	if len(schema) == 0 {
		return json.RawMessage(emptyEntrySchema)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(emptyEntrySchema)
	}
	return raw
}

// emptyEntrySchema is the schema of an entry that declares no arguments.
const emptyEntrySchema = `{"type":"object","properties":{},"additionalProperties":false}`
