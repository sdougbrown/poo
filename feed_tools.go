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
// it may ask for and what each feed's scope means. Its data carries the
// instance cap beside the feeds, so the prose names the cap the
// refusal renders.
var subscribeFeedSection = prompt.MustCompile(SubscribeFeedToolName, subscribeFeedSectionText, func(e prompt.Env) any {
	return struct {
		MaxInstances  int
		OptionalFeeds []prompt.Feed
	}{MaxFeedInstances, e.OptionalFeeds}
})

// SubscribeFeedToolName is the name of the core's tool for starting a feed
// the session does not start itself.
const SubscribeFeedToolName = "subscribe_feed"

// defaultPromptMinNoProbabilityText is the schema's description of the
// threshold a prompt takes, written from the default beside the
// filter-prompt type so the two cannot disagree.
var defaultPromptMinNoProbabilityText = fmt.Sprintf(
	"The probability of no at which a message is dropped, used with the prompt. The feed's own value applies when it names one, and %v when it names none.",
	DefaultPromptMinNoProbability,
)

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
		Description: "Start one of the session's optional feeds, with the values it watches. It runs until cancel_job stops it. A prompt screens the feed's messages through the session's decision model.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "feed": {"type": "string", "description": "The feed's name, as the section that lists the optional feeds spells it."},
    "scope": {"type": "object", "description": "The values the feed watches, keyed by the scope fields the feed declares.", "additionalProperties": true},
    "prompt": {"type": "string", "description": "The question to ask the decision model about each message, replacing the feed's own question while it is set. Phrase it so yes means deliver. An empty string clears it, and the feed's own question applies again."},
    "min_no_probability": {"type": "number", "exclusiveMinimum": 0.5, "maximum": 1, "description": "` + defaultPromptMinNoProbabilityText + `"}
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
		// Prompt is the question the call sets on the instance, and nil
		// when the call names none: a plain subscription leaves the
		// instance's own question alone.
		Prompt *string `json:"prompt"`
		// MinNoProbability is the drop threshold the call names beside
		// the prompt, and nil when it names none.
		MinNoProbability *float64 `json:"min_no_probability"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, InvalidArgsf("%s: %v", SubscribeFeedToolName, err)
		}
	}
	if a.Feed == "" {
		return nil, InvalidArgsf("%s: the feed is required", SubscribeFeedToolName)
	}
	var prompt *FeedFilterPrompt
	hasMinNoProbability := false
	if a.Prompt != nil || a.MinNoProbability != nil {
		prompt = &FeedFilterPrompt{}
		if a.Prompt != nil {
			prompt.Question = *a.Prompt
		}
		if a.MinNoProbability != nil {
			prompt.MinNoProbability = *a.MinNoProbability
			hasMinNoProbability = true
		}
	}
	return &subscribeFeedPrepared{feed: a.Feed, scope: a.Scope, prompt: prompt, hasMinNoProbability: hasMinNoProbability}, nil
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
	instance, started, err := t.session.subscribeFeed(p.feed, p.scope, p.prompt, p.hasMinNoProbability)
	if err != nil {
		return ToolResult{}, err
	}
	state := "is running"
	if !started {
		state = "is already running"
	}
	label, _ := FeedInstance(Origin(instance))
	text := fmt.Sprintf("The feed %s %s, and the jobs tool lists it as %s.", label, state, instance)
	// The call's result says what the prompt did, so a model that set one
	// knows the filter is the one it wrote, and not the manifest's.
	if p.prompt != nil {
		if p.prompt.Question == "" {
			text += " The filter prompt is cleared."
		} else {
			text += " The filter prompt is set, and each message is asked that question."
		}
		if notice := filterOverrideNotice(t.session.optionalFeeds[p.feed], p.prompt); notice != "" {
			text += " " + notice
		}
	}
	return ToolResult{Text: text}, nil
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
	// prompt is the filter prompt the call sets, and nil when it names
	// none.
	prompt *FeedFilterPrompt
	// hasMinNoProbability reports whether the call named a
	// min_no_probability at all: the value's zero is the "named none"
	// convention, so an explicit zero is out of range and is refused,
	// not folded into the clear.
	hasMinNoProbability bool
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
