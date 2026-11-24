package prompt

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
)

// FamilySection is the name of the prompt's model-family section.
//
// The section carries a text per model family, keyed by the name the
// model's prompt variant names: a model whose prompt_variant is
// "gpt-6.2" renders the "gpt-6.2" text. The section holds no default
// text, so it renders nothing until the variant names one: a family
// the section does not know leaves the prompt unchanged.
const FamilySection = "family"

// The family texts the prompt ships, keyed by the prompt variant that
// names them. The words are poo's own, not a vendor's: they carry the
// working discipline the family needs beside the profile's guidance,
// and they say nothing the base contract does not already cover.
//
//go:embed family/gpt-5.6.md
var familyGpt56 string

//go:embed family/gpt-6.md
var familyGpt6 string

//go:embed family/gpt-6.2.md
var familyGpt62 string

// FamilyVariants returns the family texts the prompt ships, keyed by
// the prompt variant that selects them.
//
// The map is a copy, so a consumer that changes it changes nothing but
// the copy.
func FamilyVariants() map[string]string {
	return map[string]string{
		"gpt-5.6": familyGpt56,
		"gpt-6":   familyGpt6,
		"gpt-6.2": familyGpt62,
	}
}

// FamilyHolds reports whether the builder holds the family section: a
// prompt that never opted in — or a raw one — carries none, and the
// warnings that explain an empty section stay quiet about it.
func FamilyHolds(b *Builder) bool {
	return slices.Contains(b.Names(), FamilySection)
}

// FamilyRenders reports whether the family section the builder holds
// renders guidance for the variant named: the section's text for the
// variant — the family's own, or a default the prompt directory
// supplied — must not be empty.
func FamilyRenders(b *Builder, variant string) bool {
	sections, err := b.WithVariant(variant).SectionsError()
	if err != nil {
		// The prompt cannot render at all: the render error reaches the
		// caller on its own path, and this predicate only answers
		// whether the family's guidance is among what renders.
		return false
	}
	for _, section := range sections {
		if section.Name == FamilySection {
			return strings.TrimSpace(section.Text) != ""
		}
	}
	return false
}

// FamilyWarning returns the warning a caller surfaces when the family
// section the builder holds renders no guidance for the variant named.
// The rule and its wording are the family's own, so every path that
// explains an empty section — a model switch, a variant change, the
// build — says the same thing. The second result reports whether there
// is something to say: a prompt without the section, or one whose
// section renders, stays unmentioned.
func FamilyWarning(b *Builder, variant string) (string, bool) {
	if !FamilyHolds(b) || FamilyRenders(b, variant) {
		return "", false
	}
	if variant == "" {
		return "the prompt renders with no variant, so the model family's guidance does not render", true
	}
	return fmt.Sprintf("the prompt variant %q names no family the prompt ships, so the model family's guidance does not render in a session that keeps it", variant), true
}
