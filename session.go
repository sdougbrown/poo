package poo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
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

	// state is where the session is in the FSM.
	state State
	// transitions records the states the session moved through, in
	// order.
	transitions []State

	// history is the model's view: what has been said so far.
	history []Message
	// turns summarises each turn of the current run.
	turns []TurnSummary
	// results holds the tool results of the current run, by handle.
	results map[string]ToolResult
	// messageIDs hands out the session-monotonic message ids.
	messageIDs *MessageIDs
	// repeats counts the consecutive repeats of a call, for the doom
	// loop.
	repeats *repeatCounter
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
	// broken reports that the store failed and the session cannot be
	// used any more.
	broken bool
	// closed reports that the session is closing or closed.
	closed bool

	// steerQueue holds the steer and interrupt messages, in arrival
	// order.
	steerQueue []queuedInput
	// followUpQueue holds the follow-up messages, in arrival order.
	followUpQueue []queuedInput

	// events is the session's event bus.
	events *eventBus

	// sunk reports that the store failed.
	sunk bool

	// handleSeq numbers the tool-result handles the session assigned.
	handleSeq int
	// inputSeq numbers the inputs the session accepts.
	inputSeq int
	// runSeq numbers the runs the session starts.
	runSeq int

	// est is the context-budget estimator.
	est estimator
	// noticeArmed reports that the context notice may fire. The notice
	// re-arms when the estimate drops back below the notice ratio.
	noticeArmed bool

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
	// requests counts the provider requests made so far.
	requests int
	// tokens counts the run's cumulative input and output tokens.
	tokens int
	// startedAt is when the run began.
	startedAt time.Time
	// trigger says what started the run.
	trigger TriggerKind
	// continuations counts the BeforeSettle continuations the run took.
	continuations int
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
	// workDone is signalled when a pending call gets its result.
	workDone chan struct{}

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
	// ModelID is the provider's model identifier.
	ModelID string
	// ContextLimit is the model's context window in tokens. It is
	// required: a value of 0 or less is a construction error
	// (ErrSessionInit), so no run starts without a window to measure
	// against.
	ContextLimit int
	// MaxOutput is the model's output-token ceiling, 0 when unknown.
	MaxOutput int
	// SystemPrompt is the system message prepended to the model's view.
	SystemPrompt string
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
	// The policy slot is a before-tool hook that only ever allows or
	// denies, and it is installed here as the first one.
	BeforeTool []BeforeToolHook
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
	if opts.Transport == nil {
		return nil, &ErrSessionInit{Cause: errors.New("poo: the session has no chat transport")}
	}
	if opts.ContextLimit <= 0 {
		return nil, &ErrSessionInit{Cause: errors.New("poo: the session has no context limit")}
	}
	if err := checkFinishTool(&opts); err != nil {
		return nil, &ErrSessionInit{Cause: err}
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
		opts:        opts,
		state:       StateIdle,
		results:     map[string]ToolResult{},
		messageIDs:  ids,
		repeats:     newRepeatCounter(),
		events:      newEventBus(),
		noticeArmed: true,
	}
	s.est.charsPerToken = CharsPerToken

	// The observers start before anything can emit an event, and they
	// are stopped if the construction fails.
	for _, o := range opts.Observers {
		s.events.addObserver(o)
	}

	// The session-start hooks' output is buffered: a construction that
	// fails leaves nothing written.
	buffered, err := s.runSessionStart()
	if err != nil {
		s.events.stop()
		return nil, err
	}
	for _, rec := range buffered {
		if err := s.write(context.Background(), rec); err != nil {
			s.events.stop()
			return nil, &ErrSessionInit{Cause: err}
		}
	}
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

// Transitions returns the states the session moved through, in order.
func (s *Session) Transitions() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]State(nil), s.transitions...)
}

// ID returns the session's identifier.
func (s *Session) ID() string { return s.opts.SessionID }

// Broken reports that the store failed and the session cannot be used.
func (s *Session) Broken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
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
	Exact int
	// Near is how many times the model made a call with the same
	// primary argument.
	Near int
}

// runSessionStart runs the session-start hooks.
//
// The hooks' output is buffered in memory: a construction that fails
// leaves nothing written.
func (s *Session) runSessionStart() ([]Record, error) {
	if len(s.opts.SessionStart) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.HookTimeout)
	defer cancel()
	var buffered []Record
	for _, h := range s.opts.SessionStart {
		decision, err := h.SessionStart(s.hookContext(ctx, 0, false))
		if err != nil {
			return nil, &ErrSessionInit{Cause: fmt.Errorf("poo: the session_start hook: %w", err)}
		}
		for _, msg := range decision.Append {
			msg = hookMessage(msg, hookName(h))
			s.appendMessages(msg)
			buffered = append(buffered, &MessageRecord{
				RecordMeta: NewRecordMetaFrom("message", msg.Origin),
				Role:       msg.Role,
				Content:    msg.Content,
			})
		}
	}
	return buffered, nil
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
	s.mu.Lock()
	runID := ""
	if s.run != nil {
		runID = s.run.id
	}
	s.mu.Unlock()
	return HookContext{
		Context:     ctx,
		SessionID:   s.opts.SessionID,
		RunID:       runID,
		Turn:        turn,
		Interrupted: interrupted,
		History:     s.view(),
		Budget:      s.budget(),
	}
}

// view returns the model's history as a read-only view.
func (s *Session) view() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return NewHistory(s.history, s.results, s.turns)
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
	return Budget{
		ContextLimit:   s.opts.ContextLimit,
		EstimatedInput: s.estimatedInputLocked(),
		NoticeRatio:    s.opts.NoticeRatio,
		HardRatio:      s.opts.HardRatio,
		CharsPerToken:  s.est.charsPerToken,
	}
}

// appendMessages adds messages to the model's view.
func (s *Session) appendMessages(msgs ...Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msgs...)
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
// memory, and a session that cannot write cannot be trusted.
func (s *Session) write(ctx context.Context, rec Record) error {
	if s.opts.Sink == nil {
		return nil
	}
	s.mu.Lock()
	if s.sunk {
		s.mu.Unlock()
		return errors.New("poo: the session store is broken")
	}
	s.mu.Unlock()

	wctx, cancel := recordContext(ctx)
	defer cancel()
	if err := s.opts.Sink.Append(wctx, rec); err != nil {
		s.mu.Lock()
		s.sunk = true
		s.broken = true
		s.mu.Unlock()
		return fmt.Errorf("poo: writing %s: %w", rec.Meta().Record, err)
	}
	return nil
}

// writeBestEffort appends one record to the sink and ignores the
// failure.
//
// The accounting of a request that failed is worth less than the record
// that says the run ended, so a write that fails here neither stops the
// run nor marks the store broken.
func (s *Session) writeBestEffort(ctx context.Context, rec Record) {
	if s.opts.Sink == nil {
		return
	}
	s.mu.Lock()
	if s.sunk {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	wctx, cancel := recordContext(ctx)
	defer cancel()
	_ = s.opts.Sink.Append(wctx, rec)
}

// recordWriteBudget bounds one record write or flush.
var recordWriteBudget = 5 * time.Second

// recordContext returns the context a record write runs under.
//
// A record is written even when the run's context has ended, because the
// log has to say what the run did and how it ended. The write is still
// bounded, so a store that never answers cannot hang the process.
func recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), recordWriteBudget)
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
