package chat_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/chat"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// errorEvent is an error event the endpoint sent inside the stream.
func errorEvent(message string) string {
	return `data: {"error":{"message":"` + message + `","type":"invalid_request_error"}}` + "\n\n"
}

// nextError reads the stream until it fails.
func nextError(t *testing.T, reader *chat.Reader) error {
	t.Helper()
	for i := 0; i < 8; i++ {
		if _, err := reader.Next(); err != nil {
			return err
		}
	}
	t.Fatalf("the stream did not fail")
	return nil
}

// streamReader reads a scripted stream body.
func streamReader(body string) *chat.Reader {
	return chat.NewReader(support.Stream{
		Body:    io.NopCloser(strings.NewReader(body)),
		Reader:  bufio.NewReader(strings.NewReader(body)),
		Context: context.Background(),
		Parent:  context.Background(),
	}, chat.ReaderOptions{})
}

// TestAMaxTokensRejectionInAStreamIsAnOutputLimit covers the same rejection
// sent as an error event rather than a 400. The stream is already open, so
// the event carries no status, and the words beside the field name are what
// classify it: without the class the session settles instead of asking
// again under the cap the endpoint named.
func TestAMaxTokensRejectionInAStreamIsAnOutputLimit(t *testing.T) {
	reader := streamReader(errorEvent("max_tokens is too large: 32768. This model supports at most 16384 completion tokens."))
	defer reader.Close()

	err := nextError(t, reader)
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the failure is %v, want a provider error", err)
	}
	if perr.Kind != poo.ProviderKindOutputLimit {
		t.Errorf("the failure is %q, want %q", perr.Kind, poo.ProviderKindOutputLimit)
	}
	if perr.OutputLimit != 16384 {
		t.Errorf("the cap is %d, want 16384", perr.OutputLimit)
	}
}

// TestAStreamErrorNamingOnlyMaxTokensKeepsItsClass covers the other half of
// the rule: a statusless event that names the field without calling it too
// large is the server's failure, the way the same words under a 400 are a
// rejection with nothing to learn.
func TestAStreamErrorNamingOnlyMaxTokensKeepsItsClass(t *testing.T) {
	reader := streamReader(errorEvent("This request's input plus max_tokens exceeds the model's limit"))
	defer reader.Close()

	err := nextError(t, reader)
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the failure is %v, want a provider error", err)
	}
	if perr.Kind != poo.ProviderKindServer {
		t.Errorf("the failure is %q, want %q", perr.Kind, poo.ProviderKindServer)
	}
}
