package poo

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/sdougbrown/poo/decide"
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

// FeedImage is the image a feed's input names, by its path.
//
// The feed's runner hands the reference over without reading it. The
// session reads the bytes when the input is accepted, so an image that
// does not read, or is not an image, fails the attempt the way a
// refused input does.
type FeedImage struct {
	// Path is the image's file. A relative path answers to the
	// directory the feed's command runs in.
	Path string
	// MediaType is the media type the feed declared, empty when the
	// feed named none. The session's own read must agree with it.
	MediaType string
}

// FeedIO is what a feed's runner may use.
type FeedIO struct {
	// Dir is the session's working directory, where the feed's process
	// runs. The session hands over its own: the directory a new session
	// was created in, or, for a restored session, the recorded one.
	Dir string
	// SessionID is the session's own ID, which a manifest's session
	// references resolve to. It is the recorded one, unless the restore
	// options name another, so a feed restarted beside a resumed session
	// resolves to what it resolved to before.
	SessionID string
	// Emit queues one input, which may carry an image. It returns the
	// session's refusal (ErrSessionClosed, ErrSessionBroken, a store
	// error) or the image's own failure. A model that reads no images
	// or an image that does not read fails the attempt either way.
	Emit func(from, text string, image *FeedImage) error
	// Decider returns the session's recorded decision model, fetched
	// for each decision because the session can switch models while the
	// feed runs. The field is nil when the runner's builder passed no
	// seam to one. The call returns nil when the session has no model,
	// and a caller that needs one acts as though there were none.
	Decider func() decide.Decider
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
	// Description is what the feed is for. The prompt names it, so the
	// model knows what may arrive beside the user's turn.
	Description string
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

// feedRestartDelay returns how long a feed waits before its next
// attempt. It is the rule a session's feeds follow unless a test gives
// the session one of its own.
//
// An attempt that ran at least feedLongAttempt resets the wait, so a feed
// that works for a while and then fails is not punished for its earlier
// failures: a source that is down comes back within a minute.
func feedRestartDelay(ran, previous time.Duration) time.Duration {
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

// MissingFeedWarning is what the build and the restore say when a
// profile declares a feed the settings do not load.
//
// The profile declares a need; the operator's feeds.paths still control
// which feeds load, so the loss is a warning, never a failure.
func MissingFeedWarning(profile, feed string) string {
	return fmt.Sprintf("the profile %q declares the feed %q, and the settings load no feed by that name", profile, feed)
}

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
		runAt, attempt, ok := s.beginFeedAttempt(f)
		if !ok {
			return
		}
		err := s.runFeedAttempt(f, attempt)
		outcome := s.feedOutcome(f, err)
		var delay time.Duration
		if outcome == FeedExited || outcome == FeedFailed {
			delay = s.feedDelay(time.Since(runAt), wait)
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
//
// The attempt's number is the token its emits are admitted by: an emit is
// an input of the attempt that made it, and no other.
func (s *Session) beginFeedAttempt(f *feedState) (time.Time, int, bool) {
	startedAt := time.Now()
	s.mu.Lock()
	if s.closed || s.broken || f.ctx.Err() != nil {
		s.mu.Unlock()
		s.setFeedStatus(f, feedStopped)
		return startedAt, 0, false
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
		return startedAt, attempt, false
	}
	if err := s.writeClaimed(context.Background(), rec, gate); err != nil {
		s.setFeedStatus(f, feedStopped)
		return startedAt, attempt, false
	}
	s.events.emit(EventFeedStarted, map[string]any{
		"feed":    f.name,
		"origin":  string(f.origin),
		"attempt": attempt,
	})
	return startedAt, attempt, true
}

// runFeedAttempt runs one attempt of a feed.
//
// A panic in a runner is the attempt's failure: a feed is the session's
// own work, but a panic in whatever it wraps must not bring the process
// down.
func (s *Session) runFeedAttempt(f *feedState, attempt int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("poo: the feed %q panicked: %v", f.name, r)
		}
	}()
	return f.spec.Run(f.ctx, FeedIO{
		Dir:       s.opts.WorkingDir,
		SessionID: s.opts.SessionID,
		Emit: func(from, text string, image *FeedImage) error {
			return s.emitFeedInput(f, attempt, from, text, image)
		},
		Decider: s.Decider,
		Log:     s.feedLogger(f.name),
	})
}

// feedLogger returns the logger a feed's runner writes to.
func (s *Session) feedLogger(name string) *slog.Logger {
	return s.logger().With(slog.String("feed", name))
}

// emitFeedInput queues one input on behalf of the attempt that emitted
// it.
//
// The input is the attempt's, so the acceptance is bound to the attempt:
// the admission is decided in the critical section that accepts the
// input, and an input the runner emits after its attempt's end is claimed
// is refused with ErrSessionClosed. An emit that races its attempt's end
// is therefore accepted before the end's claim, or refused after it,
// never lost in between.
//
// An input that names an image is admitted by feedImageAttachments, and
// the image's own failures end the attempt here.
func (s *Session) emitFeedInput(f *feedState, attempt int, from, text string, image *FeedImage) error {
	attachments, err := s.feedImageAttachments(f.name, image)
	if err != nil {
		return err
	}
	return s.queueFeedInput(f.name, from, text, attachments, f.spec.OnIdle, func() bool {
		return f.ended < attempt
	})
}

// feedImageAttachments reads the image a feed's input names, with the
// admission checks the image answers to.
//
// A model that reads no images would drop the whole input at the
// request — the wake is spent on nothing — so the refusal is the feed's
// to hear, the way a refused input is. An image that does not read, an
// image the head does not name, or a declared media type the head does
// not confirm is the attempt's failure too. The read happens before the
// admission, so an emit that races its attempt's end can fail on the
// image rather than the session's refusal: either one fails the
// attempt.
func (s *Session) feedImageAttachments(feed string, image *FeedImage) ([]Attachment, error) {
	if image == nil {
		return nil, nil
	}
	if image.Path == "" {
		return nil, fmt.Errorf("poo: the feed %q names an image with no path", feed)
	}
	s.mu.Lock()
	readsImages := s.opts.Model.Reads(ModalityImage)
	s.mu.Unlock()
	if !readsImages {
		return nil, fmt.Errorf("poo: the feed %q emitted an image, and the model does not read images", feed)
	}
	attachment, err := s.readFeedImage(feed, image)
	if err != nil {
		return nil, err
	}
	return []Attachment{attachment}, nil
}

// readFeedImage reads the image a feed's input names.
//
// A path relative to the feed's working directory is resolved against
// it. The bytes are capped at MaxImageBytes, and the head names the
// media type. A declared type the head does not confirm is a failure
// that names both, so a feed that mislabels its images is heard from.
func (s *Session) readFeedImage(feed string, image *FeedImage) (Attachment, error) {
	path := image.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.opts.WorkingDir, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("poo: the feed %q names the image %q, and it does not read: %w", feed, image.Path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		return Attachment{}, fmt.Errorf("poo: the feed %q names the image %q, and it does not read: %w", feed, image.Path, err)
	}
	if len(data) > MaxImageBytes {
		return Attachment{}, fmt.Errorf("poo: the feed %q names the image %q, and it is larger than the %d bytes a model takes", feed, image.Path, MaxImageBytes)
	}
	media, ok := ImageMediaType(data)
	if !ok {
		return Attachment{}, fmt.Errorf("poo: the feed %q names the image %q, and its bytes are not an image the session reads", feed, image.Path)
	}
	// The sniffed type is the one the request carries, so a type the
	// core does not accept is a failure here rather than a picture the
	// delivery silently drops.
	if !IsMediaType(ModalityImage, media) {
		return Attachment{}, fmt.Errorf("poo: the feed %q names the image %q, and its bytes are %q, which no provider takes", feed, image.Path, media)
	}
	if image.MediaType != "" && image.MediaType != media {
		return Attachment{}, fmt.Errorf("poo: the feed %q names the image %q as %q, and its bytes are %q", feed, image.Path, image.MediaType, media)
	}
	return Attachment{Modality: ModalityImage, MediaType: media, Data: data}, nil
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

// listedFeedsLocked returns the feeds the job lists show, in start
// order: the ones still running or waiting to restart.
//
// The caller holds the session's lock.
func (s *Session) listedFeedsLocked() []*feedState {
	var out []*feedState
	for _, f := range s.feedOrder {
		if f.listed() {
			out = append(out, f)
		}
	}
	return out
}

// feedNameOf returns the feed a listed id names.
//
// A feed is listed under its origin, so a cancel names it the way the
// list did.
func feedNameOf(id string) (string, bool) {
	name, ok := FeedName(Origin(id))
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
