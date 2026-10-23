package prompt_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

// The tests drive the builder through its public surface: a section
// holds a default text and the named alternatives it offers, and the
// prompt renders in one variant.

// variantEnv returns an environment that renders in the given variant.
func variantEnv(variant string, tools ...string) prompt.Env {
	env := testEnv(tools...)
	env.Variant = variant
	return env
}

// TestTheVariantRendersTheAlternative covers the rule that a section
// renders the alternative it offers for the variant the prompt renders
// in.
func TestTheVariantRendersTheAlternative(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")

	want := "the small local base\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestASectionWithoutTheVariantRendersItsDefault covers the fallback:
// a section that offers no alternative for the variant renders its
// default text.
func TestASectionWithoutTheVariantRendersItsDefault(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.Set("catalog", "the catalog")

	want := "the default base\n\nthe catalog\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestTheEmptyVariantMeansTheDefaultText covers the rule that the
// empty name and "default" name the default text of every section.
func TestTheEmptyVariantMeansTheDefaultText(t *testing.T) {
	b := prompt.New(variantEnv(""))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")

	for _, variant := range []string{"", "default"} {
		c := b.WithVariant(variant)
		if got := c.Render(); got != "the default base\n" {
			t.Errorf("the prompt in the %q variant is %q, want the default base", variant, got)
		}
	}
}

// TestAVariantOnlySectionRendersNothingByDefault covers the rule that
// a section that holds nothing but an alternative renders nothing
// until the variant is named.
func TestAVariantOnlySectionRendersNothingByDefault(t *testing.T) {
	b := prompt.New(variantEnv(""))
	b.SetVariant("base", "small-local", "the small local base")

	if got := b.Render(); got != "" {
		t.Errorf("the prompt is %q, want it empty", got)
	}
	if got := b.WithVariant("small-local").Render(); got != "the small local base\n" {
		t.Errorf("the prompt in the variant is %q, want the small local base", got)
	}
}

// TestSetDefaultKeepsTheAlternatives covers the rule that an earlier
// layer contributes a default and its alternatives together.
func TestSetDefaultKeepsTheAlternatives(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.SetDefault("base", "the first base")
	b.SetVariant("base", "small-local", "the small local base")
	b.SetDefault("base", "the second base")

	if got := b.WithVariant("").Render(); got != "the second base\n" {
		t.Errorf("the prompt is %q, want the second base", got)
	}
	if got := b.Render(); got != "the small local base\n" {
		t.Errorf("the prompt in the variant is %q, want the small local base", got)
	}
}

// TestSetOverridesTheAlternatives covers the plan's merge order: a
// later layer's Set wins over the section's alternative.
func TestSetOverridesTheAlternatives(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.SetDefault("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.Set("base", "the later base")

	if got := b.Render(); got != "the later base\n" {
		t.Errorf("the prompt in the variant is %q, want the later base", got)
	}
	if got := b.WithVariant("").Render(); got != "the later base\n" {
		t.Errorf("the prompt is %q, want the later base", got)
	}
}

// TestInsertOverridesTheAlternatives covers the plan's merge order for
// a later layer's Insert.
func TestInsertOverridesTheAlternatives(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.SetDefault("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.Set("catalog", "the catalog")
	b.Insert("base", "base", "the other base")

	if got := b.Render(); got != "the other base\n\nthe catalog\n" {
		t.Errorf("the prompt in the variant is %q, want the other base and the catalog", got)
	}
}

// TestSetVariantLeavesTheDefaultAlone covers the rule that setting an
// alternative does not touch the section's default text.
func TestSetVariantLeavesTheDefaultAlone(t *testing.T) {
	b := prompt.New(variantEnv(""))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")

	if got := b.Render(); got != "the default base\n" {
		t.Errorf("the prompt is %q, want the default base", got)
	}
}

// TestSetVariantInTheDefaultNameSetsTheDefault covers the rule that the
// empty name and "default" name the section's default text.
func TestSetVariantInTheDefaultNameSetsTheDefault(t *testing.T) {
	b := prompt.New(variantEnv(""))
	b.Set("base", "the first base")
	b.SetVariant("base", "default", "the second base")

	if got := b.Render(); got != "the second base\n" {
		t.Errorf("the prompt is %q, want the second base", got)
	}
	if got := b.WithVariant("small-local").Render(); got != "the second base\n" {
		t.Errorf("the prompt in the variant is %q, want the second base", got)
	}
}

// TestTheAppendedTextIsAddedToTheVariant covers the rule that the text
// appended to a section belongs to the section and not to one of its
// alternatives: it is added to whatever the section renders.
func TestTheAppendedTextIsAddedToTheVariant(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.Append("base", " and more")

	want := "the small local base and more\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestSetTakesTheAppendedTextWithIt covers the rule that the default
// text is the whole text the section carries when no variant is named:
// setting it takes the appended text away.
func TestSetTakesTheAppendedTextWithIt(t *testing.T) {
	b := prompt.New(variantEnv(""))
	b.Set("base", "the base")
	b.Append("base", " and more")
	b.Set("base", "the other base")

	if got := b.Render(); got != "the other base\n" {
		t.Errorf("the prompt is %q, want the other base", got)
	}
}

// TestRemoveTakesTheAlternativesWithIt covers the rule that a section
// goes with its alternatives.
func TestRemoveTakesTheAlternativesWithIt(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.Remove("base")

	if got := b.Render(); got != "" {
		t.Errorf("the prompt is %q, want it empty", got)
	}
}

// TestRemoveVariantKeepsTheSection covers the rule that removing one
// alternative leaves the section with the others.
func TestRemoveVariantKeepsTheSection(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.RemoveVariant("base", "small-local")

	if got := b.Render(); got != "the default base\n" {
		t.Errorf("the prompt is %q, want the default base", got)
	}
}

// TestRemoveVariantInTheDefaultNameTakesTheDefaultText covers the rule
// that removing the default text leaves the section with its
// alternatives alone.
func TestRemoveVariantInTheDefaultNameTakesTheDefaultText(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.RemoveVariant("base", "default")

	want := "the small local base\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestInsertVariantSetsAnAlternative covers the rule that inserting a
// variant sets one alternative and leaves the default text alone.
func TestInsertVariantSetsAnAlternative(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.Set("catalog", "the catalog")
	b.InsertVariant("catalog", "extra", "small-local", "the extra section")

	want := "the default base\n\nthe catalog\n\nthe extra section\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
	if got := b.WithVariant("").Render(); got != "the default base\n\nthe catalog\n" {
		t.Errorf("the prompt is %q, want the base and the catalog", got)
	}
}

// TestReplaceTakesThePromptOverWhateverTheVariant covers the rule that
// the replaced prompt is the same whatever variant it renders in.
func TestReplaceTakesThePromptOverWhateverTheVariant(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.Replace("the whole prompt")

	for _, variant := range []string{"", "small-local"} {
		if got := b.WithVariant(variant).Render(); got != "the whole prompt" {
			t.Errorf("the prompt in the %q variant is %q, want the whole prompt", variant, got)
		}
	}
}

// TestACopyIsIndependentOfTheAlternatives covers the rule that the
// alternatives come with the copy: a copy may change one without the
// builder it came from changing, and the other way around.
func TestACopyIsIndependentOfTheAlternatives(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")

	c := b.Copy()
	c.SetVariant("base", "small-local", "the other small local base")
	c.SetVariant("base", "reasoning", "the reasoning base")

	if got := b.Render(); got != "the small local base\n" {
		t.Errorf("the builder renders %q, want the small local base", got)
	}
	if got := c.Render(); got != "the other small local base\n" {
		t.Errorf("the copy renders %q, want the other small local base", got)
	}
	if got := c.WithVariant("reasoning").Render(); got != "the reasoning base\n" {
		t.Errorf("the copy renders %q in the reasoning variant, want the reasoning base", got)
	}
}

// TestWithVariantLeavesTheBuilderAlone covers the rule that a run's
// variant never changes the builder the session holds.
func TestWithVariantLeavesTheBuilderAlone(t *testing.T) {
	b := prompt.New(variantEnv(""))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")

	if got := b.WithVariant("small-local").Render(); got != "the small local base\n" {
		t.Errorf("the copy renders %q, want the small local base", got)
	}
	if got := b.Render(); got != "the default base\n" {
		t.Errorf("the builder renders %q, want the default base", got)
	}
}

// TestTheToolSectionRendersItsVariant covers the rule that a tool's
// section renders the alternative it offers, in the tools' place.
func TestTheToolSectionRendersItsVariant(t *testing.T) {
	b := prompt.New(variantEnv("small-local", "bash", "read"))
	b.Set("base", "the default base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.SetVariant(prompt.ToolSection("bash"), "small-local", "the small local bash tool")
	b.Set(prompt.ToolSection("read"), "the read tool")

	want := "the default base\n\nthe small local bash tool\n\nthe read tool\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestTheVariantReachesTheDynamicSections covers the rule that a
// section that renders from the environment reads the variant the
// prompt renders in.
func TestTheVariantReachesTheDynamicSections(t *testing.T) {
	b := prompt.New(variantEnv("small-local"))
	b.SetRender("environment", func(e prompt.Env) string {
		return "the model is " + e.Model + " in the " + e.Variant + " variant"
	})
	b.SetRenderVariant("environment", "small-local", func(e prompt.Env) string {
		return "the model is " + e.Model + ", told in the " + e.Variant + " variant"
	})

	want := "the model is gemma4, told in the small-local variant\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestThePromptIsDeterministicInAVariant covers the rule that the same
// builder renders the same prompt in the same variant.
func TestThePromptIsDeterministicInAVariant(t *testing.T) {
	b := prompt.New(variantEnv("small-local", "bash"))
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")
	b.SetVariant(prompt.ToolSection("bash"), "small-local", "the small local bash tool")

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

// TestAVariantThatNoSectionOffersChangesNothing covers the rule that a
// variant no section offers leaves the prompt as it is.
func TestAVariantThatNoSectionOffersChangesNothing(t *testing.T) {
	b := prompt.New(variantEnv("small-local", "bash"))
	b.Set("base", "the default base")
	b.Set(prompt.ToolSection("bash"), "the bash tool")

	want := "the default base\n\nthe bash tool\n"
	if got := b.Render(); got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
	if !strings.Contains(b.Render(), "the default base") {
		t.Errorf("the prompt is %q, want the default base", b.Render())
	}
}
