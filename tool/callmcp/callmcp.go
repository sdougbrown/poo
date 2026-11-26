// Package callmcp offers the MCP servers' tools to the model.
//
// A session's MCP servers are its own processes, started with it and
// stopped with it. Their tools are not offered one by one, because the
// session cannot know them before the servers start and a server may list
// any number of them: the model reaches them through one dispatcher.
// call_mcp names the tool as <server>/<tool> and passes its arguments, and
// find_tool searches them beside the registered commands.
//
// # One offering
//
// The tool takes the servers a session started, never the manifests: the
// settings decide which servers a profile selects, and this package decides
// what the model reads and what it can call. A tool the servers did not list
// is not callable, and the refusal says where to look for the name instead.
//
// # Hints
//
// The annotations a server attaches to a tool ride along as hints, in the
// catalog section and in a search's results. They are never a permission
// class: a server's claim about itself is not a decision about what this
// call may do, and the permission policy is the session's own.
//
// # The answer
//
// What a tool answers is what the model reads: the text contents joined, the
// structured content when it fits under the observation cap, and an image as
// an attachment when the run's model reads one. Anything dropped is said in
// one line, so the model never wonders what a call returned.
package callmcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/internal/schematext"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/prompt"
)

// CallToolName is the name of the tool that calls an MCP tool.
const CallToolName = "call_mcp"

// NameArgument is the argument that names the tool a call runs.
const NameArgument = "name"

// Server is one MCP server a session started.
//
// The group a session launched implements it; a test's stand-in does too. The
// tool reads the tools the server listed and calls one of them, by the
// server's own name for it: qualifying the name is the tool's own business,
// because the qualified name is what the model reads and writes.
type Server interface {
	// Name is the server's declared name.
	Name() string
	// Tools returns the tools the server listed at startup.
	Tools() []mcp.Tool
	// CallTool calls one of them, by the server's own name for it.
	CallTool(ctx context.Context, tool string, args json.RawMessage) (mcp.CallResult, error)
}

// Tool runs one MCP tool.
//
// The call is side-effecting: a tool server may change anything, so the
// permission policy decides about it the way it decides about bash. Its
// primary argument is the qualified name, so a rule can name the server a call
// reaches — "fixture/" allows every tool of the fixture server — without
// naming each tool.
type Tool struct {
	// servers are the servers the session started, in selection order.
	servers []Server
	// offered are this call's own entries, read from the session's snapshot
	// when the tool is built and in qualified-name order.
	offered []cli.Offered
	// search reports that the session offers find_tool over the MCP tools,
	// so an unknown name can point at the search, and a long offering can be
	// left to it.
	search bool
	// inputs are the modalities the session's model reads, for a caller that
	// names no model of its own.
	inputs map[poo.Modality]bool
}

// New returns the tool over the servers a session started.
//
// The offered entries are the session's snapshot, the one value the search
// over the same offering reads: this call lists the entries it dispatches, and
// find_tool searches all of them. A build hands it an empty snapshot, because
// a build starts no server; the attach replaces the tool with the session's
// own.
func New(servers []Server, offered []cli.Offered) *Tool {
	return &Tool{servers: servers, offered: dispatched(offered)}
}

// Entries returns the entries one server's listed tools become.
//
// This is the one place a server's tools become what the model reads: the
// entries a search returns, a hit names, and a catalog line lists are the same
// value, so the two never disagree about a tool's name, arguments or hints.
//
// A tool's schema is the server's own and may be absent. A schema that does not
// parse is no hint at all, so the entry carries the description alone and the
// call is still the server's to judge.
func Entries(server string, tools []mcp.Tool) []cli.Offered {
	entries := make([]cli.Offered, 0, len(tools))
	for _, tool := range tools {
		entry := cli.Offered{
			Name:        qualify(server, tool.Name),
			Description: tool.Description,
			Dispatcher:  CallToolName,
			Argument:    NameArgument,
			ReadOnly:    tool.Annotations.ReadOnlyHint,
			Destructive: tool.Annotations.DestructiveHint,
		}
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &entry.Schema); err != nil {
				entry.Schema = nil
			}
		}
		entry.Args = schematext.ArgsHint(entry.Schema)
		entries = append(entries, entry)
	}
	return entries
}

// Offered returns every entry the servers hold, in selection order.
//
// It is Entries over a list of servers, for a caller that holds the servers and
// not a launcher's handles: the session's snapshot asks each handle for its own
// entries with Entries, and the two agree because they are one adapter.
func Offered(servers []Server) []cli.Offered {
	var entries []cli.Offered
	for _, server := range servers {
		entries = append(entries, Entries(server.Name(), server.Tools())...)
	}
	return entries
}

// dispatched returns the entries this call dispatches, in qualified-name order.
// The snapshot also holds the entries another call runs, and they are that
// tool's to describe.
func dispatched(entries []cli.Offered) []cli.Offered {
	own := make([]cli.Offered, 0, len(entries))
	for _, entry := range entries {
		if entry.Dispatcher != CallToolName {
			continue
		}
		own = append(own, entry)
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].Name < own[j].Name })
	return own
}

// WithSearch says whether the session offers a search over the MCP tools.
func (t *Tool) WithSearch(search bool) *Tool {
	t.search = search
	return t
}

// WithInputs reports which modalities the run's model reads.
//
// A model that reads none of them is told what the tool returned instead, so
// an image costs a line and not a request the provider rejects.
func (t *Tool) WithInputs(modalities ...poo.Modality) *Tool {
	t.inputs = make(map[poo.Modality]bool, len(modalities))
	for _, modality := range modalities {
		t.inputs[modality] = true
	}
	return t
}

// callDescription is what the tool says it is for.
//
// The name alone reads as a dispatcher, so the description says what the call
// cannot do: a tool the session offers beside it, the call itself, and a shell
// are all called directly, never through it.
const callDescription = CallToolName + " runs one of the tools the MCP servers this session started offer, and only those. " +
	"It is not a general dispatcher: the tools this session offers beside it are called directly, and " +
	CallToolName + " cannot run them, itself, a shell, or anything the servers do not list."

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        CallToolName,
		Description: callDescription,
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "` + NameArgument + `": {"type": "string", "description": "One line of the MCP catalog: the tool's qualified name, \"<server>/<tool>\"."},
    "args": {"type": "object", "additionalProperties": true, "description": "The tool's arguments, as the catalog line describes them."}
  },
  "required": ["` + NameArgument + `"]
,"additionalProperties":false}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: NameArgument,
		// A tool server runs beside the session, so a call leaves the turn
		// the same way a command does. Background true turns the call into a
		// job, and the result arrives as a message.
		Background: poo.BackgroundAllowed,
	}
}

// The sections the tool brings.
//
// The call's own section says how a call is made and what it answers. The
// catalog's says what is there: the servers are the session's, so the list is
// rendered when the session attaches, and a prompt that shows the build's own
// instance carries the guidance alone.
//
//go:embed call.md
var callText string

//go:embed catalog.md
var catalogText string

var callSection = prompt.MustCompile(CallToolName, callText, nil)

// PromptSection renders the tool's guidance and the catalog behind it.
func (t *Tool) PromptSection(env prompt.Env) string {
	section := strings.TrimRight(callSection.Render(env), "\n")
	tools := t.listing()
	if len(tools) == 0 {
		return section
	}
	catalog := prompt.MustCompile("call_mcp_catalog", catalogText, func(prompt.Env) any {
		return catalogData{
			// A search pays for itself only when the offering is too long to
			// show, and a session without one lists the tools whatever their
			// number: the catalog is the only place the names are.
			Search:   t.search && len(tools) > cli.FindThreshold,
			Count:    len(tools),
			Find:     cli.FindToolName,
			Call:     CallToolName,
			Argument: NameArgument,
			Tools:    tools,
		}
	})
	return section + "\n\n" + strings.TrimRight(catalog.Render(env), "\n")
}

// catalogData is the data the catalog's section renders with.
type catalogData struct {
	// Search says the tools are behind a search rather than listed whole.
	Search bool
	// Count is the number of tools the servers offer.
	Count int
	// Find and Call are the tools that search the offering and that call one
	// of its entries, and Argument is the argument that names one.
	Find     string
	Call     string
	Argument string
	// Tools are the tools, in qualified-name order.
	Tools []cli.Offered
}

// listing returns every tool the session's servers offer, by qualified name.
func (t *Tool) listing() []cli.Offered { return t.offered }

// qualify names one tool the way the model writes it: its server's name, a
// slash, its own.
func qualify(server, tool string) string { return server + "/" + tool }

// callArgs are the tool's arguments.
type callArgs struct {
	// Name is the qualified name of the tool to call.
	Name string `json:"name"`
	// Args are the tool's arguments, as a JSON object.
	Args json.RawMessage `json:"args,omitempty"`
}

// preparedCall is a validated call_mcp call.
type preparedCall struct {
	// server and tool are the name's two halves.
	server string
	tool   string
	// name is the qualified name the model wrote, trimmed.
	name string
	// args are the arguments as the object the server reads.
	args json.RawMessage
	// key is the call's canonical input.
	key json.RawMessage
}

// Summary describes the call for a human reader.
func (p *preparedCall) Summary() string {
	return strings.TrimSpace(p.name + " " + string(p.args))
}

// CanonicalKey returns the call's prepared input.
func (p *preparedCall) CanonicalKey() json.RawMessage { return p.key }

// EvidenceArguments returns the arguments a decision model may see.
func (p *preparedCall) EvidenceArguments() json.RawMessage { return p.key }

// Prepare validates the call's arguments.
//
// The name is validated for its shape and nothing more: whether a server
// listed the tool is the answer a call gets, because the offering is the
// session's and may change under it. The arguments are the tool's own object;
// a value that does not fit its schema is the server's to refuse, and its
// refusal is what the model reads.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a callArgs
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("%s: %v", CallToolName, err)
	}
	name := strings.TrimSpace(a.Name)
	server, tool, ok := strings.Cut(name, "/")
	if !ok || strings.TrimSpace(server) == "" || strings.TrimSpace(tool) == "" {
		return nil, poo.InvalidArgsf("%s: %q is not a tool's qualified name; name one as \"<server>/<tool>\", "+
			"the way the MCP catalog writes it", CallToolName, name)
	}
	args := map[string]any{}
	trimmed := strings.TrimSpace(string(a.Args))
	if trimmed != "" {
		// An omitted args is an empty object, because a tool that takes no
		// arguments is called with the name alone. An explicit null is not
		// the same thing: the property's type is an object.
		if trimmed == "null" {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object, not null", CallToolName, name)
		}
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return nil, poo.InvalidArgsf("%s: %s: args must be an object: %v", CallToolName, name, err)
		}
	}
	if args == nil {
		return nil, poo.InvalidArgsf("%s: %s: args must be an object", CallToolName, name)
	}
	return &preparedCall{
		server: strings.TrimSpace(server),
		tool:   strings.TrimSpace(tool),
		name:   name,
		args:   marshal(args),
		key: marshal(struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		}{name, args}),
	}, nil
}

// Call runs the tool on the server that offers it.
//
// A name no server lists is the model's to correct, so it comes back as an
// invalid call that says where the names are: the search when the session
// offers one, and the prompt's catalog when it does not. A server that is not
// running is a recoverable result, because the session outlives its servers'
// own failures. And a call that reached a server is never retried, whatever
// it answered: the server may have had effects, and a repeat is the model's
// word.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*preparedCall)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("%s: prepared value is not a %s call", CallToolName, CallToolName)
	}
	server := t.server(p.server)
	if server == nil {
		return poo.ToolResult{}, poo.InvalidArgsf("%s: the session started no mcp server named %q; %s",
			CallToolName, p.server, t.namesHint())
	}
	res, err := server.CallTool(ctx, p.tool, p.args)
	switch {
	case errors.Is(err, mcp.ErrUnknownTool):
		return poo.ToolResult{}, poo.InvalidArgsf("%s: %q is no tool of the mcp server %q; %s",
			CallToolName, p.name, p.server, t.namesHint())
	case errors.Is(err, mcp.ErrServerDown):
		return poo.ToolResult{
			Text:    fmt.Sprintf("server %s is not running", p.server),
			IsError: true,
		}, nil
	case err != nil:
		return poo.ToolResult{}, fmt.Errorf("%s: %w", CallToolName, err)
	}
	return t.result(res, env), nil
}

// server returns the named server, or nil when the session started none by
// that name.
func (t *Tool) server(name string) Server {
	for _, server := range t.servers {
		if server.Name() == name {
			return server
		}
	}
	return nil
}

// namesHint says where a name the session cannot call comes from.
//
// A session that offers a search points at it, because the offering may be
// longer than the prompt lists. One that does not points at the prompt's own
// catalog, which is the only place its names are.
func (t *Tool) namesHint() string {
	if t.search {
		return "call " + cli.FindToolName + " with what you want to do"
	}
	return "the MCP catalog in the prompt lists the tools this session offers"
}

// result renders what a call answered.
//
// The text the server returned is the answer, and it is capped the way a
// command's observation is: a call that answered more than the model may read
// has its middle elided, and the count says how much. The structured content
// rides beside the text when it is valid JSON and fits under the same cap; a
// larger one is left out whole, because a structured value cut in half is not
// the value the server returned. An image becomes an attachment only when the
// run's model reads one. Whatever is left out is said in one line.
func (t *Tool) result(res mcp.CallResult, env poo.ToolEnv) poo.ToolResult {
	cap := env.Settings.ObservationCapChars
	if cap <= 0 {
		cap = caps.ObservationMax
	}
	var notes []string
	text := elide(strings.Join(res.Text, "\n"), cap)

	var structured json.RawMessage
	if len(res.Structured) > 0 {
		switch {
		case !json.Valid(res.Structured):
			notes = append(notes, "the tool's structured content is not JSON, so it was left out")
		case caps.RuneCount(string(res.Structured)) > cap:
			notes = append(notes, fmt.Sprintf("the tool's structured content is longer than the %d characters a result may carry, so it was left out", cap))
		default:
			structured = res.Structured
		}
	}

	var attachments []poo.Attachment
	for _, image := range res.Images {
		if !poo.IsMediaType(poo.ModalityImage, image.MediaType) {
			notes = append(notes, fmt.Sprintf("the tool returned an image of type %q, which poo cannot hand a model, so it was left out", image.MediaType))
			continue
		}
		if !t.accepts(poo.ModalityImage, env) {
			notes = append(notes, fmt.Sprintf("the tool returned an image (%s, %d bytes), and this run's model does not accept image input, so it was left out",
				image.MediaType, len(image.Data)))
			continue
		}
		attachments = append(attachments, poo.Attachment{
			Modality:  poo.ModalityImage,
			MediaType: image.MediaType,
			Data:      image.Data,
		})
	}

	for _, note := range notes {
		if text == "" {
			text = note
			continue
		}
		text += "\n" + note
	}
	return poo.ToolResult{Text: text, Structured: structured, Attachments: attachments, IsError: res.IsError}
}

// accepts reports whether the model this call runs for reads a modality.
//
// The run's model decides it, so a session that switched models answers for
// the model it has now; the tool's own answer is the fallback for a caller
// that names no model.
func (t *Tool) accepts(modality poo.Modality, env poo.ToolEnv) bool {
	if env.Model.Inputs != nil {
		return env.Model.Reads(modality)
	}
	return t.inputs[modality]
}

// elide keeps the head and the tail of an answer that is longer than the cap.
//
// The two halves share the cap, so one call's answer keeps its size whatever
// the server returned, and the note in the middle says how much of it the
// model is not reading.
func elide(text string, cap int) string {
	total := caps.RuneCount(text)
	if total <= cap {
		return text
	}
	head, tail := caps.SplitRunes(text, cap/2)
	return head + fmt.Sprintf("\n[the middle of the answer was elided: %d of %d characters]\n", total-2*(cap/2), total) + tail
}

// decodeStrict decodes one JSON value and nothing else.
//
// A second value, or anything after the first, is a malformed call: the model
// sent more than it meant to, and taking the first object would silently call
// something else.
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
