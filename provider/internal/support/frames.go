package support

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// The SSE framing.
//
// An SSE stream is lines: `data:` lines that carry a payload, fields that carry
// something else, and a blank line that ends an event. Some servers separate
// events with a single newline and never send the blank line, so a payload
// that parses whole is an event of its own. A payload may also be spread over
// several data lines, which the grammar joins with a newline.
//
// Frames reads that framing and nothing else: what a payload means is the
// transport's own event grammar. Every transport reads the same stream the same
// way because the rules live here.

// Frames reads SSE frames from a scanner.
type Frames struct {
	// scan is the stream's lines.
	scan *bufio.Scanner
	// data holds the data lines of the frame being read.
	data []string
	// finished reports that the stream's end was read, so the frame it
	// carried has been handed out and the next call is the end.
	finished bool
	// err is the failure the stream ended with, which the frame that
	// arrived before it is handed out ahead of.
	err error
}

// NewFrames returns a frame reader over a stream.
func NewFrames(scan *bufio.Scanner) *Frames {
	return &Frames{scan: scan}
}

// Next returns the payload of the next frame.
//
// A frame whose payload cannot become valid JSON — one with a syntax error — is
// dropped rather than carried: no later data line repairs it, and a frame this
// reader cannot parse must not swallow the events after it. The dropped frame
// is reported as io.EOF with no payload only when the stream ends.
//
// io.EOF means the stream is over: the scanner ran out, or the endpoint sent
// its terminator. The transport decides what an ended stream means.
func (f *Frames) Next() ([]byte, error) {
	if f.finished {
		if f.err != nil {
			return nil, f.err
		}
		return nil, io.EOF
	}
	for {
		if !f.scan.Scan() {
			if err := f.scan.Err(); err != nil {
				// A frame the lines had already completed is handed out
				// before the failure, so an event that arrived ahead of a
				// broken connection is not lost with it.
				f.finished = true
				f.err = err
				if payload, ok := f.take(); ok {
					return payload, nil
				}
				return nil, err
			}
			// The stream ended. A frame the lines had already completed is
			// handed out before the end, so an event that arrived without a
			// blank line after it is not lost with the connection.
			f.finished = true
			if payload, ok := f.take(); ok {
				return payload, nil
			}
			return nil, io.EOF
		}
		line := f.scan.Text()
		if line == "" {
			// A blank line ends the frame.
			if payload, ok := f.take(); ok {
				return payload, nil
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			// Comments and other SSE fields carry no payload.
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			f.finished = true
			if whole, ok := f.take(); ok {
				return whole, nil
			}
			return nil, io.EOF
		}

		// A server that separates its events with a single newline never
		// sends the blank line that ends one, so a frame that is already
		// whole is read as it is.
		if state := payloadState(f.data); state == payloadWhole {
			whole, _ := f.take()
			f.data = append(f.data, payload)
			return whole, nil
		} else if state == payloadBroken {
			// The frame cannot be repaired, so the lines it holds are
			// dropped before this payload starts the next one.
			f.data = nil
		}
		f.data = append(f.data, payload)
	}
}

// take returns the frame the reader has collected, and whether it held one.
//
// A frame whose payload does not parse is not returned: it is a frame this
// reader cannot read, and the caller would only try to decode it again.
func (f *Frames) take() ([]byte, bool) {
	if len(f.data) == 0 {
		return nil, false
	}
	payload := strings.Join(f.data, "\n")
	f.data = nil
	if !json.Valid([]byte(payload)) {
		return nil, false
	}
	return []byte(payload), true
}

// frameState says how much of a frame the collected data lines hold.
type frameState int

const (
	// payloadOpen means another data line may still complete the frame.
	payloadOpen frameState = iota
	// payloadWhole means the lines already decode as one payload.
	payloadWhole
	// payloadBroken means no further data line can make them decode.
	payloadBroken
)

// payloadState reports how much of a frame the collected data lines hold.
//
// A payload that ran out without a syntax error is kept for the next line,
// because the frame may be spread over several of them. A payload with a syntax
// error is not: JSON has no construct a later line repairs.
func payloadState(data []string) frameState {
	if len(data) == 0 {
		return payloadOpen
	}
	payload := strings.Join(data, "\n")
	if json.Valid([]byte(payload)) {
		return payloadWhole
	}
	// The decoder tells a payload that ran out (what a payload spread over
	// several lines looks like) from one that cannot be repaired.
	var raw json.RawMessage
	err := json.NewDecoder(bytes.NewReader([]byte(payload))).Decode(&raw)
	switch {
	case err == nil:
		return payloadWhole
	case err == io.ErrUnexpectedEOF || err == io.EOF:
		return payloadOpen
	default:
		return payloadBroken
	}
}
