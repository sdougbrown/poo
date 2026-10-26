package decide_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
			name: "a bool with no descriptions",
			qs:   []decide.Question{{ID: "q1", Instructions: "is it?", Kind: decide.Bool{}}},
		},
		{
			name: "a score with an empty level name",
			qs: []decide.Question{{ID: "q1", Instructions: "how much?", Kind: decide.Score{
				Levels: []string{"", "b"},
			}}},
		},
		{
			name: "too few levels",
			qs: []decide.Question{{ID: "q1", Instructions: "how much?", Kind: decide.Score{
				Levels: manyLevels(1),
			}}},
		},
		{
			name: "a choice with no options",
			qs:   []decide.Question{{ID: "q1", Instructions: "which?", Kind: decide.Choice{}}},
		},
		{
			name: "a choice with an option with no name",
			qs: []decide.Question{{ID: "q1", Instructions: "which?", Kind: decide.Choice{
				Options: []decide.Option{{Name: ""}},
			}}},
		},
		{
			name: "a choice with two options of one name",
			qs: []decide.Question{{ID: "q1", Instructions: "which?", Kind: decide.Choice{
				Options: []decide.Option{{Name: "a"}, {Name: "a"}},
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
	bq := decide.Question{ID: "q1", Kind: decide.Bool{True: "yes", False: "no"}}
	if got := bq.Keys(); len(got) != 2 || got[0] != "yes" || got[1] != "no" {
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
			// The length check passes (two entries, two keys), so this
			// case is the only one that reaches the per-key check: one
			// required key absent, one key the question never asked
			// about.
			name: "a missing key beside an extra key",
			answers: []decide.Answer{
				{ID: "q1", Probabilities: map[string]float64{"yes": 1, "bogus": 0}},
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
	payload := decide.Evidence([]decide.Utterance{
		{Role: "user", Text: "fix the bug"},
		{Role: "tool", ToolName: "bash", ToolInput: `{"command":"ls"}`, ToolOutput: "a.go"},
	}, decide.EvidenceOptions{WorkingDir: "/work"})

	frame, ok := payload.(decide.EvidencePayload)
	if !ok {
		t.Fatalf("the evidence is a %T, want the evidence payload", payload)
	}
	if frame.WorkingDir != "/work" {
		t.Errorf("the payload's working directory is %q, want /work", frame.WorkingDir)
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

// TestTheNilStateHashesLikeANullJSONValue covers the nil state: it hashes
// the way a JSON null does, so a caller that hashes a missing state and
// one that hashes an explicit null get the same value.
func TestTheNilStateHashesLikeANullJSONValue(t *testing.T) {
	if decide.StateHash(nil) != decide.StateHash([]byte("null")) {
		t.Errorf("a nil state hashed to %q, want the JSON null hash", decide.StateHash(nil))
	}
}

// TestAnUnencodableStateStillHashes covers the fallback: a state the
// encoder cannot render still gets an id. The rendering is the value's
// fmt.Sprintf output, so a stable value (a complex number) hashes
// stably, and an address-carrying value (a channel) at least hashes to
// something non-empty.
func TestAnUnencodableStateStillHashes(t *testing.T) {
	if got := decide.StateHash(make(chan int)); got == "" {
		t.Fatal("an unencodable state hashed to the empty string")
	}
	first := decide.StateHash(complex(1, 2))
	second := decide.StateHash(complex(1, 2))
	if first == "" {
		t.Fatal("an unencodable state hashed to the empty string")
	}
	if first != second {
		t.Errorf("two renders of the same unencodable state hashed to %q and %q", first, second)
	}
}

// TestAQuestionWithoutAKindHasNoKeys covers the nil-kind guards: a
// question with no kind has no keys, and a typed nil kind is guarded the
// way Validate and MarshalJSON guard it, not a panic.
func TestAQuestionWithoutAKindHasNoKeys(t *testing.T) {
	if got := (decide.Question{ID: "q1"}).Keys(); got != nil {
		t.Errorf("a kind-less question returned keys %v, want none", got)
	}
	if got := (decide.Question{ID: "q2", Kind: (*decide.Bool)(nil)}).Keys(); got != nil {
		t.Errorf("a typed-nil kind returned keys %v, want none", got)
	}
}

// TestTheRecordedOutcomeSplitsInvalidFromDeferred covers the recorded
// outcome: a caller bug is recorded as invalid with no error text, and
// any other failure is recorded as a deferral that carries the error.
func TestTheRecordedOutcomeSplitsInvalidFromDeferred(t *testing.T) {
	sink := &recordingSink{}
	invalid := decide.Recorded(decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		return nil, fmt.Errorf("wrapped: %w", decide.ErrInvalidQuestion)
	}), sink, "the caller")
	if _, err := invalid.Decide(context.Background(), nil, "", nil); !errors.Is(err, decide.ErrInvalidQuestion) {
		t.Fatalf("the invalid call returned %v, want ErrInvalidQuestion", err)
	}
	if got := sink.records[0].Outcome; got != decide.OutcomeInvalid {
		t.Errorf("the invalid call was recorded as %q, want %q", got, decide.OutcomeInvalid)
	}
	if sink.records[0].Error != "" {
		t.Errorf("the invalid call recorded the error %q, want none", sink.records[0].Error)
	}

	sink2 := &recordingSink{}
	failed := decide.Recorded(decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		return nil, errors.New("the endpoint was unreachable")
	}), sink2, "the caller")
	if _, err := failed.Decide(context.Background(), nil, "", nil); err == nil || errors.Is(err, decide.ErrInvalidQuestion) {
		t.Fatalf("the failed call returned %v, want an unrelated error", err)
	}
	if got := sink2.records[0].Outcome; got != decide.OutcomeDeferred {
		t.Errorf("the failed call was recorded as %q, want %q", got, decide.OutcomeDeferred)
	}
	if sink2.records[0].Error != "the endpoint was unreachable" {
		t.Errorf("the failed call recorded the error %q, want the decider's error", sink2.records[0].Error)
	}
}

// TestASuccessIsRecordedAsOKWithItsAnswers covers the success branch: a
// call that answers is recorded as ok, and the record carries the
// answers the decider returned.
func TestASuccessIsRecordedAsOKWithItsAnswers(t *testing.T) {
	sink := &recordingSink{}
	want := []decide.Answer{{ID: "q1", Probabilities: map[string]float64{"yes": 1, "no": 0}}}
	recorder := decide.Recorded(decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		return want, nil
	}), sink, "the caller")
	answers, err := recorder.Decide(context.Background(), nil, "", nil)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if got := sink.records[0].Outcome; got != decide.OutcomeOK {
		t.Errorf("the successful call was recorded as %q, want %q", got, decide.OutcomeOK)
	}
	if sink.records[0].Error != "" {
		t.Errorf("the successful call recorded the error %q, want none", sink.records[0].Error)
	}
	if len(answers) != len(want) || answers[0].ID != want[0].ID {
		t.Errorf("the successful call returned %v, want the decider's answers", answers)
	}
	if len(sink.records[0].Answers) != len(want) || sink.records[0].Answers[0].ID != want[0].ID {
		t.Errorf("the record holds answers %v, want the decider's answers", sink.records[0].Answers)
	}
}

// TestAMalformedAnswerIsRecordedAsADeferral covers the wrapper's own
// check: an inner decider that fails no call but answers against the
// questions is recorded as a deferral that carries the reason, and the
// caller still gets the answers and the nil error.
func TestAMalformedAnswerIsRecordedAsADeferral(t *testing.T) {
	qs := []decide.Question{
		{ID: "q1", Kind: decide.Bool{True: "yes", False: "no"}},
		{ID: "q2", Kind: decide.Bool{True: "yes", False: "no"}},
	}
	good := func(id string) decide.Answer {
		return decide.Answer{ID: id, Probabilities: map[string]float64{"yes": 0.5, "no": 0.5}}
	}
	tests := []struct {
		name    string
		answers []decide.Answer
		want    string
	}{
		{
			name:    "missing",
			answers: []decide.Answer{good("q1")},
			want:    `the question "q2" has no answer`,
		},
		{
			name:    "duplicate",
			answers: []decide.Answer{good("q1"), good("q1")},
			want:    `the question "q1" has two answers`,
		},
		{
			name: "bad-distribution",
			answers: []decide.Answer{
				{ID: "q1", Probabilities: map[string]float64{"yes": 0.9, "no": 0.9}},
				good("q2"),
			},
			want: `the question "q1" has probabilities summing to 1.8`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			recorder := decide.Recorded(decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
				return tt.answers, nil
			}), sink, "the caller")

			answers, err := recorder.Decide(context.Background(), nil, "", qs)
			if err != nil {
				t.Fatalf("the call returned %v, want the inner nil error", err)
			}
			if len(answers) != len(tt.answers) {
				t.Errorf("the call returned %d answers, want the decider's %d", len(answers), len(tt.answers))
			}
			if len(sink.records) != 1 {
				t.Fatalf("the sink holds %d records, want 1", len(sink.records))
			}
			rec := sink.records[0]
			if rec.Outcome != decide.OutcomeDeferred {
				t.Errorf("the malformed answer was recorded as %q, want %q", rec.Outcome, decide.OutcomeDeferred)
			}
			if !strings.Contains(rec.Error, tt.want) {
				t.Errorf("the record holds the error %q, want %q", rec.Error, tt.want)
			}
		})
	}
}

// TestASinkThatRefusesARecordDoesNotFailTheDecision covers the recording
// contract: a sink that refuses the record does not change the answer
// the caller gets or the error the caller sees.
func TestASinkThatRefusesARecordDoesNotFailTheDecision(t *testing.T) {
	writes := 0
	broken := decide.SinkFunc(func(ctx context.Context, rec decide.Decision) error {
		writes++
		return errors.New("the log is full")
	})
	want := []decide.Answer{{ID: "q1"}}
	recorder := decide.Recorded(decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		return want, nil
	}), broken, "the caller")
	answers, err := recorder.Decide(context.Background(), nil, "", nil)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if writes != 1 {
		t.Errorf("the sink was called %d times, want 1", writes)
	}
	if len(answers) != 1 || answers[0].ID != "q1" {
		t.Errorf("the call returned %v, want the decider's answers", answers)
	}
}

// TestTheQuestionIDsComeBackSorted covers the id order: IDs sorts, so a
// record built from it reads the same twice whatever order the questions
// arrived in, and it does not reorder the caller's slice.
func TestTheQuestionIDsComeBackSorted(t *testing.T) {
	qs := []decide.Question{
		{ID: "q3", Instructions: "is it?", Kind: decide.Bool{True: "yes"}},
		{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "yes"}},
		{ID: "q2", Instructions: "is it?", Kind: decide.Bool{True: "yes"}},
	}
	got := decide.IDs(qs)
	want := []string{"q1", "q2", "q3"}
	if len(got) != len(want) {
		t.Fatalf("IDs returned %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("IDs returned %v, want %v", got, want)
		}
	}
	inputOrder := []string{"q3", "q1", "q2"}
	for i, q := range qs {
		if q.ID != inputOrder[i] {
			t.Errorf("the caller's question %d has id %q, want %q: IDs reordered its input", i, q.ID, inputOrder[i])
		}
	}
}

// TestTheSplitFallsBackToTheLimitWhenTheSizeIsNotPositive covers the
// batch size fallback: a size the caller cannot mean (zero or negative)
// means the request limit, not a panic and not one batch.
func TestTheSplitFallsBackToTheLimitWhenTheSizeIsNotPositive(t *testing.T) {
	qs := make([]decide.Question, 150)
	for i := range qs {
		qs[i] = decide.Question{ID: "q" + itoa(i), Instructions: "is it?", Kind: decide.Bool{True: "yes"}}
	}
	for _, size := range []int{0, -1} {
		batches := decide.SplitQuestions(qs, size)
		if len(batches) != 3 {
			t.Errorf("SplitQuestions(qs, %d) split %d questions into %d batches, want 3", size, len(qs), len(batches))
		}
		if len(batches[0]) != decide.MaxQuestionsPerRequest {
			t.Errorf("the first batch holds %d questions, want the request limit %d", len(batches[0]), decide.MaxQuestionsPerRequest)
		}
		for i, b := range batches {
			if len(b) > decide.MaxQuestionsPerRequest {
				t.Errorf("batch %d holds %d questions, the most is %d", i, len(b), decide.MaxQuestionsPerRequest)
			}
		}
	}
}

// TestAnUnknownQuestionKindIsAnError covers the wire shape: a recorded
// question whose kind the reader does not know is an error, not a
// silently empty kind.
func TestAnUnknownQuestionKindIsAnError(t *testing.T) {
	var q decide.Question
	err := json.Unmarshal([]byte(`{"id":"q1","instructions":"x","kind":"boolx"}`), &q)
	if err == nil {
		t.Fatal("unmarshaling an unknown kind returned no error")
	}
	if !strings.Contains(err.Error(), "unknown kind") {
		t.Errorf("the error for an unknown kind is %q, want it to name the kind", err)
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
// that does not leaves the count unreported.
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
	if records[0].Requests == nil {
		t.Fatal("the record has no request count, want 3")
	}
	if *records[0].Requests != 3 {
		t.Errorf("the record's request count is %d, want 3", *records[0].Requests)
	}

	// A decider that does not report a count leaves it unreported, and
	// the record says so: a reader can tell "no requests were made"
	// from "the count was never reported".
	sink2 := &recordingSink{}
	recorder2 := decide.Recorded(plainDecider{}, sink2, "the caller")
	if _, err := recorder2.Decide(context.Background(), nil, "", nil); err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if got := sink2.records[0].Requests; got != nil {
		t.Errorf("the record's request count is %d, want unreported", *got)
	}
}

// TestTheUnreportedRequestCountIsAbsentFromTheRecord covers the wire
// shape: an unreported count is absent, not a zero.
func TestTheUnreportedRequestCountIsAbsentFromTheRecord(t *testing.T) {
	sink := &recordingSink{}
	recorder := decide.Recorded(plainDecider{}, sink, "the caller")
	if _, err := recorder.Decide(context.Background(), nil, "", nil); err != nil {
		t.Fatalf("deciding: %v", err)
	}
	raw, err := json.Marshal(sink.records[0])
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if _, ok := decoded["requests"]; ok {
		t.Errorf("the record has a request count, want none: %v", decoded)
	}

	// A reported count of zero is a count, and it is in the record.
	sink2 := &recordingSink{}
	recorder2 := decide.Recorded(countingDecider{requests: 0}, sink2, "the caller")
	if _, err := recorder2.Decide(context.Background(), nil, "", nil); err != nil {
		t.Fatalf("deciding: %v", err)
	}
	raw2, err := json.Marshal(sink2.records[0])
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded2 map[string]any
	if err := json.Unmarshal(raw2, &decoded2); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded2["requests"] != float64(0) {
		t.Errorf("the record's request count is %v, want 0", decoded2["requests"])
	}
}

// recordingSink keeps the records it receives.
type recordingSink struct {
	// records holds the records, in order.
	records []decide.Decision
}

// Record keeps one record.
func (s *recordingSink) Record(ctx context.Context, rec decide.Decision) error {
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

// plainDecider is a decider that does not report a request count.
type plainDecider struct{}

// Decide answers nothing.
func (d plainDecider) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	return nil, nil
}

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

// TestTheQuestionKindSurvivesARecordRoundTrip covers the record's
// questions: a recorded decision has to be re-readable, and a question
// that lost its kind is an id and some prose.
func TestTheQuestionKindSurvivesARecordRoundTrip(t *testing.T) {
	questions := []decide.Question{
		{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "it is a finding", False: "it is not"}},
		{ID: "q2", Instructions: "which one?", Kind: decide.Choice{Options: []decide.Option{
			{Name: "alpha", Desc: "the first one"},
			{Name: "beta", Desc: "the second one"},
		}}},
		{ID: "q3", Instructions: "how bad?", Kind: decide.Score{Levels: []string{"low", "mid", "high"}}},
	}

	rec := decide.Decision{
		AskedBy:   "hook:policy",
		Questions: questions,
		StateHash: "abc",
		Outcome:   decide.OutcomeOK,
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var back decide.Decision
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(back.Questions) != len(questions) {
		t.Fatalf("the record holds %d questions, want %d", len(back.Questions), len(questions))
	}

	for i, want := range questions {
		got := back.Questions[i]
		if got.ID != want.ID {
			t.Errorf("question %d has the id %q, want %q", i, got.ID, want.ID)
		}
		if got.Instructions != want.Instructions {
			t.Errorf("question %d has the instructions %q, want %q", i, got.Instructions, want.Instructions)
		}
		if got.Kind == nil {
			t.Fatalf("question %d came back without a kind", i)
		}
		switch w := want.Kind.(type) {
		case decide.Bool:
			g, ok := got.Kind.(decide.Bool)
			if !ok {
				t.Errorf("question %d came back as a %T, want a bool", i, got.Kind)
				continue
			}
			if g.True != w.True || g.False != w.False {
				t.Errorf("question %d came back as %+v, want %+v", i, g, w)
			}
		case decide.Choice:
			g, ok := got.Kind.(decide.Choice)
			if !ok {
				t.Errorf("question %d came back as a %T, want a choice", i, got.Kind)
				continue
			}
			if len(g.Options) != len(w.Options) {
				t.Errorf("question %d came back with %d options, want %d", i, len(g.Options), len(w.Options))
				continue
			}
			for j, opt := range w.Options {
				if g.Options[j].Name != opt.Name || g.Options[j].Desc != opt.Desc {
					t.Errorf("question %d's option %d came back as %+v, want %+v", i, j, g.Options[j], opt)
				}
			}
		case decide.Score:
			g, ok := got.Kind.(decide.Score)
			if !ok {
				t.Errorf("question %d came back as a %T, want a score", i, got.Kind)
				continue
			}
			if len(g.Levels) != len(w.Levels) {
				t.Errorf("question %d came back with %d levels, want %d", i, len(g.Levels), len(w.Levels))
				continue
			}
			for j, level := range w.Levels {
				if g.Levels[j] != level {
					t.Errorf("question %d's level %d came back as %q, want %q", i, j, g.Levels[j], level)
				}
			}
		}
	}
}

// TestTheRecordedQuestionNamesItsKind covers the wire shape: the record
// says which kind the question was, so a reader does not have to guess.
func TestTheRecordedQuestionNamesItsKind(t *testing.T) {
	questions := []decide.Question{
		{ID: "q1", Instructions: "is it?", Kind: decide.Bool{True: "yes", False: "no"}},
		{ID: "q2", Instructions: "which one?", Kind: decide.Choice{Options: []decide.Option{{Name: "alpha"}}}},
		{ID: "q3", Instructions: "how bad?", Kind: decide.Score{Levels: []string{"low", "high"}}},
	}
	wantKinds := []string{"bool", "choice", "score"}

	for i, q := range questions {
		raw, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("encoding question %d: %v", i, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decoding question %d: %v", i, err)
		}
		if decoded["kind"] != wantKinds[i] {
			t.Errorf("question %d names the kind %v, want %q", i, decoded["kind"], wantKinds[i])
		}
	}
}

func TestAPointerKindIsRecordedLikeItsValue(t *testing.T) {
	// A caller may write the kind as a pointer. Validate accepts it, so
	// the record has to encode it too, or the decision is made and its
	// record is lost.
	for _, kind := range []decide.Kind{&decide.Bool{True: "yes", False: "no"}, &decide.Choice{Options: []decide.Option{{Name: "a"}, {Name: "b"}}}, &decide.Score{Levels: []string{"low", "mid", "high"}}} {
		q := decide.Question{ID: "q1", Instructions: "does this fit?", Kind: kind}
		data, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("encoding a question with kind %T: %v", kind, err)
		}
		var back decide.Question
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("decoding %s: %v", data, err)
		}
		if back.Kind == nil {
			t.Errorf("the question with kind %T came back with no kind: %s", kind, data)
		}
	}
}

// TestANilPointerKindIsInvalidAndDoesNotPanic covers a typed-nil kind: it
// satisfies Kind, so only a check on the pointer catches it.
func TestANilPointerKindIsInvalidAndDoesNotPanic(t *testing.T) {
	kinds := map[string]decide.Kind{
		"bool":   (*decide.Bool)(nil),
		"choice": (*decide.Choice)(nil),
		"score":  (*decide.Score)(nil),
	}
	for name, kind := range kinds {
		t.Run(name, func(t *testing.T) {
			q := decide.Question{ID: "q1", Instructions: "which?", Kind: kind}
			if err := q.Validate(); !errors.Is(err, decide.ErrInvalidQuestion) {
				t.Errorf("the validation returned %v, want ErrInvalidQuestion", err)
			}
			if _, err := json.Marshal(q); err == nil {
				t.Error("the question encoded, want an error")
			}
		})
	}
}
