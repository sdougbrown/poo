package poo

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// A feed is a long-lived source that pushes messages into a session.
//
// The session owns the feed's lifecycle: it starts one attempt, records
// how it ended, and starts the next when the feed asks to be restarted.
// The runner owns whatever the source is, so the core never imports a
// process or a client for it.
//
// A feed's input joins the session's job queue, so the idle policy, the
// wake limit and the close treat it as they treat a job's result.

// FeedIO is what a feed's runner may use.
type FeedIO struct {
	// Emit queues one input. It returns the session's refusal
	// (ErrSessionClosed, ErrSessionBroken, a store error); the runner
	// stops and returns that error.
	Emit func(from, text string) error
	// Log receives the runner's diagnostics, such as the child's stderr.
	// It is never nil.
	Log *slog.Logger
}

// FeedRunner runs one attempt of a feed.
//
// The runner returns when the feed's source ends, when it fails, or when
// the context ends. A context error after the session cancelled the
// attempt is how a runner reports that it stopped; the session decides
// the attempt's outcome, so the error's text is kept and the outcome is
// the session's.
type FeedRunner func(ctx context.Context, io FeedIO) error

// FeedSpec declares one feed.
type FeedSpec struct {
	// Name is the feed's name. It is part of the feed's origin, so it
	// has the shape of an origin's name: ^[a-z0-9][a-z0-9_-]*$.
	Name string
	// Run runs one attempt.
	Run FeedRunner
	// Restart reports that the session starts the next attempt after an
	// attempt that exited or failed, once the backoff has passed.
	Restart bool
	// OnIdle says whether an input the feed emits may start a run while
	// the session is idle. Empty inherits the session's jobs.on_idle.
	OnIdle OnIdle
}

// WithFeeds gives the session the feeds it runs.
//
// The session starts every feed when it is built, or, for a restored
// session, once the results the restore queued are on the queue. A feed
// runs until cancel_job stops it, the session closes, or the store
// breaks.
func WithFeeds(specs ...FeedSpec) Option {
	return func(opts *Options) {
		opts.Feeds = append(opts.Feeds, specs...)
	}
}

// FeedStartedRecord records that one attempt of a feed started.
//
// The attempt's end is the next feed.ended record of the same feed: one
// feed's attempts never overlap.
type FeedStartedRecord struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Feed is the feed's name.
	Feed string `json:"feed"`
	// Attempt numbers the feed's attempts in this session, from 1.
	Attempt int `json:"attempt"`
	// StartedAt is the RFC 3339 start time in UTC.
	StartedAt string `json:"started_at"`
}

// FeedEndedRecord records how one attempt of a feed ended.
//
// The record is terminal: a load never runs a feed again, and a feed's
// failure reaches the model through the record and the event, never as a
// message.
type FeedEndedRecord struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Feed is the feed's name.
	Feed string `json:"feed"`
	// Attempt numbers the feed's attempts in this session, from 1.
	Attempt int `json:"attempt"`
	// Outcome says how the attempt ended.
	Outcome FeedOutcome `json:"outcome"`
	// Error is the runner's error, when it returned one.
	Error string `json:"error,omitempty"`
	// RestartInMS is how long the session waits before the next
	// attempt, set when a restart is scheduled.
	RestartInMS int `json:"restart_in_ms,omitempty"`
	// EndedAt is the RFC 3339 end time in UTC.
	EndedAt string `json:"ended_at"`
}

// FeedOutcome says how one attempt of a feed ended.
type FeedOutcome string

const (
	// FeedExited means the runner returned without an error.
	FeedExited FeedOutcome = "exited"
	// FeedFailed means the runner returned an error or panicked.
	FeedFailed FeedOutcome = "failed"
	// FeedCancelled means cancel_job stopped the attempt.
	FeedCancelled FeedOutcome = "cancelled"
	// FeedSessionClosed means the session closed under the attempt, or
	// its store broke.
	FeedSessionClosed FeedOutcome = "session_closed"
	// FeedInterrupted means the process stopped during the attempt. Only
	// a recovery writes it.
	FeedInterrupted FeedOutcome = "interrupted"
)

// The statuses a feed is listed with.
const (
	// feedRunning means an attempt is running.
	feedRunning = JobRunning
	// feedRestarting means the feed waits for its next attempt.
	feedRestarting = JobRestarting
	// feedStopped means the feed has no attempt and no restart.
	feedStopped = "stopped"
)

// The restart waits a feed's attempt starts with.
const (
	// feedFirstRestartDelay is the wait after the first attempt that
	// failed or exited.
	feedFirstRestartDelay = time.Second
	// feedMaxRestartDelay caps the doubling wait.
	feedMaxRestartDelay = 60 * time.Second
	// feedLongAttempt is an attempt long enough to count as progress: a
	// feed that ran this long restarts without the doubled wait.
	feedLongAttempt = 60 * time.Second
)

// feedRestartDelay returns how long the session waits before a feed's
// next attempt.
//
// An attempt that ran at least feedLongAttempt resets the wait, so a feed
// that works for a while and then fails is not punished for its earlier
// failures: a source that is down comes back within a minute.
var feedRestartDelay = func(ran, previous time.Duration) time.Duration {
	if ran >= feedLongAttempt || previous <= 0 {
		return feedFirstRestartDelay
	}
	next := previous * 2
	if next > feedMaxRestartDelay {
		next = feedMaxRestartDelay
	}
	return next
}

// feedNamePattern is the shape a feed's name takes.
//
// The name is part of the feed's origin, and an origin names what a
// record came from: a name outside the pattern would make origins that
// cannot be read back.
var feedNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// checkFeeds validates the feeds a session is built with.
func checkFeeds(specs []FeedSpec) error {
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if !feedNamePattern.MatchString(spec.Name) {
			return fmt.Errorf("poo: the feed %q is not a valid name", spec.Name)
		}
		if seen[spec.Name] {
			return fmt.Errorf("poo: the feed %q is defined twice", spec.Name)
		}
		seen[spec.Name] = true
		if spec.Run == nil {
			return fmt.Errorf("poo: the feed %q has no runner", spec.Name)
		}
		switch spec.OnIdle {
		case "", OnIdleWake, OnIdleHold:
		default:
			return fmt.Errorf("poo: the feed %q has an unknown idle policy %q", spec.Name, spec.OnIdle)
		}
	}
	return nil
}

// feedState is one feed's lifecycle.
//
// The state is the session's: the feed's context, its attempt counter and
// the attempt whose end is claimed are all read and written under the
// session's lock, because the close, a cancel and the feed's own
// supervisor all act on them.
type feedState struct {
	// name is the feed's name.
	name string
	// origin is the origin the feed's records and messages carry.
	origin Origin
	// spec is the feed's declaration.
	spec FeedSpec
	// ctx is the feed's context. It comes from the session's job context,
	// so an interrupt, a cancel of a run and a settle never stop the
	// feed: only a cancel of the feed, a close and a broken store do.
	ctx context.Context
	// cancel stops the feed's attempt and its restarts.
	cancel context.CancelCauseFunc
	// done is closed when the feed's supervisor has stopped.
	done chan struct{}
	// started reports that the supervisor has been started.
	started bool
	// attempt is the number of the latest attempt, from 1.
	attempt int
	// ended is the attempt whose end is claimed. The single terminal path
	// claims it, so an attempt has exactly one feed.ended whatever races
	// the close has with the runner.
	ended int
	// status is what the feed is doing: feedRunning, feedRestarting or
	// feedStopped.
	status string
	// startedAt is when the latest attempt started.
	startedAt time.Time
}

// listed reports that the feed is a job a list should carry.
func (f *feedState) listed() bool {
	return f.status == feedRunning || f.status == feedRestarting
}

// buildFeeds creates the feeds' states and contexts.
//
// The contexts come from the session's job context, so they are built
// with it, after every fallible step of the construction.
func (s *Session) buildFeeds() {
	for _, spec := range s.opts.Feeds {
		ctx, cancel := context.WithCancelCause(s.jobCtx)
		f := &feedState{
			name:   spec.Name,
			origin: FeedOrigin(spec.Name),
			spec:   spec,
			ctx:    ctx,
			cancel: cancel,
			done:   make(chan struct{}),
		}
		s.feeds[f.name] = f
		s.feedOrder = append(s.feedOrder, f)
	}
}

// startFeeds starts one supervisor per feed.
//
// It runs at the end of the construction, or, for a restored session,
// once the results the restore queued are on the queue.
func (s *Session) startFeeds() {
	s.mu.Lock()
	var start []*feedState
	for _, f := range s.feedOrder {
		if f.started {
			continue
		}
		f.started = true
		start = append(start, f)
	}
	s.mu.Unlock()
	for _, f := range start {
		go s.runFeed(f)
	}
}

// runFeed runs one feed: each attempt, its end, and the wait before the
// next one.
//
// The supervisor is the feed's only owner, so a restart is a loop and not
// a second registry the close would have to know about.
func (s *Session) runFeed(f *feedState) {
	defer close(f.done)
	var wait time.Duration
	for {
		runAt, ok := s.beginFeedAttempt(f)
		if !ok {
			return
		}
		err := s.runFeedAttempt(f)
		outcome := s.feedOutcome(f, err)
		var delay time.Duration
		if outcome == FeedExited || outcome == FeedFailed {
			delay = feedRestartDelay(time.Since(runAt), wait)
		}
		if !s.endFeedAttempt(f, outcome, err, delay) {
			return
		}
		wait = delay
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			// The cancel or the close ends the feed during its wait: the
			// attempt's end record is already written, and no attempt
			// follows it.
			timer.Stop()
			s.setFeedStatus(f, feedStopped)
			return
		}
	}
}

// beginFeedAttempt starts one attempt and records its feed.started.
//
// The attempt's number and the record's write are registered under one
// lock, so a close that sees the attempt waits for its record. A record
// the store refuses stops the feed for good: the log cannot say the
// attempt ran, so the session does not pretend it did.
func (s *Session) beginFeedAttempt(f *feedState) (time.Time, bool) {
	startedAt := time.Now()
	s.mu.Lock()
	if s.closed || s.broken || f.ctx.Err() != nil {
		s.mu.Unlock()
		s.setFeedStatus(f, feedStopped)
		return startedAt, false
	}
	f.attempt++
	f.startedAt = startedAt
	f.status = feedRunning
	attempt := f.attempt
	gate, ok := s.beginWriteLocked("feed.started")
	rec := &FeedStartedRecord{
		RecordMeta: NewRecordMetaFrom("feed.started", f.origin),
		SessionID:  s.opts.SessionID,
		Feed:       f.name,
		Attempt:    attempt,
		StartedAt:  startedAt.UTC().Format(time.RFC3339Nano),
	}
	s.mu.Unlock()
	if !ok {
		s.setFeedStatus(f, feedStopped)
		return startedAt, false
	}
	if err := s.writeClaimed(context.Background(), rec, gate); err != nil {
		s.setFeedStatus(f, feedStopped)
		return startedAt, false
	}
	s.events.emit(EventFeedStarted, map[string]any{
		"feed":    f.name,
		"origin":  string(f.origin),
		"attempt": attempt,
	})
	return startedAt, true
}

// runFeedAttempt runs one attempt of a feed.
//
// A panic in a runner is the attempt's failure: a feed is the session's
// own work, but a panic in whatever it wraps must not bring the process
// down.
func (s *Session) runFeedAttempt(f *feedState) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("poo: the feed %q panicked: %v", f.name, r)
		}
	}()
	return f.spec.Run(f.ctx, FeedIO{
		Emit: func(from, text string) error {
			return s.emitFeedInput(f, from, text)
		},
		Log: s.feedLogger(f.name),
	})
}

// feedLogger returns the logger a feed's runner writes to.
func (s *Session) feedLogger(name string) *slog.Logger {
	return s.logger().With(slog.String("feed", name))
}

// emitFeedInput queues one input on behalf of a running attempt.
//
// An input the runner emits after the attempt's end is claimed is refused
// with ErrSessionClosed: the delivery would join a feed the session has
// already ended.
func (s *Session) emitFeedInput(f *feedState, from, text string) error {
	s.mu.Lock()
	closed := s.closed || (f.attempt > 0 && f.ended >= f.attempt)
	onIdle := f.spec.OnIdle
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}
	return s.queueFeedInput(f.name, from, text, onIdle)
}

// feedOutcome says how an attempt ended.
//
// The order is the precedence: a session that closed or broke under the
// attempt wins over a cancel, a cancel wins over the runner's error, and
// a runner that returned no error exited.
func (s *Session) feedOutcome(f *feedState, err error) FeedOutcome {
	s.mu.Lock()
	closed := s.closed || s.broken
	s.mu.Unlock()
	switch {
	case closed:
		return FeedSessionClosed
	case f.ctx.Err() != nil:
		return FeedCancelled
	case err != nil:
		return FeedFailed
	}
	return FeedExited
}

// endFeedAttempt claims an attempt's end and records it.
//
// The first claim wins and later callers return false: the close and the
// feed's own supervisor may both see an attempt end, and the log says it
// once. The record and the event are written outside the lock, with the
// record's write registered under it.
//
// The call reports whether a restart is scheduled. A scheduled restart is
// recorded with its wait, so a reader of the log knows the feed was not
// gone for good.
func (s *Session) endFeedAttempt(f *feedState, outcome FeedOutcome, err error, delay time.Duration) bool {
	s.mu.Lock()
	if f.ended >= f.attempt {
		s.mu.Unlock()
		return false
	}
	f.ended = f.attempt
	attempt := f.attempt
	restart := delay > 0 && f.spec.Restart && !s.closed && !s.broken && f.ctx.Err() == nil
	if restart {
		f.status = feedRestarting
	} else {
		f.status = feedStopped
		delay = 0
	}
	rec := &FeedEndedRecord{
		RecordMeta: NewRecordMetaFrom("feed.ended", f.origin),
		SessionID:  s.opts.SessionID,
		Feed:       f.name,
		Attempt:    attempt,
		Outcome:    outcome,
		EndedAt:    nowRFC3339(),
	}
	if err != nil {
		rec.Error = err.Error()
	}
	if restart {
		// The wait is recorded in whole milliseconds, rounded up, so a
		// restart that is scheduled is always named.
		rec.RestartInMS = int((delay + time.Millisecond - 1) / time.Millisecond)
	}
	gate, ok := s.beginWriteLocked("feed.ended")
	s.mu.Unlock()
	if !ok {
		return restart
	}
	if werr := s.writeClaimed(context.Background(), rec, gate); werr != nil {
		// The store refused the record, and the session is broken with
		// it: the feeds stop, and the record that could not be written is
		// the broken store's to explain.
		s.setFeedStatus(f, feedStopped)
		return false
	}
	payload := map[string]any{
		"feed":    f.name,
		"origin":  string(f.origin),
		"attempt": attempt,
		"outcome": string(outcome),
	}
	if rec.Error != "" {
		payload["error"] = rec.Error
	}
	if restart {
		payload["restart_in_ms"] = rec.RestartInMS
	}
	s.events.emit(EventFeedEnded, payload)
	return restart
}

// setFeedStatus records what a feed is doing.
//
// A feed that already stopped stays stopped: the terminal state is not
// undone by a caller that arrives late.
func (s *Session) setFeedStatus(f *feedState, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.status == feedStopped {
		return
	}
	f.status = status
}

// cancelFeed stops one feed.
//
// The attempt's context ends, and a feed that waits for its next attempt
// waits no more: the attempt's end record is already in the log.
func (s *Session) cancelFeed(name string) error {
	s.mu.Lock()
	f, ok := s.feeds[name]
	if !ok || f.status == feedStopped {
		s.mu.Unlock()
		return ErrUnknownJob
	}
	cancel := f.cancel
	s.mu.Unlock()
	cancel(ErrJobCancelled)
	return nil
}

// feedNameOf returns the feed a listed id names.
//
// A feed is listed under its origin, so a cancel names it the way the
// list did.
func feedNameOf(id string) (string, bool) {
	name, ok := strings.CutPrefix(id, "feed:")
	return name, ok && name != ""
}

// closeFeeds stops every feed and waits for the teardown.
//
// An attempt that has not claimed its end by the deadline is ended here as
// session_closed, so the log says how every attempt ended. The claims are
// the same one the supervisor takes, so an attempt that ends as the close
// reaches it is recorded once.
func (s *Session) closeFeeds() {
	s.mu.Lock()
	feeds := make([]*feedState, 0, len(s.feedOrder))
	for _, f := range s.feedOrder {
		if !f.started || f.status == feedStopped {
			continue
		}
		feeds = append(feeds, f)
	}
	s.mu.Unlock()
	if len(feeds) == 0 {
		return
	}
	for _, f := range feeds {
		f.cancel(ErrJobCancelled)
	}
	// The teardown is bounded once for the whole wait, so a runner that
	// ignores its context never holds the close up.
	teardown, stopTeardown := context.WithTimeout(context.Background(), s.recordBudget())
	defer stopTeardown()
	for _, f := range feeds {
		select {
		case <-f.done:
		case <-teardown.Done():
		}
	}
	for _, f := range feeds {
		s.endFeedAttempt(f, FeedSessionClosed, nil, 0)
		s.setFeedStatus(f, feedStopped)
	}
}

// feedCancelsLocked returns the cancels of the feeds that are still going.
//
// The caller holds the session's lock and runs the cancels after it
// releases it.
func (s *Session) feedCancelsLocked() []context.CancelCauseFunc {
	var out []context.CancelCauseFunc
	for _, f := range s.feedOrder {
		if f.status == feedStopped {
			continue
		}
		out = append(out, f.cancel)
	}
	return out
}
