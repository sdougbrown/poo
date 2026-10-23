package poo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

// Session is a conversation with a model.
//
// A session owns the loop, the history, the input queues, the event bus
// and the record sink. One session runs one run at a time: a second
// prompt while a run is active is refused with ErrBusy.
type Session struct {
	// mu guards everything below it.
	mu sync.Mutex

	// opts are the session's configuration.
	opts Options
	// recordWriteBudget bounds one record write or flush on this
	// session.
	//
	// It is per session, copied from the default when the session is
	// built, so a test that shrinks it never races another session's
	// run goroutine.
	recordWriteBudget time.Duration

	// state is where the session is in the FSM.
	state State
	// transitions records the states the session moved through, in
	// order.
	transitions []State

	// history is the model's view: what has been said so far.
	history []Message
	// turns summarises each turn of the session.
	//
	// The summaries cover every turn of the session, not only the
	// current run's: the compaction seam names a range of turns, and a
	// range may cross a run boundary.
	turns []TurnSummary
	// results holds the tool results of the session, by handle.
	//
	// A handle names a call for the life of the session, so its result
	// stays known after the run that made it has settled.
	results map[string]ToolResult
	// resultMeta holds what the session knows about each result: the
	// tool it called and the provider's id for the call.
	//
	// The core needs both to validate a compaction's rewrites and to
	// find the result in the view.
	resultMeta map[string]resultMeta
	// spans holds the session's turns, in order, with the range of the
	// view each one occupies.
	//
	// The spans are the seam's map from a turn to the messages it
	// occupies, so a compaction can cut a range of turns out of the
	// view.
	spans []turnSpan
	// turnSeq numbers the session's turns. The number never restarts,
	// so a turn keeps its number when a compaction removes it.
	turnSeq int
	// messageIDs hands out the session-monotonic message ids.
	messageIDs *MessageIDs
	// recorded holds the recorded messages, by id.
	//
	// The record outlives the view: a compaction may remove a message
	// from the view, and a delivery note still renders from the
	// original recorded message.
	recorded map[string]Message
	// repeatCounts are the repeat counts of each call, by handle.
	//
	// The counts are per call, so a batch that runs in parallel does
	// not race over them: the counts of the call the core is asking
	// about are the ones it wrote.
	repeatCounts map[string]repeatCounts
	// repeatOrder is the handles the calls were counted in, so the
	// last call's counts can be named without a map's random order.
	repeatOrder []string

	// run is the active run, nil when the session is idle.
	run *runState
	// reserved reports that a run has been admitted but has not been
	// installed yet.
	//
	// The reservation is taken under the session lock, so a wake and
	// a concurrent prompt race for one reservation: the loser of a
	// prompt race gets busy, and nothing is accepted.
	reserved bool
	// broken reports that the store failed and the session cannot be
	// used any more.
	broken bool
	// closed reports that the session is closing or closed.
	closed bool
	// fenced reports that the close has begun: a record write that has
	// not registered yet is refused, and the close waits for the ones
	// that have before it flushes.
	fenced bool
	// gates are the record writes that are in flight, by registration.
	gates []*writeGate
	// gatesDone is signalled when a write deregisters, so the close can
	// wait for them without polling.
	gatesDone chan struct{}

	// steerQueue holds the steer and interrupt messages, in arrival
	// order.
	steerQueue []queuedInput
	// followUpQueue holds the follow-up messages, in arrival order.
	followUpQueue []queuedInput

	// jobs holds the session's jobs, by job id.
	jobs map[string]*jobState
	// jobOrder is the job ids in start order, so a list of jobs is
	// stable.
	jobOrder []string
	// jobSeq numbers the session's jobs.
	jobSeq int
	// jobCompletions counts the job completions whose terminal record
	// or queued result is still being handled. WaitJobs treats them as
	// outstanding work, so it cannot return in the gap between the
	// running job and its queued result.
	jobCompletions int
	// jobQueue holds the finished job results, in completion order.
	jobQueue []jobQueueItem
	// pendingHeld is the held record that is waiting to be written, with
	// the write registration its decision was taken under.
	//
	// The write takes the session's lock, so the record is built under
	// the lock and written outside it.
	pendingHeld *heldRecord
	// raceHooks are the seams the job races are tested at.
	raceHooks *JobRaceHooks
	// sealHooks are the seams the seal protocol is tested at.
	sealHooks *SealRaceHooks
	// jobQueued is signalled when a job result is queued.
	jobQueued chan struct{}
	// jobCtx is the session's job context. The jobs run under it, so
	// an interrupt, a cancel and a settle never stop them.
	jobCtx context.Context
	// jobCancel stops the session's jobs.
	jobCancel context.CancelCauseFunc
	// unattendedWakes counts the woken runs that followed one another
	// with no client input in between.
	unattendedWakes int

	// events is the session's event bus.
	events *eventBus

	// sunk reports that the store failed.
	sunk bool

	// handleSeq numbers the tool-result handles the session assigned.
	handleSeq int
	// inputSeq numbers the job results' input identifiers. A client
	// input's id is a ULID; a job result's id keeps the session's
	// prefix so a log reader can tell where it came from.
	inputSeq int

	// est is the context-budget estimator.
	est estimator
	// noticeArmed reports that the context notice may fire. The notice
	// re-arms when the estimate drops back below the notice ratio.
	noticeArmed bool
	// decider is the session's decision model, or nil when it has none.
	//
	// The handle is the recording wrapper: every call writes a record
	// to the session's store and emits the decision event.
	decider decide.Decider
	// deciderModel is the descriptor of the decision model.
	deciderModel ModelInfo
	// log is the session's logger, when the consumer supplied one.
	log *slog.Logger
	// logs is the non-blocking dispatcher behind log.
	logs *logDispatcher
	// promptBuilder holds the sections the system prompt is assembled
	// from. A run renders a copy of it, so a run's prompt may change
	// without the session's prompt changing.
	promptBuilder *prompt.Builder
	// promptVariant is the variant the prompt renders in.
	//
	// It may change between the runs: the prompt a run carries is
	// rendered once, at the start of the run, so a change never
	// reaches a run that is going.
	promptVariant string
	// lastPromptText is the last rendered prompt that was recorded, so
	// an unchanged prompt is not written again.
	lastPromptText string
	// promptRendered reports that a run has rendered the prompt.
	//
	// The state tells a client that the prompt it reads is a preview:
	// before the first run, nothing has rendered it yet.
	promptRendered bool

	// waiters are the goroutines waiting for the run to settle.
	waiters []chan Result
	// lastResult is the outcome of the run that finished last.
	//
	// A waiter that arrives after the run has settled reads it here,
	// so the answer does not depend on whether the caller got to Wait
	// before or after the settle.
	lastResult Result
	// lastRunID is the id of the run that finished last.
	lastRunID string
	// runResults holds each settled run's outcome, by run id, so a
	// client can ask for the run it started even after a wake moved on.
	runResults map[string]Result
	// settleCodes holds the exit codes of the runs that have settled,
	// in order. A woken run's code joins the first run's, so a client
	// can combine them.
	settleCodes []int
}

// runState is the state of the active run.
type runState struct {
	// id identifies the run.
	id string
	// ctx is the run's context. Cancelling it stops the run's work.
	ctx context.Context
	// cancel stops the run's work, with a cause.
	cancel context.CancelCauseFunc
	// turn counts the provider requests made so far.
	turn int
	// globalTurn is the session's number for the turn the run is in.
	//
	// The run's turn number counts from one in every run, and the
	// session's number counts from one for the life of the session. The
	// compaction seam names a turn by the session's number.
	globalTurn int
	// requests counts the provider requests made so far.
	requests int
	// tokens counts the run's cumulative input and output tokens.
	tokens int
	// startedAt is when the run began.
	startedAt time.Time
	// stopTimer stops the run's wall-clock budget timer.
	stopTimer func()
	// systemMessage is the prompt this run rendered, frozen for the
	// run. Earlier runs' system messages stay in the record but are
	// not sent again.
	systemMessage *Message
	// trigger says what started the run.
	trigger TriggerKind
	// continuations counts the BeforeSettle continuations the run took.
	continuations int
	// recoverableNotes counts the consecutive turns a recoverable
	// response failure opened.
	recoverableNotes int
	// noticeFired reports that the context notice already fired in
	// this run.
	noticeFired bool
	// lastRendered is the snapshot of the last main request, so a
	// compactor can fork it.
	lastRendered Rendered
	// hasRendered reports that lastRendered holds a snapshot.
	hasRendered bool

	// activityCtx is the current activity's context. The activity is
	// the request, the tool batch, the compaction or the turn-end
	// hooks, and an interrupt ends it.
	activityCtx context.Context
	// activityCancel ends the current activity.
	activityCancel context.CancelCauseFunc
	// interruptPending reports that an interrupt arrived and has not
	// been acted on yet.
	interruptPending bool
	// pendingInterruptIDs are the inputs whose messages aborted the
	// current activity.
	pendingInterruptIDs []string

	// pendingCalls are the calls that were issued but have no result
	// yet.
	pendingCalls map[string]*issuedCall
	// settledCalls are the calls that have a result, by handle. A call
	// gets exactly one result, so a call that finished at the same
	// moment the run settled does not get a second one.
	settledCalls map[string]bool
	// resultWrites counts the claimed results whose records are still
	// being written. The finalization waits for them as it does for the
	// pending calls.
	resultWrites int
	// runWrites counts the records a hook wrote for the run whose
	// writes are still in flight. The finalization waits for them, so
	// a hook that appends a message while the finalization is under
	// way cannot add a record after run.settled.
	runWrites int
	// writeClaims are the claimed hook writes, so the seal can end a
	// write the finalization stopped waiting for.
	writeClaims []*runWriteClaim
	// callWrites counts the tool.call records whose writes are still in
	// flight. The finalization waits for them, so no record of the run
	// follows run.settled.
	callWrites int
	// sealed reports that the finalization owns the run's remaining
	// records. A claim that would write a call record, a call result or
	// an input's acceptance checks the seal when it is claimed, so
	// nothing is added for the run after it.
	sealed bool
	// asks are the permission asks the run has published and not yet
	// answered, by ask id. The finalization resolves the ones still
	// pending before it settles.
	asks map[string]*PermissionRequest
	// jobStarts counts the job acceptance records whose writes are still
	// in flight. The finalization waits for them as it does for the
	// pending calls.
	jobStarts int
	// workDone is signalled when a pending call gets its result.
	workDone chan struct{}
	// repeats and the current permission call belong to this run.
	repeats          *repeatCounter
	permissionHandle string
	permissionTool   string

	// accepting are the inputs whose acceptance record is still being
	// written. The finalization waits for them, and reports the ones it
	// seals as undelivered.
	accepting []queuedInput
	// inputsClosed reports that the finalization has made the run's input
	// decision: the queues have been looked at, so an input whose
	// acceptance lands from now on can no longer be delivered. Send hands
	// such an input to the finalization, which reports it dropped.
	inputsClosed bool
	// delivering are the inputs taken from the queues whose delivery
	// records are still being written, in delivery order. The
	// finalization waits for them, and reports the ones it seals as
	// undelivered.
	delivering []queuedInput
	// deliveringJobs are the job results taken from the session's job
	// queue whose delivery records are still being written. The
	// finalization waits for them; a result it abandons goes back on
	// the queue, because settling never drops a job result.
	deliveringJobs []jobQueueItem
	// abandonedInputs are taken inputs whose delivery was abandoned
	// because the run settled; the finalization reports them as
	// undelivered.
	abandonedInputs []queuedInput

	// candidates are the settle candidates the run has collected.
	candidates []candidate
	// finalizing reports that the finalization is under way.
	finalizing bool
	// committed reports that the outcome is fixed.
	committed bool
	// result is the committed outcome.
	result Result
	// finalizeDone is closed when the finalization ends.
	finalizeDone chan struct{}
	// finalizing is signalled when the finalization starts, so a
	// second call only adds a candidate.
	finalizeStarted chan struct{}
	// wake is signalled when the run leaves the waiting state.
	wake chan struct{}
}

// issuedCall is a call that was issued but has no result yet.
type issuedCall struct {
	// handle is the call's handle, e.g. "r1".
	handle string
	// tool is the name of the tool the model called.
	tool string
	// id is the provider's id for the call.
	id string
	// turn is the turn the call belongs to.
	turn int
	// globalTurn is the session's number for the turn the call belongs
	// to.
	globalTurn int
	// arguments are the model's arguments, kept so the finalization can
	// write the call's record when the call was claimed before its
	// record was written.
	arguments json.RawMessage
	// recorded reports that the tool.call record is written or owned for
	// writing.
	recorded bool
	// writing reports that the call's tool.call record write is in flight.
	writing bool
	// recordCtx is the context the call's tool.call record is written
	// under. The claim makes it, so the finalization can end the write
	// when it seals the call.
	recordCtx context.Context
	// recordCancel ends recordCtx.
	recordCancel context.CancelCauseFunc
	// started reports that the tool is running.
	started bool
}

// Options configure a session.
//
// The options are the session's whole configuration, and every one of
// them is a typed value: there is no hidden default for the context
// window, and no option that silently changes the loop's behaviour.
type Options struct {
	// Transport sends the provider requests. A session without one
	// cannot be built.
	Transport ChatTransport
	// Tools are the tools the model may call, keyed by name.
	Tools map[string]Tool
	// ToolOrder is the order the tools are offered to the model in.
	ToolOrder []string
	// Sink receives the session records.
	Sink RecordSink
	// SessionID identifies the session.
	SessionID string
	// Model is the session's chat model.
	//
	// The descriptor names the model the session talks to: the
	// identifier the endpoint knows it by, the API it is reached
	// through, the window the session measures against and the output
	// ceiling the requests carry.
	//
	// The API is the Chat Completions API when the descriptor names
	// none, because it is the only API v1 speaks. The window is
	// required: a value of 0 or less is a construction error
	// (ErrSessionInit), so no run starts without a window to measure
	// against.
	Model ModelInfo
	// Profile is the name of the profile the session was built with.
	//
	// A profile is a named tool set with its own defaults. The name
	// goes into the session's metadata, and a restored session keeps
	// the recorded name rather than the current default.
	Profile string
	// Profiles are the profiles this binary offers.
	//
	// A restore checks the recorded profile against them: a profile
	// this build does not offer is an unknown profile.
	Profiles []Profile
	// Models are the models the current settings offer, by name.
	//
	// A restore checks the recorded roles against them: a role that
	// names a model the settings do not offer is an unknown model. A
	// caller that names no models at all has no catalog to check
	// against, and the roles are not checked.
	Models map[string]ModelInfo
	// SettingsHash is the hash of the settings the session was built
	// with.
	//
	// A restore compares it with the recorded one, and reports a
	// difference as a warning.
	SettingsHash string
	// Build names this binary.
	//
	// A restore compares it with the recorded one, and reports a
	// difference as a warning.
	Build BuildInfo
	// Decider is the session's decision model, or nil when it has none.
	//
	// The session wraps it, so every call is recorded.
	Decider decide.Decider
	// DeciderModel describes the decision model.
	//
	// The model must serve the operation its role needs: the systemone
	// one or the decisions one. A model that serves neither is a
	// construction error.
	DeciderModel ModelInfo
	// SystemPrompt is the system message prepended to the model's view.
	//
	// The text is the whole prompt: the sections the core assembles are
	// not used when it is set, and the text is the prompt's base
	// section, so a later edit still has somewhere to go.
	SystemPrompt string
	// Prompt changes the session's system prompt.
	//
	// Every mutation is called with the session's prompt builder, in
	// the order they were added, after the core's own sections and the
	// tools' sections are in it. A mutation that is nil is skipped.
	Prompt []func(*prompt.Builder)
	// PromptVariant names the prompt variant the prompt renders in.
	//
	// The variant picks the alternative text of the sections that
	// offer one; a section that offers none renders its default text.
	// The empty name and "default" mean the default text.
	//
	// It is the variant the session starts in. The session's own
	// variant may change between the runs.
	PromptVariant string
	// WorkingDir is the directory the tools work in.
	WorkingDir string
	// Env is the scrubbed child environment for the tools.
	Env []string
	// Settings holds the tools' tunable knobs.
	Settings ToolSettings
	// ToolsMode is how the run asks for tool calls.
	ToolsMode string
	// ReasoningEffort is the reasoning_effort the run sends, empty when
	// it sends none.
	ReasoningEffort string
	// ProtectedPaths are the paths the tools must not touch.
	ProtectedPaths []string

	// MaxTurns is the turn ceiling. 0 means unlimited. A run that
	// reaches it settles max_turns.
	MaxTurns int
	// MaxRunTokens is the run's token ceiling. 0 means unlimited. A run
	// that reaches it settles budget_exhausted.
	MaxRunTokens int
	// MaxDuration is the run's wall-clock ceiling. 0 means unlimited. A
	// run that reaches it settles budget_exhausted.
	MaxDuration time.Duration
	// MaxContinuations is how many BeforeSettle continuations a run may
	// take. It defaults to 3.
	MaxContinuations int
	// StopConditions are the conditions that end a run. Each one that
	// matches adds a candidate stop_requested.
	StopConditions []StopCondition
	// NoticeRatio is the share of the window at which the model is
	// told that the context is filling up. It defaults to 0.80.
	NoticeRatio float64
	// HardRatio is the share of the window at which the run stops. It
	// defaults to 0.97.
	HardRatio float64
	// Compactor is the strategy that compacts the view. A session
	// without one never enters the compacting state.
	Compactor Compactor
	// HookTimeout bounds one hook call. It defaults to 30 seconds.
	HookTimeout time.Duration
	// FinishSchema is the JSON schema a finish tool validates its
	// result against. A session with one gets the finish tool.
	FinishSchema json.RawMessage
	// MessageIDs hands out the session-monotonic message ids. A
	// session owns one counter, and a run continues it, so a second
	// run's ids do not repeat the first run's.
	//
	// Nil means the session starts its own counter at one.
	MessageIDs *MessageIDs
	// Observers see every event.
	Observers []Observer
	// SessionStart runs once when the session is built.
	SessionStart []SessionStartHook
	// BeforeRun runs on each run start, before the first request.
	BeforeRun []BeforeRunHook
	// AfterTurn runs at every turn end, including an interrupted turn.
	AfterTurn []AfterTurnHook
	// BeforeSettle runs when the model stopped on its own.
	BeforeSettle []BeforeSettleHook
	// BeforeTool runs after a call is validated and before it runs.
	//
	// The hooks run after the policy slot, in registration order.
	BeforeTool []BeforeToolHook
	// Policy is the policy slot: the before-tool hook the permission
	// engine fills.
	//
	// The slot runs before every other before-tool hook, and it is
	// exempt from the hook deadline: the permission engine keeps its
	// own chain deadline and always returns a decision.
	Policy PolicyHook
	// AfterTool runs after a call ran and before the model sees the
	// result.
	//
	// A job's chain runs at the job's completion, under the session's
	// own context.
	AfterTool []AfterToolHook
	// Jobs configures the session's background jobs.
	Jobs JobOptions
	// Logger receives the core's diagnostic messages: observer errors
	// and overflow, candidates that arrive after the commit point, and
	// completions that arrive after it.
	//
	// Nil means a no-op. The core logs outside the session lock, so a
	// log call never changes a record or an event, and never fails a
	// run. A consumer's handler should not block: a slog call is
	// synchronous.
	Logger *slog.Logger
}

// New builds a session.
//
// The build is transactional: a failure at any step rolls the session
// back, and nothing is recorded. A session without a transport or
// without a context window is a typed error, because a session that
// cannot talk to a model, or that cannot measure its context, would
// fail later in a way that is much harder to see.
func New(opts Options, fns ...Option) (*Session, error) {
	for _, fn := range fns {
		if fn != nil {
			fn(&opts)
		}
	}
	// The core adds its own job tools and the order they are offered
	// in, so it works on copies: a caller may build two sessions from
	// one tools map and one order.
	if opts.Tools != nil {
		tools := make(map[string]Tool, len(opts.Tools))
		for name, tool := range opts.Tools {
			tools[name] = tool
		}
		opts.Tools = tools
	}
	opts.ToolOrder = append([]string(nil), opts.ToolOrder...)
	if opts.Transport == nil {
		return nil, &ErrSessionInit{Cause: errors.New("poo: the session has no chat transport")}
	}
	// The session speaks the Chat Completions API unless the descriptor
	// names another one: it is the only API v1 speaks, and an unknown
	// API would skip the check that keeps a foreign block out of the
	// requests.
	if opts.Model.API == "" {
		opts.Model.API = APIChatCompletions
	}
	if opts.Model.ContextLimit <= 0 {
		return nil, &ErrSessionInit{Cause: errors.New("poo: the session has no context limit")}
	}
	if err := checkFinishTool(&opts); err != nil {
		return nil, &ErrSessionInit{Cause: err}
	}
	if opts.Decider != nil {
		if err := checkDeciderModel(opts.DeciderModel); err != nil {
			return nil, &ErrSessionInit{Cause: err}
		}
	}
	if opts.MaxContinuations <= 0 {
		opts.MaxContinuations = DefaultMaxContinuations
	}
	if opts.NoticeRatio <= 0 {
		opts.NoticeRatio = DefaultNoticeRatio
	}
	if opts.HardRatio <= 0 {
		opts.HardRatio = DefaultHardRatio
	}
	if opts.HookTimeout <= 0 {
		opts.HookTimeout = DefaultHookTimeout
	}
	ids := opts.MessageIDs
	if ids == nil {
		ids = &MessageIDs{}
	}
	opts.MessageIDs = ids

	s := &Session{
		opts:              opts,
		recordWriteBudget: defaultRecordWriteBudget,
		state:             StateIdle,
		results:           map[string]ToolResult{},
		resultMeta:        map[string]resultMeta{},
		recorded:          map[string]Message{},
		messageIDs:        ids,
		noticeArmed:       true,
		jobs:              map[string]*jobState{},
		jobQueued:         make(chan struct{}, 1),
		gatesDone:         make(chan struct{}, 1),
		deciderModel:      opts.DeciderModel,
	}
	// A construction that fails rolls back everything it started. The
	// event bus's reporter is a goroutine, and no session is returned
	// for the caller to close.
	complete := false
	defer func() {
		if complete {
			return
		}
		if s.events != nil {
			s.events.stop()
		}
		if s.jobCancel != nil {
			s.jobCancel(nil)
		}
	}()
	s.est.charsPerToken = CharsPerToken
	if opts.Logger != nil {
		s.logs = newLogDispatcher(opts.Logger.Handler())
		s.log = slog.New(s.logs.handler())
	}
	s.jobCtx, s.jobCancel = context.WithCancelCause(context.Background())

	// The job tools are the core's own, and they are registered
	// whenever a tool may run in the background.
	if err := s.registerJobTools(); err != nil {
		return nil, &ErrSessionInit{Cause: err}
	}

	// The prompt is assembled from the sections the core and the
	// consumer put in the builder. The job tools are registered by
	// now, so their own sections are in it too.
	s.promptBuilder = buildPrompt(&s.opts)
	s.promptVariant = s.opts.PromptVariant

	// The observers start before anything can emit an event, and the
	// bus's reporter starts with them.
	s.events = newEventBus(s.logger().With(slog.String("session_id", opts.SessionID)))
	// The decision model is wrapped before the session-start hooks run,
	// so a hook may ask it a question.
	s.decider = s.wrapDecider(opts.Decider)
	for _, o := range opts.Observers {
		s.events.addObserver(o)
	}
	// The overflow records are the store's, and they are written by the
	// event bus's own reporter.
	s.events.setDropReporter(s.writeObserverDropped)

	// The session-start hooks' output is buffered: a construction that
	// fails leaves nothing written.
	buffered, err := s.runSessionStart()
	if err != nil {
		return nil, err
	}
	for _, rec := range buffered {
		if err := s.write(context.Background(), rec); err != nil {
			return nil, &ErrSessionInit{Cause: err}
		}
	}
	complete = true
	return s, nil
}

// ErrSessionInit is the error a failed construction returns.
//
// The construction failure is typed, so a caller can tell it from a
// failure that happened while a run was going.
type ErrSessionInit struct {
	// Cause is what went wrong.
	Cause error
}

func (e *ErrSessionInit) Error() string { return e.Cause.Error() }

// Unwrap returns the cause.
func (e *ErrSessionInit) Unwrap() error { return e.Cause }

// Defaults the plan fixes.
const (
	// DefaultMaxContinuations is how many BeforeSettle continuations a
	// run may take.
	DefaultMaxContinuations = 3
	// DefaultNoticeRatio is the share of the context window at which
	// the model is told that the context is filling up.
	DefaultNoticeRatio = 0.80
	// DefaultHardRatio is the share of the context window at which the
	// run stops.
	DefaultHardRatio = 0.97
	// DefaultHookTimeout bounds one hook call.
	DefaultHookTimeout = 30 * time.Second
	// DefaultMaxOutput is the output-token ceiling a model without one
	// gets.
	DefaultMaxOutput = 8192
	// MinOutputTokens is the smallest output room a request may ask
	// for. Below it, the context is effectively full.
	MinOutputTokens = 256
	// OutputReserve is the room the core keeps free when it computes
	// the output ceiling.
	OutputReserve = 256
	// CharsPerToken is the starting estimate of characters per token.
	// The first usage report calibrates it.
	CharsPerToken = 3.5
	// MinCharsPerToken and MaxCharsPerToken bound the calibration.
	MinCharsPerToken = 2.0
	MaxCharsPerToken = 5.0
	// ContextNoticeText is the notice the model reads when the
	// context window is filling up.
	ContextNoticeText = "[Context notice: ~80% of the context window used. Prefer narrow reads; finish or report soon.]"
)

// State returns where the session is in the FSM.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Running reports that the session has a run.
//
// A settled session keeps the settled state until its next run starts,
// so the state alone does not say whether the session is busy. A client
// that wants to know may ask this.
func (s *Session) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run != nil
}

// ActiveRunID returns the id of the run that is active.
//
// It is empty when the session has no run.
func (s *Session) ActiveRunID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return ""
	}
	return s.run.id
}

// RunResult returns the outcome of a run that has settled.
//
// It reports false for a run the session does not know: a run that is
// still going and a run that never was are both unknown to it.
func (s *Session) RunResult(runID string) (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.runResults[runID]
	return result, ok
}

// Transitions returns the states the session moved through, in order.
func (s *Session) Transitions() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]State(nil), s.transitions...)
}

// ID returns the session's identifier.
func (s *Session) ID() string { return s.opts.SessionID }

// ChatModel returns the model the session talks to.
func (s *Session) ChatModel() ModelInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opts.Model
}

// DeciderModel returns the decision model and reports whether the
// session has one.
func (s *Session) DeciderModel() (ModelInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deciderModel.Name == "" && s.deciderModel.ModelID == "" {
		return ModelInfo{}, false
	}
	return s.deciderModel, true
}

// Broken reports that the store failed and the session cannot be used.
func (s *Session) Broken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
}

// LastAssistantMessage returns the text of the last assistant message.
//
// It is the answer the model gave, and it is what a one-shot client
// prints when the run produced no typed result.
func (s *Session) LastAssistantMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].Role != RoleAssistant {
			continue
		}
		if text := s.history[i].Text(); text != "" {
			return text
		}
	}
	return ""
}

// MessageIDs returns the session's message-id counter.
//
// The counter belongs to the session, so a run's ids continue where the
// session's last ones stopped, and a delivery note can name any message
// the session has assigned an id to.
func (s *Session) MessageIDs() *MessageIDs { return s.opts.MessageIDs }

// Events returns the events the session published, in order.
func (s *Session) Events() []Event { return s.events.events() }

// RepeatCounts returns the repeat counts of the call that ran last.
//
// The counts are the doom loop's: the exact count is how many times the
// model made this exact call, and the near count is how many times it
// made a call with the same primary argument. The policy slot reads
// them when it is asked about a call.
func (s *Session) RepeatCounts() RepeatCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRepeatCountsLocked()
}

// RepeatCountsFor returns the repeat counts of one call.
//
// A handle that was never counted has no counts.
func (s *Session) RepeatCountsFor(handle string) RepeatCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts, ok := s.repeatCounts[handle]
	if !ok {
		return RepeatCounts{}
	}
	return RepeatCounts{Exact: counts.exact, Near: counts.near}
}

// lastRepeatCountsLocked returns the repeat counts of the call that ran
// last.
//
// The caller holds the session's lock.
func (s *Session) lastRepeatCountsLocked() RepeatCounts {
	if len(s.repeatOrder) == 0 {
		return RepeatCounts{}
	}
	counts := s.repeatCounts[s.repeatOrder[len(s.repeatOrder)-1]]
	return RepeatCounts{Exact: counts.exact, Near: counts.near}
}

// RepeatCounts are the repeat counters of one call.
type RepeatCounts struct {
	// Exact is how many times the model made this exact call.
	Exact int `json:"exact"`
	// Near is how many times the model made a call with the same
	// primary argument.
	Near int `json:"near"`
}

// hookMessage turns a hook's appended message into a user message.
//
// The step's output enters the history as a user-role message tagged
// with the hook's name, so a reader of the record knows which hook put
// it there.
func hookMessage(msg Message, name string) Message {
	msg.Role = RoleUser
	msg.Origin = HookOrigin(name)
	return msg
}

// hookContext builds the context a hook is called with.
func (s *Session) hookContext(ctx context.Context, turn int, interrupted bool) HookContext {
	return s.hookContextFor(ctx, "", "", turn, interrupted)
}

// hookContextFor builds the context a hook is called with, naming the
// run and the job the hook runs for.
//
// A job's completion names the run that started the job, which may
// already have settled, and the job itself.
func (s *Session) hookContextFor(ctx context.Context, runID, job string, turn int, interrupted bool) HookContext {
	s.mu.Lock()
	if runID == "" && s.run != nil {
		runID = s.run.id
	}
	s.mu.Unlock()
	return HookContext{
		Context:     ctx,
		SessionID:   s.opts.SessionID,
		RunID:       runID,
		Job:         job,
		Turn:        turn,
		Interrupted: interrupted,
		History:     s.view(),
		Budget:      s.budget(),
		Decider:     s.Decider(),
	}
}

// view returns the model's history as a read-only view.
func (s *Session) view() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return NewHistory(s.modelMessagesLocked(), s.results, s.turns)
}

// modelMessagesLocked returns the messages in the order the model reads
// them.
//
// The current run's own system prompt comes first, and the rest of the
// history follows in the order it was recorded. Earlier runs' system
// messages stay in the records, but they are not sent again: a run's
// prompt is frozen for that run.
//
// The caller holds the session's lock.
func (s *Session) modelMessagesLocked() []Message {
	messages := make([]Message, 0, len(s.history)+1)
	if s.run != nil && s.run.systemMessage != nil {
		messages = append(messages, *s.run.systemMessage)
	}
	for _, msg := range s.history {
		if msg.Role == RoleSystem {
			continue
		}
		messages = append(messages, msg)
	}
	// The provider blocks the current model cannot read are left out of
	// the request: the history keeps them, and the switch that brought
	// the model recorded the omission.
	messages = stripForeignBlocks(messages, s.opts.Model.API)
	return CloneMessages(messages)
}

// budget returns the run's context budget.
func (s *Session) budget() Budget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.budgetLocked()
}

// budgetLocked returns the run's context budget.
//
// The caller holds the session's lock.
func (s *Session) budgetLocked() Budget {
	turns := 0
	if s.run != nil {
		turns = s.run.turn
	}
	return Budget{
		ContextLimit:   s.opts.Model.ContextLimit,
		EstimatedInput: s.estimatedInputLocked(),
		NoticeRatio:    s.opts.NoticeRatio,
		HardRatio:      s.opts.HardRatio,
		CharsPerToken:  s.est.charsPerToken,
		Turns:          turns,
	}
}

// appendMessages adds messages to the model's view.
func (s *Session) appendMessages(msgs ...Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msgs...)
}

// recordMessage keeps a message under its id.
//
// The record outlives the view: a compaction may remove a message from
// the view, and a delivery note still renders from the original
// recorded message.
func (s *Session) recordMessage(msg Message) {
	if msg.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorded[msg.ID] = msg
}

// markBroken marks the session broken.
//
// A broken session refuses every later prompt: the store is the
// session's memory, and a session that cannot write cannot be trusted.
func (s *Session) markBroken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broken = true
	s.sunk = true
}

// write appends one record to the sink.
//
// A refused write marks the session broken: the store is the session's
// memory, and a session that cannot write cannot be trusted. A write the
// close has fenced is abandoned instead: the session is going away, and
// the record is one nobody will read.
func (s *Session) write(ctx context.Context, rec Record) error {
	return s.writeClaimed(ctx, rec, nil)
}

// writeClaimed appends one record whose write was registered under the
// session's lock, in the same critical section as the decision that
// produced it.
//
// The registration is the caller's; the write releases it.
func (s *Session) writeClaimed(ctx context.Context, rec Record, gate *writeGate) error {
	wctx, cancel := s.recordContext(ctx)
	defer cancel()
	return s.appendRecord(wctx, rec, nil, gate)
}

// writeGate is the registration of one record write against the close's
// fence.
type writeGate struct {
	// record names the record, so a close that outlives its wait can say
	// which write held it up.
	record string
	// released reports that the write has ended.
	released bool
}

// heldRecord is a held record waiting to be written, with the write
// registration its decision was taken under.
type heldRecord struct {
	// rec is the held record.
	rec *InputHeld
	// gate is the write's registration.
	gate *writeGate
}

// beginWriteLocked registers a record write against the close's fence.
//
// The caller holds the session's lock, so a decision taken in the same
// critical section has its write counted before the close can fence the
// session. The gate is released with endWrite. ok is false once the close
// has fenced the session, and the write is then abandoned.
func (s *Session) beginWriteLocked(record string) (*writeGate, bool) {
	if s.fenced {
		return nil, false
	}
	if s.gatesDone == nil {
		s.gatesDone = make(chan struct{}, 1)
	}
	gate := &writeGate{record: record}
	s.gates = append(s.gates, gate)
	return gate, true
}

// beginWrite registers a record write, taking the session's lock.
func (s *Session) beginWrite(record string) (*writeGate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginWriteLocked(record)
}

// endWrite releases a registration and wakes a close that is waiting.
func (s *Session) endWrite(gate *writeGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endWriteLocked(gate)
}

// endWriteLocked releases a registration.
//
// The caller holds the session's lock.
func (s *Session) endWriteLocked(gate *writeGate) {
	if gate == nil || gate.released {
		return
	}
	gate.released = true
	for i, held := range s.gates {
		if held == gate {
			s.gates = append(s.gates[:i], s.gates[i+1:]...)
			break
		}
	}
	s.signalGatesLocked()
}

// signalGatesLocked wakes the close, which waits for the writes.
//
// The caller holds the session's lock.
func (s *Session) signalGatesLocked() {
	if s.gatesDone == nil {
		return
	}
	select {
	case s.gatesDone <- struct{}{}:
	default:
	}
}

// awaitWrites waits for the record writes that are in flight, bounded by
// the record-write budget.
//
// The wait names the writes it is still waiting for when the bound runs
// out, so a close that cannot finish says which records held it up.
func (s *Session) awaitWrites() error {
	deadline := time.After(s.recordBudget())
	for {
		s.mu.Lock()
		if len(s.gates) == 0 {
			s.mu.Unlock()
			return nil
		}
		done := s.gatesDone
		s.mu.Unlock()
		select {
		case <-done:
		case <-deadline:
			s.mu.Lock()
			names := s.gateNamesLocked()
			s.mu.Unlock()
			return fmt.Errorf("poo: closing the session: the %s writes are still in flight: %w",
				strings.Join(names, ", "), ErrCloseStalled)
		}
	}
}

// gateNamesLocked returns the names of the record writes in flight.
//
// The caller holds the session's lock.
func (s *Session) gateNamesLocked() []string {
	out := make([]string, 0, len(s.gates))
	for _, gate := range s.gates {
		out = append(out, gate.record)
	}
	return out
}

// writeRun appends one record that belongs to a run.
//
// The record is claimed against the run's seal: a hook that appends a
// message while the finalization is under way writes nothing, because
// the run's records are the finalization's from the seal on. A write
// that is claimed before the seal is waited for by the finalization, so
// no record of the run follows run.settled.
func (s *Session) writeRun(run *runState, rec Record) error {
	if run == nil {
		return s.write(context.Background(), rec)
	}
	ctx, release, ok := s.beginRunWrite(run)
	if !ok {
		// The finalization sealed the run: the record is abandoned.
		return fmt.Errorf("poo: writing %s: %w", rec.Meta().Record, errCallRecordSealed)
	}
	defer release()
	return s.appendRecord(ctx, rec, run, nil)
}

// runWriteClaim is one claimed hook write for a run.
type runWriteClaim struct {
	// cancel ends the write, with the seal's cause when the
	// finalization stops waiting for it.
	cancel context.CancelCauseFunc
	// timeout ends the claim's own record-write budget.
	timeout context.CancelFunc
}

// beginRunWrite claims a record's write against the run's seal.
//
// The claim is made under the session's lock, so a write that arrives
// after the seal is refused, and one that arrives before it is waited
// for by the finalization. The returned context ignores the run's own
// cancellation, because the record has to say what the run did, and the
// seal cancels it, so the write never follows run.settled.
func (s *Session) beginRunWrite(run *runState) (context.Context, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginRunWriteLocked(run)
}

// beginRunWriteLocked claims a record's write against the run's seal.
//
// The caller holds the session's lock. The claim is made under the
// lock, so a write that registers work and claims its record does so
// atomically with respect to the seal.
func (s *Session) beginRunWriteLocked(run *runState) (context.Context, func(), bool) {
	if run.sealed {
		return nil, nil, false
	}
	base := run.ctx
	if base == nil {
		base = context.Background()
	}
	timed, timeout := context.WithTimeout(context.WithoutCancel(base), s.recordBudget())
	ctx, cancel := context.WithCancelCause(timed)
	claim := &runWriteClaim{cancel: cancel, timeout: timeout}
	run.writeClaims = append(run.writeClaims, claim)
	run.runWrites++

	release := func() {
		cancel(nil)
		timeout()
		s.mu.Lock()
		for i, held := range run.writeClaims {
			if held == claim {
				run.writeClaims = append(run.writeClaims[:i], run.writeClaims[i+1:]...)
				break
			}
		}
		run.runWrites--
		s.signalWorkLocked(run)
		s.mu.Unlock()
	}
	return ctx, release, true
}

// errWriteFenced reports that the close refused a record write: the
// record is abandoned, and the store is not at fault.
var errWriteFenced = errors.New("poo: the close fenced the record writer")

// appendToSink writes one record to the session's store.
//
// The write is registered against the close's fence before it calls the
// sink: a close that has begun refuses it, and a close that has not yet
// waits for it. A registration the caller took under the session's lock
// together with the decision that produced the record is handed in; a nil
// one is taken here. Either way the registration is released when the
// write ends.
func (s *Session) appendToSink(ctx context.Context, rec Record, gate *writeGate) error {
	if s.opts.Sink == nil {
		s.endWrite(gate)
		return nil
	}
	if gate == nil {
		var ok bool
		gate, ok = s.beginWrite(rec.Meta().Record)
		if !ok {
			return errWriteFenced
		}
	}
	defer s.endWrite(gate)
	s.mu.Lock()
	sunk := s.sunk
	s.mu.Unlock()
	if sunk {
		return errors.New("poo: the session store is broken")
	}
	return s.opts.Sink.Append(ctx, rec)
}

// appendRecord appends one record under the context given.
//
// A refused write marks the session broken, unless the run the record
// belongs to is sealed or the close has fenced the writer: a record the
// finalization ended at the seal, or the close refused, is abandoned, and
// its sink's refusal says nothing about the store.
func (s *Session) appendRecord(ctx context.Context, rec Record, sealedRun *runState, gate *writeGate) error {
	err := s.appendToSink(ctx, rec, gate)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errWriteFenced):
		return nil
	}
	if errors.Is(context.Cause(ctx), errCallRecordSealed) || s.sealedRun(sealedRun) {
		return fmt.Errorf("poo: writing %s: %w", rec.Meta().Record, errCallRecordSealed)
	}
	s.mu.Lock()
	s.sunk = true
	s.broken = true
	s.mu.Unlock()
	return fmt.Errorf("poo: writing %s: %w", rec.Meta().Record, err)
}

// sealedRun reports that the finalization has sealed a run.
func (s *Session) sealedRun(run *runState) bool {
	if run == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return run.sealed
}

// errCallRecordSealed reports that the finalization ended a call record's
// write at the seal: the record is abandoned, not refused.
var errCallRecordSealed = errors.New("poo: the finalization sealed the call record")

// writeCallRecord appends a call's tool.call record.
//
// A record a claim is writing runs under the claim's own context: it
// ignores the run's own cancellation, so the record is still written when
// the run ends, and the finalization ends it at the seal, so a record it
// stopped waiting for never follows run.settled. The claim's context has
// no bound of its own, because the finalization's wait bounds it.
//
// A record the finalization writes itself is bounded by the record-write
// budget, because no claim is waiting for it any more. Either way, a
// record the store does not take after the seal is abandoned rather than
// a store failure: the run's outcome is already fixed.
func (s *Session) writeCallRecord(run *runState, call *issuedCall, rec Record) error {
	if call.recordCtx != nil {
		return s.appendRecord(call.recordCtx, rec, run, nil)
	}
	base := run.ctx
	if base == nil {
		base = context.Background()
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(base), s.recordBudget())
	defer cancel()
	return s.appendRecord(wctx, rec, run, nil)
}

// defaultRecordWriteBudget bounds one record write or flush.
const defaultRecordWriteBudget = 5 * time.Second

// recordBudget returns the session's record-write budget.
//
// A session built by hand in a test may not have one; the default
// covers it.
func (s *Session) recordBudget() time.Duration {
	if s.recordWriteBudget > 0 {
		return s.recordWriteBudget
	}
	return defaultRecordWriteBudget
}

// recordContext returns the context a record write runs under.
//
// A record is written even when the run's context has ended, because the
// log has to say what the run did and how it ended. The write is still
// bounded, so a store that never answers cannot hang the process.
func (s *Session) recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.recordBudget())
}

// hashText returns the hex SHA-256 of a text.
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// nowRFC3339 returns the current time in UTC, RFC 3339 with nanoseconds.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// recordArgs returns the arguments as JSON, or the raw text as a JSON
// string when the model did not send valid JSON.
//
// A record always holds valid JSON, so a consumer can decode the
// arguments without a special case, and a model that sent malformed
// arguments is still recorded faithfully.
func recordArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	if json.Valid(raw) {
		return raw
	}
	quoted, err := json.Marshal(string(raw))
	if err != nil {
		return json.RawMessage("{}")
	}
	return quoted
}

// estimateMessageChars returns the approximate serialized size of a
// message.
func estimateMessageChars(m Message) int {
	chars := len(string(m.Role))
	for _, c := range m.Content {
		chars += len(c.Text) + len(c.Name) + len(c.ToolCallID)
		if len(c.Arguments) > 0 {
			chars += len(c.Arguments)
		}
		if c.Provider != nil {
			chars += len(c.Provider.Data)
		}
	}
	// The delivery note's text is already part of the message's content,
	// where it was prefixed, so it is not counted again.
	return chars
}

// hookName names a hook.
//
// A hook that names itself is asked for its name; the rest are named
// after their type, so a settle record says which hook failed.
func hookName(h any) string {
	if n, ok := h.(interface{ HookName() string }); ok {
		if name := n.HookName(); name != "" {
			return name
		}
	}
	t := reflect.TypeOf(h)
	if t == nil {
		return "hook"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.Name()
	if name == "" {
		name = "hook"
	}
	return name
}
