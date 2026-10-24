// Package perm implements poo's permission policy.
//
// The package holds three things: the policy that decides what a tool
// call may do, the chain of askers that answers a call the policy asks
// about, and the seam a wire client implements to answer one.
//
// The policy is a before-tool hook. The core installs it with
// poo.WithPolicy, and it runs before every other before-tool hook. It
// only ever allows or denies a call: an ask happens inside the policy,
// and the policy blocks on the asker chain until the ask is answered.
package perm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sdougbrown/poo"
)

// Action is what a rule, a setting or a fallback says to do with a
// call.
type Action string

const (
	// ActionAllow lets the call run.
	ActionAllow Action = "allow"
	// ActionAsk asks the asker chain about the call.
	ActionAsk Action = "ask"
	// ActionDeny refuses the call.
	ActionDeny Action = "deny"
)

// The defaults a policy uses when nothing says otherwise.
const (
	// DefaultUnmatched is the action for a side-effecting call no rule
	// matches.
	DefaultUnmatched = ActionAsk
	// DefaultFallback is the decision when the asker chain is
	// exhausted.
	DefaultFallback = ActionDeny
	// DefaultChainTimeout is how long the whole chain may take.
	DefaultChainTimeout = 120 * time.Second
	// DefaultDoomLoopThreshold is the exact repeat count that triggers
	// the doom loop's action.
	DefaultDoomLoopThreshold = 3
	// DefaultDoomLoopNearThreshold is the near repeat count that
	// triggers the doom loop's action.
	DefaultDoomLoopNearThreshold = 5
	// DefaultDoomLoopAction is what a doom loop does.
	DefaultDoomLoopAction = ActionAsk
)

// The texts a refusal shows the model.
const (
	// DeniedText is the text a policy refusal shows the model.
	DeniedText = "Blocked: the permission policy denied this call."
	// DoomLoopDeniedText is the text a doom-loop refusal shows the
	// model. The count is the number of repeats that triggered the
	// loop.
	DoomLoopDeniedText = "Blocked: this exact call was repeated %d times. Try a different approach."
	// AskDeniedText is the text a refused ask shows the model. The name
	// is who refused it: an asker, or the fallback.
	AskDeniedText = "Blocked: the permission ask was denied by %s."
)

// Rule is one policy rule.
//
// A rule matches on the tool's name or on its class, and a bash rule
// may match on the command's prefix.
type Rule struct {
	// Tool is the name of the tool the rule matches, exactly.
	Tool string
	// Class is the class of the tools the rule matches.
	Class poo.ToolKind
	// CommandPrefix matches the raw command of a bash call.
	//
	// The prefix is a lexical heuristic and not a safety boundary: it
	// compares the command's text, so "git status && rm -rf x" matches
	// the prefix "git status".
	CommandPrefix string
	// Action is what the rule says to do with a matching call.
	Action Action
}

// DoomLoop is the doom loop's settings.
//
// The loop is a permission case: the core counts the repeats of a call
// and the policy decides what a loop does.
type DoomLoop struct {
	// Threshold is the exact repeat count that triggers the action.
	Threshold int
	// NearThreshold is the near repeat count that triggers the action.
	NearThreshold int
	// Action is what a loop does: allow, ask or deny.
	Action Action
}

// Options configure a policy.
type Options struct {
	// Rules are the policy's rules, in order. The first match wins.
	Rules []Rule
	// Unmatched is the action for a side-effecting call no rule
	// matches. It defaults to DefaultUnmatched.
	Unmatched Action
	// Fallback is the decision when the asker chain is exhausted. It
	// defaults to DefaultFallback.
	Fallback Action
	// DoomLoop is the doom loop's settings. The zero value takes the
	// defaults.
	DoomLoop DoomLoop
	// Askers are the asker chain, in order.
	Askers []Asker
	// ChainTimeout is how long the whole chain may take. It defaults
	// to DefaultChainTimeout.
	ChainTimeout time.Duration
	// WorkingDir is the directory the run works in.
	//
	// The directory is part of the evidence a decision model is asked
	// about.
	WorkingDir string
}

// Policy is a permission policy.
//
// The policy decides about every prepared call: the doom loop is asked
// about first, then the rules, and a side-effecting call no rule
// matches takes the unmatched action. A read-only call no rule matches
// is allowed.
type Policy struct { // rules are the policy's rules, in order.
	rules []Rule
	// unmatched is the action for a side-effecting call no rule
	// matches.
	unmatched Action
	// doomLoop is the doom loop's settings.
	doomLoop DoomLoop
	// chain is the asker chain.
	chain *chain
	// workingDir is the directory the run works in.
	workingDir string
	// mu guards the ask counter.
	mu sync.Mutex
	// asks counts the asks the policy made, so every ask has its own
	// id.
	asks int
}

// The policy is the core's policy slot: the core installs it with
// poo.WithPolicy, and it only ever allows or denies.
var _ poo.PolicyHook = (*Policy)(nil)

// New builds a policy.
//
// A malformed policy is a construction error: a rule with no matcher or
// with two, an unknown action, a command prefix on a rule that is not a
// bash rule, or a fallback that is not a decision.
func New(opts Options) (*Policy, error) {
	unmatched := opts.Unmatched
	if unmatched == "" {
		unmatched = DefaultUnmatched
	}
	if err := checkAction("the unmatched action", unmatched, ActionAllow, ActionAsk, ActionDeny); err != nil {
		return nil, err
	}
	fallback := opts.Fallback
	if fallback == "" {
		fallback = DefaultFallback
	}
	if err := checkAction("the fallback", fallback, ActionAllow, ActionDeny); err != nil {
		return nil, err
	}
	doomLoop, err := checkDoomLoop(opts.DoomLoop)
	if err != nil {
		return nil, err
	}
	for i, rule := range opts.Rules {
		if err := checkRule(i, rule); err != nil {
			return nil, err
		}
	}
	timeout := opts.ChainTimeout
	if timeout <= 0 {
		timeout = DefaultChainTimeout
	}
	return &Policy{
		rules:      append([]Rule(nil), opts.Rules...),
		unmatched:  unmatched,
		doomLoop:   doomLoop,
		workingDir: opts.WorkingDir,
		chain: &chain{
			askers:   append([]Asker(nil), opts.Askers...),
			timeout:  timeout,
			fallback: fallback,
			clock:    systemClock{},
		},
	}, nil
}

// checkRule reports whether a rule is well formed.
func checkRule(index int, rule Rule) error {
	where := fmt.Sprintf("rule %d", index)
	switch {
	case rule.Tool == "" && rule.Class == "":
		return fmt.Errorf("perm: %s matches neither a tool nor a class", where)
	case rule.Tool != "" && rule.Class != "":
		return fmt.Errorf("perm: %s matches both a tool and a class", where)
	}
	if rule.Class != "" {
		switch rule.Class {
		case poo.ClassReadOnly, poo.ClassSideEffecting:
		default:
			return fmt.Errorf("perm: %s matches the unknown class %q", where, rule.Class)
		}
	}
	if rule.CommandPrefix != "" && rule.Tool != "bash" {
		return fmt.Errorf("perm: %s has a command prefix, which is only valid for a bash rule", where)
	}
	return checkAction(where+"'s action", rule.Action, ActionAllow, ActionAsk, ActionDeny)
}

// checkAction reports whether an action is one of the allowed ones.
func checkAction(where string, action Action, allowed ...Action) error {
	for _, a := range allowed {
		if action == a {
			return nil
		}
	}
	return fmt.Errorf("perm: %s is %q, want one of %v", where, action, allowed)
}

// checkDoomLoop returns the doom loop's settings with the defaults
// filled in.
func checkDoomLoop(loop DoomLoop) (DoomLoop, error) {
	if loop.Threshold == 0 {
		loop.Threshold = DefaultDoomLoopThreshold
	}
	if loop.NearThreshold == 0 {
		loop.NearThreshold = DefaultDoomLoopNearThreshold
	}
	if loop.Action == "" {
		loop.Action = DefaultDoomLoopAction
	}
	if loop.Threshold < 1 {
		return loop, fmt.Errorf("perm: the doom loop's threshold is %d, want a positive count", loop.Threshold)
	}
	if loop.NearThreshold < 1 {
		return loop, fmt.Errorf("perm: the doom loop's near threshold is %d, want a positive count", loop.NearThreshold)
	}
	if err := checkAction("the doom loop's action", loop.Action, ActionAllow, ActionAsk, ActionDeny); err != nil {
		return loop, err
	}
	return loop, nil
}

// BeforeTool decides what a call may do.
//
// The doom loop is asked about first: a call the model keeps making is
// a permission case, and the loop's action applies before the rules.
// Then the rules are tried in order, and the first match wins. A
// side-effecting call no rule matches takes the unmatched action, and a
// read-only call no rule matches is allowed.
//
// An ask blocks here: the policy waits for the chain, and the chain's
// deadline is the policy's own.
func (p *Policy) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	action, reason, counts := p.decide(info)
	switch action {
	case ActionAllow:
		if reason == poo.ReasonDoomLoop {
			// The loop is forgiven, so it starts over, and the outcome
			// is visible even though no asker answered.
			p.answer(ctx, decisionAllow, reason)
			p.reset(ctx, info)
		}
		return poo.Allow(), nil
	case ActionDeny:
		if reason == poo.ReasonDoomLoop {
			p.answer(ctx, decisionDeny, reason)
			return poo.Deny(fmt.Sprintf(DoomLoopDeniedText, doomLoopCount(counts))), nil
		}
		return poo.Deny(DeniedText), nil
	}
	return p.ask(ctx, info, reason, counts)
}

// The decisions a deterministic action reports.
const (
	decisionAllow = "allow"
	decisionDeny  = "deny"
)

// decide returns what the policy does with a call.
//
// A call that failed its validation is not a call the rules can judge:
// only the doom loop applies, because the same malformed call repeated
// is the same loop. The core gives it the preparation failure as its
// result.
func (p *Policy) decide(info poo.ToolCallInfo) (Action, string, *poo.RepeatCounts) {
	if info.PrepareFailed {
		if counts, ok := p.doomLoopCounts(info); ok {
			return p.doomLoop.Action, poo.ReasonDoomLoop, counts
		}
		return ActionAllow, poo.ReasonPolicy, nil
	}
	if counts, ok := p.doomLoopCounts(info); ok {
		return p.doomLoop.Action, poo.ReasonDoomLoop, counts
	}
	if rule, ok := p.match(info); ok {
		return rule.Action, poo.ReasonPolicy, nil
	}
	if info.Spec.Class == poo.ClassReadOnly {
		return ActionAllow, poo.ReasonPolicy, nil
	}
	return p.unmatched, poo.ReasonPolicy, nil
}

// doomLoopCounts returns the counts that trigger the doom loop.
//
// The exact count triggers the loop at the exact threshold, and the near
// count at the near threshold.
func (p *Policy) doomLoopCounts(info poo.ToolCallInfo) (*poo.RepeatCounts, bool) {
	exact := info.RepeatCount >= p.doomLoop.Threshold
	near := info.NearRepeatCount >= p.doomLoop.NearThreshold
	if !exact && !near {
		return nil, false
	}
	return &poo.RepeatCounts{Exact: info.RepeatCount, Near: info.NearRepeatCount}, true
}

// doomLoopCount returns the number of repeats that triggered the loop.
//
// The count is the larger of the two: the call itself, or the shape it
// repeats, was made that many times.
func doomLoopCount(counts *poo.RepeatCounts) int {
	if counts == nil {
		return 0
	}
	if counts.Exact >= counts.Near {
		return counts.Exact
	}
	return counts.Near
}

// match returns the first rule that matches the call.
//
// The rules are tried in order, and the first match wins. A rule with a
// command prefix matches only a call whose command starts with the
// prefix.
func (p *Policy) match(info poo.ToolCallInfo) (Rule, bool) {
	for _, rule := range p.rules {
		if !rule.matchesTool(info) {
			continue
		}
		if rule.CommandPrefix != "" && !rule.matchesCommand(info) {
			continue
		}
		return rule, true
	}
	return Rule{}, false
}

// matchesTool reports whether the rule matches the call's tool.
func (r Rule) matchesTool(info poo.ToolCallInfo) bool {
	if r.Tool != "" {
		return r.Tool == info.Spec.Name
	}
	return r.Class != "" && r.Class == info.Spec.Class
}

// matchesCommand reports whether the call's command starts with the
// rule's prefix.
//
// The comparison is lexical: the command's leading whitespace is
// trimmed and the rest is compared as text. A compound command matches
// its first part, so the prefix is a heuristic and not a safety
// boundary.
func (r Rule) matchesCommand(info poo.ToolCallInfo) bool {
	name := info.Spec.PrimaryArg
	if name == "" {
		// A bash rule matches the bash tool's command whatever the
		// tool's own spec says.
		name = "command"
	}
	value, ok := stringField(info.Arguments, name)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimLeftFunc(value, unicode.IsSpace), r.CommandPrefix)
}

// stringField returns the raw value of a top-level string argument.
func stringField(args json.RawMessage, name string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return "", false
	}
	raw, ok := fields[name]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// reset clears the repeat counters of a call.
//
// The counters are the doom loop's, and a loop the policy allowed starts
// over: the next call of the same shape is counted from one.
func (p *Policy) reset(ctx poo.HookContext, info poo.ToolCallInfo) {
	if ctx.Permissions == nil {
		return
	}
	ctx.Permissions.ResetRepeats(info.Handle)
}

// answer reports a doom loop's deterministic outcome.
//
// The plan makes the outcome visible either way: an allow and a deny
// through the action are recorded as a permission answer, with the
// policy as who answered and the loop as the reason.
func (p *Policy) answer(ctx poo.HookContext, decision, reason string) {
	if ctx.Permissions == nil {
		return
	}
	ctx.Permissions.Answered(poo.PermissionAnswer{
		Decision:   decision,
		AnsweredBy: policyName,
		Provenance: poo.ProvenanceAnswered,
		Reason:     reason,
	})
}

// policyName names the policy in a deterministic answer.
const policyName = "policy"

// ask asks the chain about a call.
//
// The ask is reported before the chain is asked, so a client knows what
// the run is waiting on, and the answer is reported when the ask is
// resolved. The ask resolves once: an answer that arrives after the
// resolution is dropped.
func (p *Policy) ask(ctx poo.HookContext, info poo.ToolCallInfo, reason string, counts *poo.RepeatCounts) (poo.ToolDecision, error) {
	ask := Ask{
		AskID:       p.nextAskID(),
		SessionID:   ctx.SessionID,
		RunID:       ctx.RunID,
		Tool:        info.Spec.Name,
		Summary:     summaryOf(info),
		Class:       info.Spec.Class,
		Reason:      reason,
		RepeatCount: counts,
		Arguments:   argumentsOf(info),
		HistoryView: ctx.HistoryView,
		WorkingDir:  p.workingDir,
		Interrupted: ctx.Interrupted,
	}
	resolution := p.chain.askReported(ctx, ask, ctx.Permissions)
	if ctx.Permissions != nil {
		ctx.Permissions.Answered(poo.PermissionAnswer{
			AskID:      ask.AskID,
			Decision:   string(resolution.decision),
			AnsweredBy: resolution.answeredBy,
			Provenance: resolution.provenance,
			Reason:     ask.Reason,
		})
	}
	if resolution.decision == DecisionAllow {
		return poo.Allow(), nil
	}
	// A refused ask about a loop is the loop's refusal, whatever refused
	// it: the model is told what it repeated, not who said no.
	if reason == poo.ReasonDoomLoop {
		return poo.Deny(fmt.Sprintf(DoomLoopDeniedText, doomLoopCount(counts))), nil
	}
	return poo.Deny(fmt.Sprintf(AskDeniedText, resolution.answeredBy)), nil
}

// argumentsOf returns the arguments a decision model may see.
//
// A tool that names its evidence arguments chooses what is exposed; any
// other tool exposes the raw arguments the model wrote.
func argumentsOf(info poo.ToolCallInfo) json.RawMessage {
	if evidence, ok := info.Prepared.(poo.EvidenceArgumenter); ok {
		if args := evidence.EvidenceArguments(); json.Valid(args) {
			return args
		}
	}
	return validJSON(info.Arguments)
}

// validJSON returns the arguments as valid JSON.
//
// A model that sent malformed arguments is still shown faithfully: the
// raw text becomes a JSON string rather than breaking the evidence.
func validJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	if json.Valid(raw) {
		return append(json.RawMessage(nil), raw...)
	}
	quoted, err := json.Marshal(string(raw))
	if err != nil {
		return json.RawMessage("{}")
	}
	return quoted
}

// nextAskID returns the id of the next ask.
//
// The ids are unique within the policy, and a policy belongs to one
// session.
func (p *Policy) nextAskID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asks++
	return fmt.Sprintf("a%d", p.asks)
}

// summaryOf returns the call's summary.
//
// A call with no prepared value has no summary: the core never asks
// the policy about such a call, but the policy does not assume it.
func summaryOf(info poo.ToolCallInfo) string {
	if info.Prepared == nil {
		return ""
	}
	return info.Prepared.Summary()
}

// Askers returns the policy's asker chain.
//
// The chain is the one the policy asks, in the order it asks it.
func (p *Policy) Askers() []Asker {
	return append([]Asker(nil), p.chain.askers...)
}

// canAsk reports whether the policy can produce an ask.
//
// The unmatched action, a rule that asks and the doom loop's action all
// ask.
func (p *Policy) canAsk() bool {
	if p.unmatched == ActionAsk {
		return true
	}
	if p.doomLoop.Action == ActionAsk {
		return true
	}
	for _, rule := range p.rules {
		if rule.Action == ActionAsk {
			return true
		}
	}
	return false
}

// Warning returns the warning a config load prints, or an empty string
// when there is nothing to warn about.
//
// The warning fires when the policy can produce asks and no asker in the
// chain can ever answer one: every ask then falls back to the policy's
// fallback decision, and the warning names it.
func Warning(policy *Policy, askers []Asker) string {
	if policy == nil || !policy.canAsk() {
		return ""
	}
	for _, asker := range askers {
		if canEverAnswer(asker) {
			return ""
		}
	}
	return fmt.Sprintf("the permission policy can ask, but no asker in the chain can ever answer; every ask falls back to %q",
		policy.chain.fallback)
}

// canEverAnswer reports whether the asker may answer an ask at some
// point in this build.
//
// An asker that does not say whether it can ever answer is taken at its
// word: it may answer later, so the chain is not dead.
func canEverAnswer(asker Asker) bool {
	if asker == nil {
		return false
	}
	available, err := guard(func() (bool, error) {
		if a, ok := asker.(Availability); ok {
			return a.CanEverAnswer(), nil
		}
		return true, nil
	})
	return err == nil && available
}

// askerName returns the name an asker's answers carry.
//
// The name is the asker's own when it has one, and its type name
// otherwise.
func askerName(asker Asker) string {
	if named, ok := asker.(interface{ AskerName() string }); ok {
		if name := named.AskerName(); name != "" {
			return name
		}
	}
	t := reflect.TypeOf(asker)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t != nil && t.Name() != "" {
		return t.Name()
	}
	return "asker"
}
