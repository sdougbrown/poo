package poo

import (
	"errors"
	"fmt"
	"strings"
)

// The compaction seam's application.
//
// The core validates a compaction and applies it to the model's view.
// The store keeps every original record: a compaction changes what the
// model sees, not what the session did.

// ErrInvalidCompaction reports a compaction the core rejected.
//
// A rejected compaction is a compactor failure: below the hard limit
// the run continues uncompacted, and at or above it the run settles
// context_exhausted.
var ErrInvalidCompaction = errors.New("poo: the compaction is invalid")

// CompactionMarkerText is the message the core appends after a
// provider window.
//
// The window is the model's own compacted state, and the marker tells
// the model that its history was compacted.
const CompactionMarkerText = "[context compacted by provider]"

// validateCompaction checks that a compaction is one the core can
// apply.
//
// A compaction carries exactly one form, and the form has to be one the
// core can apply: a replacement over turns that are still in the view,
// a rewrite of results that exist, a window the session's own transport
// produced, or an end with a handoff.
func (s *Session) validateCompaction(c Compaction) error {
	forms := 0
	if c.Replace != nil {
		forms++
	}
	if len(c.Rewrite) > 0 {
		forms++
	}
	if c.Window != nil {
		forms++
	}
	if c.End != nil {
		forms++
	}
	if forms == 0 {
		return fmt.Errorf("%w: it carries no form", ErrInvalidCompaction)
	}
	if forms > 1 {
		return fmt.Errorf("%w: it carries %d forms, and it carries exactly one", ErrInvalidCompaction, forms)
	}
	switch {
	case c.Replace != nil:
		return s.validateReplace(*c.Replace)
	case len(c.Rewrite) > 0:
		return s.validateRewrite(c.Rewrite)
	case c.Window != nil:
		return s.validateWindow(*c.Window)
	default:
		if strings.TrimSpace(c.End.Handoff) == "" {
			return fmt.Errorf("%w: the end carries no handoff", ErrInvalidCompaction)
		}
		return nil
	}
}

// validateReplace checks a replacement over a range of turns.
func (s *Session) validateReplace(r RangeReplacement) error {
	if len(r.Messages) == 0 {
		return fmt.Errorf("%w: the replacement carries no messages", ErrInvalidCompaction)
	}
	for _, m := range r.Messages {
		if strings.TrimSpace(m.Text()) == "" {
			return fmt.Errorf("%w: the replacement carries a message with no visible text", ErrInvalidCompaction)
		}
	}
	if r.Cut.First < 1 {
		return fmt.Errorf("%w: the cut starts at the turn %d", ErrInvalidCompaction, r.Cut.First)
	}
	if r.Cut.Last < r.Cut.First {
		return fmt.Errorf("%w: the cut %d to %d is empty", ErrInvalidCompaction, r.Cut.First, r.Cut.Last)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.cutSpansLocked(r.Cut)
	return err
}

// validateRewrite checks the rewrites of tool results.
//
// A rewrite names a handle the session knows, and the new content keeps
// the handle and carries the compacted marker, so the model can tell
// the result was rewritten.
func (s *Session) validateRewrite(rewrites []ToolResultRewrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rw := range rewrites {
		meta, ok := s.resultMeta[rw.Handle]
		if !ok {
			return fmt.Errorf("%w: the handle %q is not a call of this session", ErrInvalidCompaction, rw.Handle)
		}
		if _, ok := s.results[rw.Handle]; !ok {
			return fmt.Errorf("%w: the handle %q has no result", ErrInvalidCompaction, rw.Handle)
		}
		if !strings.HasPrefix(rw.Content, CompactedHandleLine(rw.Handle, meta.tool)) {
			return fmt.Errorf("%w: the rewrite of %q does not start with %q",
				ErrInvalidCompaction, rw.Handle, CompactedHandleLine(rw.Handle, meta.tool))
		}
	}
	return nil
}

// validateWindow checks a provider window.
//
// The window is the provider's own compacted state, so it can only come
// from the session's own transport, and every one of its items is
// required provider data.
func (s *Session) validateWindow(w ProviderWindow) error {
	if len(w.Items) == 0 {
		return fmt.Errorf("%w: the window carries no items", ErrInvalidCompaction)
	}
	if _, ok := s.opts.Transport.(NativeCompactor); !ok {
		return fmt.Errorf("%w: the session's transport cannot compact", ErrInvalidCompaction)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || !s.run.hasRendered {
		return fmt.Errorf("%w: the session's transport has no request to compact", ErrInvalidCompaction)
	}
	api := s.run.lastRendered.API
	for _, item := range w.Items {
		if item.Provider == nil {
			return fmt.Errorf("%w: the window holds an item that is not provider data", ErrInvalidCompaction)
		}
		if !item.Provider.Required {
			return fmt.Errorf("%w: the window holds an item that is not required", ErrInvalidCompaction)
		}
		if item.Provider.API != api {
			return fmt.Errorf("%w: the window holds %s data, and the transport speaks %s",
				ErrInvalidCompaction, item.Provider.API, api)
		}
	}
	return nil
}

// applyCompaction applies a compaction to the model's view.
//
// The caller has validated the compaction. The application changes only
// the view: the store keeps every original record.
func (s *Session) applyCompaction(c Compaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case c.Replace != nil:
		return s.applyReplaceLocked(*c.Replace)
	case len(c.Rewrite) > 0:
		return s.applyRewriteLocked(c.Rewrite)
	case c.Window != nil:
		return s.applyWindowLocked(*c.Window)
	}
	return nil
}

// cutSpansLocked returns the spans a cut covers.
//
// The cut must name turns that exist and that are still in the view: a
// range that reaches over a turn a compaction removed is not
// contiguous, and the core rejects it.
//
// The caller holds the session's lock.
func (s *Session) cutSpansLocked(cut TurnRange) ([]turnSpan, error) {
	var out []turnSpan
	for turn := cut.First; turn <= cut.Last; turn++ {
		span, ok := s.spanForTurnLocked(turn)
		if !ok {
			return nil, fmt.Errorf("%w: the turn %d is not in the session", ErrInvalidCompaction, turn)
		}
		if span.removed {
			return nil, fmt.Errorf("%w: the turn %d was compacted away", ErrInvalidCompaction, turn)
		}
		out = append(out, span)
	}
	return out, nil
}

// applyReplaceLocked replaces a range of turns with visible messages.
//
// The caller holds the session's lock.
func (s *Session) applyReplaceLocked(r RangeReplacement) error {
	spans, err := s.cutSpansLocked(r.Cut)
	if err != nil {
		return err
	}
	// The replacement is visible: the model reads it as a user message
	// that says its history was replaced.
	replacement := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		m.Role = RoleUser
		m.Origin = OriginCompaction
		m.ID = ""
		m.DeliveryNote = nil
		replacement = append(replacement, m)
	}
	// The cut's messages leave the view. The messages between two cut
	// turns stay: a message that arrived between the turns is not part
	// of either.
	out := make([]Message, 0, len(s.history))
	pos := 0
	removed := 0
	for i, span := range spans {
		out = append(out, s.history[pos:span.start]...)
		if i == 0 {
			out = append(out, replacement...)
		}
		removed += span.end - span.start
		pos = span.end
	}
	out = append(out, s.history[pos:]...)
	s.history = out

	// The spans after the cut move with the view, and the cut's own
	// turns are gone.
	shift := len(replacement) - removed
	for i := range s.spans {
		switch {
		case s.spans[i].turn < r.Cut.First:
			continue
		case s.spans[i].turn <= r.Cut.Last:
			s.spans[i].removed = true
			s.spans[i].start = 0
			s.spans[i].end = 0
		default:
			s.spans[i].start += shift
			s.spans[i].end += shift
		}
	}
	return nil
}

// applyRewriteLocked rewrites the content of tool results.
//
// The call and the result stay paired: the result's message keeps its
// place and its call id, and only its content changes.
//
// The caller holds the session's lock.
func (s *Session) applyRewriteLocked(rewrites []ToolResultRewrite) error {
	for _, rw := range rewrites {
		idx := s.resultMessageIndexLocked(rw.Handle)
		if idx < 0 {
			return fmt.Errorf("%w: the result %q is not in the view", ErrInvalidCompaction, rw.Handle)
		}
		msg := &s.history[idx]
		replaced := false
		for i := range msg.Content {
			if msg.Content[i].Kind != ContentToolResult {
				continue
			}
			msg.Content[i].Text = rw.Content
			replaced = true
		}
		if !replaced {
			return fmt.Errorf("%w: the result %q is not in the view", ErrInvalidCompaction, rw.Handle)
		}
		// The stored result follows the view.
		result := s.results[rw.Handle]
		result.Text = rw.Content
		s.results[rw.Handle] = result
	}
	return nil
}

// resultMessageIndexLocked returns the index of the message that
// carries a result.
//
// The handle line names the result, so the message is found by the line
// the core put there.
//
// The caller holds the session's lock.
func (s *Session) resultMessageIndexLocked(handle string) int {
	prefix := "[" + handle + " ·"
	for i, m := range s.history {
		for _, c := range m.Content {
			if c.Kind == ContentToolResult && strings.HasPrefix(c.Text, prefix) {
				return i
			}
		}
	}
	return -1
}

// applyWindowLocked makes the view the provider's window.
//
// The window is the model's own compacted state, so the view becomes
// it, and the marker message tells the model that its history was
// compacted.
//
// The caller holds the session's lock.
func (s *Session) applyWindowLocked(w ProviderWindow) error {
	window := Message{Role: RoleUser, Content: w.Items, Origin: OriginCompaction}
	marker := NewTextMessage(RoleUser, CompactionMarkerText)
	marker.Origin = OriginCompaction
	s.history = []Message{window, marker}
	// The window is the whole model view, so the run's frozen prompt
	// goes with it: the provider's own compacted state is what the
	// model reads from here on.
	if s.run != nil {
		s.run.systemMessage = nil
	}
	for i := range s.spans {
		s.spans[i].removed = true
		s.spans[i].start = 0
		s.spans[i].end = 0
	}
	return nil
}
