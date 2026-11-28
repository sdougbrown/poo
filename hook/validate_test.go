package hook_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/hook"
)

// wireFixture is the hook spec the wire contract fixes, in the JSON the
// settings are normalized to. A key this package spells differently
// makes decoding fail, because the decoder refuses what it does not
// know.
const wireFixture = `{
  "point": "before_tool",
  "match": {
    "tool": "bash",
    "command_contains": ["git push", "git commit"]
  },
  "max_fires_per_run": 3,
  "firing_timeout_ms": 8000,
  "on_uncertain": "fail_open",
  "on_error": "fail_open",
  "context": {
    "turns": 8,
    "include_tool_call": true,
    "include_working_dir": true,
    "probes": [
      {
        "name": "unformatted_files",
        "command": ["gofmt", "-l", "."],
        "timeout_ms": 3000
      },
      {
        "name": "go_mod",
        "tool": {
          "name": "read",
          "args": { "path": "go.mod", "limit": 30 }
        }
      }
    ]
  },
  "entry": "is_pushing_go_code",
  "steps": {
    "is_pushing_go_code": {
      "timeout_ms": 1500,
      "question": {
        "kind": "bool",
        "instructions": "Is the pending bash command committing or pushing Go changes while unformatted_files is non-empty?",
        "true": "committing or pushing while unformatted Go files exist",
        "false": "safe to proceed"
      },
      "branches": {
        "yes": { "min_probability": 0.75, "next": "classify_fix" },
        "no": { "min_probability": 0.5, "action": { "kind": "allow" } }
      }
    },
    "classify_fix": {
      "timeout_ms": 1500,
      "question": {
        "kind": "choice",
        "instructions": "Which instruction should be given to the agent?",
        "options": [
          { "name": "run_gofmt", "desc": "Tell the agent to run gofmt first" },
          { "name": "allow_anyway", "desc": "False positive; allow the command" }
        ]
      },
      "branches": {
        "run_gofmt": {
          "min_probability": 0.70,
          "action": {
            "kind": "deny",
            "message": "Unformatted Go files were detected. Run gofmt -w . before committing or pushing."
          }
        },
        "allow_anyway": { "min_probability": 0.5, "action": { "kind": "allow" } }
      }
    }
  }
}`

// decodeFixture decodes a hook spec strictly.
func decodeFixture(t *testing.T, source string) hook.Spec {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader([]byte(source)))
	decoder.DisallowUnknownFields()
	var spec hook.Spec
	if err := decoder.Decode(&spec); err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	return spec
}

// neutral is the decision that changes nothing at a point.
func neutral(point hook.Point) hook.ActionKind {
	switch point {
	case hook.PointBeforeTool:
		return hook.ActionAllow
	case hook.PointAfterTool:
		return hook.ActionKeep
	case hook.PointBeforeSettle:
		return hook.ActionAccept
	default:
		return hook.ActionPass
	}
}

// validSpec is one step that asks a bool question and takes the
// neutral decision on both answers.
func validSpec(point hook.Point) hook.Spec {
	action := hook.Action{Kind: neutral(point)}
	return hook.Spec{
		Point: point,
		Entry: "ask",
		Steps: map[string]hook.Step{
			"ask": {
				Question: hook.QuestionSpec{
					Kind:         hook.QuestionBool,
					Instructions: "Does the condition hold?",
					True:         "it holds",
					False:        "it does not",
				},
				Branches: map[string]hook.Branch{
					"yes": {MinProbability: 0.75, Action: &action},
					"no":  {MinProbability: 0.5, Action: &action},
				},
			},
		},
	}
}

// points is every point a hook may run at.
var points = []hook.Point{
	hook.PointBeforeRun,
	hook.PointBeforeTool,
	hook.PointAfterTool,
	hook.PointAfterTurn,
	hook.PointBeforeSettle,
}

// TestValidateAcceptsTheWireFixture proves the wire contract decodes
// into this package's own spec and compiles.
func TestValidateAcceptsTheWireFixture(t *testing.T) {
	compiled, err := hook.Validate("pre-push-gofmt", decodeFixture(t, wireFixture))
	if err != nil {
		t.Fatalf("the wire fixture was refused: %v", err)
	}
	if compiled.Name != "pre-push-gofmt" || compiled.Point != hook.PointBeforeTool || compiled.Entry != "is_pushing_go_code" {
		t.Fatalf("compiled hook is %q at %s from %q", compiled.Name, compiled.Point, compiled.Entry)
	}
	if compiled.MaxFiresPerRun != 3 || compiled.FiringTimeoutMs != 8000 {
		t.Fatalf("compiled limits are %d fires and %dms", compiled.MaxFiresPerRun, compiled.FiringTimeoutMs)
	}
	if compiled.OnError != hook.FailOpen || compiled.OnUncertain != hook.FailOpen {
		t.Fatalf("compiled policies are %s and %s", compiled.OnError, compiled.OnUncertain)
	}
	if len(compiled.Steps) != 2 {
		t.Fatalf("compiled %d steps, want 2", len(compiled.Steps))
	}
	entry, ok := compiled.Steps["is_pushing_go_code"]
	if !ok {
		t.Fatalf("the entry step is missing: %v", compiled.Steps)
	}
	if entry.Question.ID != "hook:pre-push-gofmt:is_pushing_go_code" {
		t.Fatalf("the question id is %q", entry.Question.ID)
	}
	if entry.Branches["yes"].Next != "classify_fix" {
		t.Fatalf("the yes branch goes to %q", entry.Branches["yes"].Next)
	}
	if got := entry.Branches["no"].Action; got == nil || got.Kind != hook.ActionAllow {
		t.Fatalf("the no branch takes %v", got)
	}
	fix := compiled.Steps["classify_fix"]
	if got := fix.Branches["run_gofmt"].Action; got == nil || got.Kind != hook.ActionDeny || !strings.Contains(got.Message, "gofmt -w .") {
		t.Fatalf("the run_gofmt branch takes %v", got)
	}
	if compiled.Idempotent {
		t.Fatalf("a hook with a command probe is not idempotent")
	}
	if len(compiled.Context.Probes) != 2 || compiled.Context.Turns != 8 {
		t.Fatalf("compiled context is %+v", compiled.Context)
	}
}

// TestValidateAcceptsEveryPoint takes each point's own decision.
func TestValidateAcceptsEveryPoint(t *testing.T) {
	for _, point := range points {
		for _, kind := range []hook.ActionKind{
			hook.ActionPass, hook.ActionAppend,
			hook.ActionAllow, hook.ActionDeny, hook.ActionShortCircuit,
			hook.ActionKeep, hook.ActionAnnotate, hook.ActionReplace, hook.ActionRedact,
			hook.ActionStop, hook.ActionAccept, hook.ActionContinue,
		} {
			spec := validSpec(point)
			action := hook.Action{Kind: kind}
			if needsMessage(kind) {
				action.Message = "the message"
			}
			spec.Steps["ask"].Branches["yes"] = hook.Branch{MinProbability: 0.75, Action: &action}
			compiled, err := hook.Validate("h", spec)
			if legalDecision(point, kind) {
				if err != nil {
					t.Errorf("%s accepts %s: %v", point, kind, err)
					continue
				}
				if compiled.Point != point {
					t.Errorf("compiled point is %s, want %s", compiled.Point, point)
				}
				continue
			}
			if err == nil {
				t.Errorf("%s accepted %s", point, kind)
			}
		}
	}
}

// needsMessage reports whether a decision carries text.
func needsMessage(kind hook.ActionKind) bool {
	switch kind {
	case hook.ActionAppend, hook.ActionDeny, hook.ActionShortCircuit,
		hook.ActionAnnotate, hook.ActionReplace, hook.ActionStop, hook.ActionContinue:
		return true
	default:
		return false
	}
}

// legalDecision reports whether a point allows a decision.
func legalDecision(point hook.Point, kind hook.ActionKind) bool {
	switch point {
	case hook.PointBeforeRun:
		return kind == hook.ActionPass || kind == hook.ActionAppend
	case hook.PointBeforeTool:
		return kind == hook.ActionAllow || kind == hook.ActionDeny || kind == hook.ActionShortCircuit
	case hook.PointAfterTool:
		return kind == hook.ActionKeep || kind == hook.ActionAnnotate || kind == hook.ActionReplace || kind == hook.ActionRedact
	case hook.PointAfterTurn:
		return kind == hook.ActionPass || kind == hook.ActionAppend || kind == hook.ActionStop
	case hook.PointBeforeSettle:
		return kind == hook.ActionAccept || kind == hook.ActionContinue || kind == hook.ActionStop
	}
	return false
}

// TestValidateRejectsAMissingPoint proves the one policy key without a
// default is refused where it belongs, and not at the branch that could
// not use it.
func TestValidateRejectsAMissingPoint(t *testing.T) {
	spec := validSpec(hook.PointBeforeTool)
	spec.Point = ""
	_, err := hook.Validate("h", spec)
	path, message := refused(t, err)
	if path != "point" || !strings.Contains(message, "before_run") {
		t.Fatalf("refused %q with %q, want the point key and its own words", path, message)
	}
}

// TestValidateAcceptsEveryMatcher proves each point's fields are the ones
// a spec may set, and that the compiled policy carries them.
func TestValidateAcceptsEveryMatcher(t *testing.T) {
	tests := []struct {
		name  string
		point hook.Point
		match hook.Match
	}{
		{"a person's run", hook.PointBeforeRun, hook.Match{Trigger: hook.TriggerPrompt}},
		{"a job's run", hook.PointBeforeRun, hook.Match{Trigger: hook.TriggerJob}},
		{"a feed's run", hook.PointBeforeRun, hook.Match{Trigger: hook.TriggerFeed}},
		{"a tool", hook.PointBeforeTool, hook.Match{Tool: "bash"}},
		{"a class", hook.PointBeforeTool, hook.Match{Class: hook.ClassReadOnly}},
		{"a command prefix", hook.PointBeforeTool, hook.Match{Tool: "bash", CommandPrefix: "git push"}},
		{"command substrings", hook.PointBeforeTool, hook.Match{Tool: "bash", CommandContains: []string{"git push", "git commit"}}},
		{"an error result", hook.PointAfterTool, hook.Match{IsError: boolOf(true)}},
		{"a result that was kept", hook.PointAfterTool, hook.Match{IsError: boolOf(false)}},
		{"a tool and its command", hook.PointAfterTool, hook.Match{Tool: "bash", CommandPrefix: "go test"}},
		{"the first turn", hook.PointAfterTurn, hook.Match{MinTurn: intOf(1)}},
		{"every turn", hook.PointAfterTurn, hook.Match{MinTurn: intOf(1), Interrupted: boolOf(true)}},
		{"an end turn", hook.PointBeforeSettle, hook.Match{Reason: hook.ReasonEndTurn}},
		{"a truncated settle", hook.PointBeforeSettle, hook.Match{Reason: hook.ReasonOutputTruncated}},
		{"a later settle", hook.PointBeforeSettle, hook.Match{MinTurn: intOf(3)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec(test.point)
			spec.Match = test.match
			compiled, err := hook.Validate("h", spec)
			if err != nil {
				t.Fatalf("the matcher was refused: %v", err)
			}
			if !reflect.DeepEqual(compiled.Match, test.match) {
				t.Fatalf("the compiled matcher is %+v, want %+v", compiled.Match, test.match)
			}
		})
	}
}

// TestValidateAcceptsAScoreQuestion proves the third question kind
// compiles, and that its answers are the level indices a decision model
// reports.
func TestValidateAcceptsAScoreQuestion(t *testing.T) {
	spec := validSpec(hook.PointBeforeTool)
	step := spec.Steps["ask"]
	step.Question = hook.QuestionSpec{
		Kind:         hook.QuestionScore,
		Instructions: "How close is the change to release?",
		Levels:       []string{"not close", "nearly", "ready"},
	}
	branches := make(map[string]hook.Branch, 3)
	for _, key := range []string{"0", "1", "2"} {
		branches[key] = hook.Branch{MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}}
	}
	step.Branches = branches
	spec.Steps["ask"] = step
	compiled, err := hook.Validate("h", spec)
	if err != nil {
		t.Fatalf("the score question was refused: %v", err)
	}
	if got := compiled.Steps["ask"].Question.Keys(); !reflect.DeepEqual(got, []string{"0", "1", "2"}) {
		t.Fatalf("the compiled question's answers are %v, want the level indices", got)
	}
}

// TestValidateRejectsAMessageTheDecisionCannotCarry proves each point's
// decisions carry their message exactly when they need one.
func TestValidateRejectsAMessageTheDecisionCannotCarry(t *testing.T) {
	for _, point := range points {
		for _, kind := range []hook.ActionKind{
			hook.ActionPass, hook.ActionAppend,
			hook.ActionAllow, hook.ActionDeny, hook.ActionShortCircuit,
			hook.ActionKeep, hook.ActionAnnotate, hook.ActionReplace, hook.ActionRedact,
			hook.ActionStop, hook.ActionAccept, hook.ActionContinue,
		} {
			if !legalDecision(point, kind) {
				continue
			}
			wrong := hook.Action{Kind: kind}
			if !needsMessage(kind) {
				wrong.Message = "the message"
			}
			spec := validSpec(point)
			spec.Steps["ask"] = hook.Step{
				Question: spec.Steps["ask"].Question,
				Branches: map[string]hook.Branch{
					"yes": {MinProbability: 0.75, Action: &wrong},
					"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: neutral(point)}},
				},
			}
			_, err := hook.Validate("h", spec)
			path, _ := refused(t, err)
			if want := "steps.ask.branches.yes.action.message"; path != want {
				t.Errorf("%s/%s: refused %q, want %q", point, kind, path, want)
			}
		}
	}
}

// TestValidateAcceptsFailRunEverywhere proves failing the run is allowed
// at every point and for both policies, and that a fill-in answer may
// still be refused with a message.
func TestValidateAcceptsFailRunEverywhere(t *testing.T) {
	for _, point := range points {
		for _, key := range []string{"on_error", "on_uncertain"} {
			spec := validSpec(point)
			if key == "on_error" {
				spec.OnError = hook.FailRun
			} else {
				spec.OnUncertain = hook.FailRun
			}
			if _, err := hook.Validate("h", spec); err != nil {
				t.Errorf("%s refused %s fail_run: %v", point, key, err)
			}
		}
	}
}

// TestValidateTakesDefaults proves an absent timeout and policy take the
// values a runner needs.
func TestValidateTakesDefaults(t *testing.T) {
	compiled, err := hook.Validate("h", validSpec(hook.PointBeforeTool))
	if err != nil {
		t.Fatalf("the spec was refused: %v", err)
	}
	if compiled.FiringTimeoutMs != hook.DefaultFiringTimeoutMs {
		t.Fatalf("the timeout is %d, want %d", compiled.FiringTimeoutMs, hook.DefaultFiringTimeoutMs)
	}
	if compiled.OnError != hook.FailOpen || compiled.OnUncertain != hook.FailOpen {
		t.Fatalf("the policies are %s and %s", compiled.OnError, compiled.OnUncertain)
	}
	if !compiled.Idempotent {
		t.Fatalf("a hook with no command probe is idempotent")
	}
	spec := validSpec(hook.PointBeforeTool)
	spec.Context.Probes = []hook.Probe{{Name: "read", Tool: &hook.ToolProbe{Name: "read"}}}
	compiled, err = hook.Validate("h", spec)
	if err != nil {
		t.Fatalf("a tool probe was refused: %v", err)
	}
	if !compiled.Idempotent {
		t.Fatalf("a hook whose probes are tool calls is idempotent")
	}
	if len(compiled.Context.Probes) != 1 {
		t.Fatalf("compiled %d probes", len(compiled.Context.Probes))
	}
}

// TestValidateRejectsPolicyForPoint proves a point with no denial refuses
// a fail_closed policy, and that fail_closed needs its message.
func TestValidateRejectsPolicyForPoint(t *testing.T) {
	for _, point := range []hook.Point{hook.PointBeforeRun, hook.PointAfterTool} {
		for _, key := range []string{"on_error", "on_uncertain"} {
			spec := validSpec(point)
			spec.FailClosedMessage = "the run stops here"
			if key == "on_error" {
				spec.OnError = hook.FailClosed
			} else {
				spec.OnUncertain = hook.FailClosed
			}
			_, err := hook.Validate("h", spec)
			path, message := refused(t, err)
			if path != key || !strings.Contains(message, "fail_closed") {
				t.Errorf("%s: refused %q with %q, want the key and its fail_closed reason", point, path, message)
			}
		}
	}
	for _, point := range []hook.Point{hook.PointBeforeTool, hook.PointAfterTurn, hook.PointBeforeSettle} {
		for _, key := range []string{"on_error", "on_uncertain"} {
			spec := validSpec(point)
			spec.FailClosedMessage = "the run stops here"
			if key == "on_error" {
				spec.OnError = hook.FailClosed
			} else {
				spec.OnUncertain = hook.FailClosed
			}
			if _, err := hook.Validate("h", spec); err != nil {
				t.Errorf("%s refused a %s fail_closed policy with its message: %v", point, key, err)
			}
		}
	}
}

// TestValidateRejectsFailClosedWithoutMessage proves fail_closed needs
// the text it hands the model, and that a text with no fail_closed is
// refused.
func TestValidateRejectsFailClosedWithoutMessage(t *testing.T) {
	for _, key := range []string{"on_error", "on_uncertain"} {
		spec := validSpec(hook.PointBeforeTool)
		if key == "on_error" {
			spec.OnError = hook.FailClosed
		} else {
			spec.OnUncertain = hook.FailClosed
		}
		_, err := hook.Validate("h", spec)
		path, message := refused(t, err)
		if path != key || !strings.Contains(message, "fail_closed_message") {
			t.Fatalf("%s: refused %q with %q, want the key and its message reason", key, path, message)
		}
	}
	spec := validSpec(hook.PointBeforeTool)
	spec.FailClosedMessage = "a stray message"
	_, err := hook.Validate("h", spec)
	path, text := refused(t, err)
	if path != "fail_closed_message" || !strings.Contains(text, "fail_closed") {
		t.Fatalf("refused %q with %q, want the message key and its fail_closed reason", path, text)
	}
}

// TestValidateRejectsBadPolicyValues proves the words a policy is
// spelled with, the point, and the two limits.
func TestValidateRejectsBadPolicyValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
		edit func(*hook.Spec)
	}{
		{"unknown point", "point", func(s *hook.Spec) { s.Point = hook.Point("after_run") }},
		{"unknown on_error", "on_error", func(s *hook.Spec) { s.OnError = "explode" }},
		{"unknown on_uncertain", "on_uncertain", func(s *hook.Spec) { s.OnUncertain = "explode" }},
		{"negative timeout", "firing_timeout_ms", func(s *hook.Spec) { s.FiringTimeoutMs = -1 }},
		{"negative fires", "max_fires_per_run", func(s *hook.Spec) { s.MaxFiresPerRun = -2 }},
		{"negative turns", "context.turns", func(s *hook.Spec) { s.Context.Turns = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec(hook.PointBeforeTool)
			test.edit(&spec)
			_, err := hook.Validate("h", spec)
			path, _ := refused(t, err)
			if path != test.key {
				t.Fatalf("refused %q, want %q", path, test.key)
			}
		})
	}
}

// TestValidateRejectsMatcherForPoint proves a matcher the point has
// nothing to match on is a spec error, and the values are the words the
// points know.
func TestValidateRejectsMatcherForPoint(t *testing.T) {
	tests := []struct {
		name  string
		point hook.Point
		key   string
		match hook.Match
	}{
		{"tool at before_run", hook.PointBeforeRun, "match.tool", hook.Match{Tool: "bash"}},
		{"trigger at after_tool", hook.PointAfterTool, "match.trigger", hook.Match{Trigger: hook.TriggerPrompt}},
		{"reason at after_tool", hook.PointAfterTool, "match.reason", hook.Match{Reason: hook.ReasonEndTurn}},
		{"interrupted at before_settle", hook.PointBeforeSettle, "match.interrupted", hook.Match{Interrupted: boolOf(true)}},
		{"unknown trigger", hook.PointBeforeRun, "match.trigger", hook.Match{Trigger: "timer"}},
		{"unknown class", hook.PointBeforeTool, "match.class", hook.Match{Class: "mutating"}},
		{"unknown reason", hook.PointBeforeSettle, "match.reason", hook.Match{Reason: "max_tokens"}},
		{"tool and class", hook.PointBeforeTool, "match", hook.Match{Tool: "bash", Class: hook.ClassReadOnly}},
		{"prefix and substrings", hook.PointBeforeTool, "match", hook.Match{Tool: "bash", CommandPrefix: "git", CommandContains: []string{"push"}}},
		{"prefix without a tool", hook.PointBeforeTool, "match.command_prefix", hook.Match{CommandPrefix: "git"}},
		{"substring without a tool", hook.PointBeforeTool, "match.command_contains", hook.Match{CommandContains: []string{"git"}}},
		{"empty substring", hook.PointBeforeTool, "match.command_contains", hook.Match{Tool: "bash", CommandContains: []string{""}}},
		{"turn zero", hook.PointAfterTurn, "match.min_turn", hook.Match{MinTurn: intOf(0)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec(test.point)
			spec.Match = test.match
			_, err := hook.Validate("h", spec)
			path, _ := refused(t, err)
			if path != test.key {
				t.Fatalf("refused %q, want %q", path, test.key)
			}
		})
	}
	if _, err := hook.Validate("h", func() hook.Spec {
		spec := validSpec(hook.PointAfterTool)
		spec.Match = hook.Match{Tool: "bash", IsError: boolOf(true), CommandPrefix: "go test"}
		return spec
	}()); err != nil {
		t.Fatalf("a tool matcher with is_error and a prefix was refused: %v", err)
	}
}

// TestValidateRejectsProbes proves a probe gathers one kind of evidence,
// names itself once, and is bounded.
func TestValidateRejectsProbes(t *testing.T) {
	tests := []struct {
		name   string
		probes []hook.Probe
		key    string
	}{
		{"both", []hook.Probe{{Name: "p", Command: []string{"true"}, Tool: &hook.ToolProbe{Name: "read"}}}, "context.probes[0]"},
		{"neither", []hook.Probe{{Name: "p"}}, "context.probes[0]"},
		{"no name", []hook.Probe{{Command: []string{"true"}}}, "context.probes[0]"},
		{"repeated", []hook.Probe{{Name: "p", Command: []string{"true"}}, {Name: "p", Command: []string{"true"}}}, "context.probes[1]"},
		{"negative timeout", []hook.Probe{{Name: "p", Command: []string{"true"}, TimeoutMs: -1}}, "context.probes[0].timeout_ms"},
		{"no program", []hook.Probe{{Name: "p", Command: []string{""}}}, "context.probes[0].command"},
		{"unnamed tool", []hook.Probe{{Name: "p", Tool: &hook.ToolProbe{}}}, "context.probes[0].tool.name"},
		{"args are not an object", []hook.Probe{{Name: "p", Tool: &hook.ToolProbe{Name: "read", Args: json.RawMessage(`[1, 2]`)}}}, "context.probes[0].tool.args"},
		{"too many", []hook.Probe{
			{Name: "a", Command: []string{"true"}},
			{Name: "b", Command: []string{"true"}},
			{Name: "c", Command: []string{"true"}},
			{Name: "d", Command: []string{"true"}},
			{Name: "e", Command: []string{"true"}},
		}, "context.probes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec(hook.PointBeforeTool)
			spec.Context.Probes = test.probes
			_, err := hook.Validate("h", spec)
			path, _ := refused(t, err)
			if path != test.key {
				t.Fatalf("refused %q, want %q", path, test.key)
			}
		})
	}
}

// TestValidateRejectsQuestions proves a step's question and its branches
// are checked against each other.
func TestValidateRejectsQuestions(t *testing.T) {
	tests := []struct {
		name string
		key  string
		edit func(*hook.Spec)
	}{
		{"unknown kind", "steps.ask.question", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Question = hook.QuestionSpec{Kind: "rank", Instructions: "How much?"}
			s.Steps["ask"] = step
		}},
		{"bool with options", "steps.ask.question", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Question.Options = []decide.Option{{Name: "a"}}
			s.Steps["ask"] = step
		}},
		{"no instructions", "steps.ask.question", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Question.Instructions = ""
			s.Steps["ask"] = step
		}},
		{"repeated option", "steps.ask.question", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Question = hook.QuestionSpec{
				Kind:         hook.QuestionChoice,
				Instructions: "Which?",
				Options:      []decide.Option{{Name: "a"}, {Name: "a"}},
			}
			step.Branches = map[string]hook.Branch{"a": {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}}}
			s.Steps["ask"] = step
		}},
		{"answer the question does not have", "steps.ask.branches.maybe", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches["maybe"] = hook.Branch{MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}}
			s.Steps["ask"] = step
		}},
		{"probability zero", "steps.ask.branches.yes.min_probability", func(s *hook.Spec) {
			step := s.Steps["ask"]
			branch := step.Branches["yes"]
			branch.MinProbability = 0
			step.Branches["yes"] = branch
			s.Steps["ask"] = step
		}},
		{"probability above one", "steps.ask.branches.yes.min_probability", func(s *hook.Spec) {
			step := s.Steps["ask"]
			branch := step.Branches["yes"]
			branch.MinProbability = 1.5
			step.Branches["yes"] = branch
			s.Steps["ask"] = step
		}},
		{"no branches", "steps.ask.branches", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches = nil
			s.Steps["ask"] = step
		}},
		{"next and action", "steps.ask.branches.yes", func(s *hook.Spec) {
			s.Steps["ask"] = hook.Step{
				Question: s.Steps["ask"].Question,
				Branches: map[string]hook.Branch{
					"yes": {MinProbability: 0.5, Next: "ask", Action: &hook.Action{Kind: hook.ActionAllow}},
					"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
				},
			}
		}},
		{"next and no action", "steps.ask.branches.yes", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches["yes"] = hook.Branch{MinProbability: 0.5}
			s.Steps["ask"] = step
		}},
		{"next that is not a step", "steps.ask.branches.yes.next", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches["yes"] = hook.Branch{MinProbability: 0.5, Next: "nowhere"}
			s.Steps["ask"] = step
		}},
		{"negative step timeout", "steps.ask.timeout_ms", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.TimeoutMs = -1
			s.Steps["ask"] = step
		}},
		{"action with no kind", "steps.ask.branches.yes.action.kind", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches["yes"] = hook.Branch{MinProbability: 0.5, Action: &hook.Action{}}
			s.Steps["ask"] = step
		}},
		{"decision that needs a message", "steps.ask.branches.yes.action.message", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches["yes"] = hook.Branch{MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionDeny}}
			s.Steps["ask"] = step
		}},
		{"decision that shows no message", "steps.ask.branches.yes.action.message", func(s *hook.Spec) {
			step := s.Steps["ask"]
			step.Branches["yes"] = hook.Branch{MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow, Message: "hi"}}
			s.Steps["ask"] = step
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validSpec(hook.PointBeforeTool)
			test.edit(&spec)
			_, err := hook.Validate("h", spec)
			path, _ := refused(t, err)
			if path != test.key {
				t.Fatalf("refused %q, want %q", path, test.key)
			}
		})
	}
}

// TestValidateRejectsGraph proves the graph is one a runner can walk:
// it starts at the entry, reaches every step, and never loops.
func TestValidateRejectsGraph(t *testing.T) {
	loop := validSpec(hook.PointBeforeTool)
	loop.Steps["ask"] = hook.Step{
		Question: loop.Steps["ask"].Question,
		Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Next: "ask"},
			"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		},
	}
	_, err := hook.Validate("h", loop)
	path, message := refused(t, err)
	if path != "steps.ask" || !strings.Contains(message, "loops back") {
		t.Fatalf("refused %q with %q, want the looping step", path, message)
	}

	// A cycle two steps below the entry names the step it came back to,
	// not the entry.
	twoStep := loop
	twoStep.Steps["ask"] = hook.Step{
		Question: twoStep.Steps["ask"].Question,
		Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Next: "middle"},
			"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		},
	}
	twoStep.Steps["middle"] = hook.Step{
		Question: twoStep.Steps["ask"].Question,
		Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Next: "ask"},
			"no":  {MinProbability: 0.5, Next: "end"},
		},
	}
	twoStep.Steps["end"] = hook.Step{
		Question: twoStep.Steps["ask"].Question,
		Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
			"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		},
	}
	_, err = hook.Validate("h", twoStep)
	path, message = refused(t, err)
	if path != "steps.ask" || !strings.Contains(message, "loops back") {
		t.Fatalf("refused %q with %q, want the step the cycle comes back to", path, message)
	}

	stranded := validSpec(hook.PointBeforeTool)
	stranded.Steps["spare"] = hook.Step{
		Question: stranded.Steps["ask"].Question,
		Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
			"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		},
	}
	_, err = hook.Validate("h", stranded)
	path, message = refused(t, err)
	if path != "steps.spare" || !strings.Contains(message, "no branch reaches") {
		t.Fatalf("refused %q with %q, want the stranded step", path, message)
	}

	missing := validSpec(hook.PointBeforeTool)
	missing.Entry = "start"
	_, err = hook.Validate("h", missing)
	path, _ = refused(t, err)
	if path != "entry" {
		t.Fatalf("refused %q, want the entry", path)
	}

	empty := validSpec(hook.PointBeforeTool)
	empty.Steps = nil
	_, err = hook.Validate("h", empty)
	path, _ = refused(t, err)
	if path != "steps" {
		t.Fatalf("refused %q, want the steps", path)
	}
}

// TestValidateWalksADiamond proves a step two branches reach is one step
// of the walk, not a loop: the check reads the shared step once.
func TestValidateWalksADiamond(t *testing.T) {
	spec := validSpec(hook.PointBeforeTool)
	question := spec.Steps["ask"].Question
	leaf := map[string]hook.Branch{
		"yes": {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
	}
	spec.Steps = map[string]hook.Step{
		"ask": {Question: question, Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.75, Next: "left"},
			"no":  {MinProbability: 0.5, Next: "right"},
		}},
		"left": {Question: question, Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Next: "done"},
			"no":  {MinProbability: 0.5, Next: "done"},
		}},
		"right": {Question: question, Branches: leaf},
		"done": {Question: question, Branches: map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
			"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		}},
	}
	compiled, err := hook.Validate("h", spec)
	if err != nil {
		t.Fatalf("the diamond was refused: %v", err)
	}
	if len(compiled.Steps) != 4 {
		t.Fatalf("the compiled graph has %d step(s), want 4", len(compiled.Steps))
	}
}

// TestValidateRejectsTooManySteps proves a graph is bounded.
func TestValidateRejectsTooManySteps(t *testing.T) {
	spec := validSpec(hook.PointBeforeTool)
	line := make(map[string]hook.Step, hook.MaxSteps+1)
	for i := 0; i <= hook.MaxSteps; i++ {
		name := string(rune('a' + i))
		branches := map[string]hook.Branch{
			"yes": {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
			"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
		}
		if i < hook.MaxSteps {
			branches["yes"] = hook.Branch{MinProbability: 0.5, Next: string(rune('a' + i + 1))}
		}
		line[name] = hook.Step{Question: spec.Steps["ask"].Question, Branches: branches}
	}
	spec.Steps = line
	spec.Entry = "a"
	_, err := hook.Validate("h", spec)
	path, _ := refused(t, err)
	if path != "steps" {
		t.Fatalf("refused %q, want the steps", path)
	}
}

// TestValidateRejectsNamelessHook proves a hook's name is what the core
// renders its origin from.
func TestValidateRejectsNamelessHook(t *testing.T) {
	if _, err := hook.Validate("", validSpec(hook.PointBeforeTool)); err == nil {
		t.Fatalf("a hook with no name was accepted")
	}
}

// TestCompiledIgnoresLaterSpecChanges proves Validate copies what it
// keeps, so a policy cannot change under a run.
func TestCompiledIgnoresLaterSpecChanges(t *testing.T) {
	spec := decodeFixture(t, wireFixture)
	compiled, err := hook.Validate("pre-push-gofmt", spec)
	if err != nil {
		t.Fatalf("the wire fixture was refused: %v", err)
	}
	spec.Steps["is_pushing_go_code"].Branches["yes"] = hook.Branch{MinProbability: 0.1, Next: "classify_fix"}
	spec.Steps["classify_fix"].Branches["run_gofmt"].Action.Message = "changed"
	spec.Match.CommandContains[0] = "changed"
	spec.Context.Probes[0].Command[0] = "changed"
	choice := spec.Steps["classify_fix"]
	choice.Question = hook.QuestionSpec{Kind: hook.QuestionChoice, Options: []decide.Option{{Name: "changed"}}}
	spec.Steps["classify_fix"] = choice
	spec.Context.Probes[1].Tool.Args = json.RawMessage(`{"path":"changed"}`)

	if got := compiled.Steps["is_pushing_go_code"].Branches["yes"].MinProbability; got != 0.75 {
		t.Fatalf("the compiled branch needs %v", got)
	}
	if got := compiled.Steps["classify_fix"].Branches["run_gofmt"].Action.Message; strings.Contains(got, "changed") {
		t.Fatalf("the compiled action message is %q", got)
	}
	if got := compiled.Match.CommandContains[0]; got != "git push" {
		t.Fatalf("the compiled matcher holds %q", got)
	}
	if got := compiled.Context.Probes[0].Command[0]; got != "gofmt" {
		t.Fatalf("the compiled probe runs %q", got)
	}
	if got := compiled.Steps["classify_fix"].Question.Keys(); !reflect.DeepEqual(got, []string{"run_gofmt", "allow_anyway"}) {
		t.Fatalf("the compiled question's answers are %v, want the fixture's", got)
	}
	if got := string(compiled.Context.Probes[1].Tool.Args); !strings.Contains(got, "go.mod") {
		t.Fatalf("the compiled probe's arguments are %q", got)
	}
}

// TestCompiledCopiesThePointersASpecSets proves the compiled matcher does
// not share the spec's own pointers, so a caller that keeps its Spec
// cannot change a policy mid-run.
func TestCompiledCopiesThePointersASpecSets(t *testing.T) {
	spec := validSpec(hook.PointBeforeSettle)
	spec.Match = hook.Match{Reason: hook.ReasonEndTurn, MinTurn: intOf(2)}
	compiled, err := hook.Validate("h", spec)
	if err != nil {
		t.Fatalf("the spec was refused: %v", err)
	}
	*spec.Match.MinTurn = 7
	if got := *compiled.Match.MinTurn; got != 2 {
		t.Fatalf("the compiled matcher needs turn %d, want the spec's 2", got)
	}
}

// refused reports the key and the explanation a refused spec names.
func refused(t *testing.T, err error) (string, string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the spec was accepted")
	}
	var failure *hook.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, not a hook.Error", err)
	}
	if failure.Message == "" {
		t.Fatalf("the failure %q explains nothing", failure.Error())
	}
	return failure.Path, failure.Message
}

// boolOf is a pointer to a bool.
func boolOf(value bool) *bool { return &value }

// intOf is a pointer to an int.
func intOf(value int) *int { return &value }
