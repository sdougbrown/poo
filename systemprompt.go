package poo

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"time"

	"github.com/sdougbrown/poo/prompt"
)

// The system prompt is assembled from named sections and rendered once,
// when the session is built: the model, the tools, the working directory,
// the variant and the consumer's own changes are all known by then. The
// session keeps the rendered text, so a run carries a prompt that already
// rendered and a loaded file that cannot render fails the session instead
// of a run.

// ErrPromptRender reports that a prompt section did not render.
//
// A loaded prompt file's template can fail on the data the session
// supplies, which the load-time check does not prove either way. The
// prompt renders when the session is built, so the failure refuses the
// session and the error names the file to fix.
var ErrPromptRender = errors.New("a prompt section did not render")

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
		Model:   opts.Model.ModelID,
		Tools:   tools,
		Variant: opts.PromptVariant,
	}
}

// baseSection is the operating contract the model works under, as the
// prompt package embeds it.
//
// It is the shape mini-swe-agent uses: what the model is, how to work, and
// what "done" looks like. The guidance for a tool is not here: a tool
// brings a section of its own, and a tool that is not linked takes its
// guidance with it.
var baseSection = prompt.Base()

// environmentSection renders the environment section.
//
// The section tells the model where it is: the directory it works in,
// the operating system, the date and the model it talks to.
var environmentSection = prompt.Environment()

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
	b.SetTemplateDefault("base", baseSection)
	if opts.SystemPrompt != "" {
		b.Set("base", opts.SystemPrompt)
	} else {
		b.SetTemplateDefault("environment", environmentSection)
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

// PromptBuilder returns the prompt builder a session with these options
// would assemble.
//
// The builder is the session's own: the core's own section, the
// sections the linked tools bring and the consumer's changes, in the
// order they happen. A consumer that shows the prompt to a person uses
// this, so the text the person reads is the text the model gets.
func PromptBuilder(opts Options) *prompt.Builder {
	return buildPrompt(&opts)
}

// CheckPrompt reports whether a session with these options has a prompt
// that renders.
//
// A build calls it, so a loaded file that cannot render on the build's own
// data fails the build before a session is opened.
func CheckPrompt(opts Options) error {
	_, _, err := renderPrompt(buildPrompt(&opts), opts.SystemPrompt)
	return err
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

// renderPrompt renders a builder in the variant it holds and returns the
// prompt's text and the names of its sections, in render order.
//
// A raw prompt is the whole prompt, byte for byte. The builder adds a
// trailing newline to the sections it renders; that newline belongs to the
// rendered form, not to a raw prompt the consumer supplied.
func renderPrompt(b *prompt.Builder, raw string) (string, []string, error) {
	text, err := b.RenderError()
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrPromptRender, err)
	}
	return rawPromptText(text, raw), b.Names(), nil
}

// rawPromptText returns the text a render produced, after the raw prompt's
// rule: a prompt the consumer supplied whole stays that text.
func rawPromptText(rendered, raw string) string {
	if raw != "" && (rendered == raw || rendered == raw+"\n") {
		return raw
	}
	return rendered
}

// renderPromptLocked renders the session's prompt and keeps it, for the
// session's runs.
//
// The builder renders in the session's variant, so the text is the one the
// next run carries.
func (s *Session) renderPromptLocked() error {
	text, sections, err := renderPrompt(s.promptBuilder.WithVariant(s.promptVariant), s.opts.SystemPrompt)
	if err != nil {
		return err
	}
	s.promptText = text
	s.promptSections = sections
	return nil
}

// runSystemPrompt returns the prompt the run carries and the variant it
// renders in.
//
// The session's prompt is the one rendered when the session was built, and
// a run reuses it as it is. A run whose hooks edited the prompt renders
// their copy: the edit surface sets texts and appends to them, so the copy
// holds only templates the session has already rendered in the same
// environment, and the render cannot fail.
func (s *Session) runSystemPrompt(beforeRun []SystemPromptEdit) (string, string) {
	s.mu.Lock()
	text, variant := s.promptText, s.promptVariant
	builder, raw := s.promptBuilder, s.opts.SystemPrompt
	s.mu.Unlock()
	if len(beforeRun) == 0 {
		return text, variant
	}
	// The session's builder is never changed in place, so the copy is
	// made and rendered without the session's lock: an edit that blocks
	// cannot hold up the cancel that ends the run.
	b := builder.WithVariant(variant)
	for _, edit := range beforeRun {
		if edit != nil {
			edit(b)
		}
	}
	return rawPromptText(b.Render(), raw), variant
}

// PromptState describes the session's system prompt.
//
// The text is the prompt the next run carries: it was rendered when the
// session was built, or when the last change to it was made.
type PromptState struct {
	// Text is the rendered prompt.
	Text string
	// Hash is the hash of the text.
	Hash string
	// Sections are the names of the prompt's sections, in order.
	Sections []string
	// Preview reports that no run has rendered the prompt yet.
	//
	// A session renders its prompt when it is built, so a constructed
	// session never reports one; the field stays for the clients that
	// read it.
	Preview bool
}

// PromptState returns the session's system prompt.
//
// Reading the state renders nothing: the text is the one the last render
// produced, and it is the prompt the next run carries.
func (s *Session) PromptState() PromptState {
	s.mu.Lock()
	text, sections := s.promptText, s.promptSections
	s.mu.Unlock()
	return PromptState{
		Text:     text,
		Hash:     hashText(text),
		Sections: slices.Clone(sections),
	}
}

// SetPromptVariant sets the variant the prompt renders in.
//
// The prompt renders again in the new variant, so a section that offers an
// alternative for it renders that one and a section that does not renders
// its default text. A prompt that cannot render in the variant refuses the
// change, and the session keeps the variant and the prompt it had.
func (s *Session) SetPromptVariant(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	variant := s.promptVariant
	s.promptVariant = name
	if err := s.renderPromptLocked(); err != nil {
		s.promptVariant = variant
		return err
	}
	return nil
}

// SetPrompt applies prompt changes to the session.
//
// The changes are applied in the order they are given, and a later one
// wins over an earlier one. The prompt renders again with them, so the
// change is in the next run's prompt. A change that leaves the prompt
// unable to render is refused, and the session keeps the prompt it had.
func (s *Session) SetPrompt(edits ...func(*prompt.Builder)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.promptBuilder.Copy()
	for _, edit := range edits {
		if edit != nil {
			edit(b)
		}
	}
	text, sections, err := renderPrompt(b.WithVariant(s.promptVariant), s.opts.SystemPrompt)
	if err != nil {
		return err
	}
	s.promptBuilder = b
	s.promptText = text
	s.promptSections = sections
	return nil
}

// promptChanged reports whether a rendered prompt differs from the last
// one written to the record.
func (s *Session) promptChanged(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return text != s.lastPromptText
}

// rememberPrompt records the prompt that was written, so an unchanged
// prompt is not written again.
func (s *Session) rememberPrompt(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPromptText = text
}

// applyPromptEdit applies a system-prompt edit to the session's prompt.
//
// The edit is the one a session-start hook's decision carries. The
// edit's failure propagates: a hook edit that breaks the prompt refuses
// the session, so the session never starts with a prompt that does not
// render.
func (s *Session) applyPromptEdit(edit SystemPromptEdit) error {
	if edit == nil {
		return nil
	}
	return s.SetPrompt(func(b *prompt.Builder) { edit(b) })
}
