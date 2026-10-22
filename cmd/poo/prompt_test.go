package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The prompt the command sends is the operating contract the model
// works under, and the sections it is assembled from render it byte
// for byte. The base was redrafted in Stage 5, and the notes of that
// stage hold the prompt before and after the change.

// renderPrompt renders the prompt the command builds.
//
// The session renders the prompt in the environment it works in, and
// the environment holds the tools the model may call. A tool's section
// renders only when the tool is linked, so the environment names it.
func renderPrompt(modelID string, tools []poo.ToolSpec, toolsMode, project string) string {
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
	systemPrompt(modelID, tools, toolsMode, project)(b)
	return b.Render()
}

// promptNames returns the names of the sections the command's prompt
// renders, in render order.
func promptNames(modelID string, tools []poo.ToolSpec, toolsMode, project string) []string {
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
	systemPrompt(modelID, tools, toolsMode, project)(b)
	return b.Names()
}

// testTools returns the tool specs the command links.
func testTools() []poo.ToolSpec {
	return []poo.ToolSpec{
		{Name: "bash", Description: "Run a shell command in the working directory and return its output."},
		{Name: "read", Description: "Read a range of lines from a file."},
	}
}

// TestTheNativeModePromptIsTheContract covers the prompt's bytes: the
// sections render the operating contract the command sends.
func TestTheNativeModePromptIsTheContract(t *testing.T) {
	want := "You are a coding agent. You solve the task by inspecting the code and changing what is needed.\n" +
		"\n" +
		"How to work:\n" +
		"- Inspect before you edit. Read the code around the place you will change.\n" +
		"- Prefer narrow reads over whole files: read the lines that matter and search for what you need, rather than reading everything.\n" +
		"- Make the smallest change that solves the task, then check that it works.\n" +
		"- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n" +
		"- Keep going until the task is done. Do not stop to ask whether you should go on.\n" +
		"\n" +
		"Done means:\n" +
		"- The task is actually complete, not just started.\n" +
		"- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n" +
		"- You then stop and say what you did, in a few sentences. Do not keep going after the task is done.\n" +
		"\n" +
		"Bash:\n" +
		"- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n" +
		"- Background processes are killed when the command returns.\n" +
		"- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.\n" +
		"\n" +
		"You are running as the model gemma4.\n"

	got := renderPrompt("gemma4", testTools(), "native", "")
	if got != want {
		t.Errorf("the prompt is\n%q\nwant\n%q", got, want)
	}
}

// TestTheTextModePromptIsTheContract covers the same bytes for the
// text mode, which names the tools and the call format.
func TestTheTextModePromptIsTheContract(t *testing.T) {
	want := "You are a coding agent. You solve the task by inspecting the code and changing what is needed.\n" +
		"\n" +
		"How to work:\n" +
		"- Inspect before you edit. Read the code around the place you will change.\n" +
		"- Prefer narrow reads over whole files: read the lines that matter and search for what you need, rather than reading everything.\n" +
		"- Make the smallest change that solves the task, then check that it works.\n" +
		"- Work out what is wrong before you try to fix it. If a command fails, read the error and adapt.\n" +
		"- Keep going until the task is done. Do not stop to ask whether you should go on.\n" +
		"\n" +
		"Done means:\n" +
		"- The task is actually complete, not just started.\n" +
		"- You have verified the change, for example by running the project's tests or the command that exercises the behaviour.\n" +
		"- You then stop and say what you did, in a few sentences. Do not keep going after the task is done.\n" +
		"\n" +
		"Bash:\n" +
		"- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one, so use absolute paths or start each command where you want it.\n" +
		"- Background processes are killed when the command returns.\n" +
		"- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.\n" +
		"\n" +
		"You are running as the model gemma4.\n" +
		"\n" +
		"Tools:\n" +
		"- bash: Run a shell command in the working directory and return its output.\n" +
		"- read: Read a range of lines from a file.\n" +
		"\n" +
		"Call a tool by writing a <tool_call> block holding a JSON object with a name and an arguments object, for example:\n" +
		"<tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls -la\"}}</tool_call>\n" +
		"Call one tool at a time, and wait for its result before the next call.\n"

	got := renderPrompt("gemma4", testTools(), "text", "")
	if got != want {
		t.Errorf("the prompt is\n%q\nwant\n%q", got, want)
	}
}

// TestThePromptIsTheSameEveryTime covers the rule that the prompt is
// deterministic: the same sections render the same text.
func TestThePromptIsTheSameEveryTime(t *testing.T) {
	first := renderPrompt("gemma4", testTools(), "text", "")
	for i := 0; i < 4; i++ {
		if got := renderPrompt("gemma4", testTools(), "text", ""); got != first {
			t.Fatalf("the prompt changed on the %d render", i)
		}
	}
}

// TestTheProjectSectionRendersAfterTheEnvironment covers the order of
// the sections: the project's instructions come after the
// environment, and the base comes first.
func TestTheProjectSectionRendersAfterTheEnvironment(t *testing.T) {
	want := []string{"base", "tools.bash", "environment", "project"}
	got := promptNames("gemma4", testTools(), "native", "the project's instructions")
	if !slices.Equal(got, want) {
		t.Errorf("the sections are %q, want %q", got, want)
	}
}

// TestTheProjectSectionIsNotThereWithoutAFile covers the rule that a
// directory with no project file holds no project section.
func TestTheProjectSectionIsNotThereWithoutAFile(t *testing.T) {
	want := []string{"base", "tools.bash", "environment"}
	got := promptNames("gemma4", testTools(), "native", "")
	if !slices.Equal(got, want) {
		t.Errorf("the sections are %q, want %q", got, want)
	}
}

// TestTheProjectSectionHoldsTheInstructions covers the rule that the
// project's instructions are the section's text.
func TestTheProjectSectionHoldsTheInstructions(t *testing.T) {
	got := renderPrompt("gemma4", testTools(), "native", "the project's instructions")
	if !strings.Contains(got, "the project's instructions") {
		t.Errorf("the prompt holds no project instructions: %q", got)
	}
}

// TestTheBaseSectionIsTheSameInEveryVariant covers the rule that a
// section that offers no alternative for the variant renders its
// default text.
func TestTheBaseSectionIsTheSameInEveryVariant(t *testing.T) {
	base := renderPrompt("gemma4", testTools(), "native", "")

	for _, variant := range []string{"", "default", "small-local"} {
		env := prompt.Env{
			Cwd:     "/work",
			OS:      "linux",
			Date:    "2026-02-03",
			Model:   "gemma4",
			Tools:   []string{"bash", "read"},
			Variant: variant,
		}
		b := prompt.New(env)
		systemPrompt("gemma4", testTools(), "native", "")(b)
		if got := b.WithVariant(variant).Render(); got != base {
			t.Errorf("the prompt in the %q variant is\n%q\nwant\n%q", variant, got, base)
		}
	}
}
