package poo

import (
	"encoding/json"
	"strings"
)

// Role is the author of a message in the model's history.
type Role string

const (
	// RoleSystem carries the system prompt.
	RoleSystem Role = "system"
	// RoleUser carries input from the user or from a tool.
	RoleUser Role = "user"
	// RoleAssistant carries model output.
	RoleAssistant Role = "assistant"
	// RoleTool carries a tool result back to the model.
	RoleTool Role = "tool"
)

// Origin says who produced a message.
//
// The origin is poo's own bookkeeping: it tells a reader of the record who
// put the message there, and it tells a hook whether the message it is
// looking at came from a person, from a job or from another hook.
type Origin string

const (
	// OriginUser is a message from the person driving the session.
	OriginUser Origin = "user"
	// OriginModel is a message the model produced. A nested call's
	// tool.call record keeps this origin, and its Mode ("nested") and
	// Parent mark it as a call a tool made.
	OriginModel Origin = "model"
	// OriginTool is a message carrying a tool's result.
	OriginTool Origin = "tool"
	// OriginSystem is the session's system prompt.
	OriginSystem Origin = "system"
	// OriginJob is a message carrying a background job's result.
	OriginJob Origin = "job"
	// OriginSteer is a message the user sent while the run was active.
	OriginSteer Origin = "steer"
	// OriginInterrupt is a message that aborted the current activity.
	OriginInterrupt Origin = "interrupt"
	// OriginFollowUp is a message the user sent for the next turn.
	OriginFollowUp Origin = "follow_up"
	// OriginCompaction is a message a compactor wrote to replace a
	// range of the history. The replacement is visible: the model sees
	// that its history was replaced.
	OriginCompaction Origin = "compaction"
)

// HookOrigin returns the origin a hook's appended messages carry.
//
// The origin names the hook, so a reader of the record knows which hook
// put the message there.
func HookOrigin(name string) Origin {
	return Origin("hook:" + name)
}

// feedOriginPrefix is the one spelling of a feed's origin.
const feedOriginPrefix = "feed:"

// FeedOrigin returns the origin a feed's records and messages carry.
//
// The origin names the feed, so a reader of the record knows which feed
// put the message there. A feed is a long-lived source that pushes
// messages into a session; its messages never stand in for the user's
// turn.
func FeedOrigin(name string) Origin {
	return Origin(feedOriginPrefix + name)
}

// FeedName returns the feed an origin names, and reports whether the
// origin is a feed's at all.
//
// A reader that takes a feed's name out of an origin uses this rather
// than its own spelling of the prefix.
func FeedName(o Origin) (string, bool) {
	name, ok := strings.CutPrefix(string(o), feedOriginPrefix)
	if !ok {
		return "", false
	}
	return name, true
}

// IsFeedOrigin reports that the origin belongs to a feed.
//
// A reader that recognizes a feed's message, record or input id uses
// this rather than its own spelling of the prefix.
func IsFeedOrigin(o Origin) bool {
	_, ok := FeedName(o)
	return ok
}

// API names a wire API.
//
// The model is provider-neutral, but a piece of opaque provider data is
// not: it belongs to the API that produced it, and only that API can read
// it back.
type API string

const (
	// APIChatCompletions is the OpenAI Chat Completions API.
	APIChatCompletions API = "chat_completions"
	// APIResponses is the OpenAI Responses API.
	APIResponses API = "responses"
	// APIAnthropicMessages is the Anthropic Messages API.
	APIAnthropicMessages API = "anthropic_messages"
)

// ContentKind names the type of a content block.
type ContentKind string

const (
	// ContentText is plain text.
	ContentText ContentKind = "text"
	// ContentThinking is model reasoning the provider returned separately.
	ContentThinking ContentKind = "thinking"
	// ContentToolCall is a request from the model to run a tool.
	ContentToolCall ContentKind = "tool_call"
	// ContentToolResult is the result of a tool call.
	ContentToolResult ContentKind = "tool_result"
	// ContentImage is an image a tool returned.
	//
	// The block carries the bytes and their media type, so a transport can
	// hand the model the picture itself. Only a model whose inputs declare
	// images is offered one.
	ContentImage ContentKind = "image"
	// ContentAudio is audio a tool returned.
	//
	// The block carries the bytes and their media type, so a transport can
	// hand the model the sound itself. Only a model whose inputs declare
	// audio is offered one.
	ContentAudio ContentKind = "audio"
	// ContentProvider is opaque provider data.
	//
	// A provider block holds a value the model must carry without
	// interpreting it: a reasoning signature, an encrypted reasoning
	// payload, a compaction item. It rides alone, or as the companion
	// of a visible block.
	ContentProvider ContentKind = "provider"
)

// ProviderBlock is opaque data a provider produced.
//
// The block holds a payload the harness never interprets and must give
// back unchanged. The API says which API produced it, and the type says
// which kind of payload it is.
//
// A required block belongs to a request that cannot be made without it: an
// omitted required block makes the request fail with
// `session_incompatible`, while an omitted optional block is simply left
// out of the request.
type ProviderBlock struct {
	// API is the wire API the payload belongs to.
	API API `json:"api"`
	// Type is the provider's own name for the payload, e.g.
	// "thinking_signature" or "reasoning".
	Type string `json:"type"`
	// Data is the payload, verbatim.
	Data json.RawMessage `json:"data"`
	// Required reports whether the request needs the payload.
	Required bool `json:"required,omitempty"`
}

// ContentBlock is one piece of a message's content.
type ContentBlock struct {
	Kind ContentKind `json:"kind"`
	Text string      `json:"text,omitempty"`

	// ToolCallID and Name are set when Kind is ContentToolCall.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	// Arguments holds the raw JSON arguments of a tool call.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Origin says where a tool call came from: "wire" for a call the
	// endpoint sent, "parsed_text" for a call the model wrote as text.
	// It is set when Kind is ContentToolCall.
	Origin CallOrigin `json:"origin,omitempty"`

	// ToolCallID is reused for a tool result, so the field above carries it.
	// IsError marks a tool result that the model must see as a failure.
	IsError bool `json:"is_error,omitempty"`

	// MediaType and Data are set when the kind is one an attachment rides
	// as: its media type, and the bytes themselves.
	MediaType string `json:"media_type,omitempty"`
	Data      []byte `json:"data,omitempty"`

	// Provider holds the opaque companion of this block, when it has one.
	//
	// An Anthropic thinking block carries its signature here: the text
	// stays in Text and the signature rides beside it, so the transport
	// can put the two back into the one block the API expects.
	Provider *ProviderBlock `json:"provider,omitempty"`
}

// DeliveryNote says how much of a reply the user had seen.
//
// The note is rendered from the recorded reply when the input is
// delivered and is part of the recorded message, so a replay shows
// exactly what the user saw. It is never a trimmed assistant message:
// the note is the record.
type DeliveryNote struct {
	// MessageID names the assistant message the user had seen part of.
	MessageID string `json:"message_id"`
	// UpToChars is how many characters of that message the user had
	// seen, counted in code points over its text blocks.
	UpToChars int `json:"upto_chars"`
	// Text is the rendered note, as it was prefixed to the message's text.
	// It is empty when the whole reply was presented.
	Text string `json:"text"`
}

// Message is one turn of the conversation as the model sees it.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
	// ID is the message's session-monotonic id, e.g. "m7". It is set on
	// the messages the model produced, and it is what a delivery note
	// names.
	ID string `json:"id,omitempty"`
	// Origin says who produced the message.
	Origin Origin `json:"origin,omitempty"`
	// DeliveryNote says how much of the previous reply the user had
	// seen, when the message carries one.
	DeliveryNote *DeliveryNote `json:"delivery_note,omitempty"`
	// Partial reports that the message was cut short.
	Partial bool `json:"partial,omitempty"`
	// Cause says why the message was cut short: "interrupt" or
	// "provider_error". It is set only on a partial message.
	Cause Cause `json:"cause,omitempty"`
}

// Text returns the concatenated text blocks of the message.
func (m Message) Text() string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Kind == ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// ToolCalls returns the tool call blocks of the message, in order.
func (m Message) ToolCalls() []ContentBlock {
	var out []ContentBlock
	for _, c := range m.Content {
		if c.Kind == ContentToolCall {
			out = append(out, c)
		}
	}
	return out
}

// NewTextMessage builds a message with a single text block.
func NewTextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentBlock{{Kind: ContentText, Text: text}}}
}

// ToolResultMessage builds the message that carries a tool result back.
//
// An attachment the tool returned rides in its own block after the text, which
// is the shape both the Responses API and the Messages API take inside a tool
// result; a transport whose API cannot carry one there places it instead.
func ToolResultMessage(id, name, text string, isError bool, attachments []Attachment) Message {
	return Message{
		Role: RoleTool,
		Content: append([]ContentBlock{{
			Kind:       ContentToolResult,
			ToolCallID: id,
			Name:       name,
			Text:       text,
			IsError:    isError,
		}}, attachmentBlocks(attachments)...),
	}
}

// attachmentKind is what a modality brings with it: the block kind an
// attachment rides as, and the media types providers accept for it.
//
// A modality is added in one place, its entry here. What one costs is the
// estimator's rule, beside the other cost rules in the session.
type attachmentKind struct {
	// kind is the block kind the modality rides as.
	kind ContentKind
	// mediaTypes are the media types a block of that kind may carry.
	mediaTypes map[string]bool
}

// attachmentKinds are the modalities an attachment may have.
var attachmentKinds = map[Modality]attachmentKind{
	ModalityImage: {
		kind: ContentImage,
		// The four providers accept, which are the four the read tool
		// recognises.
		mediaTypes: map[string]bool{
			"image/png":  true,
			"image/jpeg": true,
			"image/gif":  true,
			"image/webp": true,
		},
	},
	// The two the Chat Completions audio part carries. A provider that takes
	// another format would need a conversion the harness does not ship.
	ModalityAudio: {
		kind: ContentAudio,
		mediaTypes: map[string]bool{
			"audio/wav":  true,
			"audio/mpeg": true,
		},
	},
}

// IsMediaType reports whether a media type is one providers accept for a
// modality.
//
// It is the one list of them: a tool that sniffs a file's type checks it here,
// so a type that is not accepted cannot be produced and quietly dropped.
func IsMediaType(modality Modality, mediaType string) bool {
	return attachmentKinds[modality].mediaTypes[mediaType]
}

// modalityOf returns the input modality a block's kind carries, and whether the
// kind carries one at all.
func modalityOf(kind ContentKind) (Modality, bool) {
	for modality, attachment := range attachmentKinds {
		if attachment.kind == kind {
			return modality, true
		}
	}
	return "", false
}

// attachmentBlocks renders attachments as content blocks, in order.
//
// An attachment whose modality or media type no provider accepts is left out: a
// data URL the provider refuses would fail every request the history carries,
// and a tool that mislabels a file is not a reason to stop a run.
func attachmentBlocks(attachments []Attachment) []ContentBlock {
	if len(attachments) == 0 {
		return nil
	}
	blocks := make([]ContentBlock, 0, len(attachments))
	for _, attachment := range attachments {
		rule, ok := attachmentKinds[attachment.Modality]
		if !ok || !rule.mediaTypes[attachment.MediaType] {
			continue
		}
		blocks = append(blocks, ContentBlock{
			Kind:      rule.kind,
			MediaType: attachment.MediaType,
			Data:      attachment.Data,
		})
	}
	return blocks
}

// cloneAttachments returns a copy that owns its bytes.
func cloneAttachments(attachments []Attachment) []Attachment {
	if attachments == nil {
		return nil
	}
	out := make([]Attachment, len(attachments))
	for i, attachment := range attachments {
		out[i] = attachment
		if attachment.Data != nil {
			out[i].Data = append([]byte(nil), attachment.Data...)
		}
	}
	return out
}

// ToolCall is a request from the model to run a tool.
type ToolCall struct {
	// CallID is the provider's identifier for the call. The tool's result
	// goes back to the model under this id, because that is the id the
	// provider matches it against.
	CallID string `json:"id"`
	// Name is the tool the model called.
	Name string `json:"name"`
	// Arguments are the raw JSON arguments.
	Arguments json.RawMessage `json:"arguments"`
	// Origin says where the call came from: "wire" for a call the
	// endpoint sent, "parsed_text" for a call the model wrote as text
	// and the transport read out of it. It is empty when the transport
	// did not say.
	Origin CallOrigin `json:"origin,omitempty"`
}

// Outcome is the result of a tool call.
//
// The outcome is a closed enum: the wire and the records name it, so a
// consumer can read it without guessing.
type Outcome string

const (
	// OutcomeOK means the call succeeded.
	OutcomeOK Outcome = "ok"
	// OutcomeError means the call failed recoverably.
	OutcomeError Outcome = "error"
	// OutcomeDenied means a policy hook refused the call.
	OutcomeDenied Outcome = "denied"
	// OutcomeSkipped means the call never ran.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeCancelled means the call was cancelled.
	OutcomeCancelled Outcome = "cancelled"
	// OutcomeInterrupted means the call was cut short by an interrupt.
	OutcomeInterrupted Outcome = "interrupted"
	// OutcomeStarted means the call became a background job.
	OutcomeStarted Outcome = "started"
)

// ToolResult is what a tool returns to the model.
//
// The outcome is not here: a tool does not choose it. The core assigns
// it — a denial comes from the policy hook, a skip from a truncated
// response, a cancellation from an interrupt, and a "started" from the
// job machinery. The outcome lives on the record and on the event.
// Attachment is a file a tool returned beside its text.
//
// The modality says what kind of input it is, and the media type says what the
// bytes are: a transport sends them together, and a model reads the attachment
// when its inputs declare the modality.
type Attachment struct {
	// Modality is the kind of input the attachment is.
	Modality Modality `json:"modality"`
	// MediaType is the attachment's media type, e.g. "image/png".
	MediaType string `json:"media_type"`
	// Data is the attachment's bytes.
	Data []byte `json:"data"`
}

type ToolResult struct {
	// Text is the model-facing text.
	Text string `json:"text"`
	// Attachments are the files the tool returned beside the text.
	Attachments []Attachment `json:"attachments,omitempty"`
	// Structured is an optional side channel for consumers.
	Structured json.RawMessage `json:"structured,omitempty"`
	// IsError marks a recoverable failure. The model sees the text and
	// may correct itself; the run continues.
	IsError bool `json:"is_error,omitempty"`
}
