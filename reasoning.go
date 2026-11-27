package poo

import (
	"errors"
	"strings"
)

// The session's reasoning level.
//
// The level is the session's, not the model's: a switch takes the model's
// own levels into account, and the runs that follow send the level the
// session is at.

// SetReasoning sets the reasoning level the requests that follow ask for.
//
// The level is one the session's chat model takes: one of the model's own
// levels when it declares them, and otherwise whatever its reasoning
// control accepts. See ModelInfo.CheckReasoning for the rule, and for the
// refusal that names the levels the model does take.
//
// The change happens between the runs, like a model switch: a session with a
// run going refuses it, so the level a run started with is the level its
// requests carry.
//
// An empty level sends no reasoning setting at all, which is what a
// session that never named one does.
func (s *Session) SetReasoning(level string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	if s.broken {
		return ErrSessionBroken
	}
	if s.run != nil || s.reserved {
		return ErrBusy
	}
	// The session's own model is what the level is checked against: a
	// switch may have moved it since the session was built.
	if err := s.opts.Model.CheckReasoning(level); err != nil {
		// A refused level is the client's to act on, and the message is
		// what it acts on: it names the levels the model takes. The
		// settings field the check was written for is not the client's.
		var config *ConfigError
		if errors.As(err, &config) {
			return &ErrInvalidInput{Message: config.Message}
		}
		return err
	}
	s.opts.ReasoningEffort = strings.ToLower(strings.TrimSpace(level))
	return nil
}

// ReasoningLevel returns the reasoning level the requests send.
//
// It is empty when the session sends none.
func (s *Session) ReasoningLevel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opts.ReasoningEffort
}

// resetReasoningLocked takes the session's reasoning level back to one the
// model it now talks to can run at.
//
// A switch that left the level alone would make every request a
// configuration error, so a level the new model does not take falls back
// to off. Off is the one setting every model takes: a model that declares
// an off word or a switch is asked for quiet explicitly, a model whose
// effort control has no off word is sent its quietest declared level, and
// a model that controls no reasoning, or that thinks by budget, is sent
// no reasoning field at all. The request that asks for quiet is the same
// one in every case: the closest to off the model's wire can say.
//
// The caller holds the session's lock.
func (s *Session) resetReasoningLocked() {
	level := s.opts.ReasoningEffort
	if level == "" || s.opts.Model.CheckReasoning(level) == nil {
		return
	}
	s.opts.ReasoningEffort = "off"
}
