package decide_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sdougbrown/poo/decide"
)

// The decide package validates before it sends, and it defers rather than
// guesses.

// TestAQuestionIsValidatedBeforeAnythingIsSent covers the validation: an
// invalid question is a caller bug, and it is reported as such.
func TestAQuestionIsValidatedBeforeAnythingIsSent(t *testing.T) {
	cases := []struct {
		name string
		qs   []decide.Question
	}{
		{
			name: "a question with no id",
			qs:   []decide.Question{{Instructions: "is it?", Kind: decide.Bool{True: "yes"}}},
		},
		{
			name: "a question with no instructions",
			qs:   []decide.Question{{ID: "q1", Kind: decide.Bool{True: "yes"}}},
		},
		{
			name: "a question with no kind",
			qs:   []decide.Question{{ID: "q1", Instructions: "is it?"}},
		},
		{
			name: "a repeated id",
			qs: []decide.Question{
				{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "yes"}},
				{ID: "q1", Instructions: "is it really?", Kind: decide.Bool{True: "yes"}},
			},
		},
		{
			name: "too many options",
			qs: []decide.Question{{ID: "q1", Instructions: "which?", Kind: decide.Choice{
				Options: manyOptions(27),
			}}},
		},
		{
			name: "too many levels",
			qs: []decide.Question{{ID: "q1", Instructions: "how much?", Kind: decide.Score{
				Levels: manyLevels(11),
			}}},
		},
		{
			name: "too few levels",
			qs: []decide.Question{{ID: "q1", Instructions: "how much?", Kind: decide.Score{
				Levels: manyLevels(1),
			}}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := decide.ValidateQuestions(c.qs)
			if !errors.Is(err, decide.ErrInvalidQuestion) {
				t.Errorf("the validation returned %v, want ErrInvalidQuestion", err)
			}
		})
	}
}

// TestTheLimitsAreThePlanLimits covers the two limits the plan names: a
// choice question has at most twenty-six options and a score question at
// most ten levels.
func TestTheLimitsAreThePlanLimits(t *testing.T) {
	if err := decide.ValidateQuestions([]decide.Question{{
		ID: "q1", Instructions: "which?", Kind: decide.Choice{Options: manyOptions(26)},
	}}); err != nil {
		t.Errorf("twenty-six options were rejected: %v", err)
	}
	if err := decide.ValidateQuestions([]decide.Question{{
		ID: "q1", Instructions: "how much?", Kind: decide.Score{Levels: manyLevels(10)},
	}}); err != nil {
		t.Errorf("ten levels were rejected: %v", err)
	}
}

// TestTheAnswerKeysFollowTheKind covers the answer keys: a bool is keyed
// yes and no, a choice by the option names and a score by the level
// index.
func TestTheAnswerKeysFollowTheKind(t *testing.T) {
	bool := decide.Question{ID: "q1", Kind: decide.Bool{True: "yes", False: "no"}}
	if got := bool.Keys(); len(got) != 2 || got[0] != "yes" || got[1] != "no" {
		t.Errorf("the bool's keys are %v, want yes and no", got)
	}

	choice := decide.Question{ID: "q2", Kind: decide.Choice{Options: []decide.Option{
		{Name: "alpha"}, {Name: "beta"},
	}}}
	if got := choice.Keys(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("the choice's keys are %v, want alpha and beta", got)
	}

	score := decide.Question{ID: "q3", Kind: decide.Score{Levels: []string{"low", "mid", "high"}}}
	if got := score.Keys(); len(got) != 3 || got[0] != "0" || got[2] != "2" {
		t.Errorf("the score's keys are %v, want 0, 1 and 2", got)
	}
}

// TestABadDistributionIsADeferral covers the answer validation: a
// distribution that does not sum to one cannot be used, so the call is a
// deferral rather than a failure.
func TestABadDistributionIsADeferral(t *testing.T) {
	questions := []decide.Question{{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "yes", False: "no"}}}

	cases := []struct {
		name    string
		answers []decide.Answer
	}{
		{
			name: "no answer at all",
			answers: []decide.Answer{
				{ID: "q2", Probabilities: map[string]float64{"yes": 1}},
			},
		},
		{
			name: "a probability out of range",
			answers: []decide.Answer{
				{ID: "q1", Probabilities: map[string]float64{"yes": 1.5, "no": -0.5}},
			},
		},
		{
			name: "a distribution that does not sum to one",
			answers: []decide.Answer{
				{ID: "q1", Probabilities: map[string]float64{"yes": 0.5, "no": 0.2}},
			},
		},
		{
			name: "a missing key",
			answers: []decide.Answer{
				{ID: "q1", Probabilities: map[string]float64{"yes": 1}},
			},
		},
		{
			name: "no distribution at all",
			answers: []decide.Answer{
				{ID: "q1"},
			},
		},
		{
			name: "two answers for one question",
			answers: []decide.Answer{
				{ID: "q1", Probabilities: map[string]float64{"yes": 1, "no": 0}},
				{ID: "q1", Probabilities: map[string]float64{"yes": 0, "no": 1}},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := decide.ValidateAnswers(questions, c.answers)
			if !errors.Is(err, decide.ErrDeferred) {
				t.Errorf("the validation returned %v, want ErrDeferred", err)
			}
		})
	}
}

// TestAValidDistributionIsAccepted covers the happy path.
func TestAValidDistributionIsAccepted(t *testing.T) {
	questions := []decide.Question{{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "yes", False: "no"}}}
	answers := []decide.Answer{{
		ID:            "q1",
		Probabilities: map[string]float64{"yes": 0.8, "no": 0.2},
	}}
	if err := decide.ValidateAnswers(questions, answers); err != nil {
		t.Errorf("the validation returned %v, want no error", err)
	}
}

// TestTheQuestionsAreSplitIntoBatches covers the splitting: a call with
// more than sixty-four questions is split, and a question is never split
// across batches.
func TestTheQuestionsAreSplitIntoBatches(t *testing.T) {
	var qs []decide.Question
	for i := 0; i < 150; i++ {
		qs = append(qs, decide.Question{
			ID:           "q" + itoa(i),
			Instructions: "is it?",
			Kind:         decide.Bool{True: "yes", False: "no"},
		})
	}

	batches := decide.SplitQuestions(qs, decide.MaxQuestionsPerRequest)
	if len(batches) != 3 {
		t.Fatalf("the questions were split into %d batches, want 3", len(batches))
	}
	total := 0
	seen := map[string]bool{}
	for _, b := range batches {
		if len(b) > decide.MaxQuestionsPerRequest {
			t.Errorf("a batch holds %d questions, the most is %d", len(b), decide.MaxQuestionsPerRequest)
		}
		total += len(b)
		for _, q := range b {
			if seen[q.ID] {
				t.Errorf("the question %q is in two batches", q.ID)
			}
			seen[q.ID] = true
		}
	}
	if total != len(qs) {
		t.Errorf("the batches hold %d questions, want %d", total, len(qs))
	}
}

// TestTheEvidenceIsFramedAsData covers the evidence: the payload frames
// the turns as data to assess, not instructions to follow.
func TestTheEvidenceIsFramedAsData(t *testing.T) {
	payload := decide.Evidence([]decide.Turn{
		{Role: "user", Text: "fix the bug"},
		{Role: "tool", ToolName: "bash", ToolInput: `{"command":"ls"}`, ToolOutput: "a.go"},
	}, decide.EvidenceOptions{Cwd: "/work"})

	frame, ok := payload.(decide.EvidencePayload)
	if !ok {
		t.Fatalf("the evidence is a %T, want the evidence payload", payload)
	}
	if frame.Cwd != "/work" {
		t.Errorf("the payload's working directory is %q, want /work", frame.Cwd)
	}
	if len(frame.Turns) != 2 {
		t.Fatalf("the payload holds %d turns, want 2", len(frame.Turns))
	}
	if frame.Instructions == "" {
		t.Error("the payload carries no framing")
	}
}

// TestTheStateHashIsStable covers the state hash: the same state gives
// the same hash, and a different state gives a different one.
func TestTheStateHashIsStable(t *testing.T) {
	a := decide.StateHash(map[string]any{"turn": 1})
	b := decide.StateHash(map[string]any{"turn": 1})
	if a != b {
		t.Errorf("the same state hashed to %q and %q", a, b)
	}
	c := decide.StateHash(map[string]any{"turn": 2})
	if a == c {
		t.Errorf("a different state hashed to the same value %q", a)
	}
}

// TestTheDeciderFuncAdaptsAFunction covers the adapter.
func TestTheDeciderFuncAdaptsAFunction(t *testing.T) {
	d := decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		return []decide.Answer{{ID: "q1"}}, nil
	})
	answers, err := d.Decide(context.Background(), nil, "", nil)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if len(answers) != 1 {
		t.Errorf("the decider returned %d answers, want 1", len(answers))
	}
}

// TestTheAnswersAreSortedInTheOrderOfTheQuestions covers the ordering:
// the answers read the way the questions were asked, and an answer for a
// question that was not asked is kept rather than dropped.
func TestTheAnswersAreSortedInTheOrderOfTheQuestions(t *testing.T) {
	questions := []decide.Question{
		{ID: "q2", Instructions: "second?", Kind: decide.Bool{True: "yes", False: "no"}},
		{ID: "q1", Instructions: "first?", Kind: decide.Bool{True: "yes", False: "no"}},
	}
	answers := []decide.Answer{
		{ID: "q1", Choice: "the first"},
		{ID: "q2", Choice: "the second"},
		{ID: "q3", Choice: "the unasked"},
	}

	sorted := decide.SortAnswers(questions, answers)
	if len(sorted) != 3 {
		t.Fatalf("the sorted answers hold %d, want 3", len(sorted))
	}
	if sorted[0].ID != "q2" {
		t.Errorf("the first answer is %q, want q2", sorted[0].ID)
	}
	if sorted[1].ID != "q1" {
		t.Errorf("the second answer is %q, want q1", sorted[1].ID)
	}
	if sorted[2].ID != "q3" {
		t.Errorf("the third answer is %q, want q3: an answer for a question that was not asked is kept", sorted[2].ID)
	}
}

// TestTheRequestCountComesFromTheDecider covers the request count: a
// decider that knows how many wire requests it made reports it, and one
// that does not leaves the count at zero.
func TestTheRequestCountComesFromTheDecider(t *testing.T) {
	sink := &recordingSink{}
	recorder := decide.Recorded(countingDecider{requests: 3}, sink, "the caller")

	if _, err := recorder.Decide(context.Background(), nil, "", nil); err != nil {
		t.Fatalf("deciding: %v", err)
	}
	records := sink.records
	if len(records) != 1 {
		t.Fatalf("the sink holds %d records, want 1", len(records))
	}
	if records[0].Requests != 3 {
		t.Errorf("the record's request count is %d, want 3", records[0].Requests)
	}

	// A decider that does not report a count leaves it at zero, which is
	// the truth: the count was never reported.
	sink2 := &recordingSink{}
	recorder2 := decide.Recorded(countingDecider{}, sink2, "the caller")
	if _, err := recorder2.Decide(context.Background(), nil, "", nil); err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if got := sink2.records[0].Requests; got != 0 {
		t.Errorf("the record's request count is %d, want 0", got)
	}
}

// recordingSink keeps the records it receives.
type recordingSink struct {
	// records holds the records, in order.
	records []decide.Record
}

// Record keeps one record.
func (s *recordingSink) Record(ctx context.Context, rec decide.Record) error {
	s.records = append(s.records, rec)
	return nil
}

// countingDecider is a decider that reports a request count.
type countingDecider struct {
	// requests is the count it reports.
	requests int
}

// Decide answers nothing.
func (d countingDecider) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	return nil, nil
}

// Requests reports the request count.
func (d countingDecider) Requests() int { return d.requests }

// manyOptions returns n named options.
func manyOptions(n int) []decide.Option {
	out := make([]decide.Option, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, decide.Option{Name: "o" + itoa(i)})
	}
	return out
}

// manyLevels returns n named levels.
func manyLevels(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "l"+itoa(i))
	}
	return out
}

// itoa formats an int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
