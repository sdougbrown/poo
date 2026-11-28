package poo

import (
	"context"
	"encoding/json"
	"fmt"
)

// HookToolMaxOutputChars is the cap on a read-only probe's text, in
// characters.
//
// A probe is evidence for a decision, not the model's observation, so
// the cap is the probe's own and not the session's context-relative
// one: a hook that reasons over a whole tree would otherwise hold text
// the model never sees.
const HookToolMaxOutputChars = 8192

// hookProbeTools are the tools a hook may probe, by name.
//
// A probe reads, so it never runs a tool whose effect outlives the
// call. The list is closed, so a consumer's tool of the same read-only
// class is not reached by a hook that names it.
var hookProbeTools = map[string]struct{}{
	"read":    {},
	"grep":    {},
	"history": {},
}

// HookReadOnlyToolAllowed reports whether a hook may probe a tool.
//
// A tool is probeable when it says it reads, is one of the tools a probe
// may name, and runs in the turn its caller runs in: a probe is a call
// the hook waits on, so a tool that only runs as a background job has no
// answer to give it.
func HookReadOnlyToolAllowed(name string, spec ToolSpec) bool {
	if spec.Class != ClassReadOnly {
		return false
	}
	if spec.Background == BackgroundAlways {
		return false
	}
	_, ok := hookProbeTools[name]
	return ok
}

// hookProbeRefusal reports why a hook may not probe a tool by name, or
// "" when it may.
func hookProbeRefusal(name string, spec ToolSpec) string {
	if _, core := coreToolNames[name]; core {
		return fmt.Sprintf("the tool %q is the run's own and cannot run as a read-only probe", name)
	}
	if nestedRefusal(name, spec) != "" {
		return fmt.Sprintf("the tool %q cannot run as a read-only probe", name)
	}
	if !HookReadOnlyToolAllowed(name, spec) {
		return fmt.Sprintf("the tool %q cannot run as a read-only probe: only read, grep and history may", name)
	}
	return ""
}

// readOnlyToolCaller is the ReadOnlyToolCaller a hook's context carries.
//
// The caller names the session and the run, and holds the hook's own
// context, so a probe ends with the hook even when the hook hands in a
// context of its own.
type readOnlyToolCaller struct {
	session *Session
	// runID is the run the hook runs for. It may already have settled,
	// which is why the caller binds no activity of its own.
	runID string
	// parentCtx is the hook's context.
	parentCtx context.Context
}

// CallReadOnly runs one read-only tool and returns what it saw.
//
// The tool is validated and prepared the way a call the model makes is,
// and it runs in the session's environment, so a probe obeys the
// session's read restrictions. The permission engine does not see it: a
// probe raises no ask, so a deny rule and an ask for a path outside the
// working directory leave it alone, and only the tool's own read rules
// bound what it reaches. What follows the call is nothing: the probe
// appends no history, writes no record, emits no event, raises no
// permission ask and touches no repeat counter.
func (c *readOnlyToolCaller) CallReadOnly(ctx context.Context, name string, args json.RawMessage) (observation HookToolObservation, err error) {
	// A tool that panics runs here on the hook's goroutine, where the
	// hook dispatch recovers nothing: the panic would end the process.
	// The model's own call path reports a panicking tool as a fatal
	// tool error, and a probe reports one to the hook the same way.
	defer func() {
		if r := recover(); r != nil {
			observation = HookToolObservation{}
			err = Fatalf("the %s tool panicked: %v", name, r)
		}
	}()

	s := c.session
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	closed := s.closed
	jobCtx := s.jobCtx
	s.mu.Unlock()
	if closed {
		return HookToolObservation{}, ErrSessionClosed
	}
	// A probe never starts for a caller that has already ended: the
	// hook's own deadline or the session's jobs have stopped it.
	for _, candidate := range []context.Context{ctx, c.parentCtx, jobCtx} {
		if candidate != nil && candidate.Err() != nil {
			return HookToolObservation{}, candidate.Err()
		}
	}

	tool, ok := s.opts.Tools[name]
	if !ok {
		return HookToolObservation{}, InvalidArgsf("unknown tool %q", name)
	}
	if refusal := hookProbeRefusal(name, s.toolSpecFor(name)); refusal != "" {
		return HookToolObservation{}, InvalidArgsf("%s", refusal)
	}
	if err := s.argSchemas.validate(name, args); err != nil {
		return HookToolObservation{}, err
	}
	prepared, err := tool.Prepare(args, s.prepareEnv())
	if err != nil {
		return HookToolObservation{}, err
	}
	if _, finish := prepared.(FinishResult); finish {
		return HookToolObservation{}, InvalidArgsf("the tool %q cannot run as a read-only probe", name)
	}

	// The probe ends with its caller, with the hook and with the
	// session's jobs: whichever ends first stops it.
	ctx, release := probeContext(ctx, c.parentCtx, jobCtx)
	defer release()
	if err := ctx.Err(); err != nil {
		return HookToolObservation{}, err
	}

	env := s.toolEnvFor(c.runID, "")
	env.Nested = nil
	env.Decider = nil
	env.Settings.ObservationCapChars = HookToolMaxOutputChars
	env.Settings.ReadOutputCapChars = HookToolMaxOutputChars
	result, callErr := tool.Call(ctx, prepared, env)
	if callErr != nil {
		return HookToolObservation{}, callErr
	}
	text, elided := cutRuneHead(result.Text, HookToolMaxOutputChars)
	return HookToolObservation{Text: text, IsError: result.IsError, Truncated: elided > 0}, nil
}

// probeContext binds a probe to its caller's context and to the
// contexts it borrowed.
//
// The returned release ends the probe and detaches it from the contexts
// that outlive it.
func probeContext(ctx context.Context, parents ...context.Context) (context.Context, func()) {
	bound, cancel := context.WithCancel(ctx)
	stops := make([]func() bool, 0, len(parents))
	for _, parent := range parents {
		if parent == nil {
			continue
		}
		stops = append(stops, context.AfterFunc(parent, cancel))
	}
	return bound, func() {
		for _, stop := range stops {
			stop()
		}
		cancel()
	}
}
