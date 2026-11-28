package hook

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sdougbrown/poo/decide"
)

// Error is a hook spec the check refused.
//
// The failure names the hook and the key the operator wrote, so a
// broken policy is one line of a file and not a search through it.
type Error struct {
	// Hook is the hook's name, empty when the failure is the name.
	Hook string
	// Path is the key path inside the hook's spec, when there is one.
	Path string
	// Message says what the spec got wrong.
	Message string
}

// Error renders the failure.
func (e *Error) Error() string {
	var out strings.Builder
	if e.Hook != "" {
		fmt.Fprintf(&out, "hook %q: ", e.Hook)
	}
	if e.Path != "" {
		out.WriteString(e.Path)
		out.WriteString(": ")
	}
	out.WriteString(e.Message)
	return out.String()
}

// matchersByPoint is the match fields each point accepts.
var matchersByPoint = map[Point][]string{
	PointBeforeRun:    {"trigger"},
	PointBeforeTool:   {"tool", "class", "command_prefix", "command_contains"},
	PointAfterTool:    {"tool", "class", "command_prefix", "command_contains", "is_error"},
	PointAfterTurn:    {"min_turn", "interrupted"},
	PointBeforeSettle: {"reason", "min_turn"},
}

// actionsByPoint is the terminal decisions each point allows.
var actionsByPoint = map[Point][]ActionKind{
	PointBeforeRun:    {ActionPass, ActionAppend},
	PointBeforeTool:   {ActionAllow, ActionDeny, ActionShortCircuit},
	PointAfterTool:    {ActionKeep, ActionAnnotate, ActionReplace, ActionRedact},
	PointAfterTurn:    {ActionPass, ActionAppend, ActionStop},
	PointBeforeSettle: {ActionAccept, ActionContinue, ActionStop},
}

// actionMessages names the decisions that carry text.
var actionMessages = map[ActionKind]bool{
	ActionAppend:       true,
	ActionDeny:         true,
	ActionShortCircuit: true,
	ActionAnnotate:     true,
	ActionReplace:      true,
	ActionStop:         true,
	ActionContinue:     true,
}

// Validate checks a hook spec and compiles it.
//
// The check is deterministic: the same spec is accepted or refused the
// same way, and every step and branch is read in name order so the
// failure names the first one an operator would see in the file.
//
// The returned Compiled is a copy, so the caller's Spec and the check's
// result do not share anything.
func Validate(name string, spec Spec) (*Compiled, error) {
	if err := checkPolicy(name, spec); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, &Error{Message: "a hook needs a name"}
	}
	if err := checkShape(name, spec); err != nil {
		return nil, err
	}
	if err := checkMatchers(name, spec); err != nil {
		return nil, err
	}
	if err := checkContext(name, spec); err != nil {
		return nil, err
	}
	steps, err := compileSteps(name, spec)
	if err != nil {
		return nil, err
	}
	if err := checkGraph(name, spec); err != nil {
		return nil, err
	}
	return &Compiled{
		Name:              name,
		Point:             spec.Point,
		Match:             copyMatch(spec.Match),
		Context:           copyContext(spec.Context),
		Entry:             spec.Entry,
		Steps:             steps,
		MaxFiresPerRun:    spec.MaxFiresPerRun,
		FiringTimeoutMs:   firingTimeout(spec),
		OnError:           policyOrOpen(spec.OnError),
		OnUncertain:       policyOrOpen(spec.OnUncertain),
		FailClosedMessage: spec.FailClosedMessage,
		Idempotent:        commandProbes(spec) == 0,
	}, nil
}

// policyFields pairs a policy key with the field the generated check
// reports for it and whether the spec set it at all.
//
// A key the spec leaves out takes its default, and a default cannot be
// wrong for the point it is taken at.
type policyField struct {
	path    string
	status  FieldStatus
	present bool
}

// checkPolicy runs the generated policy check.
//
// A key the point forbids is refused the way the settings refuse one:
// a field that is not available here is an unavailability, and a value
// the point does not allow is a bad value. The generated check's own
// account of the field is the failure's explanation.
func checkPolicy(name string, spec Spec) error {
	availability := Check(hookFields(spec), HookConditions{}, HookFields{})
	fields := []policyField{
		{"point", availability.Point, spec.Point != ""},
		{"on_error", availability.OnError, spec.OnError != ""},
		{"on_uncertain", availability.OnUncertain, spec.OnUncertain != ""},
		{"fail_closed_message", availability.FailClosedMessage, spec.FailClosedMessage != ""},
		{"firing_timeout_ms", availability.FiringTimeoutMs, spec.FiringTimeoutMs != 0},
		{"max_fires_per_run", availability.MaxFiresPerRun, spec.MaxFiresPerRun != 0},
	}
	for _, field := range fields {
		if !field.present {
			continue
		}
		switch {
		case !field.status.Enabled:
			return &Error{Hook: name, Path: field.path, Message: reasonOf(field.status, "the hook does not allow this field here")}
		case !field.status.Fair:
			return &Error{Hook: name, Path: field.path, Message: reasonOf(field.status, "the hook does not allow this value")}
		}
	}
	return nil
}

// hookFields are the policy keys the spec set.
func hookFields(spec Spec) HookFields {
	var fields HookFields
	if spec.Point != "" {
		point := string(spec.Point)
		fields.Point = &point
	}
	if spec.OnError != "" {
		policy := string(spec.OnError)
		fields.OnError = &policy
	}
	if spec.OnUncertain != "" {
		policy := string(spec.OnUncertain)
		fields.OnUncertain = &policy
	}
	if spec.FailClosedMessage != "" {
		message := spec.FailClosedMessage
		fields.FailClosedMessage = &message
	}
	if spec.FiringTimeoutMs != 0 {
		timeout := float64(spec.FiringTimeoutMs)
		fields.FiringTimeoutMs = &timeout
	}
	if spec.MaxFiresPerRun != 0 {
		fires := float64(spec.MaxFiresPerRun)
		fields.MaxFiresPerRun = &fires
	}
	return fields
}

// reasonOf is the field's own account of itself.
func reasonOf(status FieldStatus, fallback string) string {
	if status.Reason != nil && *status.Reason != "" {
		return *status.Reason
	}
	return fallback
}

// checkShape checks the keys every point shares.
func checkShape(name string, spec Spec) error {
	if len(spec.Steps) == 0 {
		return &Error{Hook: name, Path: "steps", Message: "a hook needs at least one step"}
	}
	if len(spec.Steps) > MaxSteps {
		return &Error{Hook: name, Path: "steps", Message: fmt.Sprintf("a hook has %d steps, the most is %d", len(spec.Steps), MaxSteps)}
	}
	if _, ok := spec.Steps[spec.Entry]; !ok {
		return &Error{Hook: name, Path: "entry", Message: fmt.Sprintf("%q is not a step", spec.Entry)}
	}
	return nil
}

// checkMatchers checks the matcher against the point and its values
// against the words the points know.
func checkMatchers(name string, spec Spec) error {
	allowed := matchersByPoint[spec.Point]
	match := spec.Match
	keys := []struct {
		key string
		set bool
	}{
		{"trigger", match.Trigger != ""},
		{"tool", match.Tool != ""},
		{"class", match.Class != ""},
		{"command_prefix", match.CommandPrefix != ""},
		{"command_contains", len(match.CommandContains) > 0},
		{"is_error", match.IsError != nil},
		{"min_turn", match.MinTurn != nil},
		{"interrupted", match.Interrupted != nil},
		{"reason", match.Reason != ""},
	}
	for _, key := range keys {
		if key.set && !slices.Contains(allowed, key.key) {
			return &Error{
				Hook:    name,
				Path:    "match." + key.key,
				Message: fmt.Sprintf("%s has no %s to match: it matches on %s", spec.Point, key.key, strings.Join(allowed, ", ")),
			}
		}
	}
	if err := checkMatcherValues(name, spec); err != nil {
		return err
	}
	switch {
	case match.Tool == "" && match.Class == "":
	case match.Tool != "" && match.Class != "":
		return &Error{Hook: name, Path: "match", Message: "the matcher names a tool or a class, never both"}
	}
	switch {
	case match.CommandPrefix != "" && len(match.CommandContains) > 0:
		return &Error{Hook: name, Path: "match", Message: "the matcher carries a command prefix or command substrings, never both"}
	case match.CommandPrefix != "" && match.Tool == "":
		return &Error{Hook: name, Path: "match.command_prefix", Message: "a command prefix is only valid on a matcher that names a tool"}
	case len(match.CommandContains) > 0 && match.Tool == "":
		return &Error{Hook: name, Path: "match.command_contains", Message: "a command substring is only valid on a matcher that names a tool"}
	}
	for _, substring := range match.CommandContains {
		if substring == "" {
			return &Error{Hook: name, Path: "match.command_contains", Message: "a command substring is empty"}
		}
	}
	return nil
}

// checkMatcherValues checks the words a matcher is spelled with.
func checkMatcherValues(name string, spec Spec) error {
	match := spec.Match
	words := []struct {
		key     string
		value   string
		allowed []string
	}{
		{"match.trigger", match.Trigger, []string{TriggerPrompt, TriggerJob, TriggerFeed}},
		{"match.class", match.Class, []string{ClassReadOnly, ClassSideEffecting}},
		{"match.reason", match.Reason, []string{ReasonEndTurn, ReasonOutputTruncated}},
	}
	for _, word := range words {
		if word.value == "" || slices.Contains(word.allowed, word.value) {
			continue
		}
		return &Error{
			Hook:    name,
			Path:    word.key,
			Message: fmt.Sprintf("%q is not one of %s", word.value, strings.Join(word.allowed, ", ")),
		}
	}
	if match.MinTurn != nil && *match.MinTurn < 1 {
		return &Error{Hook: name, Path: "match.min_turn", Message: "the turns a run counts start at one"}
	}
	return nil
}

// checkContext checks the evidence a hook gathers.
func checkContext(name string, spec Spec) error {
	if spec.Context.Turns < 0 {
		return &Error{Hook: name, Path: "context.turns", Message: "the number of turns must not be negative"}
	}
	probes := spec.Context.Probes
	if len(probes) > MaxProbes {
		return &Error{Hook: name, Path: "context.probes", Message: fmt.Sprintf("a hook runs %d probes, the most is %d", len(probes), MaxProbes)}
	}
	seen := make(map[string]bool, len(probes))
	for i, probe := range probes {
		path := fmt.Sprintf("context.probes[%d]", i)
		if probe.Name == "" {
			return &Error{Hook: name, Path: path, Message: "a probe needs a name"}
		}
		if seen[probe.Name] {
			return &Error{Hook: name, Path: path, Message: fmt.Sprintf("the probe %q is repeated", probe.Name)}
		}
		seen[probe.Name] = true
		switch {
		case len(probe.Command) > 0 && probe.Tool != nil:
			return &Error{Hook: name, Path: path, Message: "a probe runs a command or a tool, never both"}
		case len(probe.Command) == 0 && probe.Tool == nil:
			return &Error{Hook: name, Path: path, Message: "a probe needs a command or a tool"}
		}
		if err := checkProbe(name, path, probe); err != nil {
			return err
		}
	}
	return nil
}

// checkProbe checks one probe's tool and timeout.
func checkProbe(name, path string, probe Probe) error {
	if probe.TimeoutMs < 0 {
		return &Error{Hook: name, Path: path + ".timeout_ms", Message: fmt.Sprintf("the probe %q has a negative timeout", probe.Name)}
	}
	if probe.Tool == nil {
		return nil
	}
	if probe.Tool.Name == "" {
		return &Error{Hook: name, Path: path + ".tool.name", Message: "a tool probe needs a tool name"}
	}
	if len(probe.Tool.Args) == 0 {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(probe.Tool.Args, &object); err != nil {
		return &Error{Hook: name, Path: path + ".tool.args", Message: "the arguments are not a JSON object"}
	}
	return nil
}

// compileSteps compiles every step's question and checks its branches
// against that question and its terminal decisions against the point.
func compileSteps(name string, spec Spec) (map[string]CompiledStep, error) {
	steps := make(map[string]CompiledStep, len(spec.Steps))
	for _, stepName := range slices.Sorted(maps.Keys(spec.Steps)) {
		step := spec.Steps[stepName]
		path := "steps." + stepName
		question, err := compileQuestion(name, stepName, step.Question)
		if err != nil {
			return nil, &Error{Hook: name, Path: path + ".question", Message: err.Error()}
		}
		if err := question.Validate(); err != nil {
			return nil, &Error{Hook: name, Path: path + ".question", Message: err.Error()}
		}
		if step.TimeoutMs < 0 {
			return nil, &Error{Hook: name, Path: path + ".timeout_ms", Message: "a step's timeout must not be negative"}
		}
		branches, err := compileBranches(name, spec, stepName, question)
		if err != nil {
			return nil, err
		}
		steps[stepName] = CompiledStep{
			Name:      stepName,
			TimeoutMs: step.TimeoutMs,
			Question:  question,
			Branches:  branches,
		}
	}
	return steps, nil
}

// compileQuestion builds the question a step asks.
func compileQuestion(name, step string, spec QuestionSpec) (decide.Question, error) {
	question := decide.Question{
		ID:           "hook:" + name + ":" + step,
		Instructions: spec.Instructions,
	}
	switch spec.Kind {
	case QuestionBool:
		if len(spec.Options) > 0 || len(spec.Levels) > 0 {
			return decide.Question{}, fmt.Errorf("a bool question carries a true and a false description, no options and no levels")
		}
		question.Kind = decide.Bool{True: spec.True, False: spec.False}
	case QuestionChoice:
		if spec.True != "" || spec.False != "" || len(spec.Levels) > 0 {
			return decide.Question{}, fmt.Errorf("a choice question carries options, no true and false descriptions and no levels")
		}
		question.Kind = decide.Choice{Options: slices.Clone(spec.Options)}
	case QuestionScore:
		if spec.True != "" || spec.False != "" || len(spec.Options) > 0 {
			return decide.Question{}, fmt.Errorf("a score question carries levels, no true and false descriptions and no options")
		}
		question.Kind = decide.Score{Levels: slices.Clone(spec.Levels)}
	default:
		return decide.Question{}, fmt.Errorf("%q is not one of %s", spec.Kind, strings.Join([]string{QuestionBool, QuestionChoice, QuestionScore}, ", "))
	}
	return question, nil
}

// compileBranches checks a step's branches against its question.
func compileBranches(name string, spec Spec, stepName string, question decide.Question) (map[string]CompiledBranch, error) {
	step := spec.Steps[stepName]
	path := "steps." + stepName + ".branches"
	if len(step.Branches) == 0 {
		return nil, &Error{Hook: name, Path: path, Message: "a step needs a branch for each answer it asks about"}
	}
	keys := question.Keys()
	branches := make(map[string]CompiledBranch, len(step.Branches))
	for _, key := range slices.Sorted(maps.Keys(step.Branches)) {
		branch := step.Branches[key]
		where := path + "." + key
		if !slices.Contains(keys, key) {
			return nil, &Error{Hook: name, Path: where, Message: fmt.Sprintf("the question has no answer %q: its answers are %s", key, strings.Join(keys, ", "))}
		}
		if !(branch.MinProbability > 0 && branch.MinProbability <= 1) {
			return nil, &Error{Hook: name, Path: where + ".min_probability", Message: "the probability a branch needs is above zero and at most one"}
		}
		switch {
		case branch.Next != "" && branch.Action != nil:
			return nil, &Error{Hook: name, Path: where, Message: "a branch takes a next step or an action, never both"}
		case branch.Next == "" && branch.Action == nil:
			return nil, &Error{Hook: name, Path: where, Message: "a branch needs a next step or an action"}
		}
		if _, ok := spec.Steps[branch.Next]; branch.Next != "" && !ok {
			return nil, &Error{Hook: name, Path: where + ".next", Message: fmt.Sprintf("%q is not a step", branch.Next)}
		}
		compiled := CompiledBranch{MinProbability: branch.MinProbability, Next: branch.Next}
		if branch.Action != nil {
			if err := checkAction(name, spec.Point, where+".action", *branch.Action); err != nil {
				return nil, err
			}
			action := *branch.Action
			compiled.Action = &action
		}
		branches[key] = compiled
	}
	return branches, nil
}

// checkAction checks a terminal decision against the point.
func checkAction(name string, point Point, path string, action Action) error {
	if action.Kind == "" {
		return &Error{Hook: name, Path: path + ".kind", Message: "an action needs a kind"}
	}
	allowed := actionsByPoint[point]
	if !slices.Contains(allowed, action.Kind) {
		names := make([]string, len(allowed))
		for i, kind := range allowed {
			names[i] = string(kind)
		}
		return &Error{
			Hook:    name,
			Path:    path + ".kind",
			Message: fmt.Sprintf("%s takes %s, not %q", point, strings.Join(names, ", "), action.Kind),
		}
	}
	switch {
	case actionMessages[action.Kind] && action.Message == "":
		return &Error{Hook: name, Path: path + ".message", Message: fmt.Sprintf("a %s action needs a message", action.Kind)}
	case !actionMessages[action.Kind] && action.Message != "":
		return &Error{Hook: name, Path: path + ".message", Message: fmt.Sprintf("a %s action shows no message", action.Kind)}
	}
	return nil
}

// checkGraph walks the graph from the entry.
//
// Every step is reached from the entry, no path loops back to a step it
// already took, and the longest path is at most MaxSteps.
func checkGraph(name string, spec Spec) error {
	const (
		unvisited = iota
		onPath
		done
	)
	state := make(map[string]int, len(spec.Steps))
	depth := make(map[string]int, len(spec.Steps))
	var walk func(step string) (int, error)
	walk = func(step string) (int, error) {
		switch state[step] {
		case onPath:
			return 0, &Error{Hook: name, Path: "steps." + step, Message: "a path loops back to this step"}
		case done:
			return depth[step], nil
		}
		state[step] = onPath
		longest := 0
		branches := spec.Steps[step].Branches
		for _, key := range slices.Sorted(maps.Keys(branches)) {
			next := branches[key].Next
			if next == "" {
				continue
			}
			below, err := walk(next)
			if err != nil {
				return 0, err
			}
			longest = max(longest, below)
		}
		state[step] = done
		depth[step] = longest + 1
		return depth[step], nil
	}
	entryDepth, err := walk(spec.Entry)
	if err != nil {
		return err
	}
	if entryDepth > MaxSteps {
		return &Error{Hook: name, Path: "entry", Message: fmt.Sprintf("a path takes %d steps, the most is %d", entryDepth, MaxSteps)}
	}
	for _, step := range slices.Sorted(maps.Keys(spec.Steps)) {
		if state[step] == unvisited {
			return &Error{Hook: name, Path: "steps." + step, Message: fmt.Sprintf("no branch reaches this step from %q", spec.Entry)}
		}
	}
	return nil
}

// firingTimeout is the hook's timeout with the default taken.
func firingTimeout(spec Spec) int {
	if spec.FiringTimeoutMs > 0 {
		return spec.FiringTimeoutMs
	}
	return DefaultFiringTimeoutMs
}

// policyOrOpen is the policy with its default taken.
func policyOrOpen(policy FailurePolicy) FailurePolicy {
	if policy == "" {
		return FailOpen
	}
	return policy
}

// commandProbes counts the probes that run a subprocess.
func commandProbes(spec Spec) int {
	count := 0
	for _, probe := range spec.Context.Probes {
		if len(probe.Command) > 0 {
			count++
		}
	}
	return count
}

// copyMatch copies the matcher, so a change to the spec does not reach
// the compiled policy.
func copyMatch(match Match) Match {
	match.CommandContains = slices.Clone(match.CommandContains)
	if match.IsError != nil {
		value := *match.IsError
		match.IsError = &value
	}
	if match.MinTurn != nil {
		value := *match.MinTurn
		match.MinTurn = &value
	}
	if match.Interrupted != nil {
		value := *match.Interrupted
		match.Interrupted = &value
	}
	return match
}

// copyContext copies the evidence, so a change to the spec does not
// reach the compiled policy.
func copyContext(context ContextSpec) ContextSpec {
	if context.Probes == nil {
		return context
	}
	probes := make([]Probe, len(context.Probes))
	for i, probe := range context.Probes {
		probes[i] = probe
		probes[i].Command = slices.Clone(probe.Command)
		if probe.Tool != nil {
			tool := *probe.Tool
			tool.Args = slices.Clone(probe.Tool.Args)
			probes[i].Tool = &tool
		}
	}
	context.Probes = probes
	return context
}
