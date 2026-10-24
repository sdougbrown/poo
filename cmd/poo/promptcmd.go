package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/prompt/agentsmd"
)

// promptOptions are the flags of `poo prompt`.
type promptOptions struct {
	sharedFlags
	// projectPrompt includes the working directory's project
	// instructions in the prompt.
	projectPrompt bool
}

// runPrompt executes `poo prompt`.
//
// The command prints the prompt a session would send, with the sections
// marked, so an operator can see what the model is told.
func runPrompt(args []string) int {
	fs := flag.NewFlagSet("poo prompt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o promptOptions
	o.register(fs)
	fs.BoolVar(&o.projectPrompt, "project-prompt", false, "Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := o.checkUnmatched(); err != nil {
		fmt.Fprintf(os.Stderr, "poo prompt: %v\n", err)
		return 2
	}

	settings, err := o.settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo prompt: %v\n", err)
		return 2
	}
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo prompt: %v\n", err)
		return 1
	}
	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo prompt: %v\n", err)
		return 2
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		Askers:         []string{"decide"},
		// The command shows a prompt, and a prompt does not need a
		// model: the prompt names the model, and a name may be empty.
		NoChatModel: true,
		Build:       poo.BuildInfo{Name: "poo", Version: version},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo prompt: %v\n", err)
		return 2
	}
	project := ""
	if o.projectPrompt {
		text, err := agentsmd.Load(workDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo prompt: --project-prompt: %v\n", err)
			return 2
		}
		project = text
	}
	model := runtime.Options.Model
	runtime.Options.Prompt = append(runtime.Options.Prompt, promptMutation(runtime, model, project))

	// The build rendered the prompt; the sections the command adds after
	// it are its own fixed templates, so the render cannot fail here.
	sections := poo.PromptBuilder(runtime.Options).Sections()
	for _, section := range sections {
		fmt.Printf("--- %s ---\n%s\n", section.Name, section.Text)
	}
	return 0
}
