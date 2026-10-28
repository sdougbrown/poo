package eval

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestAFindTranscriptRefusesAMissingRunsDirectory covers a mistyped
// results path: a missing runs directory is an error, not an empty set
// of verdicts.
func TestAFindTranscriptRefusesAMissingRunsDirectory(t *testing.T) {
	judge := &Judge{opts: JudgeOptions{ResultsDir: t.TempDir()}}
	if _, err := judge.findTranscripts(); err == nil {
		t.Fatal("the missing runs directory was accepted")
	}
}

// TestTheVerdictFileNameIsFlat covers a model id that carries a path
// separator: the verdict file lives flat in one directory.
func TestTheVerdictFileNameIsFlat(t *testing.T) {
	name := verdictFileName(Verdict{Task: "fix-a", Model: "org/model", Seed: 1})
	if strings.Contains(name, "/") {
		t.Errorf("the verdict file name %q carries a path separator", name)
	}
}

// TestTheJudgeDoesNotPassARunThatSaidNothing covers the judge's no-report
// rule: only a run that settled end_turn answered, so a run that settled
// any other reason, or that recorded no assistant message, is not a
// passed check. A model that kept producing invalid turns settles
// invalid_turns, and the judge must not read that as a pass.
func TestTheJudgeDoesNotPassARunThatSaidNothing(t *testing.T) {
	cases := []struct {
		name    string
		reason  poo.Reason
		records []LogRecord
		want    bool
	}{
		{
			name:   "end_turn without an answer",
			reason: poo.ReasonEndTurn,
		},
		{
			name:   "an answer",
			reason: poo.ReasonEndTurn,
			records: []LogRecord{
				{Record: "message", Origin: "model", Role: "assistant"},
			},
			want: true,
		},
		{
			name:   "an invalid-turn bound",
			reason: poo.ReasonInvalidTurns,
		},
		{
			name:   "an invalid-turn bound after an answer",
			reason: poo.ReasonInvalidTurns,
			records: []LogRecord{
				{Record: "message", Origin: "model", Role: "assistant"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			records := append([]LogRecord{{Record: "run.settled", Reason: string(c.reason)}}, c.records...)
			judge := &Judge{}
			payload := judge.buildPayload(&Log{Records: records}, "the transcript",
				transcript{task: "long-find-error-in-log", model: "gemma4", seed: 1}, nil)
			if payload.CheckPassed != c.want {
				t.Errorf("the judge read the run as check_passed=%v, want %v", payload.CheckPassed, c.want)
			}
		})
	}
}
