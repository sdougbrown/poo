package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The record of a call the model wrote as text.
//
// The plan: the calls the transport reads out of the model's own text
// are flagged "parsed_text" in the record, so a reader can tell them
// from the calls the endpoint sent.

// recordingSink keeps the records a run wrote.
type recordingSink struct {
	mu sync.Mutex
	// records holds the records, in order.
	records []poo.Record
}

// Append keeps the record.
func (s *recordingSink) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing.
func (s *recordingSink) Flush(ctx context.Context) error { return nil }

// Messages returns the message records the sink kept.
func (s *recordingSink) Messages() []*poo.MessageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*poo.MessageRecord
	for _, rec := range s.records {
		if msg, ok := rec.(*poo.MessageRecord); ok {
			out = append(out, msg)
		}
	}
	return out
}

// answeringServer answers with the given streams, one per request.
//
// The last stream answers every request after it, so a model that asks
// for a tool once and then answers ends its run. A server that always
// answered with the same tool call would send the run around the
// loop forever, and the run's history would grow with every turn.
type answeringServer struct {
	*httptest.Server
	mu sync.Mutex
	// bodies holds the streams, in order.
	bodies []string
	// served counts the requests the server answered.
	served int
}

// newAnsweringServer starts a server that answers with the streams.
func newAnsweringServer(t *testing.T, bodies ...string) *answeringServer {
	t.Helper()
	s := &answeringServer{bodies: bodies}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		s.mu.Lock()
		body := s.bodies[len(s.bodies)-1]
		if s.served < len(s.bodies) {
			body = s.bodies[s.served]
		}
		s.served++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

// TestTheParsedCallsAreFlaggedInTheRecord covers the flag: a call the
// model wrote as text is recorded as one, and a call the endpoint sent
// is not.
func TestTheParsedCallsAreFlaggedInTheRecord(t *testing.T) {
	srv := newAnsweringServer(t,
		fixture(t, "vllm-text-tool-call.sse"),
		fixture(t, "vllm-answer.sse"),
	)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})

	sink := &recordingSink{}
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "m",
		ContextLimit: 32768,
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
		// The ceiling is the backstop: a run whose model keeps asking
		// for a tool stops at it instead of going on for ever.
		MaxTurns: 4,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}

	var found bool
	for _, msg := range sink.Messages() {
		if msg.Role != poo.RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind != poo.ContentToolCall {
				continue
			}
			found = true
			if block.Origin != poo.CallOriginParsedText {
				t.Errorf("the call's origin is %q, want parsed_text", block.Origin)
			}
		}
	}
	if !found {
		t.Error("the log holds no tool call block")
	}
}

// TestTheWireCallsAreNotFlaggedAsParsed covers the other side: a call
// the endpoint sent carries the wire's own origin.
func TestTheWireCallsAreNotFlaggedAsParsed(t *testing.T) {
	srv := newAnsweringServer(t,
		fixture(t, "vllm-native-tool-calls.sse"),
		fixture(t, "vllm-answer.sse"),
	)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "native"})

	sink := &recordingSink{}
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "m",
		ContextLimit: 32768,
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
		MaxTurns:     4,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}

	var found bool
	for _, msg := range sink.Messages() {
		if msg.Role != poo.RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind != poo.ContentToolCall {
				continue
			}
			found = true
			if block.Origin == poo.CallOriginParsedText {
				t.Error("the call is flagged as parsed, want the wire's own")
			}
		}
	}
	if !found {
		t.Error("the log holds no tool call block")
	}
}
