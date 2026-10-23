package prompt_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

// The tests drive the builder through its public surface: a builder is
// filled with sections, and the prompt is what it renders.

// testEnv returns an environment for a test.
func testEnv(tools ...string) prompt.Env {
	return prompt.Env{
		Cwd:   "/work",
		OS:    "linux",
		Date:  "2026-02-03",
		Model: "gemma4",
		Tools: tools,
	}
}

// TestTheSectionsRenderInTheDefaultOrder covers the plan's order: the
// base, the tools, the catalog, the environment and the project, and
// then the consumer's own sections in the order they were added.
func TestTheSectionsRenderInTheDefaultOrder(t *testing.T) {
	b := prompt.New(testEnv("bash"))
	b.Set("base", "the base")
	b.Set("catalog", "the catalog")
	b.Set("environment", "the environment")
	b.Set("project", "the project")
	b.Set("first", "the first")
	b.Set("second", "the second")

	want := "the base\n\nthe catalog\n\nthe environment\n\nthe project\n\nthe first\n\nthe second\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestTheToolSectionsRenderWhereTheToolsSit covers the plan's order:
// the per-tool sections render together, after the base.
func TestTheToolSectionsRenderWhereTheToolsSit(t *testing.T) {
	b := prompt.New(testEnv("bash", "read"))
	b.Set("base", "the base")
	b.Set(prompt.ToolSection("read"), "the read tool")
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.Set("catalog", "the catalog")

	want := "the base\n\nthe bash tool\n\nthe read tool\n\nthe catalog\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestAToolSectionGoesAwayWithTheTool covers the plan's rule: a tool
// that is not linked takes its guidance with it.
func TestAToolSectionGoesAwayWithTheTool(t *testing.T) {
	b := prompt.New(testEnv("read"))
	b.Set("base", "the base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")

	got := b.Render()
	if strings.Contains(got, "the bash tool") {
		t.Errorf("the prompt mentions a tool that is not linked: %q", got)
	}
	if !strings.Contains(got, "the base") {
		t.Errorf("the prompt is %q, want the base", got)
	}
}

// TestRemovingAToolSectionTakesItAway covers the removal of a tool's
// section by name.
func TestRemovingAToolSectionTakesItAway(t *testing.T) {
	b := prompt.New(testEnv("bash"))
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.Remove(prompt.ToolSection("bash"))

	if got := b.Render(); got != "" {
		t.Errorf("the prompt is %q, want it empty", got)
	}
}

// TestSetReplacesASection covers the set mutation.
func TestSetReplacesASection(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the first base")
	b.Set("base", "the second base")

	if got := b.Render(); got != "the second base\n" {
		t.Errorf("the prompt is %q, want the second base", got)
	}
}

// TestAppendAddsToASection covers the append mutation, and the rule
// that a section that is not there yet is added.
func TestAppendAddsToASection(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Append("base", " and more")
	b.Append("new", "the new section")

	want := "the base and more\n\nthe new section\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestAppendingToADynamicSectionKeepsItDynamic covers the rule that the
// appended text is kept apart from the section's own value.
func TestAppendingToADynamicSectionKeepsItDynamic(t *testing.T) {
	b := prompt.New(testEnv())
	b.SetRender("environment", func(e prompt.Env) string { return "the model is " + e.Model })
	b.Append("environment", ", and the date is "+"2026-02-03")

	want := "the model is gemma4, and the date is 2026-02-03\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestRemoveDropsASection covers the remove mutation, and the rule that
// removing a section that is not there is not an error.
func TestRemoveDropsASection(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Set("project", "the project")
	b.Remove("project")
	b.Remove("nothing")

	if got := b.Render(); got != "the base\n" {
		t.Errorf("the prompt is %q, want the base alone", got)
	}
}

// TestInsertPutsASectionAfterAnother covers the insert mutation.
func TestInsertPutsASectionAfterAnother(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Set("catalog", "the catalog")
	b.Insert("base", "extra", "the extra section")

	want := "the base\n\nthe extra section\n\nthe catalog\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestInsertWithAnUnknownAnchorPutsTheSectionFirst covers the rule that
// an anchor that is not there puts the section first.
func TestInsertWithAnUnknownAnchorPutsTheSectionFirst(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Insert("nothing", "extra", "the extra section")

	want := "the extra section\n\nthe base\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestInsertMovesASectionThatIsAlreadyThere covers the rule that a
// section that is already there moves to the new place.
func TestInsertMovesASectionThatIsAlreadyThere(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Set("catalog", "the catalog")
	b.Insert("base", "catalog", "the catalog")

	want := "the base\n\nthe catalog\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestInsertAfterAToolSectionFollowsTheTools covers the rule that a
// tool section is anchored at the tools' place.
func TestInsertAfterAToolSectionFollowsTheTools(t *testing.T) {
	b := prompt.New(testEnv("bash"))
	b.Set("base", "the base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.Insert(prompt.ToolSection("bash"), "extra", "the extra section")

	want := "the base\n\nthe bash tool\n\nthe extra section\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestReplaceTakesOverTheWholePrompt covers the replace mutation.
func TestReplaceTakesOverTheWholePrompt(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Replace("the whole prompt")

	if got := b.Render(); got != "the whole prompt" {
		t.Errorf("the prompt is %q, want the whole prompt", got)
	}
	if names := b.Names(); len(names) != 0 {
		t.Errorf("the builder holds the sections %v, want none", names)
	}
}

// TestASectionAfterAReplacementPutsTheSectionsBack covers the rule that
// a section set after a replacement starts the sections over.
func TestASectionAfterAReplacementPutsTheSectionsBack(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Replace("the whole prompt")
	b.Set("base", "the new base")

	if got := b.Render(); got != "the new base\n" {
		t.Errorf("the prompt is %q, want the new base", got)
	}
}

// TestTheNamesAreTheSectionsThatRender covers the names of the sections
// that render, in render order.
func TestTheNamesAreTheSectionsThatRender(t *testing.T) {
	b := prompt.New(testEnv("bash"))
	b.Set("base", "the base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.Set(prompt.ToolSection("read"), "the read tool")
	b.Set("extra", "the extra section")

	want := []string{"base", "tools.bash", "extra"}
	got := b.Names()
	if len(got) != len(want) {
		t.Fatalf("the names are %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the names are %v, want %v", got, want)
			break
		}
	}
}

// TestThePromptIsDeterministic covers the rule that the same builder
// renders the same prompt, and that a copy renders the same one.
func TestThePromptIsDeterministic(t *testing.T) {
	b := prompt.New(testEnv("bash"))
	b.Set("base", "the base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.Set("extra", "the extra section")

	first := b.Render()
	for i := 0; i < 8; i++ {
		if got := b.Render(); got != first {
			t.Fatalf("the prompt is %q on the %d render, want %q", got, i, first)
		}
	}
	if got := b.Copy().Render(); got != first {
		t.Errorf("the copy renders %q, want %q", got, first)
	}
}

// TestACopyIsIndependent covers the rule that a copy may be changed
// without the builder it came from changing.
func TestACopyIsIndependent(t *testing.T) {
	b := prompt.New(testEnv("bash"))
	b.Set("base", "the base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")

	c := b.Copy()
	c.Set("base", "the other base")
	c.Remove(prompt.ToolSection("bash"))

	if got := b.Render(); got != "the base\n\nthe bash tool\n" {
		t.Errorf("the builder renders %q, want the base and the bash tool", got)
	}
	if got := c.Render(); got != "the other base\n" {
		t.Errorf("the copy renders %q, want the other base", got)
	}
}

// TestAnEmptySectionRendersNothing covers the rule that a section with
// no text renders nothing, and that it does not leave a gap behind.
func TestAnEmptySectionRendersNothing(t *testing.T) {
	b := prompt.New(testEnv())
	b.Set("base", "the base")
	b.Set("project", "")

	if got := b.Render(); got != "the base\n" {
		t.Errorf("the prompt is %q, want the base alone", got)
	}
}

// TestTheBuilderIsASectionsSurface covers that the builder may be used
// through the surface a hook edits.
func TestTheBuilderIsASectionsSurface(t *testing.T) {
	var sections prompt.Sections = prompt.New(testEnv())
	sections.Set("base", "the base")
	sections.Append("base", " and more")
	sections.Set("extra", "the extra section")
	sections.Insert("base", "inserted", "the inserted section")
	sections.Remove("extra")

	want := "the base and more\n\nthe inserted section\n"
	if got := sections.(*prompt.Builder).Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}
