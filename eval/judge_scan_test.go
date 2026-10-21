package eval

import (
	"strings"
	"testing"
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
