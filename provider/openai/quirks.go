package openai

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// The quirk framework.
//
// A quirk profile is a named set of changes one model needs: the way
// its request is shaped and the way its response is repaired. The
// model info names the profiles it needs, and the transport applies
// them in that order.
//
// A profile without a recorded fixture and a test does not belong here.

// The names of the profiles the transport ships.
//
// A model info names them, so the names are part of the contract
// between the catalog and the transport.
const (
	// QuirkThinkTags strips the thinking a model writes in its own tags
	// and makes it a thinking block.
	QuirkThinkTags = "think_tags"
	// QuirkChatTemplateThinking asks a model to think through its chat
	// template.
	QuirkChatTemplateThinking = "chat_template_thinking"
	// QuirkSimplifyToolSchemas reduces the tool schemas to what a weak
	// schema parser reads.
	QuirkSimplifyToolSchemas = "simplify_tool_schemas"
	// QuirkDegenerateStream notes a stream that repeats itself. The
	// note is logged and nothing else: the stream runs on.
	QuirkDegenerateStream = "degenerate_stream"
	// QuirkTextToolCalls reads the tool calls a model writes as text.
	QuirkTextToolCalls = "text_tool_calls"
)

// quirkProfile is one named quirk.
//
// A profile has two hooks: one shapes the request before it goes out,
// the other repairs the response as it is read. A profile that has
// nothing to do at one of the two points leaves that hook nil.
type quirkProfile struct {
	// name is the profile's name, as a model info spells it.
	name string
	// shapeRequest changes the request before it is serialized.
	shapeRequest func(*requestShape)
	// repairResponse changes the response as it is read.
	repairResponse func(*responseRepair)
}

// requestShape is the request as it is shaped, before it is serialized.
//
// A quirk changes the request here: the name of the reasoning
// parameter, the chat template arguments, the sampling parameters and
// the tool schemas.
type requestShape struct {
	// request is the wire shape of the request.
	request *chatRequest
	// params holds the named parameters the request carries.
	params map[string]json.RawMessage
	// claimed holds the parameter paths the model info's controls
	// already named. A quirk may add what the model info did not, never
	// override it.
	claimed map[string]bool
}

// setParam sets a named parameter.
//
// A name may be a path, e.g. "chat_template_kwargs.enable_thinking",
// and the value is then nested under the parts before it. A parameter
// that is already there keeps the parts another call set.
func (s *requestShape) setParam(name string, value any) {
	if s.claimed[name] {
		return
	}
	if s.claimed == nil {
		s.claimed = map[string]bool{}
	}
	s.claimed[name] = true
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		s.setRaw(name, value)
		return
	}
	var leaf any = value
	for i := len(parts) - 1; i > 0; i-- {
		leaf = map[string]any{parts[i]: leaf}
	}
	s.setRaw(parts[0], leaf)
}

// setRaw sets a parameter, merging it into an object that is there.
func (s *requestShape) setRaw(name string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	existing, ok := s.params[name]
	if !ok {
		s.params[name] = raw
		return
	}
	merged, err := mergeJSON(existing, raw)
	if err != nil {
		s.params[name] = raw
		return
	}
	s.params[name] = merged
}

// mergeJSON merges two JSON objects, one field at a time.
//
// A field the second object does not have keeps the first object's
// value.
func mergeJSON(first, second []byte) ([]byte, error) {
	var a, b map[string]json.RawMessage
	if err := json.Unmarshal(first, &a); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(second, &b); err != nil {
		return nil, err
	}
	for k, v := range b {
		a[k] = v
	}
	return json.Marshal(a)
}

// quirks holds the registered profiles, by name.
var quirks = map[string]quirkProfile{}

// registerQuirk registers a profile, so a model info can name it.
func registerQuirk(p quirkProfile) {
	quirks[p.name] = p
}

// quirksFor returns the profiles a model info named, in its order.
//
// A name no profile answers is noted and skipped: a model info may
// name a quirk that a later version ships.
func quirksFor(names []string, logger *slog.Logger) []quirkProfile {
	var out []quirkProfile
	for _, name := range names {
		p, ok := quirks[name]
		if !ok {
			logger.Warn("the model info named a quirk the transport does not know", "quirk", name)
			continue
		}
		out = append(out, p)
	}
	return out
}

// responseRepair is the response as it is read.
//
// A quirk adds a filter over the model's text: the thinking a model
// writes in its own tags becomes a thinking block, and a stream that
// repeats itself is noted.
type responseRepair struct {
	// logger receives the notes a filter makes.
	logger *slog.Logger
	// model names the model, for the notes.
	model string
	// filters holds the text filters.
	filters []textFilter
	// textCalls reports that the tool calls the model writes as text
	// are to be read.
	textCalls bool
}

// addFilter adds a filter over the model's text.
func (r *responseRepair) addFilter(f textFilter) {
	r.filters = append(r.filters, f)
}

// The profiles the transport ships.
func init() {
	registerQuirk(quirkProfile{
		name: QuirkThinkTags,
		repairResponse: func(r *responseRepair) {
			r.addFilter(newThinkFilter())
		},
	})
	registerQuirk(quirkProfile{
		name: QuirkChatTemplateThinking,
		shapeRequest: func(s *requestShape) {
			s.setParam("chat_template_kwargs.enable_thinking", true)
		},
	})
	registerQuirk(quirkProfile{
		name: QuirkSimplifyToolSchemas,
		shapeRequest: func(s *requestShape) {
			for i := range s.request.Tools {
				s.request.Tools[i].Function.Parameters = simplifySchema(s.request.Tools[i].Function.Parameters)
			}
		},
	})
	registerQuirk(quirkProfile{
		name: QuirkDegenerateStream,
		repairResponse: func(r *responseRepair) {
			r.addFilter(newDegenerateFilter(r.logger, r.model))
		},
	})
	registerQuirk(quirkProfile{
		name: QuirkTextToolCalls,
		repairResponse: func(r *responseRepair) {
			r.textCalls = true
		},
	})
}

// simplifySchema reduces a tool's schema to what a weak parser reads.
//
// The keywords a schema parser may not know are dropped, and the shape
// of the tool's arguments is kept: the model still knows which
// arguments the tool takes.
func simplifySchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schema); err != nil {
		return raw
	}
	kept := map[string]json.RawMessage{}
	for _, key := range []string{"type", "properties", "required"} {
		if v, ok := schema[key]; ok {
			kept[key] = v
		}
	}
	if len(kept) == 0 {
		return raw
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return raw
	}
	return out
}

// textFilters returns the filters the response is read through.
//
// The quirks add theirs in the order the model info named them, and the
// parser is last: it reads the text the others passed on.
func (t *Transport) textFilters(settings requestSettings) []textFilter {
	repair := &responseRepair{logger: t.logger, model: settings.info.Name}
	for _, quirk := range settings.quirks {
		if quirk.repairResponse != nil {
			quirk.repairResponse(repair)
		}
	}
	filters := repair.filters
	mode := t.toolsMode(settings.info)
	if repair.textCalls && mode != "text" && mode != "auto" {
		// The model writes its tool calls as text, whatever its mode
		// says.
		mode = "text"
	}
	if mode == "text" || mode == "auto" {
		filters = append(filters, newParser(mode, settings.info.Tools.TextFormat))
	}
	return filters
}

// sendsTools reports whether the request carries the tools.
//
// A request that carries them may also carry native tool calls, and a
// native call wins over one the model wrote as text.
func (t *Transport) sendsTools(settings requestSettings) bool {
	mode := t.toolsMode(settings.info)
	return mode == "native" || mode == "auto"
}
