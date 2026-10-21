package poo

import "errors"

// InputMode says how a client message reaches the model.
//
// The three modes differ in when the message is delivered and in what
// they do to the work that is running.
type InputMode string

const (
	// Steer delivers the message at the next turn end.
	Steer InputMode = "steer"
	// Interrupt aborts the current activity and delivers the message
	// with it.
	Interrupt InputMode = "interrupt"
	// FollowUp delivers the message only when the run would settle
	// naturally.
	FollowUp InputMode = "follow_up"
)

// String returns the mode's name.
func (m InputMode) String() string { return string(m) }

// Errors the input path reports.
var (
	// ErrNotRunning means the session has no run that could take the
	// message.
	ErrNotRunning = errors.New("poo: the session is not running")
	// ErrBusy means the session already has a run.
	ErrBusy = errors.New("poo: the session is busy")
	// ErrSessionBroken means the session's store failed and the session
	// cannot be used any more.
	ErrSessionBroken = errors.New("poo: the session is broken")
	// ErrSessionClosed means the session is closing or closed.
	ErrSessionClosed = errors.New("poo: the session is closed")
)

// ErrInvalidInput means the input itself is wrong: an unknown message
// id, or a delivery note that does not describe a number of characters.
//
// It is a typed error, so a caller can tell a bad input from a session
// that cannot take one.
type ErrInvalidInput struct {
	// Message says what was wrong.
	Message string
}

func (e *ErrInvalidInput) Error() string { return e.Message }

// InputOption changes one input.
//
// An option applies to the message a client sends and to the prompt that
// starts a run.
type InputOption func(*inputRequest)

// WithDelivered says how much of a reply the user had seen.
//
// The reference names an assistant message in the session and the count
// says how many of its characters the user had seen, counted in code
// points over its text blocks.
//
// The note is rendered from the recorded reply when the input is
// delivered, prefixed to the user's message, and recorded with it, so a
// replay shows exactly what the user saw.
func WithDelivered(messageID string, uptoChars int) InputOption {
	return func(r *inputRequest) {
		r.delivered = &DeliveryNote{MessageID: messageID, UpToChars: uptoChars}
	}
}

// inputRequest is one input on its way into the session.
type inputRequest struct {
	// delivered is the delivery note, when the client sent one.
	delivered *DeliveryNote
}

// InputQueued records an input the session accepted.
//
// The record is written before the call that accepted the input returns,
// so an accepted input is never lost: its fate is in the records.
type InputQueued struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run the input belongs to, empty when the
	// session had no run.
	RunID string `json:"run_id,omitempty"`
	// InputID is the input's identifier.
	InputID string `json:"input_id"`
	// Mode is how the input is delivered: "steer", "interrupt" or
	// "follow_up".
	Mode InputMode `json:"mode"`
	// Text is the message text.
	Text string `json:"text,omitempty"`
	// DeliveryNote says how much of the previous reply the user had
	// seen, when the input carries one.
	DeliveryNote *DeliveryNote `json:"delivery_note,omitempty"`
	// QueuedAt is the RFC 3339 time the input was accepted, in UTC.
	QueuedAt string `json:"queued_at"`
}

// InputDelivered records an input that reached the model.
type InputDelivered struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run the input was delivered to.
	RunID string `json:"run_id"`
	// InputID is the input's identifier.
	InputID string `json:"input_id"`
	// Mode is how the input was delivered.
	Mode InputMode `json:"mode"`
	// DeliveredAt is the RFC 3339 time the input was delivered, in UTC.
	DeliveredAt string `json:"delivered_at"`
}

// InputDropped records an input that never reached the model.
//
// The reason is the run's final settle reason, so a reader can tell why
// a message the client accepted was never seen by the model.
type InputDropped struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run the input belonged to.
	RunID string `json:"run_id,omitempty"`
	// InputID is the input's identifier.
	InputID string `json:"input_id"`
	// Mode is how the input was accepted.
	Mode InputMode `json:"mode"`
	// Reason is why the input was dropped.
	Reason Reason `json:"reason"`
	// DroppedAt is the RFC 3339 time the input was dropped, in UTC.
	DroppedAt string `json:"dropped_at"`
}

// TurnInterrupted records a turn that an interrupt cut short.
//
// The record follows the turn's end: a client reads it as a note on the
// turn that just closed.
type TurnInterrupted struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run the turn belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn that was interrupted.
	Turn int `json:"turn"`
	// InputIDs are the inputs whose messages aborted the turn.
	InputIDs []string `json:"input_ids"`
	// InterruptedAt is the RFC 3339 time the turn was cut short, in UTC.
	InterruptedAt string `json:"interrupted_at"`
}

// queuedInput is one accepted input waiting for its delivery.
type queuedInput struct {
	// id is the input's identifier.
	id string
	// mode is how the input is delivered.
	mode InputMode
	// text is the message text.
	text string
	// note is the delivery note the input was accepted with, when it
	// carries one. Its text is rendered again at delivery.
	note *DeliveryNote
}

// renderDeliveryNote renders the note that says how much of a reply the
// user had seen.
//
// The note becomes part of the recorded message: the assistant message
// is never trimmed, and the note is the record.
func renderDeliveryNote(note *DeliveryNote, message Message) string {
	if note == nil {
		return ""
	}
	if note.UpToChars <= 0 {
		// Nothing was presented, and that is worth saying even when
		// the reply itself was empty.
		return "[The user did not see or hear any of your reply " + note.MessageID + ".]"
	}
	text := message.Text()
	runes := []rune(text)
	if note.UpToChars >= len(runes) {
		// The whole reply was presented, so there is nothing to say.
		return ""
	}
	shown := runes[:note.UpToChars]
	// The quoted part is the last eighty code points of what the user
	// saw: enough to place the cut, not enough to quote the reply.
	if len(shown) > 80 {
		shown = shown[len(shown)-80:]
	}
	return "[The user saw or heard your reply " + note.MessageID + " only up to: \"" + escapeNoteText(string(shown)) + "\"]"
}

// withDeliveryNote prefixes a user message with its rendered note.
//
// A note that renders to nothing (the whole reply was presented) adds
// nothing.
func withDeliveryNote(text string, note *DeliveryNote) string {
	if note == nil || note.Text == "" {
		return text
	}
	return note.Text + "\n\n" + text
}

// escapeNoteText escapes the quotes and newlines of a quoted reply.
//
// The note is rendered into a message the model reads, so a reply that
// holds a quote or a newline cannot break the note's own quoting.
func escapeNoteText(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// errInterrupted is the cancellation cause an interrupt uses.
//
// It is never a settle candidate: an interrupt ends the activity, not
// the run.
var errInterrupted = errors.New("poo: interrupted by the user")

// errFinalizing is the cancellation cause the finalization uses.
//
// Errors that carry this cause are never candidates, so finalizing never
// manufactures a cancelled run.
var errFinalizing = errors.New("poo: the run is finalizing")
