package main

import (
	"fmt"
	"io"
	"os"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/skill"
)

// promptOptions are the flags of `poo prompt`.
type promptOptions struct {
	sharedFlags
	// projectPrompt includes the working directory's project
	// instructions in the prompt.
	projectPrompt bool
	// pretty formats the prompt with section headers and summarized
	// skills for human reading instead of the raw wire prompt.
	pretty bool
}

// bind reads the prompt command's flags out of a parsed invocation.
func (o *promptOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.projectPrompt = inv.Bools["project-prompt"]
	o.pretty = inv.Bools["pretty"]
}

// handlePrompt executes `poo prompt`.
//
// By default it prints the exact rendered system prompt a session would send
// to the model. With --pretty it formats each section under a header and
// summarizes the skill catalog for reading.
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
	builder := poo.PromptBuilder(runtime.Options)
	if !o.pretty {
		_, _ = io.WriteString(stdout, builder.Render())
		return exit{}
	}
	writePrettyPrompt(stdout, builder.Sections(), runtime.Skills)
	return exit{}
}

// writePrettyPrompt writes the sections under readable headers, replacing the
// default skills catalog XML with the skill set's compact summary.
func writePrettyPrompt(w io.Writer, sections []prompt.SectionText, skills *skill.Set) {
	for _, section := range sections {
		text := section.Text
		if section.Name == skill.SectionName && skills != nil && skills.Len() > 0 && text == skills.Section() {
			text = skills.Summary()
		}
		fmt.Fprintf(w, "--- %s ---\n%s\n", section.Name, text)
	}
}
