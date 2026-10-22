package poo

import (
	"encoding/json"
	"fmt"
	"time"
)

// Option changes a session's options when the session is built.
//
// The options are the session's whole configuration, and an option is a
// typed value: there is no hidden default for the context window, and
// no option that silently changes the loop's behaviour.
type Option func(*Options)

// WithFinish gives the session a typed-result tool.
//
// The schema is the JSON schema the result is validated against. The
// tool is added to the session's tools, and the model is offered it
// alongside the consumer's own tools.
//
// The tool itself is `poo/tool/finish`, which the consumer links; the
// core names the call it recognizes. A session with a finish schema and
// no tool of that name is a construction error, because the model would
// never be offered the tool the schema is for.
func WithFinish(schema json.RawMessage) Option {
	return func(opts *Options) {
		opts.FinishSchema = schema
	}
}

// WithStopCondition adds a stop condition.
//
// The conditions are asked at every turn end, after the whole batch has
// run, and each one that matches ends the run.
func WithStopCondition(c StopCondition) Option {
	return func(opts *Options) {
		opts.StopConditions = append(opts.StopConditions, c)
	}
}

// WithAfterTurn adds an after-turn hook.
//
// The hooks run on every turn, in registration order, and every hook
// still runs even when one of them asks the run to stop.
func WithAfterTurn(h AfterTurnHook) Option {
	return func(opts *Options) {
		opts.AfterTurn = append(opts.AfterTurn, h)
	}
}

// WithBeforeSettle adds a before-settle hook.
//
// The hooks run when the model stopped on its own.
func WithBeforeSettle(h BeforeSettleHook) Option {
	return func(opts *Options) {
		opts.BeforeSettle = append(opts.BeforeSettle, h)
	}
}

// WithBeforeTool adds a before-tool hook.
//
// The hooks run after a call is validated and before it runs, and they
// may refuse the call or answer it themselves.
func WithBeforeTool(h BeforeToolHook) Option {
	return func(opts *Options) {
		opts.BeforeTool = append(opts.BeforeTool, h)
	}
}

// WithObserver adds an observer.
//
// The observers see every event, in the session's order.
func WithObserver(o Observer) Option {
	return func(opts *Options) {
		opts.Observers = append(opts.Observers, o)
	}
}

// WithCompactor sets the strategy that compacts the view.
//
// A session without one never enters the compacting state.
func WithCompactor(c Compactor) Option {
	return func(opts *Options) {
		opts.Compactor = c
	}
}

// WithBudgets sets the run's budgets.
//
// A budget of zero is off, so the zero value of the options means "no
// budget", not "a budget of nothing".
func WithBudgets(maxTurns, maxRunTokens int, maxDuration time.Duration) Option {
	return func(opts *Options) {
		opts.MaxTurns = maxTurns
		opts.MaxRunTokens = maxRunTokens
		opts.MaxDuration = maxDuration
	}
}

// WithContextRatios sets the context window's ratios.
//
// The notice ratio is the share of the window at which the model is
// told that the context is filling up, and the hard ratio is the share
// at which the run stops.
func WithContextRatios(notice, hard float64) Option {
	return func(opts *Options) {
		opts.NoticeRatio = notice
		opts.HardRatio = hard
	}
}

// finishToolName is the name the finish tool is called by.
const finishToolName = FinishToolName

// checkFinishTool reports that the session has no tool for its finish
// schema.
//
// The core cannot build the tool: it is `poo/tool/finish`, and the
// dependency rules keep the core away from it. A consumer that names a
// schema links the tool and adds it.
func checkFinishTool(opts *Options) error {
	if len(opts.FinishSchema) == 0 {
		return nil
	}
	if _, ok := opts.Tools[finishToolName]; !ok {
		return fmt.Errorf("poo: the session has a finish schema but no %q tool", finishToolName)
	}
	return nil
}
