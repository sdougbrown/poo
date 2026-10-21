package poo_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The delivery note says how much of the last reply the user had seen.
// The note is rendered when the input is accepted, and it is part of the
// recorded message: the assistant message is never trimmed, and the note
// is the record.

// TestASteerWithADeliveryNoteCarriesIt covers the note: the delivered
// message carries the note, and the assistant's message is untouched.
func TestASteerWithADeliveryNoteCarriesIt(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			text: "the first answer",
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	// The model's first message has the id m1.
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 5)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}

	// The delivered message carries the note.
	var delivered *poo.MessageRecord
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginSteer {
			delivered = m
		}
	}
	if delivered == nil {
		t.Fatal("the log holds no steered message")
	}
	if delivered.DeliveryNote == nil {
		t.Fatal("the steered message carries no delivery note")
	}
	if delivered.DeliveryNote.MessageID != "m1" {
		t.Errorf("the note names %q, want m1", delivered.DeliveryNote.MessageID)
	}
	if delivered.DeliveryNote.UpToChars != 5 {
		t.Errorf("the note counts %d characters, want 5", delivered.DeliveryNote.UpToChars)
	}
	// The note is rendered.
	if !strings.Contains(delivered.DeliveryNote.Text, "m1") {
		t.Errorf("the note is %q, want it to name the message", delivered.DeliveryNote.Text)
	}
	if !strings.Contains(delivered.DeliveryNote.Text, "up to") {
		t.Errorf("the note is %q, want it to say how much was seen", delivered.DeliveryNote.Text)
	}
	// The assistant's message is byte-identical: the note never edits
	// it.
	for _, m := range sink.messages() {
		if m.Origin != poo.OriginModel {
			continue
		}
		if strings.Contains(m.Content[0].Text, "The user saw") {
			t.Errorf("the assistant's message was edited: %q", m.Content[0].Text)
		}
	}
}

// TestADeliveryNoteAtTheEndAddsNothing covers the plan's rule: a count
// at or past the end means the whole reply was presented, and no note
// is added.
func TestADeliveryNoteAtTheEndAddsNothing(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	// The count is past the end of the model's message.
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 1000)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	for _, m := range sink.messages() {
		if m.Origin != poo.OriginSteer {
			continue
		}
		if m.DeliveryNote != nil && m.DeliveryNote.Text != "" {
			t.Errorf("the note is %q, want no note", m.DeliveryNote.Text)
		}
	}
}

// TestADeliveryNoteOfZeroSaysNothingWasSeen covers the plan's rule: a
// count of zero has its own wording.
func TestADeliveryNoteOfZeroSaysNothingWasSeen(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 0)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	for _, m := range sink.messages() {
		if m.Origin != poo.OriginSteer {
			continue
		}
		if m.DeliveryNote == nil {
			t.Fatal("the steered message carries no delivery note")
		}
		want := "[The user did not see or hear any of your reply m1.]"
		if m.DeliveryNote.Text != want {
			t.Errorf("the note is %q, want %q", m.DeliveryNote.Text, want)
		}
	}
}

// TestAnUnknownMessageIDIsRefused covers the plan's rule: an unknown id
// is refused, and nothing is queued.
func TestAnUnknownMessageIDIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	_, err := s.Send("and this", poo.Steer, poo.WithDelivered("m999", 5))
	if err == nil {
		t.Fatal("the session accepted a note for an unknown message")
	}
	var invalid *poo.ErrInvalidInput
	if !asInvalidInput(err, &invalid) {
		t.Fatalf("the error is %T, want an ErrInvalidInput", err)
	}
	// Nothing was queued.
	if sink.count("input.queued") != 0 {
		t.Errorf("the log holds %d input.queued records, want none", sink.count("input.queued"))
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestANegativeCharacterCountIsRefused covers the other half of the
// rule: a negative count is refused.
func TestANegativeCharacterCountIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	_, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", -1))
	if err == nil {
		t.Fatal("the session accepted a negative character count")
	}
	var invalid *poo.ErrInvalidInput
	if !asInvalidInput(err, &invalid) {
		t.Fatalf("the error is %T, want an ErrInvalidInput", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestTheNoteCountsCodePointsOverTheTextBlocks covers the plan's rule:
// the count is over the text blocks, and the thinking is left out.
func TestTheNoteCountsCodePointsOverTheTextBlocks(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			text:     "héllo wörld",
			thinking: "a long thought that is not counted",
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	// The message is eleven code points long.
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 5)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	for _, m := range sink.messages() {
		if m.Origin != poo.OriginSteer {
			continue
		}
		if m.DeliveryNote == nil {
			t.Fatal("the steered message carries no delivery note")
		}
		// The quote holds the first five code points, and the accent
		// counts as one.
		if !strings.Contains(m.DeliveryNote.Text, "héllo") {
			t.Errorf("the note is %q, want the presented prefix", m.DeliveryNote.Text)
		}
	}
}

// TestTheQuoteEscapesTheQuotesAndNewlines covers the plan's rule: the
// quoted text cannot break the note's own quoting.
func TestTheQuoteEscapesTheQuotesAndNewlines(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			text: "he said \"hi\"\nthen left",
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	// The count is inside the message, so the note quotes the prefix.
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 20)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	for _, m := range sink.messages() {
		if m.Origin != poo.OriginSteer {
			continue
		}
		if m.DeliveryNote == nil {
			t.Fatal("the steered message carries no delivery note")
		}
		text := m.DeliveryNote.Text
		if !strings.Contains(text, `\"hi\"`) {
			t.Errorf("the note is %q, want the quotes escaped", text)
		}
		if !strings.Contains(text, `\n`) {
			t.Errorf("the note is %q, want the newline escaped", text)
		}
		// The note is one line.
		if strings.Contains(text, "\n") {
			t.Errorf("the note spans %d lines, want one", strings.Count(text, "\n")+1)
		}
	}
}

// TestADeliveryNoteOnAPrompt covers the note on a new run: the prompt
// carries it.
func TestADeliveryNoteOnAPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	// The reference names a message the session has assigned an id to.
	if _, err := s.Prompt(context.Background(), "and this", poo.WithDelivered("m1", 0)); err == nil {
		t.Fatal("the session accepted a note for a message it never made")
	}
	// A run that produced a message, and then a prompt that refers to
	// it.
	runSession(t, s, "do the thing")
	if _, err := s.Prompt(context.Background(), "and this", poo.WithDelivered("m1", 3)); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The prompt carries the note.
	found := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginUser && m.DeliveryNote != nil {
			found = true
		}
	}
	if !found {
		t.Error("the prompt carries no delivery note")
	}
}

// TestADroppedInputsNoteIsDroppedWithIt covers the plan's rule: a
// dropped input's note is dropped with it.
func TestADroppedInputsNoteIsDroppedWithIt(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	// The message the note names is the one that was streaming, and its
	// id was assigned at the first delta.
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 3)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The dropped record names the input, and the note is gone with it.
	dropped := 0
	for _, rec := range sink.allRecords() {
		d, ok := rec.(*poo.InputDropped)
		if !ok {
			continue
		}
		dropped++
		if d.Mode != poo.Steer {
			t.Errorf("the dropped input's mode is %q, want steer", d.Mode)
		}
		if d.Reason != poo.ReasonCancelled {
			t.Errorf("the dropped input's reason is %q, want cancelled", d.Reason)
		}
	}
	if dropped != 1 {
		t.Errorf("the log holds %d input.dropped records, want 1", dropped)
	}
}

// noteHold holds a run in its tool's call, so a test can act at a known
// point: the model's first message has been recorded by then.
type noteHold struct {
	// running is closed once the batch is running.
	running chan struct{}
	// release is closed to let the batch finish.
	release chan struct{}
	// once guards the running channel.
	once sync.Once
}

// newNoteHold returns a hold and the tool that waits on it.
func newNoteHold(tool *echoTool) *noteHold {
	h := &noteHold{running: make(chan struct{}), release: make(chan struct{})}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		h.once.Do(func() { close(h.running) })
		select {
		case <-h.release:
		case <-ctx.Done():
		}
		return poo.ToolResult{Text: "done"}, nil
	}
	return h
}

// wait blocks until the batch is running.
func (h *noteHold) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.running:
	case <-time.After(5 * time.Second):
		t.Fatal("the batch never started")
	}
}

// finish lets the batch finish.
func (h *noteHold) finish() { close(h.release) }

// asInvalidInput reports that the error is an ErrInvalidInput.
func asInvalidInput(err error, target **poo.ErrInvalidInput) bool {
	for err != nil {
		if e, ok := err.(*poo.ErrInvalidInput); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestTheDeliveryNoteReachesTheModel covers the plan's rule: the note is
// prefixed to the user message, so the next request carries it.
func TestTheDeliveryNoteReachesTheModel(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			text:  "the first answer",
			calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("and this", poo.Steer, poo.WithDelivered("m1", 9)); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the run made %d requests, want 2", len(reqs))
	}
	var steered string
	for _, m := range reqs[1].Messages {
		if m.Origin == poo.OriginSteer {
			steered = m.Text()
		}
	}
	want := `[The user saw or heard your reply m1 only up to: "the first"]`
	if !strings.HasPrefix(steered, want) {
		t.Errorf("the steered message is %q, want it to start with %q", steered, want)
	}
	if !strings.HasSuffix(steered, "and this") {
		t.Errorf("the steered message is %q, want it to end with the user's text", steered)
	}
	// The record holds the same message the model saw.
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginSteer && m.Content[0].Text != steered {
			t.Errorf("the recorded message is %q, but the model saw %q", m.Content[0].Text, steered)
		}
	}
}

// TestTheNoteForAStreamingReplyIsRenderedAtDelivery covers a reference
// accepted mid-stream: the note is rendered from the reply once the
// interrupted stream has been recorded.
func TestTheNoteForAStreamingReplyIsRenderedAtDelivery(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	sendWhenKnown(t, s, "stop that", poo.Interrupt, "m1", 4)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	want := `[The user saw or heard your reply m1 only up to: "part"]`
	found := false
	for _, m := range sink.messages() {
		if m.Origin != poo.OriginInterrupt {
			continue
		}
		found = true
		if m.DeliveryNote == nil || m.DeliveryNote.Text != want {
			t.Errorf("the delivered message carries note %+v, want %q", m.DeliveryNote, want)
		}
	}
	if !found {
		t.Fatal("the log holds no interrupt message")
	}
}
