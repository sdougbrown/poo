package poo

import (
	"context"
	"errors"
	"testing"
)

// stubTransport is a transport that is never asked to stream: the
// session it serves is broken before anything runs.
type stubTransport struct{}

func (stubTransport) Stream(context.Context, ChatRequest) (StreamReader, Rendered, error) {
	return nil, Rendered{}, errors.New("not used")
}

func (stubTransport) Fork(context.Context, Rendered, []Message, int) (StreamReader, error) {
	return nil, errors.New("not used")
}

// TestAReasoningSettingOnABrokenSessionIsRefused covers the broken
// guard a level is set behind: a session whose store failed accepts
// no level, whatever the level is.
func TestAReasoningSettingOnABrokenSessionIsRefused(t *testing.T) {
	s, err := New(Options{Transport: stubTransport{}, SessionID: "s2", Model: ModelInfo{Name: "m", ModelID: "m", Endpoint: "test", ContextLimit: 8192}, WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	s.markBroken()
	if err := s.SetReasoning("high"); !errors.Is(err, ErrSessionBroken) {
		t.Errorf("a broken session took the level %v, want ErrSessionBroken", err)
	}
	if got := s.ReasoningLevel(); got != "" {
		t.Errorf("the broken session runs at %q, want no level", got)
	}
}
