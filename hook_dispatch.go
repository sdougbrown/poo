package poo

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// callHook runs one hook and reports what came back.
//
// A hook's deadline is the session's hook timeout, and a decision that
// arrives after it is discarded: the run settles hook_error. A hook
// whose context was cancelled — by an interrupt, by the run's end — is
// discarded as well, and that is not a hook error.
func (s *Session) callHook(ctx context.Context, h any, call func(context.Context) (any, error)) (hookOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.HookTimeout)
	defer cancel()
	deadline, hasDeadline := ctx.Deadline()

	type result struct {
		value any
		err   error
		at    time.Time
	}
	// The hook runs in its own goroutine, so a hook that ignores its
	// context cannot hold the loop up. The goroutine may leak, which
	// is the consumer's bug.
	done := make(chan result, 1)
	go func() {
		v, err := call(ctx)
		done <- result{v, err, time.Now()}
	}()

	select {
	case r := <-done:
		if hasDeadline && !r.at.Before(deadline) {
			// The decision arrived after the deadline. It is
			// discarded, and the run settles hook_error.
			return hookOutcome{}, s.hookDeadlineError(h)
		}
		if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.DeadlineExceeded) {
			// The hook's context ended before the decision was
			// taken: the decision is discarded, and that is not a
			// hook error.
			return hookOutcome{interrupted: true}, nil
		}
		if r.err != nil {
			return hookOutcome{}, r.err
		}
		return hookOutcome{value: r.value}, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return hookOutcome{}, s.hookDeadlineError(h)
		}
		// The hook was interrupted: its decision is discarded, and
		// that is not a hook error.
		return hookOutcome{interrupted: true}, nil
	}
}

// hookDeadlineError reports that a hook missed its deadline.
func (s *Session) hookDeadlineError(h any) error {
	return fmt.Errorf("poo: the %s hook missed its %s deadline", hookName(h), s.opts.HookTimeout)
}

// hookOutcome is what one hook call produced.
type hookOutcome struct {
	// value is the hook's decision.
	value any
	// interrupted reports that the hook's context ended before it
	// answered.
	interrupted bool
}

// hookAppend is a message a hook appended.
type hookAppend struct {
	// name is the hook that appended the message.
	name string
	// message is the message itself.
	message Message
}

// startDecisions is what the start hooks decided.
//
// The session-start hooks and the before-run hooks share the shape: a
// list of messages to append and a list of prompt edits to apply.
type startDecisions struct {
	// messages are the messages the hooks appended, in registration
	// order.
	messages []hookAppend
	// edits are the prompt edits, in registration order. A later edit
	// wins over an earlier one.
	edits []SystemPromptEdit
}

// appendStartDecision adds one hook's decision to the decisions.
//
// The messages keep the hook that appended them, so the record says
// which hook put a message in the history.
func (d *startDecisions) appendStartDecision(name string, decision StartDecision) {
	for _, msg := range decision.Append {
		d.messages = append(d.messages, hookAppend{name: name, message: msg})
	}
	if decision.SystemPrompt != nil {
		d.edits = append(d.edits, decision.SystemPrompt)
	}
}

// runSessionStart runs the session-start hooks.
//
// The hooks' output is buffered in memory: a construction that fails
// leaves nothing written. The prompt edit the hooks made is kept, so
// every run's prompt carries it.
func (s *Session) runSessionStart() ([]Record, error) {
	if len(s.opts.SessionStart) == 0 {
		return nil, nil
	}
	var decisions startDecisions
	var buffered []Record
	for _, h := range s.opts.SessionStart {
		out, err := s.callHook(context.Background(), h, func(hctx context.Context) (any, error) {
			return h.SessionStart(s.hookContext(hctx, 0, false))
		})
		if out.interrupted {
			// A construction has no run to interrupt, so the only
			// thing that ends the hook's context is its deadline.
			return nil, &ErrSessionInit{Cause: s.hookDeadlineError(h)}
		}
		if err != nil {
			return nil, &ErrSessionInit{Cause: fmt.Errorf("poo: the session_start hook: %w", err)}
		}
		decision, _ := out.value.(StartDecision)
		decisions.appendStartDecision(hookName(h), decision)
	}
	// The prompt edits the session start made are kept, so the next
	// run's prompt carries them too.
	s.promptEdits = append(s.promptEdits, decisions.edits...)
	for _, added := range decisions.messages {
		msg := hookMessage(added.message, added.name)
		s.appendMessages(msg)
		buffered = append(buffered, &MessageRecord{
			RecordMeta: NewRecordMetaFrom("message", msg.Origin),
			Role:       msg.Role,
			Content:    msg.Content,
		})
	}
	return buffered, nil
}

// runBeforeRun runs the before-run hooks.
//
// The hooks run at the start of every run, before the first request.
// The trigger says what started the run, and a woken run's job results
// are already in the history when the hooks run.
func (s *Session) runBeforeRun(run *runState, trigger RunTrigger) (startDecisions, error) {
	var decisions startDecisions
	if len(s.opts.BeforeRun) == 0 {
		return decisions, nil
	}
	// The hooks run under their own activity, so an interrupt that
	// arrives while they run cancels them and their decisions are
	// discarded.
	s.startActivity(run)
	defer s.endActivity(run)
	ctx := s.activityContext(run)
	for _, h := range s.opts.BeforeRun {
		out, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.BeforeRun(s.hookContext(hctx, 0, false), trigger)
		})
		if out.interrupted {
			// The hook's decision is discarded, and that is not a
			// hook error.
			return decisions, nil
		}
		if err != nil {
			return decisions, fmt.Errorf("poo: the %s hook: %w", hookName(h), err)
		}
		decision, _ := out.value.(StartDecision)
		decisions.appendStartDecision(hookName(h), decision)
	}
	return decisions, nil
}

// appendStartMessages appends the messages the start hooks appended.
//
// The messages enter the history as user-role messages tagged with the
// hook that put them there, and the record says the same.
func (s *Session) appendStartMessages(run *runState, decisions startDecisions) {
	for _, added := range decisions.messages {
		msg := hookMessage(added.message, added.name)
		s.appendMessages(msg)
		_ = s.writeRun(run, &MessageRecord{
			RecordMeta: NewRecordMetaFrom("message", msg.Origin),
			RunID:      run.id,
			Role:       msg.Role,
			Content:    msg.Content,
		})
	}
}

// runAfterTurn runs the AfterTurn hooks in registration order.
//
// The hooks run on every turn, tool-free turns included, and every hook
// still runs even when one of them asks the run to stop.
func (s *Session) runAfterTurn(run *runState, turn int, interrupted bool) (appended bool, stop *candidate) {
	if len(s.opts.AfterTurn) == 0 {
		return false, nil
	}
	// The turn-end hooks run under their own activity, so an
	// interrupt that arrives while they run cancels them and their
	// decisions are discarded.
	s.startActivity(run)
	defer s.endActivity(run)
	ctx := s.activityContext(run)
	for _, h := range s.opts.AfterTurn {
		out, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.AfterTurn(s.hookContext(hctx, turn, interrupted))
		})
		if out.interrupted {
			// The hook's decision is discarded, and that is not a
			// hook error.
			return appended, nil
		}
		if err != nil {
			return appended, &candidate{reason: ReasonHookError, detail: SettleDetail{
				Source: "after_turn:" + hookName(h),
				Text:   err.Error(),
			}}
		}
		decision, _ := out.value.(TurnDecision)
		if len(decision.Append) > 0 {
			for _, msg := range decision.Append {
				msg = hookMessage(msg, hookName(h))
				s.appendMessages(msg)
				_ = s.writeRun(run, &MessageRecord{
					RecordMeta: NewRecordMetaFrom("message", msg.Origin),
					RunID:      run.id,
					Turn:       turn,
					Role:       msg.Role,
					Content:    msg.Content,
				})
			}
			appended = true
		}
		if decision.Stop && stop == nil {
			// The first Stop's detail is the one that settles the
			// run.
			stop = &candidate{reason: ReasonStopRequested, detail: SettleDetail{
				Source: "after_turn:" + hookName(h),
				Text:   decision.Detail,
			}}
		}
	}
	return appended, stop
}

// runBeforeSettle runs the BeforeSettle hooks in registration order.
//
// The hooks run when the model stopped on its own. A hook that returns
// Continue sends the run back to the model, and the run's continuation
// count decides how many of those it may take.
func (s *Session) runBeforeSettle(run *runState, turn int, natural Reason) (SettleDecision, bool, error) {
	if len(s.opts.BeforeSettle) == 0 {
		return SettleDecision{Accepted: true}, false, nil
	}
	// The settle hooks run under their own activity, so an interrupt
	// that arrives while they run cancels them and their decisions are
	// discarded.
	s.startActivity(run)
	defer s.endActivity(run)
	ctx := s.activityContext(run)
	var continues []hookAppend
	for _, h := range s.opts.BeforeSettle {
		out, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.BeforeSettle(s.hookContext(hctx, turn, false), natural)
		})
		if out.interrupted {
			// The hook's decision is discarded, and that is not a
			// hook error.
			return SettleDecision{}, false, nil
		}
		if err != nil {
			return SettleDecision{}, false, fmt.Errorf("poo: the %s hook: %w", hookName(h), err)
		}
		decision, _ := out.value.(SettleDecision)
		if decision.Stop {
			return decision, false, nil
		}
		if decision.Continue != nil {
			continues = append(continues, hookAppend{name: hookName(h), message: *decision.Continue})
		}
	}
	if len(continues) == 0 {
		return SettleDecision{Accepted: true}, false, nil
	}

	// The continuation count decides whether the run may continue.
	s.mu.Lock()
	run.continuations++
	count := run.continuations
	s.mu.Unlock()
	if count > s.opts.MaxContinuations {
		return SettleDecision{}, false, &hookContinueLimitError{count: count, limit: s.opts.MaxContinuations}
	}

	// Every Continue message is appended, and the run goes back to the
	// model.
	for _, added := range continues {
		msg := hookMessage(added.message, added.name)
		s.appendMessages(msg)
		_ = s.writeRun(run, &MessageRecord{
			RecordMeta: NewRecordMetaFrom("message", msg.Origin),
			RunID:      run.id,
			Turn:       turn,
			Role:       msg.Role,
			Content:    msg.Content,
		})
	}
	return SettleDecision{Accepted: true}, true, nil
}

// hookContinueLimitError reports that a run asked for more
// continuations than it may take.
type hookContinueLimitError struct {
	// count is how many continuations the run asked for.
	count int
	// limit is how many continuations the session allows.
	limit int
}

func (e *hookContinueLimitError) Error() string {
	return fmt.Sprintf("poo: the run asked for %d continuations, and the limit is %d", e.count, e.limit)
}
