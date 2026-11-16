package support

import (
	"context"
	"io"
)

// Closer releases a response once.
//
// A reader hands out the transport's answer, and the transport's bound has to
// be released with the body: a request whose context is cancelled only when the
// stream ends holds its connection until then. Both are released once, because
// a caller may close a reader it already closed.
type Closer struct {
	// Body is the response body.
	Body io.ReadCloser
	// Cancel releases the request's context. Nil when the transport set no
	// bound of its own.
	Cancel context.CancelFunc
	// closed reports that the release already happened.
	closed bool
}

// Close releases the response.
func (c *Closer) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	if c.Cancel != nil {
		c.Cancel()
	}
	if c.Body == nil {
		return nil
	}
	return c.Body.Close()
}
