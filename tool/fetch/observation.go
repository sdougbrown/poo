// The observation is one fetch's outcome, as the tool reports it, in
// the envelope the bash tool's result set. The model reads the text,
// and a consumer reads the structured payload beside it.
package fetch

import (
	"encoding/json"

	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/internal/envelope"
)

// Observation is one fetch's outcome, as the tool reports it.
type Observation struct {
	// Status is the HTTP status code of the response the body came
	// from, after the redirects.
	Status int
	// FinalURL is the URL the body came from, after the redirects.
	FinalURL string
	// ContentType is the response's Content-Type header.
	ContentType string
	// Size is the body's size in bytes, after the download limit.
	Size int64
	// Truncated reports that the body was cut at the download limit.
	Truncated bool
	// Body is the body, in the format the call asked for.
	Body string
}

// The fields the result carries. A field the call does not fill stays
// out of the result, and the text and the structured payload share the
// one list: a field is added once, and both readers see it.
const (
	// FieldStatus is the response's status code.
	FieldStatus = "status"
	// FieldFinalURL is the URL the body came from.
	FieldFinalURL = "final_url"
	// FieldContentType is the response's content type.
	FieldContentType = "content_type"
	// FieldSize is the body's size in bytes.
	FieldSize = "size"
	// FieldTruncated reports a body cut at the download limit.
	FieldTruncated = "truncated"
	// FieldBody is the body when it fits the cap.
	FieldBody = "body"
	// FieldBodyHead is the body's head when it does not fit.
	FieldBodyHead = "body_head"
	// FieldBodyTail is the body's tail when it does not fit.
	FieldBodyTail = "body_tail"
	// FieldElidedChars is how many characters the cap dropped.
	FieldElidedChars = "elided_chars"
	// FieldHint says what the elision means.
	FieldHint = "hint"
)

// ElisionHint is the hint a capped body carries.
const ElisionHint = "the body was longer than the cap: its head and tail are shown, and the middle is gone"

// render renders one fetch's outcome as the result text the model
// reads, and as the structured payload beside it.
//
// Both come from the one list of fields, and both are valid JSON, the
// bash observation's shape. The header is always whole; the cap bounds
// the body only. A body the cap cannot hold is elided the way the bash
// observation is. Its head and its tail each keep half the cap, and
// elided_chars says what the middle held. The cap and elided_chars are
// counted in characters, not bytes, and a cut never splits a rune.
func render(o Observation, capChars int) (string, json.RawMessage) {
	fields := []envelope.Field{
		{FieldStatus, o.Status},
		{FieldFinalURL, o.FinalURL},
		{FieldContentType, o.ContentType},
		{FieldSize, o.Size},
	}
	if o.Truncated {
		fields = append(fields, envelope.Field{FieldTruncated, true})
	}

	cap := capChars
	if cap <= 0 {
		cap = DefaultMaxChars
	}
	bodyChars := caps.RuneCount(o.Body)
	switch {
	case o.Body == "":
		// A body the tool withheld — a response that was not text — has
		// no field of its own: the metadata is the result.
	case bodyChars <= cap:
		fields = append(fields, envelope.Field{FieldBody, o.Body})
	default:
		head, tail := caps.SplitRunes(o.Body, cap/2)
		elided := bodyChars - 2*(cap/2)
		fields = append(fields,
			envelope.Field{FieldBodyHead, head},
			envelope.Field{FieldBodyTail, tail},
			envelope.Field{FieldElidedChars, elided},
			envelope.Field{FieldHint, ElisionHint},
		)
	}

	return envelope.Render(fields), envelope.Structured(fields)
}

// DefaultMaxChars is the cap a call runs at when the session names
// none. It is the largest observation the model ever sees, the same
// bound every tool's cap falls back to.
const DefaultMaxChars = caps.ObservationMax
