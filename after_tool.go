package poo

import (
	"context"
	"fmt"
)

// The after-tool chain runs after a call ran and before the model sees
// the result.
//
// The whole chain runs before anything about the result is recorded or
// emitted, so the record, the event and the model all see the same
// sanitized result. A hook may keep the result, annotate it, replace it
// or redact it; a redaction wins over a replacement, and a replacement
// wins over an annotation.

// The actions a hook decision names.
//
// They are the decision types the plan names, spelled the way a record
// spells them.
const (
	// ResultActionKeep leaves the result as it is.
	ResultActionKeep = "keep"
	// ResultActionAnnotate adds text to the result the model reads.
	ResultActionAnnotate = "annotate"
	// ResultActionReplace substitutes a result for the original.
	ResultActionReplace = "replace"
	// ResultActionRedact drops the original from the record.
	ResultActionRedact = "redact"
)

// RedactedResultText is what the model reads when a hook redacted a
// result and named no replacement.
const RedactedResultText = "[the result was redacted]"

// hookCompletionContext returns the context a completion's hook chain
// runs under.
//
// The chain is a completion, not part of the work that was stopped: an
// interrupt, a cancellation or a timeout that ended the call must not
// discard the hooks' decisions. The context keeps the run's values and
// drops its cancellation, and each hook gets its own deadline from
// callHook.
func hookCompletionContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

// afterToolOutcome is what the after-tool chain produced.
type afterToolOutcome struct {
	// result is the result the model reads.
	result ToolResult
	// original is the result the tool produced, when a hook replaced
	// it. It is nil when no hook replaced it, and it is nil for a
	// redacted chain: the original never reaches the record.
	original *ToolResult
	// redacted reports that the chain redacted the result.
	redacted bool
	// redactedBy names the hook that redacted the result.
	redactedBy string
	// decisions are the chain's decisions, in order.
	decisions []HookDecision
}

// runAfterTool runs the after-tool chain and returns what it produced.
//
// The hooks run in registration order, and each one sees the previous
// hook's output. A hook that fails settles the run with hook_error, and
// a job's completion turns the failure into the job's own error.
func (s *Session) runAfterTool(ctx context.Context, runID, job string, turn int, info ToolCallInfo, result ToolResult) (out afterToolOutcome, err error) {
	out = afterToolOutcome{result: result}
	// Any accepted redaction makes the whole chain redacted, including
	// every outcome that leaves the chain early because a later hook
	// failed or was interrupted.
	defer out.scrub()
	if len(s.opts.AfterTool) == 0 {
		return out, nil
	}
	current := result
	for _, h := range s.opts.AfterTool {
		hookOut, err := s.callHook(ctx, h, func(hctx context.Context) (any, error) {
			return h.AfterTool(s.hookContextFor(hctx, runID, job, turn, false), info, current)
		})
		if hookOut.interrupted {
			// The hook's decision is discarded, and that is not a
			// hook error.
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("poo: the %s hook: %w", hookName(h), err)
		}
		decision, _ := hookOut.value.(ResultDecision)
		out.decisions = append(out.decisions, decisionRecord(hookName(h), decision))
		switch {
		case decision.Redacted:
			// A redaction wins over everything else, and the
			// original never reaches the record.
			out.redacted = true
			out.redactedBy = hookName(h)
			out.original = nil
			if decision.Replaced != nil {
				current = *decision.Replaced
			} else {
				current = ToolResult{Text: RedactedResultText, IsError: result.IsError}
			}
		case decision.Replaced != nil:
			if !out.redacted {
				// The original is kept in the record: the model
				// has not seen it yet, but the log keeps what the
				// tool really said.
				original := result
				out.original = &original
			}
			current = *decision.Replaced
		case decision.Annotated != "":
			if current.Text == "" {
				current.Text = decision.Annotated
			} else {
				current.Text = current.Text + "\n" + decision.Annotated
			}
		}
		out.result = current
	}
	return out, nil
}

// scrub removes every content a redacted chain must not carry.
//
// A redaction wins over the whole chain, so the decisions omit their
// intermediate contents even when a later hook failed, missed its
// deadline or was interrupted.
func (o *afterToolOutcome) scrub() {
	if !o.redacted {
		return
	}
	o.original = nil
	for i := range o.decisions {
		o.decisions[i].Text = ""
	}
}

// decisionRecord describes what a hook decided about a result.
//
// The text is what the hook added or substituted. A redacted chain's
// decisions carry no text at all.
func decisionRecord(name string, decision ResultDecision) HookDecision {
	rec := HookDecision{Hook: name, Action: ResultActionKeep}
	switch {
	case decision.Redacted:
		rec.Action = ResultActionRedact
	case decision.Replaced != nil:
		rec.Action = ResultActionReplace
		rec.Text = decision.Replaced.Text
	case decision.Annotated != "":
		rec.Action = ResultActionAnnotate
		rec.Text = decision.Annotated
	}
	return rec
}
