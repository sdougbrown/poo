package prompt

import (
	_ "embed"
	"sort"
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

// FamilyNames returns the families the prompt ships texts for, in name
// order.
func FamilyNames() []string {
	variants := FamilyVariants()
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
