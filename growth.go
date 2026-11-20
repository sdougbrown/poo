package poo

import "math"

// The turn-growth tracker.
//
// A session that measures turn growth adds what one turn costs to the fit
// rule. A compaction then happens before a turn's own message and tool
// results push the prompt over the limit, rather than after. What a turn
// costs is measured, not assumed: it is the increase in the input the
// endpoint reports between two of the session's requests.

// growthReports is how many of the most recent positive differences the
// tracker keeps.
//
// Turn growth is the largest of them, so one large turn moves the trigger for
// the turns that follow it. An older one then ages out instead of pinning
// the trigger for the rest of the run.
const growthReports = 4

// growthTracker measures how many tokens one turn adds to the prompt.
//
// The zero value is a tracker with no report to measure against: its first
// report sets the baseline and records no difference.
type growthTracker struct {
	// prev is the input the previous report gave. Zero means there is no
	// report to measure against. None has arrived, or a compaction moved the
	// view and the next report is the new baseline.
	prev int
	// diffs are the positive differences of the most recent reports, oldest
	// first, at most growthReports of them.
	diffs []int
}

// observe records a report's input.
//
// Only an increase is a difference. An input that went down reports a view
// that did not grow, and a drop is not growth to plan for.
func (g *growthTracker) observe(input int) {
	if input <= 0 {
		return
	}
	if g.prev > 0 && input > g.prev {
		if len(g.diffs) == growthReports {
			copy(g.diffs, g.diffs[1:])
			g.diffs = g.diffs[:growthReports-1]
		}
		g.diffs = append(g.diffs, input-g.prev)
	}
	g.prev = input
}

// resetBaseline makes the next report the baseline, recording no
// difference: a compaction drops the input, and the drop is not growth.
//
// The differences recorded before the compaction stay: a turn's growth is
// what that turn cost, and a later compaction does not shrink it.
func (g *growthTracker) resetBaseline() { g.prev = 0 }

// growth returns how many tokens a turn adds to the prompt: the largest of
// the recent differences, or the observation cap before any difference
// exists.
func (g *growthTracker) growth(observationCap int) int {
	if len(g.diffs) == 0 {
		return observationCap
	}
	largest := g.diffs[0]
	for _, diff := range g.diffs[1:] {
		if diff > largest {
			largest = diff
		}
	}
	return largest
}

// noteTurnInput feeds a turn's usage report to the tracker, when the session
// measures turn growth.
//
// A request a compaction made is not a turn. The input it reports is the
// request the strategy built, not the view the run is growing.
//
// A request the transport retried is not a turn either. It reports what its
// attempts spent together, and no view grew by that much. The next report is
// a baseline, and the differences measured before it stand: the turns that
// made them cost what they cost.
//
// A turn that reported no input ends the reports the tracker measures
// across. A difference must never span two turns, so the next report is a
// baseline.
func (s *Session) noteTurnInput(meta RecordMeta, usage *UsageReport, retried bool) {
	if !s.opts.TurnGrowth || meta.Origin == OriginCompaction {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if retried || usage == nil || usage.InputTokens <= 0 {
		s.growth.resetBaseline()
		return
	}
	s.growth.observe(usage.InputTokens)
}

// observationCapTokensLocked returns the observation cap in tokens: the most
// one tool result may add to the view, at the session's calibrated ratio.
//
// The caller holds the session's lock.
func (s *Session) observationCapTokensLocked() int {
	chars := s.opts.Settings.ObservationCapChars
	if chars <= 0 {
		chars = defaultObservationCapChars
	}
	// The cap is rounded up, so a cap of one character is still some growth.
	return int(math.Ceil(float64(chars) / s.est.ratio()))
}
