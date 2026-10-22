package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/read"
)

// The prompt a run sends is the operating contract the model works
// under, and it is assembled from the core's section, the tools'
// sections and the command's own. The tests drive a session, so what
// they pin is what a user's run actually sends.

// testTools returns the tool specs the command links.
func testTools() []poo.ToolSpec {
	return []poo.ToolSpec{
		{Name: "bash", Description: "Run a shell command in the working directory and return its output."},
		{Name: "read", Description: "Read a range of lines from a file."},
	}
}

// sessionPrompt runs one turn of a session assembled the way the command
// assembles it, and returns the system prompt the request carried.
func sessionPrompt(t *testing.T, modelID, toolsMode, project string) string {
	t.Helper()
	return sessionPromptWith(t, modelID, testTools(), toolsMode, project)
}

// sessionPromptWith is sessionPrompt with the tool specs the text mode
// names.
func sessionPromptWith(t *testing.T, modelID string, toolSpecs []poo.ToolSpec, toolsMode, project string) string {
	t.Helper()
	tr := pootest.NewTransport(pootest.Turn{Text: "done"})
	s, err := poo.New(poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"bash": bash.New(), "read": read.New()},
		ToolOrder:  []string{"bash", "read"},
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: modelID, ContextLimit: 32768},
		WorkingDir: t.TempDir(),
		Env:        os.Environ(),
		ToolsMode:  toolsMode,
	}, poo.WithPrompt(systemPrompt(modelID, toolSpecs, toolsMode, project)))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	runID, err := s.Prompt(ctx, "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.WaitRun(ctx, runID); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	requests := tr.Requests()
	if len(requests) == 0 {
		t.Fatal("the transport got no request")
	}
	for _, message := range requests[0].Messages {
		if message.Role == poo.RoleSystem {
			return message.Text()
		}
	}
	t.Fatal("the request carries no system prompt")
	return ""
}

// TestTheNativeModePromptIsTheContract covers the prompt a native-mode
// run sends: the core's base, the bash and read sections, the core's jobs
// section, and the command's environment.
func TestTheNativeModePromptIsTheContract(t *testing.T) {
	prompt := sessionPrompt(t, "gemma4", "native", "")

	for _, want := range []string{
		"You are a coding agent. You solve the task by inspecting the code and changing what is needed.",
		"Done means:",
		"Bash:",
		"Every bash call runs in a fresh shell.",
		`"background": true`,
		"Read:",
		"Background jobs:",
		"You are running as the model gemma4.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Tools:") {
		t.Errorf("the native-mode prompt describes the tools:\n%s", prompt)
	}
	// The sections render in the plan's order.
	order := []string{
		"You are a coding agent.",
		"Bash:",
		"Read:",
		"Background jobs:",
		"You are running as the model gemma4.",
	}
	last := -1
	for _, section := range order {
		at := strings.Index(prompt, section)
		if at < 0 {
			t.Fatalf("the prompt does not hold %q:\n%s", section, prompt)
		}
		if at < last {
			t.Errorf("%q renders before the section ahead of it:\n%s", section, prompt)
		}
		last = at
	}
}

// TestTheTextModePromptIsTheContract covers the same prompt in the text
// mode, which names the tools and the call format.
func TestTheTextModePromptIsTheContract(t *testing.T) {
	prompt := sessionPrompt(t, "gemma4", "text", "")

	for _, want := range []string{
		"Bash:",
		"Read:",
		"Background jobs:",
		"Tools:",
		"- bash: Run a shell command in the working directory and return its output.",
		"- read: Read a range of lines from a file.",
		"<tool_call>",
		"You are running as the model gemma4.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, prompt)
		}
	}
}

// TestThePromptIsTheSameEveryTime covers the rule that the prompt is
// deterministic: the same sections render the same text.
func TestThePromptIsTheSameEveryTime(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "one"}, pootest.Turn{Text: "two"})
	s, err := poo.New(poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"bash": bash.New(), "read": read.New()},
		ToolOrder:  []string{"bash", "read"},
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "gemma4", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
		Env:        os.Environ(),
	}, poo.WithPrompt(systemPrompt("gemma4", testTools(), "native", "")))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		runID, err := s.Prompt(ctx, "again")
		if err != nil {
			t.Fatalf("starting run %d: %v", i, err)
		}
		if _, err := s.WaitRun(ctx, runID); err != nil {
			t.Fatalf("waiting for run %d: %v", i, err)
		}
	}
	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(requests))
	}
	first := systemText(t, requests[0])
	second := systemText(t, requests[1])
	if first != second {
		t.Errorf("the prompt changed between runs:\n%q\n%q", first, second)
	}
}

// systemText returns a request's system prompt.
func systemText(t *testing.T, req poo.ChatRequest) string {
	t.Helper()
	for _, message := range req.Messages {
		if message.Role == poo.RoleSystem {
			return message.Text()
		}
	}
	t.Fatal("the request carries no system prompt")
	return ""
}

// TestTheProjectSectionRendersAfterTheEnvironment covers the order of
// the sections: the project's instructions come after the environment.
func TestTheProjectSectionRendersAfterTheEnvironment(t *testing.T) {
	prompt := sessionPrompt(t, "gemma4", "native", "the project's instructions")
	environment := strings.Index(prompt, "You are running as the model gemma4.")
	project := strings.Index(prompt, "the project's instructions")
	if environment < 0 || project < 0 {
		t.Fatalf("the prompt is missing a section:\n%s", prompt)
	}
	if project < environment {
		t.Errorf("the project renders before the environment:\n%s", prompt)
	}
}

// TestTheProjectSectionIsNotThereWithoutAFile covers the rule that a
// directory with no project file holds no project section.
func TestTheProjectSectionIsNotThereWithoutAFile(t *testing.T) {
	if prompt := sessionPrompt(t, "gemma4", "native", ""); strings.Contains(prompt, "the project's instructions") {
		t.Errorf("the prompt holds a project section:\n%s", prompt)
	}
}

// TestTheProjectSectionHoldsTheInstructions covers the rule that the
// project's instructions are the section's text.
func TestTheProjectSectionHoldsTheInstructions(t *testing.T) {
	prompt := sessionPrompt(t, "gemma4", "native", "the project's instructions")
	if !strings.Contains(prompt, "the project's instructions") {
		t.Errorf("the prompt holds no project instructions: %q", prompt)
	}
}
