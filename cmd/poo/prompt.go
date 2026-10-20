package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// systemPrompt is the operating contract the model works under.
//
// The full prompt builder, with named sections and per-tool contributions,
// arrives in Stage 4. The skeleton ships one short contract, in the shape
// mini-swe-agent uses: how to work, read narrowly, every bash call is a
// fresh shell, and what "done" looks like.
func systemPrompt(modelID string) string {
	var b strings.Builder

	b.WriteString("You are a coding agent working in a shell. You solve the task by inspecting the repository and changing what is needed.\n\n")

	b.WriteString("How to work:\n")
	b.WriteString("- Inspect before you edit. Read the files that matter and the code around the place you will change.\n")
	b.WriteString("- Prefer narrow reads over whole files. Use the read tool for a specific line range, and use bash with grep, sed, head or tail when you need to search or slice.\n")
	b.WriteString("- Make the smallest change that solves the task, then check that it works.\n")
	b.WriteString("- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n\n")

	b.WriteString("Bash:\n")
	b.WriteString("- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n")
	b.WriteString("- Background processes are killed when the command returns.\n")
	b.WriteString("- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.\n\n")

	b.WriteString("Done means:\n")
	b.WriteString("- The task is actually complete, not just started.\n")
	b.WriteString("- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n")
	b.WriteString("- You then stop and say what you did, in a few sentences. Do not keep going after the task is done.\n")

	if modelID != "" {
		fmt.Fprintf(&b, "\nYou are running as the model %s.\n", modelID)
	}
	return b.String()
}

// newSessionID returns a fresh identifier for a session or a run.
//
// The identifier is random rather than a ULID: the plan's ULID arrives with
// the session seam in Stage 8, and nothing in the skeleton depends on the
// ordering.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A failing random source is not worth failing the run over; the
		// identifier only has to be unique within one log.
		return fmt.Sprintf("id-%d", len(b))
	}
	return hex.EncodeToString(b[:])
}
