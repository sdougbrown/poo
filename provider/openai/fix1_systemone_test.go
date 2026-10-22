package openai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sdougbrown/poo/decide"
)

// TestAnInvalidQuestionResetsTheRequestCount covers Requests after a
// call that was refused: the count belongs to the last call, and that
// call made no request.
func TestAnInvalidQuestionResetsTheRequestCount(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"approve": 0.5, "deny": 0.5},
		"choice":        "approve",
	}
	dec := srv.decider(t, 0)

	if _, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")}); err != nil {
		t.Fatalf("asking the valid question: %v", err)
	}
	if got := dec.Requests(); got != 1 {
		t.Fatalf("the valid call reports %d requests, want 1", got)
	}

	options := make([]decide.Option, 0, 27)
	for i := 0; i < 27; i++ {
		options = append(options, decide.Option{Name: "option"})
	}
	question := decide.Question{
		ID:           "q2",
		Instructions: "which one?",
		Kind:         decide.Choice{Options: options},
	}
	if _, err := dec.Decide(context.Background(), "the state", "", []decide.Question{question}); !errors.Is(err, decide.ErrInvalidQuestion) {
		t.Fatalf("the invalid call failed with %v, want an invalid question", err)
	}
	if got := dec.Requests(); got != 0 {
		t.Errorf("the refused call reports %d requests, want none", got)
	}
}
