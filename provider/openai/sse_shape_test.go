package openai_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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

// TestAnEventBeforeABrokenConnectionIsKept covers a server that separates
// its events with a single newline and then drops the connection: the
// last complete event still reaches the caller ahead of the failure.
func TestAnEventBeforeABrokenConnectionIsKept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijacking the connection: %v", err)
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		event := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"kept\"}}]}\n"
		_, _ = fmt.Fprintf(buf, "%x\r\n%s\r\n", len(event), event)
		_ = buf.Flush()
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
	}))
	defer srv.Close()
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var text string
	var failed bool
	for {
		ev, err := reader.Next()
		if err != nil {
			failed = !errors.Is(err, io.EOF)
			break
		}
		if ev.Kind == "delta" && ev.Delta != nil {
			text += ev.Delta.Text
		}
	}
	if text != "kept" {
		t.Errorf("the text is %q, want the event that arrived before the break", text)
	}
	if !failed {
		t.Error("the stream ended cleanly, want the broken connection reported")
	}
}
