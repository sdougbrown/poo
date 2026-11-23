package poo

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed subscribe_feed.md
var subscribeFeedSectionText string

// subscribeFeedSection is the model's guidance about the feeds it may
// start.
//
// The section names the feeds the session loads and does not start: they
// run only once the model subscribes to one, so the model has to know what
// it may ask for and what each feed's scope means.
var subscribeFeedSection = prompt.MustCompile(SubscribeFeedToolName, subscribeFeedSectionText, nil)

// SubscribeFeedToolName is the name of the core's tool for starting a feed
// the session does not start itself.
const SubscribeFeedToolName = "subscribe_feed"

// SubscribeFeedTool is the core's own tool for starting an optional feed.
//
// The tool only starts work the session declared and the operator loaded:
// the feed's command and its environment come from the manifest, and the
// model supplies the scope values the manifest's schema accepts and
// nothing else. The tool is read-only for that reason — it reaches no file
// and runs nothing of its own — and the feed it starts is listed and
// stopped like any other.
//
// A consumer does not build one: the core registers the tool when the
// session loads an optional feed. The type is exported so a prompt builder
// can ask the tool for its section.
type SubscribeFeedTool struct {
	// session is the session the tool belongs to.
	session *Session
}

// Spec describes the tool.
func (t *SubscribeFeedTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        SubscribeFeedToolName,
		Description: "Start one of the session's optional feeds, with the values it watches. It runs until cancel_job stops it.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "feed": {"type": "string", "description": "The feed's name, as the section that lists the optional feeds spells it."},
    "scope": {"type": "object", "description": "The values the feed watches, keyed by the scope fields the feed declares.", "additionalProperties": true},
    "prompt": {"type": "string", "description": "A note about what this instance watches for. Reserved: the session accepts it and does not read it."}
  },
  "required": ["feed"]
,"additionalProperties":false}`),
		Class: ClassReadOnly,
	}
}

// Prepare decodes the call's arguments.
func (t *SubscribeFeedTool) Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error) {
	var a struct {
		Feed  string            `json:"feed"`
		Scope map[string]string `json:"scope"`
		// Prompt is part of the call's shape and is not the instance's
		// state: the feed starts from the manifest's command and the scope
		// the call supplies.
		Prompt string `json:"prompt"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, InvalidArgsf("%s: %v", SubscribeFeedToolName, err)
		}
	}
	if a.Feed == "" {
		return nil, InvalidArgsf("%s: the feed is required", SubscribeFeedToolName)
	}
	return &subscribeFeedPrepared{feed: a.Feed, scope: a.Scope}, nil
}

// Call starts the feed, and reports the instance the call reached.
//
// The result names the instance the model subscribed to and the id the
// jobs tool lists it under, so the model can stop it with cancel_job.
func (t *SubscribeFeedTool) Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error) {
	p, ok := in.(*subscribeFeedPrepared)
	if !ok {
		return ToolResult{}, InvalidArgsf("%s: the call was not prepared", SubscribeFeedToolName)
	}
	instance, started, err := t.session.subscribeFeed(p.feed, p.scope)
	if err != nil {
		return ToolResult{}, err
	}
	state := "is running"
	if !started {
		state = "is already running"
	}
	label, _ := FeedInstance(Origin(instance))
	return ToolResult{Text: fmt.Sprintf("The feed %s %s, and the jobs tool lists it as %s.", label, state, instance)}, nil
}

// PromptSection renders the section that lists the feeds the model may
// start.
func (t *SubscribeFeedTool) PromptSection(env prompt.Env) string {
	return subscribeFeedSection.Render(env)
}

// subscribeFeedPrepared is a prepared subscribe_feed call.
type subscribeFeedPrepared struct {
	// feed is the feed's name.
	feed string
	// scope are the values the call supplies.
	scope map[string]string
}

// Summary describes the call.
func (p *subscribeFeedPrepared) Summary() string {
	if len(p.scope) == 0 {
		return p.feed
	}
	return feedInstanceLabel(p.feed, p.scope)
}

// registerSubscribeTool adds the tool when the session loads a feed the
// model may start.
//
// A session with no optional feed offers no way to start one: the tool
// would list nothing and every call would refuse.
func (s *Session) registerSubscribeTool() error {
	if len(s.optionalFeeds) == 0 {
		return nil
	}
	if _, taken := s.opts.Tools[SubscribeFeedToolName]; taken {
		return fmt.Errorf("poo: the tool %q is the core's own", SubscribeFeedToolName)
	}
	if s.opts.Tools == nil {
		s.opts.Tools = make(map[string]Tool, 1)
	}
	s.opts.Tools[SubscribeFeedToolName] = &SubscribeFeedTool{session: s}
	s.opts.ToolOrder = append(s.opts.ToolOrder, SubscribeFeedToolName)
	return nil
}
