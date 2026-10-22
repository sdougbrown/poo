package poo

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
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

// WithPolicy fills the policy slot.
//
// The slot is a before-tool hook that only ever allows or denies, and
// it runs before every other before-tool hook. It is exempt from the
// hook deadline: the permission engine keeps its own chain deadline and
// always returns a decision.
func WithPolicy(h PolicyHook) Option {
	return func(opts *Options) {
		opts.Policy = h
	}
}

// WithAfterTool adds an after-tool hook.
//
// The hooks run after a call ran and before the model sees the result.
// They may keep it, annotate it, replace it or redact it.
//
// A job's chain runs at the job's completion, under the session's own
// context, and never again when the result is delivered.
func WithAfterTool(h AfterToolHook) Option {
	return func(opts *Options) {
		opts.AfterTool = append(opts.AfterTool, h)
	}
}

// WithBeforeRun adds a before-run hook.
//
// The hooks run at the start of every run, before the first request.
// The trigger says what started the run.
func WithBeforeRun(h BeforeRunHook) Option {
	return func(opts *Options) {
		opts.BeforeRun = append(opts.BeforeRun, h)
	}
}

// WithSessionStart adds a session-start hook.
//
// The hook runs once, when the session is built, before the first run.
func WithSessionStart(h SessionStartHook) Option {
	return func(opts *Options) {
		opts.SessionStart = append(opts.SessionStart, h)
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

// WithPrompt changes the session's system prompt.
//
// The mutation is called with the session's prompt builder, after the
// core's own sections and the tools' sections are in it, and before the
// session's first run. Several mutations compose, in the order they
// were added, so a consumer may change the prompt in as many layers as
// it needs.
//
// A mutation that sets a section changes that section, a mutation that
// removes one takes it away, and a mutation that replaces the prompt
// takes the whole thing over.
func WithPrompt(edit func(*prompt.Builder)) Option {
	return func(opts *Options) {
		opts.Prompt = append(opts.Prompt, edit)
	}
}

// WithDecider gives the session a decision model.
//
// The model describes the decision model, and it must serve the
// operation its role needs: the systemone one or the decisions one. A
// model that serves neither is a construction error.
//
// The session wraps the decider, so every call writes a record to the
// session's store and emits the decision event.
func WithDecider(model ModelInfo, d decide.Decider) Option {
	return func(opts *Options) {
		opts.DeciderModel = model
		opts.Decider = d
	}
}

// WithPromptVariant sets the prompt variant the session's prompt
// renders in.
//
// The variant names an alternative text for the sections that offer
// one: the base section and the tools' sections. A section that offers
// no alternative for the variant renders its default text, so a
// variant that no section offers changes nothing.
//
// The variant is fixed for the length of a run, like the prompt itself,
// and it may change between the runs.
func WithPromptVariant(name string) Option {
	return func(opts *Options) {
		opts.PromptVariant = name
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
