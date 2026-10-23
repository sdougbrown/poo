package bash

import (
	"sync"
	"unicode/utf8"
)

// CappedBuffer collects output up to a limit and discards the rest.
//
// Write always reports the whole input as written, so the reader
// draining the pipe never blocks on a full buffer.
type CappedBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
	// dropped reports that the buffer refused output, so a caller can
	// say the capture is not the whole stream.
	dropped bool
}

// NewCappedBuffer returns a buffer that keeps at most limit bytes.
func NewCappedBuffer(limit int) *CappedBuffer {
	return &CappedBuffer{limit: limit}
}

// Write appends to the buffer until it is full, then discards.
func (b *CappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	if len(b.buf) < b.limit {
		n = b.limit - len(b.buf)
		if n > len(p) {
			n = len(p)
		}
		b.buf = append(b.buf, p[:n]...)
	}
	if n < len(p) {
		b.dropped = true
	}
	return len(p), nil
}

// Truncated reports whether the buffer discarded output.
func (b *CappedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// String returns the captured output.
//
// A cut that lands in the middle of a character drops the partial one.
// Bytes the command printed are otherwise kept as they are, valid UTF-8
// or not.
func (b *CappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < b.limit {
		return string(b.buf)
	}
	return trimCutRune(string(b.buf))
}

// trimCutRune drops the start of a multi-byte rune that a cut left
// incomplete at the end of a string.
func trimCutRune(s string) string {
	// A rune is at most utf8.UTFMax bytes, so its start is within the
	// last few bytes.
	for i := len(s) - 1; i >= 0 && i >= len(s)-utf8.UTFMax; i-- {
		if utf8.RuneStart(s[i]) {
			if s[i] >= utf8.RuneSelf && !utf8.FullRuneInString(s[i:]) {
				return s[:i]
			}
			return s
		}
	}
	return s
}
