package prompt

import "strings"

// Builder assembles a system prompt from named sections.
//
// A builder holds the sections, the order they render in and the
// environment a dynamic section reads. It is the surface every layer
// edits: a Go consumer edits it directly, a hook edits it through the
// Sections interface, and the core renders it once per run.
//
// The builder is not safe for concurrent use. A session keeps one
// builder and renders a copy of it per run, and the copy is what a
// run's own changes touch.
type Builder struct {
	// env is the environment a dynamic section reads.
	env Env
	// sections holds the sections, by name.
	sections map[string]Section
	// extra holds the text appended to a section, by name.
	//
	// The appended text is kept apart from the section's own value, so
	// a section that renders from the environment stays dynamic.
	extra map[string]string
	// order holds the names of the sections, in render order.
	order []string
	// replaced reports that the builder has taken over the prompt.
	replaced bool
	// text is the text that took the prompt over.
	text string
}

// toolSlot is the name of the place the per-tool sections render in.
//
// The name is reserved: a section with that name renders nothing of its
// own, because the place belongs to the tools.
const toolSlot = "tools"

// toolPrefix is the prefix a per-tool section's name carries.
const toolPrefix = "tools."

// New returns a builder for a prompt rendered in an environment.
//
// The builder starts with the default order and no sections: a section
// that was never set renders nothing.
func New(env Env) *Builder {
	return &Builder{
		env:      env,
		sections: map[string]Section{},
		extra:    map[string]string{},
		order:    Order(),
	}
}

// Env returns the environment the builder's dynamic sections read.
func (b *Builder) Env() Env { return b.env }

// Set replaces a section's text.
//
// A section that is not there yet is added. A section the default order
// knows goes at its place in that order, and a section of the
// consumer's own goes last, after the sections already there.
func (b *Builder) Set(name, text string) {
	b.dropReplacement()
	b.sections[name] = Section{Name: name, Text: text}
	delete(b.extra, name)
	b.place(name)
}

// Append adds text after a section's value.
//
// A section that is not there yet is added, with the text as its value.
// The text is kept apart from the section's own value, so appending to
// a section that renders from the environment leaves it dynamic.
func (b *Builder) Append(name, text string) {
	b.dropReplacement()
	if _, ok := b.sections[name]; !ok {
		b.sections[name] = Section{Name: name}
		b.extra[name] = text
		b.place(name)
		return
	}
	b.extra[name] += text
}

// SetRender sets a section's value to a function of the environment.
//
// The function is called every time the prompt is rendered, so the
// section follows the environment the prompt is rendered in.
func (b *Builder) SetRender(name string, render func(Env) string) {
	b.dropReplacement()
	b.sections[name] = Section{Name: name, Render: render}
	delete(b.extra, name)
	b.place(name)
}

// Remove drops a section.
//
// Removing a section that is not there is not an error. A tool's
// section goes away with the tool: a section named after a tool the
// environment does not hold is not rendered.
func (b *Builder) Remove(name string) {
	delete(b.sections, name)
	delete(b.extra, name)
	if !isToolSection(name) {
		b.order = removeName(b.order, name)
	}
}

// Insert puts a section right after another one.
//
// A section that is already there moves to the new place. An anchor
// that is not there puts the section first. A tool's section is
// anchored at the tools' place, so a section inserted after
// "tools.bash" follows every tool's section.
func (b *Builder) Insert(after, name, text string) {
	b.dropReplacement()
	b.sections[name] = Section{Name: name, Text: text}
	delete(b.extra, name)
	if isToolSection(name) {
		// The tool sections render together, in the order the
		// environment offers the tools, so the order does not hold
		// them.
		return
	}
	order := removeName(b.order, name)
	idx := indexOf(order, anchorName(after))
	if idx < 0 {
		b.order = append([]string{name}, order...)
		return
	}
	b.order = insertAt(order, idx+1, name)
}

// Replace takes over the whole prompt.
//
// The sections are dropped, and the text is the prompt. A section set
// afterwards puts the builder back in the sections' hands: the
// replacement is dropped, and the prompt is the sections again.
func (b *Builder) Replace(text string) {
	b.sections = map[string]Section{}
	b.extra = map[string]string{}
	b.order = Order()
	b.replaced = true
	b.text = text
}

// Names returns the names of the sections that render, in render order.
//
// A section named after a tool the environment does not hold is not
// among them: it does not render.
func (b *Builder) Names() []string {
	names := make([]string, 0, len(b.sections))
	for _, name := range b.order {
		if isToolSection(name) {
			continue
		}
		if name == toolSlot {
			if _, ok := b.sections[name]; ok {
				names = append(names, name)
			}
			names = append(names, b.toolNames()...)
			continue
		}
		if _, ok := b.sections[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// Render renders the prompt.
//
// The sections render in the order the builder holds, and their texts
// are joined with a blank line. A section with no text renders nothing.
// A prompt with no sections is empty.
func (b *Builder) Render() string {
	if b.replaced {
		return b.text
	}
	var blocks []string
	for _, name := range b.order {
		if isToolSection(name) {
			continue
		}
		if name == toolSlot {
			if text := b.sectionText(name); text != "" {
				blocks = append(blocks, text)
			}
			blocks = append(blocks, b.toolTexts()...)
			continue
		}
		if text := b.sectionText(name); text != "" {
			blocks = append(blocks, text)
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// Copy returns a copy of the builder.
//
// A copy is what a run renders: the run's prompt may change without
// the session's prompt changing.
func (b *Builder) Copy() *Builder {
	out := &Builder{
		env:      b.env,
		sections: make(map[string]Section, len(b.sections)),
		extra:    make(map[string]string, len(b.extra)),
		order:    append([]string(nil), b.order...),
		replaced: b.replaced,
		text:     b.text,
	}
	for name, section := range b.sections {
		out.sections[name] = section
	}
	for name, text := range b.extra {
		out.extra[name] = text
	}
	return out
}

// dropReplacement puts the builder back in the sections' hands.
//
// A builder that took the prompt over has no sections left, so a
// section set afterwards starts a new set of them.
func (b *Builder) dropReplacement() {
	if !b.replaced {
		return
	}
	b.replaced = false
	b.text = ""
}

// place puts a section in the render order.
//
// A section the default order knows goes at its place in that order, a
// section of the consumer's own goes last, and a tool's section goes
// nowhere: the tool sections render together, in the order the
// environment offers the tools.
func (b *Builder) place(name string) {
	if isToolSection(name) || indexOf(b.order, name) >= 0 {
		return
	}
	if isKnownSlot(name) {
		b.order = insertAt(b.order, b.slotPosition(name), name)
		return
	}
	b.order = append(b.order, name)
}

// slotPosition returns where a section the default order knows goes.
//
// The sections the default order knows keep that order among
// themselves, and a section of the consumer's own does not move them.
func (b *Builder) slotPosition(name string) int {
	want := slotIndex(name)
	pos := 0
	for i, n := range b.order {
		idx := slotIndex(n)
		if idx < 0 {
			continue
		}
		if idx > want {
			return i
		}
		pos = i + 1
	}
	return pos
}

// sectionText returns a section's text, as it renders.
//
// The text is used as it is: the builder does not trim it, so a section
// that ends in a newline ends in a newline.
func (b *Builder) sectionText(name string) string {
	section, ok := b.sections[name]
	if !ok {
		return ""
	}
	text := section.Text
	if section.Render != nil {
		text = section.Render(b.env)
	}
	return text + b.extra[name]
}

// toolTexts returns the texts of the tool sections, in the order the
// environment offers the tools.
//
// A tool the environment does not hold has no section in the prompt:
// the guidance a tool brings goes away with the tool.
func (b *Builder) toolTexts() []string {
	var texts []string
	for _, name := range b.toolNames() {
		if text := b.sectionText(name); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// toolNames returns the names of the tool sections that render, in the
// order the environment offers the tools.
func (b *Builder) toolNames() []string {
	var names []string
	for _, tool := range b.env.Tools {
		name := toolPrefix + tool
		if _, ok := b.sections[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// ToolSection returns the name of a tool's prompt section.
//
// The section is named after the tool, so a tool that is not linked
// takes its guidance with it.
func ToolSection(tool string) string { return toolPrefix + tool }

// isToolSection reports whether a name is a tool section's name.
func isToolSection(name string) bool {
	return len(name) > len(toolPrefix) && strings.HasPrefix(name, toolPrefix)
}

// isKnownSlot reports whether the default order knows a name.
func isKnownSlot(name string) bool { return slotIndex(name) >= 0 }

// slotIndex returns a name's place in the default order, or -1 when the
// order does not know it.
func slotIndex(name string) int {
	for i, slot := range DefaultOrder {
		if slot == name {
			return i
		}
	}
	return -1
}

// anchorName returns the name an insertion is anchored at.
//
// A tool section has no place of its own in the order: it is anchored
// at the tools' place, so a section inserted after it follows every
// tool's section.
func anchorName(after string) string {
	if after == toolSlot || isToolSection(after) {
		return toolSlot
	}
	return after
}

// indexOf returns a name's place in a list of names, or -1.
func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// removeName returns the list without a name.
func removeName(names []string, name string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == name {
			continue
		}
		out = append(out, n)
	}
	return out
}

// insertAt returns the list with a name put at a place.
func insertAt(names []string, at int, name string) []string {
	if at < 0 {
		at = 0
	}
	if at > len(names) {
		at = len(names)
	}
	out := make([]string, 0, len(names)+1)
	out = append(out, names[:at]...)
	out = append(out, name)
	out = append(out, names[at:]...)
	return out
}
