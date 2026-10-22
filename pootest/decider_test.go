package pootest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/pootest"
)

// The stub decider answers with a fixed distribution, and the scripted
// compactor answers with a fixed decision.

// TestTheStubDeciderAnswersEveryQuestion covers the decider: every
// question gets an answer, and the distribution sums to one.
func TestTheStubDeciderAnswersEveryQuestion(t *testing.T) {
	d := pootest.NewDecider(0.7)
	questions := []decide.Question{
		{ID: "q1", Instructions: "is it a bug?", Kind: decide.Bool{True: "yes", False: "no"}},
		{
			ID:           "q2",
			Instructions: "which one?",
			Kind: decide.Choice{Options: []decide.Option{
				{Name: "a"}, {Name: "b"}, {Name: "c"},
			}},
		},
	}

	answers, err := d.Decide(context.Background(), "the state", "the instructions", questions)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("the decider returned %d answers, want 2", len(answers))
	}
	if err := decide.ValidateAnswers(questions, answers); err != nil {
		t.Errorf("the answers do not validate: %v", err)
	}
	if answers[0].Probabilities["yes"] != 0.7 {
		t.Errorf("the bool's yes probability is %v, want 0.7", answers[0].Probabilities["yes"])
	}
	if d.Calls() != 1 {
		t.Errorf("the decider served %d calls, want 1", d.Calls())
	}
	if len(d.Questions()) != 1 {
		t.Errorf("the decider recorded %d calls, want 1", len(d.Questions()))
	}
}

// TestTheStubDeciderFails covers the failure the decider is told to
// return.
func TestTheStubDeciderFails(t *testing.T) {
	d := pootest.NewDecider(0.5)
	want := errors.New("the endpoint is down")
	d.SetError(want)

	if _, err := d.Decide(context.Background(), nil, "", nil); !errors.Is(err, want) {
		t.Errorf("the call returned %v, want %v", err, want)
	}
}

// TestTheRecordingDeciderWritesOneRecord covers the recording wrapper:
// one record per logical call, whatever the outcome.
func TestTheRecordingDeciderWritesOneRecord(t *testing.T) {
	sink := pootest.NewRecordingSink()
	inner := pootest.NewDecider(0.5)
	recorder := decide.Recorded(inner, sink, "the caller")

	questions := []decide.Question{
		{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "yes", False: "no"}},
	}
	if _, err := recorder.Decide(context.Background(), "the state", "the instructions", questions); err != nil {
		t.Fatalf("deciding: %v", err)
	}

	records := sink.Records()
	if len(records) != 1 {
		t.Fatalf("the sink holds %d records, want 1", len(records))
	}
	if records[0].Outcome != decide.OutcomeOK {
		t.Errorf("the record's outcome is %q, want ok", records[0].Outcome)
	}
	if records[0].Origin != "the caller" {
		t.Errorf("the record's origin is %q, want the caller", records[0].Origin)
	}
	if records[0].StateHash == "" {
		t.Error("the record holds no state hash")
	}
}

// TestTheRecordingDeciderRecordsADeferral covers the deferral: the
// record says the call was deferred, and the error still reaches the
// caller.
func TestTheRecordingDeciderRecordsADeferral(t *testing.T) {
	sink := pootest.NewRecordingSink()
	inner := pootest.NewDecider(0.5)
	inner.SetError(decide.ErrDeferred)
	recorder := decide.Recorded(inner, sink, "the caller")

	if _, err := recorder.Decide(context.Background(), nil, "", nil); !errors.Is(err, decide.ErrDeferred) {
		t.Errorf("the call returned %v, want the deferral", err)
	}
	records := sink.Records()
	if len(records) != 1 {
		t.Fatalf("the sink holds %d records, want 1", len(records))
	}
	if records[0].Outcome != decide.OutcomeDeferred {
		t.Errorf("the record's outcome is %q, want deferred", records[0].Outcome)
	}
}

// TestTheScriptedCompactorAnswersWithItsScript covers the compactor: the
// scripted answer comes back, and the call is recorded.
func TestTheScriptedCompactorAnswersWithItsScript(t *testing.T) {
	compaction := poo.Compaction{
		Strategy: "the scripted one",
		End:      &poo.EndRun{Handoff: "the handoff"},
	}
	c := pootest.NewCompactor(true, compaction)

	view := poo.NewHistory(nil, nil, nil)
	if !c.ShouldCompact(view, poo.Budget{ContextLimit: 100}) {
		t.Error("the compactor declined to compact, want the scripted answer")
	}
	got, err := c.Compact(context.Background(), view, nil)
	if err != nil {
		t.Fatalf("compacting: %v", err)
	}
	if got.Strategy != "the scripted one" {
		t.Errorf("the compaction's strategy is %q, want the scripted one", got.Strategy)
	}
	if got.End == nil || got.End.Handoff != "the handoff" {
		t.Errorf("the compaction's end is %+v, want the handoff", got.End)
	}
	if c.Calls() != 1 {
		t.Errorf("the compactor ran %d times, want 1", c.Calls())
	}
	if len(c.Views()) != 1 {
		t.Errorf("the compactor recorded %d views, want 1", len(c.Views()))
	}
}

// TestTheScriptedCompactorFails covers the failure the compactor is told
// to return.
func TestTheScriptedCompactorFails(t *testing.T) {
	c := pootest.NewCompactor(true, poo.Compaction{})
	want := errors.New("the compaction failed")
	c.SetError(want)

	if _, err := c.Compact(context.Background(), poo.NewHistory(nil, nil, nil), nil); !errors.Is(err, want) {
		t.Errorf("the compaction returned %v, want %v", err, want)
	}
}

// TestTheStubDeciderNamesItsChoice covers SetChoice: the choice the test
// sets is the answer's choice, and the answers still validate.
func TestTheStubDeciderNamesItsChoice(t *testing.T) {
	d := pootest.NewDecider(0.5)
	d.SetChoice("b")
	questions := []decide.Question{
		{ID: "q1", Instructions: "which?", Kind: decide.Choice{Options: []decide.Option{
			{Name: "a"}, {Name: "b"},
		}}},
	}

	answers, err := d.Decide(context.Background(), nil, "", questions)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if answers[0].Choice != "b" {
		t.Errorf("the answer names %q, want b", answers[0].Choice)
	}
	if err := decide.ValidateAnswers(questions, answers); err != nil {
		t.Errorf("the answers do not validate: %v", err)
	}
}

// TestTheScriptedCompactorForksTheCallItIsGiven covers Fork: the call's
// appended messages and output ceiling are recorded, the fork func runs,
// and a nil fork is an error rather than a panic.
func TestTheScriptedCompactorForksTheCallItIsGiven(t *testing.T) {
	c := pootest.NewCompactor(true, poo.Compaction{Strategy: "the scripted one"})
	appended := []poo.Message{poo.NewTextMessage(poo.RoleUser, "again")}

	msg, usage, err := c.Fork(context.Background(),
		func(ctx context.Context, appended []poo.Message, maxTokens int) (poo.Message, poo.UsageReport, error) {
			return poo.NewTextMessage(poo.RoleAssistant, "the fork"), poo.UsageReport{TTFTMS: 3}, nil
		}, appended, 32)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	if msg.Text() != "the fork" || usage.TTFTMS != 3 {
		t.Errorf("the fork returned (%q, %+v), want the fork func's answer", msg.Text(), usage)
	}
	forks := c.Forks()
	if len(forks) != 1 {
		t.Fatalf("the compactor recorded %d forks, want 1", len(forks))
	}
	if forks[0].MaxTokens != 32 {
		t.Errorf("the fork asked for %d tokens, want 32", forks[0].MaxTokens)
	}
	if len(forks[0].Appended) != 1 || forks[0].Appended[0].Text() != "again" {
		t.Errorf("the fork's appended messages are %+v, want the one message", forks[0].Appended)
	}

	if _, _, err := c.Fork(context.Background(), nil, nil, 0); err == nil {
		t.Error("a fork with no func returned no error, want one")
	}
}

// TestTheStubDeciderAnswersASingleOptionChoice covers the one-key case: a
// choice with one option gets a probability of one, so the answer
// validates.
func TestTheStubDeciderAnswersASingleOptionChoice(t *testing.T) {
	d := pootest.NewDecider(0.5)
	questions := []decide.Question{
		{ID: "q1", Instructions: "which?", Kind: decide.Choice{Options: []decide.Option{
			{Name: "only"},
		}}},
	}

	answers, err := d.Decide(context.Background(), nil, "", questions)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if answers[0].Probabilities["only"] != 1 {
		t.Errorf("the single option's probability is %v, want 1", answers[0].Probabilities["only"])
	}
	if err := decide.ValidateAnswers(questions, answers); err != nil {
		t.Errorf("the answers do not validate: %v", err)
	}
}
