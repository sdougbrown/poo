package poo

// The session's turns.
//
// A turn is one provider request and the batch it produced. The turns
// are numbered twice: the run's turn number counts from one in every
// run, and the session's turn number counts from one for the life of
// the session.
//
// The session's number is the one the compaction seam uses: a
// compaction may cut a range of turns that crosses a run boundary, and
// a turn keeps its number when a compaction removes it.

// turnSpan is the part of the view one turn occupies.
//
// The span is a range of message indexes in the view. A compaction that
// replaces a turn's messages changes the view, and the spans after it
// move with it.
type turnSpan struct {
	// turn is the turn's number in the session.
	turn int
	// run is the turn's number in its run.
	run int
	// start is the index of the turn's first message in the view.
	start int
	// end is the index after the turn's last message in the view.
	end int
	// removed reports that the turn's messages are no longer in the
	// view.
	removed bool
}

// resultMeta is what the session knows about one tool result.
//
// The core needs the tool's name and the provider's id for the call to
// validate a compaction's rewrites and to find the result in the view.
type resultMeta struct {
	// tool is the name of the tool the model called.
	tool string
	// callID is the provider's identifier for the call.
	callID string
	// turn is the session's number for the turn the call was made in.
	turn int
}

// openTurn starts a turn and returns its session number.
//
// The turn's span covers the messages the turn adds to the view: the
// model's reply and the results of the calls it made.
func (s *Session) openTurn(runTurn int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnSeq++
	turn := s.turnSeq
	s.spans = append(s.spans, turnSpan{
		turn:  turn,
		run:   runTurn,
		start: len(s.history),
		end:   len(s.history),
	})
	s.turns = append(s.turns, TurnSummary{Index: runTurn, Session: turn})
	return turn
}

// growSpan moves the open turn's end to the end of the view.
func (s *Session) growSpan(turn int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.growSpanLocked(turn)
}

// growSpanLocked moves the open turn's end to the end of the view.
//
// The caller holds the session's lock.
func (s *Session) growSpanLocked(turn int) {
	if turn < 1 || turn > len(s.spans) {
		return
	}
	span := &s.spans[turn-1]
	if span.removed {
		return
	}
	span.end = len(s.history)
}

// spanForTurn returns the span of a turn.
//
// The caller holds the session's lock.
func (s *Session) spanForTurnLocked(turn int) (turnSpan, bool) {
	if turn < 1 || turn > len(s.spans) {
		return turnSpan{}, false
	}
	return s.spans[turn-1], true
}
