package handoff

import (
	_ "embed"
	"strings"
)

// The instruction the fork appends lives as markdown beside the package,
// and scripts/lint-prompts.sh lints it as an agent instruction.
//
//go:embed prompts/instruction.md
var instructionText string

// instruction is the message the fork appends to the last request.
//
// It asks for the note and forbids a tool call: the note is the run's
// last word, and nothing it asked for could be carried out anyway.
var instruction = strings.TrimSpace(instructionText)
