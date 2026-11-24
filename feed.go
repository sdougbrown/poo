package poo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/scopeschema"
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
	// Scope are the values the subscription that started the feed named,
	// keyed by the field names the manifest declares. It is empty for a
	// feed the session started itself, and a manifest's `scope:NAME`
	// reference resolves through it.
	Scope map[string]string
	// Emit queues one input, which may carry an image and the structured
	// data a filter produced. It returns the session's refusal
	// (ErrSessionClosed, ErrSessionBroken, a store error) or the image's
	// own failure. A model that reads no images or an image that does not
	// read fails the attempt either way.
	//
	// data is the object a filter's record carried beside its text, and
	// it is nil for a feed that runs no filter: a record's data is the
	// filter's to produce.
	Emit func(from, text string, image *FeedImage, data json.RawMessage) error
	// Decider returns the session's recorded decision model, fetched
	// for each decision because the session can switch models while the
	// feed runs. The field is nil when the runner's builder passed no
	// seam to one. The call returns nil when the session has no model,
	// and a caller that needs one acts as though there were none.
	Decider func() decide.Decider
	// FilterPrompt returns the filter prompt the model set on this
	// instance, and false when it set none. It is read for each record,
	// so a prompt the model sets or clears reaches the running feed
	// without restarting it. The field is nil when the runner's builder
	// passed no seam to one.
	FilterPrompt func() (FeedFilterPrompt, bool)
	// Log receives the runner's diagnostics, such as the child's stderr.
	// It is never nil.
	Log *slog.Logger
}

// FeedFilterPrompt is the question the model set on one feed instance
// beside the manifest's own.
//
// The prompt replaces the manifest's question while it is set, and
// clearing it restores the manifest's. The model sets it with
// subscribe_feed, and setting one is the model's judgment about what is
// worth delivering, where the manifest's question is the operator's.
type FeedFilterPrompt struct {
	// Question is asked about each message the filter sees. Phrase it so
	// yes means deliver.
	Question string
	// MinNoProbability is the probability of no at which the message is
	// dropped. Zero means the call named none: the manifest's value
	// applies, or the session's default when the manifest names none. A
	// value that is set is greater than 0.5 and at most 1.
	MinNoProbability float64
}

// DefaultPromptMinNoProbability is the probability of no at which a filter
// prompt drops a message, when neither the call nor the manifest names one.
//
// A prompt is the model's judgment about what is worth delivering, and its
// threshold is not expected to be a considered one: the default sits at the
// low end of the confidence a manifest usually asks for.
const DefaultPromptMinNoProbability = 0.6

// FeedFault is a feed's filter failing closed: a record the filter would
// not pass, and the attempt it failed.
//
// A runner returns one from an attempt, and the session does what a
// filter's failure asks for: it suspends the feed, whatever the restart
// policy says, and wakes the session with a report that names the feed,
// the fault and the file holding the filter's own diagnostics. The fault
// is an error, so a consumer that wires the runner itself sees an attempt
// that failed.
type FeedFault struct {
	// Code is the fault's short name, one of the FeedFault* codes.
	Code string
	// Message is the short diagnosis the report shows.
	Message string
	// Stderr is the filter's own diagnostics for the attempt, which the
	// session writes to a file the report names. It is never delivered:
	// the report carries its path, and the agent reads the file on
	// demand.
	Stderr []byte
}

// Error implements error.
func (f *FeedFault) Error() string {
	return fmt.Sprintf("the feed's filter failed (%s): %s", f.Code, f.Message)
}

// The codes a feed's filter fault is reported with.
const (
	// FeedFaultExit means the filter's process exited with a nonzero
	// status.
	FeedFaultExit = "filter_exited"
	// FeedFaultOutput means the filter exited cleanly and wrote something
	// other than a record.
	FeedFaultOutput = "filter_output"
	// FeedFaultRecord means a record is past the byte cap one may hold:
	// the record the filter would read, or the one it wrote.
	FeedFaultRecord = "filter_record"
)

// feedFaultOf returns the fault an attempt ended with, if it ended with
// one.
func feedFaultOf(err error) *FeedFault {
	var fault *FeedFault
	if errors.As(err, &fault) {
		return fault
	}
	return nil
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
	// the session is idle. Empty inherits the session's jobs.on_idle,
	// except on an optional feed, whose empty policy holds.
	OnIdle OnIdle
	// MaxPending caps the feed's undelivered inputs. When an accepted
	// input arrives at the cap, the feed's oldest undelivered input is
	// dropped with an input.dropped record. The input that made room
	// names the drop. Zero means DefaultFeedBacklog.
	MaxPending int
	// Optional says the feed does not run at the session's start: the
	// model starts it with the subscribe_feed tool, and Scope is what a
	// subscription must supply.
	Optional bool
	// Scope is the schema of the values a subscription supplies, in name
	// order. A feed that declares none takes none.
	Scope []FeedScopeField
	// ScriptFilter reports that the feed runs a filter of its own
	// process: its records are screened before delivery, so a filter
	// prompt the model would set on the instance has no place. A feed
	// has one filter kind, and the session refuses the other.
	ScriptFilter bool
	// FilterQuestion is the question the manifest asks about each
	// message, and empty when the manifest names no question filter.
	// The model's filter prompt replaces it while the prompt is set, so
	// the session names it when a subscription overrides the operator's
	// question.
	FilterQuestion string
}

// FeedScopeField is one field of a feed's scope: a value a subscription
// supplies, and the shape the value takes.
type FeedScopeField struct {
	// Name is the field's name, as a manifest's `scope:NAME` reference
	// and the subscribe_feed call both spell it.
	Name string
	// Type is the field's type. Only "string" is accepted: a scope value
	// reaches the feed as an environment variable's value.
	Type string
	// Pattern is the expression the whole value must match, or "" when
	// any string is accepted.
	Pattern string
}

// WithFeeds gives the session the feeds it runs.
//
// The session starts the feeds that are not optional when it is built, or,
// for a restored session, once the results the restore queued are on the
// queue. An optional feed waits for the subscribe_feed tool. A feed runs
// until cancel_job stops it, the session closes, or the store breaks.
func WithFeeds(specs ...FeedSpec) Option {
	return func(opts *Options) {
		opts.Feeds = append(opts.Feeds, specs...)
	}
}

// FeedStartedRecord records that one attempt of a feed started.
//
// The attempt's end is the next feed.ended record of the same origin: one
// instance of a feed's attempts never overlap, and the origin is what says
// which instance the attempt ran beside.
type FeedStartedRecord struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Feed is the feed's name.
	Feed string `json:"feed"`
	// Attempt numbers the instance's attempts in this session, from 1.
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
	// Attempt numbers the instance's attempts in this session, from 1.
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
	// FeedSuspended means the feed's filter failed closed, so the attempt
	// ended and the feed did not restart: the session woke with a fault
	// report instead, and the feed stays down until something starts it
	// again, which for a feed a subscription started is that call.
	FeedSuspended FeedOutcome = "suspended"
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
	// feedSuspended means the feed's filter failed closed, and the feed
	// runs no attempt and no restart: it stays down until something
	// starts it again.
	feedSuspended = JobSuspended
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

// scopeNamePattern is scopeschema.ScopeNamePattern, compiled.
var scopeNamePattern = regexp.MustCompile(scopeschema.ScopeNamePattern)

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
		if spec.MaxPending < 0 {
			return fmt.Errorf("poo: the feed %q has a negative input cap %d", spec.Name, spec.MaxPending)
		}
		if len(spec.Scope) > 0 && !spec.Optional {
			return fmt.Errorf("poo: the feed %q declares a scope and does not run on demand", spec.Name)
		}
		if err := checkFeedScopeFields(spec); err != nil {
			return err
		}
	}
	return nil
}

// checkFeedScopeFields validates a feed's scope schema.
//
// The schema is the whole of what a subscription may supply, so it is
// checked when the session is built: a field name outside the pattern, a
// type that is not a string, or a pattern that does not compile would
// otherwise refuse every call the model makes.
func checkFeedScopeFields(spec FeedSpec) error {
	seen := make(map[string]bool, len(spec.Scope))
	for _, field := range spec.Scope {
		if !scopeNamePattern.MatchString(field.Name) {
			return fmt.Errorf("poo: the feed %q has a scope field %q that is not a valid name", spec.Name, field.Name)
		}
		if seen[field.Name] {
			return fmt.Errorf("poo: the feed %q declares the scope field %q twice", spec.Name, field.Name)
		}
		seen[field.Name] = true
		if field.Type != scopeschema.ScopeStringType {
			return fmt.Errorf("poo: the feed %q has a scope field %q of the type %q, want %q", spec.Name, field.Name, field.Type, scopeschema.ScopeStringType)
		}
		if field.Pattern != "" {
			if _, err := regexp.Compile(field.Pattern); err != nil {
				return fmt.Errorf("poo: the feed %q has a scope field %q whose pattern does not compile: %w", spec.Name, field.Name, err)
			}
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
	// instance is the label the feed's origin and its inputs carry: the
	// name alone, or the name and the canonical scope a subscription
	// named.
	instance string
	// origin is the origin the feed's records and messages carry.
	origin Origin
	// spec is the feed's declaration.
	spec FeedSpec
	// scope are the values the subscription named, and nil for a feed the
	// session started itself.
	scope map[string]string
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
	// status is what the feed is doing: feedRunning, feedRestarting,
	// feedSuspended or feedStopped.
	status string
	// startedAt is when the latest attempt started.
	startedAt time.Time
	// filterPrompt is the question the model set on the instance, or the
	// zero value when it set none. It belongs to the instance, so a
	// re-arm after a stop or a suspension runs with the same prompt, and
	// clearing it restores the manifest's question.
	filterPrompt FeedFilterPrompt
}

// listed reports that the feed is a job a list should carry.
func (f *feedState) listed() bool {
	return f.status == feedRunning || f.status == feedRestarting || f.status == feedSuspended
}

// indexOptionalFeeds records the feeds a subscription may start, by name.
//
// It runs before the tools are registered, so the session offers the
// subscribe_feed tool exactly when it loads a feed the tool can start.
func (s *Session) indexOptionalFeeds() {
	for _, spec := range s.opts.Feeds {
		spec = onDemandFeed(spec)
		if spec.Optional {
			s.optionalFeeds[spec.Name] = spec
		}
	}
}

// buildFeeds creates the states and contexts of the feeds the session
// starts itself.
//
// The contexts come from the session's job context, so they are built
// with it, after every fallible step of the construction. An optional feed
// gets no state here: it waits for the subscribe_feed tool.
func (s *Session) buildFeeds() {
	for _, spec := range s.opts.Feeds {
		if spec.Optional {
			continue
		}
		f := s.newFeedState(spec, nil)
		s.feeds[string(f.origin)] = f
		s.feedOrder = append(s.feedOrder, f)
	}
}

// onDemandFeed returns the spec with the idle policy a feed it declares
// means.
//
// An optional feed the manifest does not give a policy holds its inputs:
// a feed the model started mid-session is the model's to watch, and waking
// an idle session for one is the manifest's choice to make, not the
// session default's.
func onDemandFeed(spec FeedSpec) FeedSpec {
	if spec.Optional && spec.OnIdle == "" {
		spec.OnIdle = OnIdleHold
	}
	return spec
}

// newFeedState builds one instance's state with a context of its own.
//
// The context comes from the session's job context, so an interrupt, a
// cancel of a run and a settle never stop the feed. The status starts at
// running and the start time at now: the supervisor starts at once, and a
// reader that looks before it does sees the instance it is about to get.
//
// The caller holds the session's lock when another goroutine may already
// read the state.
func (s *Session) newFeedState(spec FeedSpec, scope map[string]string) *feedState {
	instance := feedInstanceLabel(spec.Name, scope)
	ctx, cancel := context.WithCancelCause(s.jobCtx)
	return &feedState{
		name:      spec.Name,
		instance:  instance,
		origin:    FeedOrigin(instance),
		spec:      spec,
		scope:     scope,
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		status:    feedRunning,
		startedAt: time.Now(),
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
	// The supervisor closes the channel it started with: a subscription
	// that re-arms a stopped instance hands it a new one.
	done := f.done
	defer close(done)
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
		fault := feedFaultOf(err)
		if fault != nil && outcome == FeedFailed {
			// A filter that failed closed suspends the feed: the attempt
			// ended, and no restart follows it, whatever the restart policy
			// says. The report is the one thing that follows, and it wakes
			// the session.
			outcome = FeedSuspended
			delay = 0
		}
		restart := s.endFeedAttempt(f, outcome, err, delay)
		if outcome == FeedSuspended {
			// A suspended feed runs no attempt and no restart: the report
			// of the fault is the last thing its supervisor does.
			if fault != nil {
				s.reportFeedFault(f, attempt, runAt, fault)
			}
			return
		}
		if !restart {
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
		Scope:     cloneScope(f.scope),
		Emit: func(from, text string, image *FeedImage, data json.RawMessage) error {
			return s.emitFeedInput(f, attempt, from, text, image, data)
		},
		Decider: s.Decider,
		// The prompt is read for each record, so setting or clearing one on
		// the running instance reaches the feed's next record: the change
		// is the filter's, and the attempt is not restarted for it.
		FilterPrompt: func() (FeedFilterPrompt, bool) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if f.filterPrompt.Question == "" {
				return FeedFilterPrompt{}, false
			}
			return f.filterPrompt, true
		},
		Log: s.feedLogger(f.instance),
	})
}

// cloneScope copies a subscription's values, so a runner may not change
// the instance's own.
func cloneScope(scope map[string]string) map[string]string {
	if len(scope) == 0 {
		return nil
	}
	out := make(map[string]string, len(scope))
	for field, value := range scope {
		out[field] = value
	}
	return out
}

// feedLogger returns the logger one instance's runner writes to.
func (s *Session) feedLogger(instance string) *slog.Logger {
	return s.logger().With(slog.String("feed", instance))
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
func (s *Session) emitFeedInput(f *feedState, attempt int, from, text string, image *FeedImage, data json.RawMessage) error {
	attachments, err := s.feedImageAttachments(f.name, image)
	if err != nil {
		return err
	}
	return s.queueFeedInput(feedInput{
		instance:    f.instance,
		from:        from,
		text:        text,
		data:        data,
		attachments: attachments,
		onIdle:      f.spec.OnIdle,
		admit: func() bool {
			return f.ended < attempt
		},
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
	switch {
	case outcome == FeedSuspended && !s.closed && !s.broken && f.ctx.Err() == nil:
		// A filter that failed closed suspends the feed: the log says how
		// the attempt ended, and the feed stays down with no restart
		// scheduled.
		f.status = feedSuspended
		delay = 0
		restart = false
	case restart:
		f.status = feedRestarting
	default:
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

// feedFault is the report a suspended feed's filter leaves the model: what
// failed, the file holding the filter's own diagnostics, and how the feed
// starts again.
type feedFault struct {
	// name is the feed's name, and instance is the label its inputs
	// carry.
	name, instance string
	// scope are the values the subscription named, so the remedy can
	// name them.
	scope map[string]string
	// optional reports that a subscription starts the feed: it says
	// which remedy the report offers.
	optional bool
	// code and message are the fault's short diagnosis.
	code, message string
	// logPath is the file the filter's stderr was written to, or empty
	// when it could not be written.
	logPath string
	// more counts the faults that joined this report before it was
	// delivered.
	more int
}

// pendingFaultReport is a fault report that is accepted but not yet
// queued, with the attempt it reports.
//
// The attempt is the reservation's owner: a re-arm starts the instance
// under a new attempt, and the older attempt's cleanup must not retire the
// newer attempt's reservation.
type pendingFaultReport struct {
	// attempt is the attempt whose fault the report carries.
	attempt int
	// report is the report the model reads.
	report *feedFault
}

// reportFeedFault writes one attempt's filter stderr beside the session's
// files and wakes the session with the report that names it.
//
// The report rides the wake path a feed's input rides, with two switches
// set differently: the idle policy is wake, because a suspension is the
// session's to hear however the feed holds its inputs, and the report
// never passes the feed's filter, because it is the session's own account
// of the filter and not a record the filter let through. Everything else
// is the wake path's: the queued input's record, the backlog accounting
// and the restart-safe storage.
//
// The fault belongs to the attempt that raised it. A subscription may
// re-arm a suspended instance before its supervisor reports, and the
// instance then runs again under a new attempt: the suspension is stale,
// and its report would offer a remedy for a feed that is already up. The
// re-arm's cancel does not stop the report from being attempted, so the
// check is what drops it.
//
// A fault that arrives before the last report was delivered joins it
// instead of waking the session again: one suspension is one wake, and the
// report counts what joined.
func (s *Session) reportFeedFault(f *feedState, attempt int, startedAt time.Time, fault *FeedFault) {
	report := &feedFault{
		name:     f.name,
		instance: f.instance,
		scope:    f.scope,
		optional: f.spec.Optional,
		code:     fault.Code,
		message:  fault.Message,
	}
	// The report names the diagnostics' file only when the file is there:
	// a reader that follows the remedy must find what the report says it
	// will.
	report.logPath, _ = s.writeFeedFaultLog(f, attempt, startedAt, fault.Stderr)
	// The fold check and the report's acceptance are one critical
	// section: a fault that arrives in the gap folds into the report in
	// flight rather than queueing one of its own, so one suspension is
	// one report, whatever the gap between the faults.
	s.mu.Lock()
	// The fault belongs to the attempt that raised it. A subscription may
	// re-arm a suspended instance before its supervisor reports, and the
	// instance then runs again under a new attempt: the suspension is
	// stale, and its report would offer a remedy for a feed that is
	// already up.
	if f.attempt != attempt || f.status != feedSuspended {
		s.mu.Unlock()
		return
	}
	if s.foldFeedFaultLocked(f, attempt) {
		s.mu.Unlock()
		return
	}
	s.pendingFaultReports[f.origin] = pendingFaultReport{attempt: attempt, report: report}
	claim, err := s.queueFeedInputClaimLocked(feedInput{
		instance: f.instance,
		onIdle:   OnIdleWake,
		fault:    report,
	})
	s.mu.Unlock()
	if err != nil {
		// The session refuses the report when it is closing or broken, and
		// the feed's suspension is already in the log.
		s.mu.Lock()
		s.retireFaultReportLocked(f.origin, attempt)
		s.mu.Unlock()
		s.logger().Warn("the feed's fault report was refused",
			slog.String("feed", f.instance), slog.String("error", err.Error()))
		return
	}
	defer func() {
		s.mu.Lock()
		s.endJobCompletionLocked()
		s.mu.Unlock()
	}()
	_ = s.queueFeedInputWrite(feedInput{
		instance: f.instance,
		onIdle:   OnIdleWake,
		fault:    report,
	}, claim)
	// The report is queued (or the store refused it): retire the
	// reservation so a fault that arrives after it queues its own report.
	s.mu.Lock()
	s.retireFaultReportLocked(f.origin, attempt)
	s.mu.Unlock()
}

// retireFaultReportLocked removes the reservation one attempt's report
// took, and only when it is still that attempt's: a re-armed instance's
// fault takes its own reservation, and an older attempt's cleanup must not
// retire it. The caller holds the session's lock.
func (s *Session) retireFaultReportLocked(origin Origin, attempt int) {
	if pending, ok := s.pendingFaultReports[origin]; ok && pending.attempt == attempt {
		delete(s.pendingFaultReports, origin)
	}
}

// writeFeedFaultLog writes one attempt's filter stderr beside the
// session's files and returns its path.
//
// The file is named for the instance, the attempt and the attempt's start
// time, so the name is unique per attempt run: a restored session reuses
// the session's directory and restarts its attempt numbering, and a report
// must never point at a later run's diagnostics. A session that keeps no
// files writes to the working directory.
//
// A write that fails returns the empty path beside the error: the report
// must never name a file the reader will not find.
func (s *Session) writeFeedFaultLog(f *feedState, attempt int, startedAt time.Time, stderr []byte) (string, error) {
	dir := s.opts.SessionDir
	if dir == "" {
		dir = s.opts.WorkingDir
	}
	path := filepath.Join(dir, feedFaultLogName(f.instance, attempt, startedAt))
	if err := os.WriteFile(path, stderr, 0o600); err != nil {
		s.logger().Warn("the feed's fault log could not be written",
			slog.String("feed", f.instance), slog.String("path", path), slog.String("error", err.Error()))
		return "", err
	}
	return path, nil
}

// feedFaultLogStem is how many bytes of the instance's label the fault
// log's name keeps. The label is ASCII, so a cut here never splits a
// character.
const feedFaultLogStem = 48

// feedFaultLogName returns the name one attempt's diagnostics take in the
// session's directory.
//
// The label carries the instance's canonical scope, and a scope value is
// accepted up to feedScopeValueMax bytes, so the label can be many times
// a file name component's limit. The name keeps a bounded leading part of
// the label for a reader to recognise the feed by, and a hash of the whole
// label so two instances never share a name: the component stays under a
// hundred bytes whatever the scope.
func feedFaultLogName(instance string, attempt int, startedAt time.Time) string {
	sum := sha256.Sum256([]byte(instance))
	stem := instance
	if len(stem) > feedFaultLogStem {
		stem = stem[:feedFaultLogStem]
	}
	return fmt.Sprintf("%s-%x-fault-%d-%d.log", stem, sum[:4], attempt, startedAt.UnixNano())
}

// foldFeedFaultLocked joins a fault from the attempt given to the report
// that is queued or in flight and not yet delivered, and reports whether it
// found one. A report in flight is joined only when it is the same
// attempt's. The caller holds the session's lock.
//
// The report the model reads is rendered again with the new count. The
// queued input's record keeps the text the report was accepted with, so a
// restored session delivers the count the acceptance knew: the count is a
// courtesy, and the fault it reports is already in the log.
//
// The joining fault's own stderr is written under its own attempt's name
// like any other, so what the later filter said is still on disk even
// though the report names the attempt that suspended the feed.
func (s *Session) foldFeedFaultLocked(f *feedState, attempt int) bool {
	for i := range s.jobQueue {
		item := &s.jobQueue[i]
		if item.result.origin != f.origin || item.result.fault == nil {
			continue
		}
		item.result.fault.more++
		item.result.text = item.result.fault.render()
		return true
	}
	if s.run != nil {
		// The delivery holds the report between the turn-end drain and
		// the message's write: a fault that arrives in that window joins
		// the one in flight, or the suspension would wake the session
		// twice for one feed.
		for i := range s.run.deliveringJobs {
			item := &s.run.deliveringJobs[i]
			if item.result.origin != f.origin || item.result.fault == nil {
				continue
			}
			item.result.fault.more++
			return true
		}
	}
	if pending, ok := s.pendingFaultReports[f.origin]; ok && pending.attempt == attempt {
		pending.report.more++
		return true
	}
	return false
}

// render returns the model-facing text of the report: the element that
// carries the feed, the fault's code and the log's path, around the
// sentences that say what happened and what starts the feed again.
func (r *feedFault) render() string {
	attrs := [][2]string{
		{"name", r.instance},
		{"fault", r.code},
	}
	if r.logPath != "" {
		attrs = append(attrs, [2]string{"log", r.logPath})
	}
	return outsideElement("feed", attrs, r.text())
}

// text returns the body of the report: the diagnosis, the count of the
// faults that joined it, and the remedy.
func (r *feedFault) text() string {
	var b strings.Builder
	b.WriteString("The feed's filter failed: ")
	b.WriteString(r.message)
	b.WriteString(". The feed is suspended, and it does not restart.\n")
	if r.logPath == "" {
		b.WriteString("The filter's own diagnostics could not be written to a file, so this report carries none.\n")
	}
	if r.more > 0 {
		fmt.Fprintf(&b, "%d more arrived while suspended.\n", r.more)
	}
	b.WriteString(r.remedy())
	return b.String()
}

// remedy returns the sentence that says how the feed starts again.
//
// An optional feed is started by a subscription, so the remedy is the call
// that starts it, with the scope values the instance runs with. A feed the
// session starts itself has no such call, and this stage offers no way to
// start it again.
func (r *feedFault) remedy() string {
	if !r.optional {
		return "This feed is one the session starts itself, so it stays suspended until the session closes.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Resubscribe with %s, naming the feed %q", SubscribeFeedToolName, r.name)
	if len(r.scope) > 0 {
		fields := slices.Sorted(maps.Keys(r.scope))
		b.WriteString(" and the scope ")
		for i, field := range fields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s=%q", field, r.scope[field])
		}
	}
	b.WriteString(", to start it again.\n")
	return b.String()
}

// cancelFeed stops one instance of a feed.
//
// The id is the instance's origin, which is how the job list names it. The
// attempt's context ends, and a feed that waits for its next attempt waits
// no more: the attempt's end record is already in the log. A suspended feed
// has no supervisor left to record the stop, so the cancel records it here.
func (s *Session) cancelFeed(instanceID string) error {
	s.mu.Lock()
	f, ok := s.feeds[instanceID]
	if !ok || f.status == feedStopped {
		s.mu.Unlock()
		return ErrUnknownJob
	}
	if f.status == feedSuspended {
		f.status = feedStopped
	}
	// The cancel runs under the lock: a subscription that re-arms a
	// suspended instance replaces the context and its cancel, so a cancel
	// captured here and invoked after the unlock could stop nothing while
	// this call reported success. Cancelling only closes a channel: it
	// waits for nothing, so a goroutine it wakes settles after the unlock.
	f.cancel(ErrJobCancelled)
	s.mu.Unlock()
	return nil
}

// listedFeedsLocked returns the feeds the job lists show, in start
// order: the ones running, waiting to restart or suspended.
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

// listedFeedCountLocked returns how many feeds the job lists show. The
// caller holds the session's lock.
func (s *Session) listedFeedCountLocked() int {
	n := 0
	for _, f := range s.feedOrder {
		if f.listed() {
			n++
		}
	}
	return n
}

// subscribeFeed starts one instance of an optional feed, or reaches the
// instance the same feed and scope already named, and applies the call's
// filter prompt when it names one.
//
// The instance is keyed by its origin, which carries the feed's name and
// the canonical form of the scope, so one feed may run twice beside a
// session with different values and a repeated call reaches what it
// started. An instance that is running or waiting for its next attempt is
// left as it is; one that stopped, or one whose filter suspended it, is
// started again with a fresh context: a subscription is the only thing that
// starts a suspended feed again.
//
// The scope is held to the feed's declared schema before anything starts,
// and a refusal is the model's to correct. A prompt is held over too: it is
// the question the decision model is asked about each record, so it replaces
// the manifest's while it is set, and a feed that cannot be asked the
// question refuses it.
//
// A new instance is refused once the session runs MaxFeedInstances of them:
// the instances are processes, and a call with a fresh scope value each time
// is the one way a model can start them without bound. A call that reaches an
// instance already there is never refused, so the cap costs no idempotence.
//
// The call reports the instance's origin and whether this call started
// it.
func (s *Session) subscribeFeed(name string, scope map[string]string, prompt *FeedFilterPrompt, hasMinNoProbability bool) (string, bool, error) {
	spec, ok := s.optionalFeeds[name]
	if !ok {
		return "", false, InvalidArgsf("%s: the feed %q is not one this session starts on demand", SubscribeFeedToolName, name)
	}
	values, err := checkFeedScope(spec, scope)
	if err != nil {
		return "", false, InvalidArgsf("%s: %v", SubscribeFeedToolName, err)
	}
	var change *FeedFilterPrompt
	if prompt != nil {
		applied, err := checkFeedPrompt(spec, s.Decider() != nil, *prompt, hasMinNoProbability)
		if err != nil {
			return "", false, InvalidArgsf("%s: %v", SubscribeFeedToolName, err)
		}
		change = &applied
	}
	instance := feedInstanceLabel(spec.Name, values)
	id := string(FeedOrigin(instance))

	s.mu.Lock()
	if s.broken {
		s.mu.Unlock()
		return "", false, ErrSessionBroken
	}
	if s.closed {
		s.mu.Unlock()
		return "", false, ErrSessionClosed
	}
	f, ok := s.feeds[id]
	started := true
	switch {
	case ok && f.status != feedStopped && f.status != feedSuspended:
		// The instance is there and running: the call reaches it, and the
		// prompt below is the whole of what it changes.
		started = false
	case ok:
		// The instance stopped or is suspended: its supervisor is gone with
		// its context, so a re-arm is a fresh context and a fresh
		// supervisor. The prompt is the instance's own, and stays with it.
		// The context being replaced is released here, because its
		// supervisor is not the only holder of it: leaving it to the
		// garbage collector would keep a child of the job context alive per
		// re-arm.
		f.cancel(ErrJobCancelled)
		f.ctx, f.cancel = context.WithCancelCause(s.jobCtx)
		f.done = make(chan struct{})
		f.startedAt = time.Now()
		f.status = feedRunning
	default:
		// The instances a session runs at once are capped: a call is the
		// model's to make, and one subscribed with a fresh scope value each
		// time would otherwise start a process per value without bound.
		if running := s.listedFeedCountLocked(); running >= MaxFeedInstances {
			s.mu.Unlock()
			return "", false, InvalidArgsf("%s: the session already runs %d feeds at once, which is the most it runs; stop one with cancel_job first", SubscribeFeedToolName, MaxFeedInstances)
		}
		f = s.newFeedState(spec, values)
		s.feeds[id] = f
		s.feedOrder = append(s.feedOrder, f)
	}
	if change != nil {
		f.filterPrompt = *change
	}
	f.started = true
	instanceLabel := f.instance
	s.mu.Unlock()
	if change != nil && spec.FilterQuestion != "" {
		// The prompt is the model's screening policy, and it replaces the
		// one the operator configured. The tool's result says the same; the
		// operator's log carries it too, because a feed's content may be
		// what talked the model into the call.
		s.logWarn("the subscribe call overrides the feed's manifest filter question",
			slog.String("feed", instanceLabel),
			slog.Bool("cleared", change.Question == ""))
	}
	if started {
		go s.runFeed(f)
	}
	return id, started, nil
}

// filterOverrideNotice returns the sentence a subscribe call's result adds
// when the call sets or clears a filter prompt on a feed whose manifest
// configured a question, and the empty string otherwise.
//
// The manifest's question is the operator's screening policy, and the
// prompt replaces it while it is set. The notice is visibility, not
// prevention: the call still applies, but the model is told whose question
// stopped applying.
func filterOverrideNotice(spec FeedSpec, prompt *FeedFilterPrompt) string {
	if prompt == nil || spec.FilterQuestion == "" {
		return ""
	}
	if prompt.Question == "" {
		return "The manifest's filter question is restored, and the operator's screening policy applies again."
	}
	return "The manifest's filter question was replaced by the prompt you set, and the operator's screening policy does not apply while it is set."
}

// checkFeedPrompt returns the filter prompt one call sets on a feed
// instance, and refuses the set the feed cannot take.
//
// A prompt is the question the decision model is asked about each record,
// so the feed must be one the model may ask: a manifest that configures a
// script filter refuses the prompt, because a feed screens its records one
// way, and a session with no decision model refuses it too, because the
// prompt would screen nothing. Both refusals name the reason, so the model
// is not left with a filter it believes is working.
//
// An empty prompt clears what an earlier call set, and clearing is never
// refused: it restores the manifest's question, whatever the feed is.
//
// hasMinNoProbability reports whether the call named a min_no_probability
// at all: the value's zero is the "named none" convention, so an explicit
// zero is out of range and is refused, not folded into the clear.
func checkFeedPrompt(spec FeedSpec, hasDecider bool, prompt FeedFilterPrompt, hasMinNoProbability bool) (FeedFilterPrompt, error) {
	question := strings.TrimSpace(prompt.Question)
	if question == "" {
		if prompt.Question != "" {
			return FeedFilterPrompt{}, fmt.Errorf("the filter prompt is blank")
		}
		if hasMinNoProbability {
			return FeedFilterPrompt{}, fmt.Errorf("the call names a min_no_probability, and a min_no_probability belongs to a filter prompt")
		}
		return FeedFilterPrompt{}, nil
	}
	if spec.ScriptFilter {
		return FeedFilterPrompt{}, fmt.Errorf("the feed %q runs a filter of its own, and a feed screens its records one way", spec.Name)
	}
	if !hasDecider {
		return FeedFilterPrompt{}, fmt.Errorf("the session has no decision model, so the filter prompt on the feed %q would screen nothing", spec.Name)
	}
	if hasMinNoProbability && !(prompt.MinNoProbability > 0.5 && prompt.MinNoProbability <= 1) {
		return FeedFilterPrompt{}, fmt.Errorf("the min_no_probability %v is outside the range a filter prompt takes, which is greater than 0.5 and at most 1", prompt.MinNoProbability)
	}
	return FeedFilterPrompt{Question: question, MinNoProbability: prompt.MinNoProbability}, nil
}

// checkFeedScope validates a subscription's scope against the feed's
// declared schema and returns the values the instance runs with.
//
// The schema is the whole of what the call may choose: a field the feed
// did not declare, a declared field the call leaves out, and a value the
// field's pattern refuses are all refusals. A feed that declares no scope
// takes none, and a call that passes one is refused rather than ignored.
func checkFeedScope(spec FeedSpec, scope map[string]string) (map[string]string, error) {
	if len(spec.Scope) == 0 {
		if len(scope) != 0 {
			return nil, fmt.Errorf("the feed %q takes no scope", spec.Name)
		}
		return nil, nil
	}
	supplied := make([]string, 0, len(scope))
	for field := range scope {
		supplied = append(supplied, field)
	}
	slices.Sort(supplied)
	for _, field := range supplied {
		if !declaresFeedScope(spec, field) {
			return nil, fmt.Errorf("the feed %q declares no scope field %q", spec.Name, field)
		}
	}
	values := make(map[string]string, len(spec.Scope))
	for _, field := range spec.Scope {
		value, ok := scope[field.Name]
		if !ok || value == "" {
			return nil, fmt.Errorf("the feed %q needs a value for the scope field %q", spec.Name, field.Name)
		}
		if strings.ContainsRune(value, 0) {
			// The value becomes an environment variable's value, and an
			// entry that carries a NUL byte cannot be one: the attempt
			// would fail before the command starts, every time.
			return nil, fmt.Errorf("the value of the scope field %q carries a NUL byte", field.Name)
		}
		if len(value) > feedScopeValueMax {
			// The value is retained in the feed's state and its origin
			// label for the instance's lifetime, so an unbounded value
			// would hold memory the instance's life is measured in.
			return nil, fmt.Errorf("the value of the scope field %q is %d bytes, and a scope value is at most %d bytes", field.Name, len(value), feedScopeValueMax)
		}
		if field.Pattern != "" {
			// The pattern was validated when the session was built, so a
			// pattern that does not compile cannot reach this compile.
			re, _ := regexp.Compile(field.Pattern)
			// The contract is that the whole value matches: a value the
			// pattern only finds inside is a refusal, not a match.
			if idx := re.FindStringIndex(value); idx == nil || idx[0] != 0 || idx[1] != len(value) {
				return nil, fmt.Errorf("the value of the scope field %q must match %q", field.Name, field.Pattern)
			}
		}
		values[field.Name] = value
	}
	return values, nil
}

// declaresFeedScope reports whether the feed's schema declares the field.
func declaresFeedScope(spec FeedSpec, field string) bool {
	for _, declared := range spec.Scope {
		if declared.Name == field {
			return true
		}
	}
	return false
}

// feedInstanceScope returns the canonical scope a subscription named: the
// fields in name order, each value escaped so that no value can spell
// another field or the label's own end.
//
// The escaping is a query value's, so a value that carries the label's
// own "," or "=" is escaped and cannot spell a field boundary. Two
// subscriptions of one feed are the same instance exactly when their
// canonical scopes are equal.
func feedInstanceScope(scope map[string]string) string {
	fields := make([]string, 0, len(scope))
	for field := range scope {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	var b strings.Builder
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(field)
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(scope[field]))
	}
	return b.String()
}

// feedInstanceLabel returns the label one instance's origin and inputs
// carry: the feed's name, and beside it the canonical scope a
// subscription named.
//
// The label and the instance's origin share one spelling: both take the
// feed's name and the canonical scope through feedInstanceName, so a
// reader of an input, of a record or of a listing knows which instance
// it came from.
func feedInstanceLabel(name string, scope map[string]string) string {
	if len(scope) == 0 {
		return name
	}
	return feedInstanceName(name, feedInstanceScope(scope))
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
	dones := make([]<-chan struct{}, 0, len(s.feedOrder))
	for _, f := range s.feedOrder {
		if !f.started || f.status == feedStopped {
			continue
		}
		feeds = append(feeds, f)
		// The done channel is the supervisor's of this attempt: a
		// subscription may hand a stopped instance a new one, and the
		// close waits for the supervisor it saw.
		dones = append(dones, f.done)
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
	for _, done := range dones {
		select {
		case <-done:
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
