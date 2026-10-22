package poo

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/sdougbrown/poo/prompt"
)

// The system prompt is assembled from named sections and rendered once
// per run. A session keeps a builder with the sections in it, and every
// run renders a copy of that builder: the prompt a run carries never
// changes while the run goes on, and the next run may render a
// different one.

// systemPromptEnv returns the environment the session's prompt renders
// in.
//
// The environment holds what a dynamic section may read: where the
// session works, the operating system, the date, the model and the
// tools the model may call. The date is the session's date, in UTC, so
// two sessions on the same day read the same.
//
// The environment names the variant the prompt renders in, so a section
// that renders from the environment knows which variant it renders
// for.
func systemPromptEnv(opts *Options) prompt.Env {
	tools := make([]string, 0, len(opts.ToolOrder))
	tools = append(tools, opts.ToolOrder...)
	return prompt.Env{
		Cwd:     opts.WorkingDir,
		OS:      runtime.GOOS,
		Date:    time.Now().UTC().Format("2006-01-02"),
		Model:   opts.ModelID,
		Tools:   tools,
		Variant: opts.PromptVariant,
	}
}

// environmentSection renders the environment section.
//
// The section tells the model where it is: the directory it works in,
// the operating system, the date and the model it talks to.
func environmentSection(env prompt.Env) string {
	var b strings.Builder
	b.WriteString("Environment:\n")
	fmt.Fprintf(&b, "- Working directory: %s\n", env.Cwd)
	fmt.Fprintf(&b, "- Operating system: %s\n", env.OS)
	fmt.Fprintf(&b, "- Date: %s\n", env.Date)
	fmt.Fprintf(&b, "- Model: %s\n", env.Model)
	return strings.TrimRight(b.String(), "\n")
}

// buildPrompt assembles the session's prompt builder.
//
// The builder holds the core's own section and the sections the linked
// tools bring. A tool that needs guidance contributes its own section,
// named after the tool, so a tool that is not linked takes its guidance
// with it.
//
// A raw system prompt is the whole prompt: it is the builder's base
// section, and neither the core's own section nor the tools' are added.
// A later edit still has the base to change.
//
// The consumer's own changes come last, so they win over the core's.
func buildPrompt(opts *Options) *prompt.Builder {
	env := systemPromptEnv(opts)
	b := prompt.New(env)
	if opts.SystemPrompt != "" {
		b.Set("base", opts.SystemPrompt)
	} else {
		b.SetRender("environment", environmentSection)
		for _, name := range opts.ToolOrder {
			tool, ok := opts.Tools[name]
			if !ok || tool == nil {
				continue
			}
			contributor, ok := tool.(PromptContributor)
			if !ok {
				continue
			}
			b.SetDefault(prompt.ToolSection(name), contributor.PromptSection(env))
			if variantContributor, ok := tool.(PromptVariantContributor); ok {
				setPromptVariants(b, prompt.ToolSection(name), variantContributor.PromptVariants(env))
			}
		}
	}
	for _, edit := range opts.Prompt {
		if edit != nil {
			edit(b)
		}
	}
	return b
}

// setPromptVariants puts a tool's alternative texts in the builder.
//
// The names are sorted, so the builder holds the alternatives in the
// same order every time.
func setPromptVariants(b *prompt.Builder, section string, variants map[string]string) {
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b.SetVariant(section, name, variants[name])
	}
}

// renderSystemPrompt renders the prompt for a run.
//
// The run renders a copy of the session's builder, so the before-run
// hooks' edits reach the run that is starting and not the next one. The
// prompt is rendered once, here, and frozen for the length of the run.
func (s *Session) renderSystemPrompt(beforeRun []SystemPromptEdit) string {
	b := s.promptCopy()
	if b == nil {
		return ""
	}
	for _, edit := range beforeRun {
		if edit != nil {
			edit(b)
		}
	}
	rendered := b.Render()
	// A raw prompt is the whole prompt, byte for byte. The builder adds
	// a trailing newline to the sections it renders; that newline
	// belongs to the rendered form, not to a raw prompt the consumer
	// supplied.
	if raw := s.opts.SystemPrompt; raw != "" && (rendered == raw || rendered == raw+"\n") {
		return raw
	}
	return rendered
}

// SetPromptVariant sets the variant the prompt renders in.
//
// The prompt a run carries is rendered once, at the start of the run,
// so the change takes effect between the runs and never inside one.
//
// A variant picks the alternative text of the sections that offer one:
// the base section and the tools' sections. A section that offers no
// alternative for the variant renders its default text.
func (s *Session) SetPromptVariant(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptVariant = name
}

// promptCopy returns a copy of the session's prompt builder, in the
// variant the session renders in.
//
// The copy is what a run renders. A nil builder means the session has
// no prompt at all.
func (s *Session) promptCopy() *prompt.Builder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.promptBuilder == nil {
		return nil
	}
	return s.promptBuilder.WithVariant(s.promptVariant)
}

// applyPromptEdit applies a system-prompt edit to the session's prompt.
//
// The edit is the one a session-start hook's decision carries. The
// session's builder is the session's own, so the edit changes every run
// that follows.
func (s *Session) applyPromptEdit(edit SystemPromptEdit) {
	if edit == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.promptBuilder == nil {
		return
	}
	edit(s.promptBuilder)
}
