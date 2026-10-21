package poo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// The message ids are session-monotonic: they never repeat within a
// session, and a second run continues where the first one stopped.

// TestTheMessageIdsCountUp covers the counter: the ids are m1, m2, m3.
func TestTheMessageIdsCountUp(t *testing.T) {
	var ids poo.MessageIDs
	for _, want := range []string{"m1", "m2", "m3"} {
		if got := ids.Next(); got != want {
			t.Errorf("the id is %q, want %q", got, want)
		}
	}
}

// TestTheMessageIdsPeekWithoutTakingOne covers the peek: it says what the
// next id will be without handing it out.
func TestTheMessageIdsPeekWithoutTakingOne(t *testing.T) {
	var ids poo.MessageIDs
	if got := ids.Peek(); got != "m1" {
		t.Errorf("the peeked id is %q, want m1", got)
	}
	if got := ids.Next(); got != "m1" {
		t.Errorf("the id is %q, want m1", got)
	}
	if got := ids.Peek(); got != "m2" {
		t.Errorf("the peeked id is %q, want m2", got)
	}
}

// TestTheMessageIdsCarryOnAfterAReload covers the reload: a session that
// reloads its records carries on after the ids it already assigned.
func TestTheMessageIdsCarryOnAfterAReload(t *testing.T) {
	var ids poo.MessageIDs
	ids.Next()
	ids.Next()
	ids.Set(7)
	if got := ids.Next(); got != "m8" {
		t.Errorf("the id is %q, want m8", got)
	}
	// The ids never go backwards.
	ids.Set(2)
	if got := ids.Next(); got != "m9" {
		t.Errorf("the id is %q, want m9", got)
	}
}

// TestTheRunsIdsContinueAcrossRuns covers the session's counter: a second
// run's ids do not repeat the first run's.
func TestTheRunsIdsContinueAcrossRuns(t *testing.T) {
	ids := &poo.MessageIDs{}
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.MessageIDs = ids

	for run := 1; run <= 2; run++ {
		if _, err := poo.NewRun(opts).Execute(context.Background(), "do the thing"); err != nil {
			t.Fatalf("run %d failed: %v", run, err)
		}
	}

	var ids_ []string
	for _, m := range sink.messages() {
		if m.Origin != poo.OriginModel {
			continue
		}
		ids_ = append(ids_, m.MessageID)
	}
	if len(ids_) != 2 {
		t.Fatalf("the log holds %d model messages, want 2", len(ids_))
	}
	if ids_[0] != "m1" || ids_[1] != "m2" {
		t.Errorf("the message ids are %v, want m1 and m2", ids_)
	}
}

// TestTheMessageRecordCarriesTheId covers the record: the model's
// message carries the id, and the user's does not.
func TestTheMessageRecordCarriesTheId(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	if _, err := poo.NewRun(testOptions(t, tr, tool, sink)).Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	messages := sink.messages()
	if len(messages) != 3 {
		t.Fatalf("the log holds %d messages, want 3", len(messages))
	}
	if messages[0].MessageID != "" {
		t.Errorf("the user's message carries the id %q, want none", messages[0].MessageID)
	}
	if messages[2].MessageID != "m1" {
		t.Errorf("the model's message carries the id %q, want m1", messages[2].MessageID)
	}

	// The id reaches the log as a string.
	raw, err := json.Marshal(messages[2])
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded["message_id"] != "m1" {
		t.Errorf("the record's message id is %v, want m1", decoded["message_id"])
	}
}
