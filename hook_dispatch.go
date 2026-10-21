package poo

import (
	"context"
	"errors"
	"fmt"
)

// callHook runs one hook and reports what came back.
//
// A hook's deadline is the session's hook timeout, and a decision that
// arrives after it is discarded: the run settles hook_error.
func (s *Session) callHook(ctx context.Context, h any, call func(context.Context) (any, error)) (hookOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.HookTimeout)
	defer cancel()

	type result struct {
		value any
		err   error
	}
	// The hook runs in its own goroutine, so a hook that ignores its
	// context cannot hold the loop up. The goroutine may leak, which is
	// the consumer's bug.
	done := make(chan result, 1)
	go func() {
		v, err := call(ctx)
		done <- result{v, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			return hookOutcome{}, r.err
		}
		return hookOutcome{value: r.value}, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return hookOutcome{}, fmt.Errorf("poo: the %s hook missed its %s deadline", hookName(h), s.opts.HookTimeout)
		}
		// The hook was interrupted: its decision is discarded, and
		// that is not a hook error.
		return hookOutcome{interrupted: true}, nil
	}
}

// hookOutcome is what one hook call produced.
type hookOutcome struct {
	// value is the hook's decision.
	value any
	// interrupted reports that the hook's context ended before it
	// answered.
	interrupted bool
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
				_ = s.write(run.ctx, &MessageRecord{
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
	var continues []Message
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
			continues = append(continues, *decision.Continue)
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
	for _, msg := range continues {
		msg = hookMessage(msg, "before_settle")
		s.appendMessages(msg)
		_ = s.write(run.ctx, &MessageRecord{
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
