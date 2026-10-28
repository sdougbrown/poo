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
// rule: a run that settled end_turn without a single assistant message
// never answered, so the settle reason alone is not a passed check.
func TestTheJudgeDoesNotPassARunThatSaidNothing(t *testing.T) {
	settled := LogRecord{Record: "run.settled", Reason: string(poo.ReasonEndTurn)}
	cases := []struct {
		name    string
		records []LogRecord
		want    bool
	}{
		{
			name:    "an empty-turn bound",
			records: []LogRecord{settled},
		},
		{
			name: "an answer",
			records: []LogRecord{
				{Record: "message", Origin: "model", Role: "assistant"},
				settled,
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			judge := &Judge{}
			payload := judge.buildPayload(&Log{Records: c.records}, "the transcript",
				transcript{task: "long-find-error-in-log", model: "gemma4", seed: 1}, nil)
			if payload.CheckPassed != c.want {
				t.Errorf("the judge read the run as check_passed=%v, want %v", payload.CheckPassed, c.want)
			}
		})
	}
}
