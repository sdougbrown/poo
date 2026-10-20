package openai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestEventsSeparatedByOneNewlineAreRead covers a server that separates
// its events with a single newline instead of a blank line: every event
// still reaches the caller.
func TestEventsSeparatedByOneNewlineAreRead(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"one\"}}]}\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"two\"},\"finish_reason\":\"stop\"}]}\n" +
		"data: [DONE]\n"

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if got := textOf(events); got != "onetwo" {
		t.Errorf("the text is %q, want onetwo", got)
	}
	var finish poo.FinishReason
	for _, ev := range events {
		if ev.Kind == "finish" {
			finish = ev.FinishReason
		}
	}
	if finish != poo.FinishStop {
		t.Errorf("the finish reason is %q, want stop", finish)
	}
}

// TestAnEventSplitOverTwoDataLinesIsStillJoined covers a server that
// separates its events with a single newline and splits one event's
// payload over two data lines: the two lines are one event.
func TestAnEventSplitOverTwoDataLinesIsStillJoined(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\n" +
		"data: \"delta\":{\"content\":\"multi\"},\"finish_reason\":\"stop\"}]}\n"

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if got := textOf(events); got != "multi" {
		t.Errorf("the text is %q, want multi", got)
	}
}

// TestEventsSeparatedByABlankLineAreRead covers the ordinary shape: a
// blank line ends the event.
func TestEventsSeparatedByABlankLineAreRead(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"one"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"content":"two"},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if got := textOf(events); got != "onetwo" {
		t.Errorf("the text is %q, want onetwo", got)
	}
}

// TestTheEventsArriveInOrder covers the reader's order: the events come
// out in the order the endpoint sent them, whichever shape the stream
// used.
func TestTheEventsArriveInOrder(t *testing.T) {
	shapes := map[string]string{
		"one newline": "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"one\"}}]}\n" +
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"two\"},\"finish_reason\":\"stop\"}]}\n" +
			"data: [DONE]\n",
		"blank line": chunk(`{"choices":[{"index":0,"delta":{"content":"one"}}]}`) +
			chunk(`{"choices":[{"index":0,"delta":{"content":"two"},"finish_reason":"stop"}]}`) + done,
	}
	for name, body := range shapes {
		t.Run(name, func(t *testing.T) {
			srv := newSSEServer(t, body)
			tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

			reader, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()

			var kinds []string
			var text string
			for _, ev := range read(t, reader) {
				kinds = append(kinds, ev.Kind)
				if ev.Kind == "delta" && ev.Delta != nil {
					text += ev.Delta.Text
				}
			}
			if text != "onetwo" {
				t.Errorf("the text is %q, want onetwo", text)
			}
			// The finish reason comes after the text it ends, and the
			// usage report comes last.
			want := []string{"delta", "delta", "finish", "usage"}
			if strings.Join(kinds, ",") != strings.Join(want, ",") {
				t.Errorf("the events are %v, want %v", kinds, want)
			}
		})
	}
}
