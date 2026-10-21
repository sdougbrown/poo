package poo

import (
	"strings"

	"github.com/sdougbrown/poo/prompt"
)

// promptSections implements the surface a hook edits the prompt
// through.
var _ prompt.Sections = (*promptSections)(nil)

// promptSections is the core's implementation of prompt.Sections.
//
// The core keeps the prompt's sections so a hook can change the prompt
// at the start of a run. The prompt package's own builder is the richer
// implementation, with the environment and the render order; this one
// holds what the core needs on its own: named text sections in render
// order.
type promptSections struct {
	// order holds the section names, in render order.
	order []string
	// texts holds the sections' texts, by name.
	texts map[string]string
}

// newPromptSections returns an empty section list.
func newPromptSections() *promptSections {
	return &promptSections{texts: map[string]string{}}
}

// Set replaces a section's text. A section that is not there yet is
// added.
func (s *promptSections) Set(name, text string) {
	if s.texts == nil {
		s.texts = map[string]string{}
	}
	if _, ok := s.texts[name]; !ok {
		s.order = append(s.order, name)
	}
	s.texts[name] = text
}

// Append adds to a section's text. A section that is not there yet is
// added.
func (s *promptSections) Append(name, text string) {
	if s.texts == nil {
		s.texts = map[string]string{}
	}
	if _, ok := s.texts[name]; !ok {
		s.order = append(s.order, name)
	}
	s.texts[name] += text
}

// Remove drops a section. Removing a section that is not there is not
// an error.
func (s *promptSections) Remove(name string) {
	if _, ok := s.texts[name]; !ok {
		return
	}
	delete(s.texts, name)
	for i, other := range s.order {
		if other == name {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// Insert adds a section after another one. A name that is not there
// puts the section first.
func (s *promptSections) Insert(after, name, text string) {
	if s.texts == nil {
		s.texts = map[string]string{}
	}
	if _, ok := s.texts[name]; ok {
		s.texts[name] = text
		return
	}
	pos := 0
	for i, other := range s.order {
		if other == after {
			pos = i + 1
			break
		}
	}
	s.order = append(s.order, "")
	copy(s.order[pos+1:], s.order[pos:])
	s.order[pos] = name
	s.texts[name] = text
}

// Replace takes over the whole prompt, dropping the sections.
//
// The replaced prompt is a section of its own, so a later edit still
// has somewhere to go.
func (s *promptSections) Replace(text string) {
	s.order = nil
	s.texts = map[string]string{}
	s.Set("", text)
}

// Names returns the section names, in render order.
func (s *promptSections) Names() []string {
	return append([]string(nil), s.order...)
}

// render renders the sections into the prompt text.
//
// The sections render in order, separated by a blank line, and an empty
// section renders as nothing.
func (s *promptSections) render() string {
	parts := make([]string, 0, len(s.order))
	for _, name := range s.order {
		text := strings.TrimSpace(s.texts[name])
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

// promptSectionsFor returns the sections a run's prompt starts from.
//
// The base is the session's own prompt text, in the "base" section, and
// a session with no prompt text has no sections at all.
func (s *Session) promptSectionsFor() *promptSections {
	sections := newPromptSections()
	if s.opts.SystemPrompt != "" {
		sections.Set("base", s.opts.SystemPrompt)
	}
	return sections
}

// renderPrompt renders the run's system prompt.
//
// The prompt is rendered once per run and frozen for the run: the base
// sections, the session-start edits and the before-run edits, in that
// order. A later edit wins over an earlier one.
func (s *Session) renderPrompt(beforeRun []SystemPromptEdit) string {
	sections := s.promptSectionsFor()
	for _, edit := range s.promptEdits {
		if edit != nil {
			edit(sections)
		}
	}
	for _, edit := range beforeRun {
		if edit != nil {
			edit(sections)
		}
	}
	return sections.render()
}
