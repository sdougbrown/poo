package main

import (
	"fmt"
	"io"
	"os"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// promptOptions are the flags of `poo prompt`.
type promptOptions struct {
	sharedFlags
	// projectPrompt includes the working directory's project
	// instructions in the prompt.
	projectPrompt bool
}

// bind reads the prompt command's flags out of a parsed invocation.
func (o *promptOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.projectPrompt = inv.Bools["project-prompt"]
}

// handlePrompt executes `poo prompt`.
//
// The command prints the prompt a session would send, with the sections
// marked, so an operator can see what the model is told.
func handlePrompt(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o promptOptions
	o.bind(inv)
	if err := o.checkUnmatched(); err != nil {
		fmt.Fprintf(stderr, "poo prompt: %v\n", err)
		return exit{usageExitCode}
	}

	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo prompt: %v\n", err)
		return exit{usageExitCode}
	}
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "poo prompt: %v\n", err)
		return exit{operationalExitCode}
	}
	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(stderr, "poo prompt: %v\n", err)
		return exit{usageExitCode}
	}
	overrides.ProjectPrompt = o.projectPrompt
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      config.EnvOverrides(overrides),
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		UserPromptFile: defaultUserPromptFile(),
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		Askers:         []string{"decide"},
		// The command shows a prompt, and a prompt does not need a
		// model: the prompt names the model, and a name may be empty.
		NoChatModel: true,
		Build:       poo.BuildInfo{Name: "poo", Version: buildVersion().String()},
	})
	if err != nil {
		fmt.Fprintf(stderr, "poo prompt: %v\n", err)
		return exit{usageExitCode}
	}
	model := runtime.Options.Model
	edits, err := runtime.SessionPrompt(workDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo prompt: %v\n", err)
		return exit{usageExitCode}
	}
	runtime.Options.Prompt = append(runtime.Options.Prompt, edits...)
	runtime.Options.Prompt = append(runtime.Options.Prompt, promptMutation(runtime, model))

	// The build rendered the prompt; the sections the command adds after
	// it are its own fixed templates, so the render cannot fail here.
	sections := poo.PromptBuilder(runtime.Options).Sections()
	for _, section := range sections {
		fmt.Fprintf(stdout, "--- %s ---\n%s\n", section.Name, section.Text)
	}
	return exit{}
}
