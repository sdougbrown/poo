package main

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The prompt the command sends is the prompt the eval baseline was
// measured with, so the sections it is assembled from have to render the
// same text, byte for byte.

// renderPrompt renders the prompt the command builds.
//
// The session renders the prompt in the environment it works in, and
// the environment holds the tools the model may call. A tool's section
// renders only when the tool is linked, so the environment names it.
func renderPrompt(modelID string, tools []poo.ToolSpec, toolsMode string) string {
	env := prompt.Env{
		Cwd:   "/work",
		OS:    "linux",
		Date:  "2026-02-03",
		Model: modelID,
	}
	for _, t := range tools {
		env.Tools = append(env.Tools, t.Name)
	}
	b := prompt.New(env)
	systemPrompt(modelID, tools, toolsMode)(b)
	return b.Render()
}

// testTools returns the tool specs the command links.
func testTools() []poo.ToolSpec {
	return []poo.ToolSpec{
		{Name: "bash", Description: "Run a shell command in the working directory and return its output."},
		{Name: "read", Description: "Read a range of lines from a file."},
	}
}

// TestTheNativeModePromptIsTheTextTheCommandSentBefore covers the
// prompt's bytes: the sections render the text the command sent before
// the sections existed.
//
// The eval baseline was measured with that text, so a change in it is a
// change in what the model reads.
func TestTheNativeModePromptIsTheTextTheCommandSentBefore(t *testing.T) {
	want := "You are a coding agent working in a shell. You solve the task by inspecting the repository and changing what is needed.\n" +
		"\n" +
		"How to work:\n" +
		"- Inspect before you edit. Read the files that matter and the code around the place you will change.\n" +
		"- Prefer narrow reads over whole files. Use the read tool for a specific line range, and use bash with grep, sed, head or tail when you need to search or slice.\n" +
		"- Make the smallest change that solves the task, then check that it works.\n" +
		"- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n" +
		"\n" +
		"Bash:\n" +
		"- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n" +
		"- Background processes are killed when the command returns.\n" +
		"- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.\n" +
		"\n" +
		"Done means:\n" +
		"- The task is actually complete, not just started.\n" +
		"- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n" +
		"- You then stop and say what you did, in a few sentences. Do not keep going after the task is done.\n" +
		"\n" +
		"You are running as the model gemma4.\n"

	got := renderPrompt("gemma4", testTools(), "native")
	if got != want {
		t.Errorf("the prompt is\n%q\nwant\n%q", got, want)
	}
}

// TestTheTextModePromptIsTheTextTheCommandSentBefore covers the same
// bytes for the text mode, which names the tools and the call format.
func TestTheTextModePromptIsTheTextTheCommandSentBefore(t *testing.T) {
	want := "You are a coding agent working in a shell. You solve the task by inspecting the repository and changing what is needed.\n" +
		"\n" +
		"How to work:\n" +
		"- Inspect before you edit. Read the files that matter and the code around the place you will change.\n" +
		"- Prefer narrow reads over whole files. Use the read tool for a specific line range, and use bash with grep, sed, head or tail when you need to search or slice.\n" +
		"- Make the smallest change that solves the task, then check that it works.\n" +
		"- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n" +
		"\n" +
		"Bash:\n" +
		"- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n" +
		"- Background processes are killed when the command returns.\n" +
		"- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.\n" +
		"\n" +
		"Tools:\n" +
		"- bash: Run a shell command in the working directory and return its output.\n" +
		"- read: Read a range of lines from a file.\n" +
		"\n" +
		"Call a tool by writing a <tool_call> block holding a JSON object with a name and an arguments object, for example:\n" +
		"<tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls -la\"}}</tool_call>\n" +
		"Call one tool at a time, and wait for its result before the next call.\n" +
		"\n" +
		"Done means:\n" +
		"- The task is actually complete, not just started.\n" +
		"- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n" +
		"- You then stop and say what you did, in a few sentences. Do not keep going after the task is done.\n" +
		"\n" +
		"You are running as the model gemma4.\n"

	got := renderPrompt("gemma4", testTools(), "text")
	if got != want {
		t.Errorf("the prompt is\n%q\nwant\n%q", got, want)
	}
}

// TestThePromptIsTheSameEveryTime covers the rule that the prompt is
// deterministic: the same sections render the same text.
func TestThePromptIsTheSameEveryTime(t *testing.T) {
	first := renderPrompt("gemma4", testTools(), "text")
	for i := 0; i < 4; i++ {
		if got := renderPrompt("gemma4", testTools(), "text"); got != first {
			t.Fatalf("the prompt changed on the %d render", i)
		}
	}
}
