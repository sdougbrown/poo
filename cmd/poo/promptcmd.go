package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/prompt"
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
	builder := poo.PromptBuilder(runtime.Options)
	if !o.pretty {
		_, _ = io.WriteString(stdout, builder.Render())
		return exit{}
	}
	writePrettyPrompt(stdout, builder.Sections())
	return exit{}
}

// writePrettyPrompt writes the sections under readable headers, summarizing
// nested skill catalog XML as a compact list.
func writePrettyPrompt(w io.Writer, sections []prompt.SectionText) {
	for _, section := range sections {
		text := section.Text
		if section.Name == "skills" {
			text = prettySkillsSection(text)
		}
		fmt.Fprintf(w, "--- %s ---\n%s\n", section.Name, text)
	}
}

// prettySkillsSection replaces an <available_skills> XML block with a compact
// list of skill names and descriptions.
func prettySkillsSection(text string) string {
	const openTag = "<available_skills>"
	const closeTag = "</available_skills>"
	start := strings.Index(text, openTag)
	if start < 0 {
		return text
	}
	end := strings.Index(text[start:], closeTag)
	if end < 0 {
		return text
	}
	end += start + len(closeTag)

	body := text[start+len(openTag) : end-len(closeTag)]
	var skills []string
	for {
		s := strings.Index(body, "<skill>")
		if s < 0 {
			break
		}
		body = body[s+len("<skill>"):]
		e := strings.Index(body, "</skill>")
		if e < 0 {
			break
		}
		entry := body[:e]
		body = body[e+len("</skill>"):]

		name := xmlTagContent(entry, "name")
		desc := xmlTagContent(entry, "description")
		if name == "" {
			continue
		}
		if desc != "" {
			skills = append(skills, fmt.Sprintf("- %s: %s", name, desc))
		} else {
			skills = append(skills, fmt.Sprintf("- %s", name))
		}
	}
	if len(skills) == 0 {
		return text
	}

	prefix := strings.TrimRight(text[:start], "\n")
	suffix := strings.TrimLeft(text[end:], "\n")
	var b strings.Builder
	if prefix != "" {
		b.WriteString(prefix)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Available skills (%d):\n", len(skills))
	b.WriteString(strings.Join(skills, "\n"))
	if suffix != "" {
		b.WriteString("\n\n")
		b.WriteString(suffix)
	}
	return b.String()
}

// xmlTagContent returns the trimmed text inside <tag>...</tag>, or "".
func xmlTagContent(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	s = s[start+len(open):]
	end := strings.Index(s, close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(s[:end])
}
