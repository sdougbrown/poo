package support

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// SSEScanner returns a scanner for SSE lines.
//
// A single event line can be large, so the line limit is 16 MiB.
func SSEScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return sc
}

// PeekFirstByte returns the first non-space byte of the body, without
// consuming it.
//
// Some servers answer a failed request with a 200 and a JSON error body
// instead of the SSE stream, and a pretty-printed body opens with a
// newline. The first byte is what tells an error body from a stream.
func PeekFirstByte(br *bufio.Reader) (byte, error) {
	for {
		b, err := br.Peek(1)
		if err != nil {
			return 0, err
		}
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			if _, err := br.ReadByte(); err != nil {
				return 0, err
			}
		default:
			return b[0], nil
		}
	}
}

// IsCompleteJSON reports whether a string is a complete JSON object.
func IsCompleteJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return json.Valid([]byte(s))
}

// TTFT is the time to the first token of a response.
//
// The clock starts when the request goes out, so the measurement includes
// the endpoint's own latency rather than the reader's.
type TTFT struct {
	// started is when the request was sent.
	started time.Time
	// at is the time the first token arrived, from started.
	at time.Duration
	// set reports that the first token has arrived.
	set bool
}

// NewTTFT starts the clock the request was sent at.
func NewTTFT(started time.Time) TTFT {
	return TTFT{started: started}
}

// Mark records that the first streamed token arrived.
//
// A response that carries more than one first token — a text delta and a
// tool call — keeps the earliest one.
func (t *TTFT) Mark() {
	if t.set {
		return
	}
	t.set = true
	t.at = time.Since(t.started)
}

// Arrived reports whether a token arrived at all.
func (t *TTFT) Arrived() bool {
	return t.set
}

// MS returns the time to the first token in milliseconds, or zero when
// no token arrived.
func (t *TTFT) MS() float64 {
	if !t.set {
		return 0
	}
	return float64(t.at.Microseconds()) / 1000
}

// Take removes the next queued event and reports whether there was one.
//
// A reader queues the events an SSE frame produced, so they leave it in the
// order the endpoint sent them.
func Take(pending *[]poo.StreamItem) (poo.StreamItem, bool) {
	if len(*pending) == 0 {
		return poo.StreamItem{}, false
	}
	ev := (*pending)[0]
	*pending = (*pending)[1:]
	return ev, true
}

// ContextError turns a read failure into the context's error when the
// context is what ended the request.
//
// A stream that already carried a delta cannot be retried: the model has
// seen part of the answer. The failure says so, so the settle reason names
// it, and the kind the failure had before the reclassification stays in the
// error's code, where the plan keeps it.
//
// The two rules it applies are this package's own: which context a failure
// belongs to, and what a failure after the first delta is called. They are
// composed here rather than restated, so a run whose request failed at open
// time and one whose stream broke settle with the same kind.
func ContextError(parent, request context.Context, arrived bool, err error) error {
	failure := ClassifyTransportError(parent, request, err)
	if !arrived {
		return failure
	}
	return InterruptedStreamError(failure)
}
