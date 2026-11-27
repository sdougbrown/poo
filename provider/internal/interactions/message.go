package interactions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/transport"
)

// Messages turns core messages into the interaction's steps, and returns the
// system instruction the messages carry.
//
// The system prompt travels in the request's own field, above the steps. The
// steps are flat: the API has no roles and no alternation, so a message is the
// step its content makes it — a user message is one input step, a model message
// is the thought, output and call steps its blocks spell, and a tool's result
// is the step that answers its call.
//
// The thinking the model did rides back as a thought step with its signature,
// because the API checks a returned thought against the one it signed. A call
// whose thought carries no signature is replayed with the sentinel the API
// documents for the check, which is how a call that came from another API, or
// from a text-parsed one, is still answered.
func Messages(msgs []poo.Message) ([]Step, string, error) {
	var system []string
	steps := make([]Step, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case poo.RoleSystem:
			text, err := systemText(m)
			if err != nil {
				return nil, "", err
			}
			system = append(system, text)
		case poo.RoleUser:
			content, err := parts(m)
			if err != nil {
				return nil, "", err
			}
			// An empty input is a step the API refuses, and the steps
			// around it say what the conversation said.
			if len(content) > 0 {
				steps = append(steps, Step{Type: StepUserInput, Content: content})
			}
		case poo.RoleAssistant:
			model, err := modelSteps(m)
			if err != nil {
				return nil, "", err
			}
			steps = append(steps, model...)
		case poo.RoleTool:
			result, err := resultSteps(m)
			if err != nil {
				return nil, "", err
			}
			steps = append(steps, result...)
		default:
			return nil, "", &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: fmt.Sprintf("the Interactions API carries no %q role", m.Role),
			}
		}
	}
	return steps, strings.Join(system, "\n\n"), nil
}

// systemText collects a system message's text.
func systemText(m poo.Message) (string, error) {
	var parts []string
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := transport.CarryError(poo.APIGoogleInteractions, false, c.Provider); err != nil {
				return "", err
			}
		}
		if c.Kind != poo.ContentText {
			return "", &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the system instruction carries a block the Interactions API cannot spell",
			}
		}
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n\n"), nil
}

// modelSteps turns one model message into the steps its blocks spell.
//
// The blocks keep their order, and consecutive text is one output step: the
// API sends a model's answer as one output step, so a message split by nothing
// is not split on the wire either.
func modelSteps(m poo.Message) ([]Step, error) {
	var steps []Step
	// signed reports that this message's thought carries its signature, so
	// its calls are replayable as they are.
	signed := false
	for _, c := range m.Content {
		if c.Provider != nil && !(c.Kind == poo.ContentThinking && c.Provider.API == poo.APIGoogleInteractions) {
			if err := transport.CarryError(poo.APIGoogleInteractions, true, c.Provider); err != nil {
				return nil, err
			}
		}
		switch c.Kind {
		case poo.ContentThinking:
			thought := thoughtStep(c)
			signed = signed || thought.Signature != ""
			steps = append(steps, thought)
		case poo.ContentText:
			if c.Text == "" {
				continue
			}
			text := Content{Type: "text", Text: c.Text}
			if n := len(steps); n > 0 && steps[n-1].Type == StepModelOutput {
				steps[n-1].Content = append(steps[n-1].Content, text)
				continue
			}
			steps = append(steps, Step{Type: StepModelOutput, Content: []Content{text}})
		case poo.ContentToolCall:
			// The API checks a call of the current turn against the
			// thought that asked for it, and a call whose thought was never
			// signed has nothing to check: Google's sentinel says so.
			if !signed {
				steps = append(steps, Step{Type: StepThought, Signature: SkipThoughtSignature})
				signed = true
			}
			arguments := c.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			steps = append(steps, Step{
				Type:      StepFunctionCall,
				ID:        c.ToolCallID,
				Name:      c.Name,
				Arguments: arguments,
			})
		case poo.ContentProvider:
			// A step the API produced and poo must give back unchanged. The
			// interaction's input carries no such step of its own, so a
			// required one was refused above and an optional one is dropped.
			continue
		}
	}
	return steps, nil
}

// thoughtStep turns a thinking block and its signature companion into the
// thought step the API expects.
//
// The summary is what the client sees of the thinking, and the signature is
// what the model reads back: a thought whose text is empty but whose signature
// is there is still a step, because the call after it is checked against it.
func thoughtStep(c poo.ContentBlock) Step {
	step := Step{Type: StepThought}
	if strings.TrimSpace(c.Text) != "" {
		step.Summary = []Content{{Type: "text", Text: c.Text}}
	}
	if c.Provider != nil && c.Provider.API == poo.APIGoogleInteractions && len(c.Provider.Data) > 0 {
		var signature string
		if err := json.Unmarshal(c.Provider.Data, &signature); err == nil {
			step.Signature = signature
		}
	}
	return step
}

// resultSteps turns one tool message into the steps that answer its calls.
//
// A tool message carries the results of the calls one assistant message made,
// and each result answers its own call by id, in the order the message holds
// them. An attachment a tool returned rides as a part of the result it came
// with, which is the only place the API takes one.
func resultSteps(m poo.Message) ([]Step, error) {
	var steps []Step
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := transport.CarryError(poo.APIGoogleInteractions, false, c.Provider); err != nil {
				return nil, err
			}
		}
		switch c.Kind {
		case poo.ContentToolResult:
			steps = append(steps, Step{
				Type:    StepFunctionResult,
				CallID:  c.ToolCallID,
				Name:    c.Name,
				Result:  c.Text,
				IsError: c.IsError,
			})
		case poo.ContentImage, poo.ContentAudio:
			part, ok := attachment(c)
			// An attachment that answers no call has no step to ride in: the
			// API takes a result only as the answer to one.
			if !ok || len(steps) == 0 {
				continue
			}
			last := &steps[len(steps)-1]
			last.Result = withPart(last.Result, part)
		}
	}
	return steps, nil
}

// withPart adds a part to a result, which the API takes as a string until it
// carries more than one thing.
func withPart(result any, part Content) any {
	parts, ok := result.([]Content)
	if !ok {
		parts = nil
		if text, ok := result.(string); ok && text != "" {
			parts = append(parts, Content{Type: "text", Text: text})
		}
	}
	return append(parts, part)
}

// parts turns a user message's content into the parts of one input step.
//
// The attachments ride after the text of their own message, which is where a
// reader of the body finds them beside the words they answer.
func parts(m poo.Message) ([]Content, error) {
	var out []Content
	var pending []Content
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := transport.CarryError(poo.APIGoogleInteractions, false, c.Provider); err != nil {
				return nil, err
			}
		}
		switch c.Kind {
		case poo.ContentText:
			if c.Text != "" {
				out = append(out, Content{Type: "text", Text: c.Text})
			}
		case poo.ContentImage, poo.ContentAudio:
			if part, ok := attachment(c); ok {
				pending = append(pending, part)
			}
		case poo.ContentProvider:
			continue
		default:
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: fmt.Sprintf("a user message carries a %q block, which an input step cannot hold", c.Kind),
			}
		}
	}
	return append(out, pending...), nil
}

// attachment turns a core attachment into the part the API takes it as.
//
// The API takes an image and a sound inline, as the bytes themselves, so the
// block's media type is all that says which part it is.
func attachment(c poo.ContentBlock) (Content, bool) {
	kind := ""
	switch c.Kind {
	case poo.ContentImage:
		kind = "image"
	case poo.ContentAudio:
		kind = "audio"
	default:
		return Content{}, false
	}
	if len(c.Data) == 0 {
		return Content{}, false
	}
	return Content{
		Type:      kind,
		MediaType: c.MediaType,
		Data:      base64.StdEncoding.EncodeToString(c.Data),
	}, true
}
