// Package skill discovers a person's skills and names them for the
// prompt.
//
// A skill is a directory holding a SKILL.md: a description of something
// the model can do, and the guidance for doing it. The person keeps
// them somewhere the session is told about, and the prompt names what
// exists so the model can read the one that fits.
//
// The prompt carries the catalog, not the guidance. A skill's body is a
// file the model reads with the read tool when the description matches
// the task, so a session offering thirty skills pays for thirty lines
// and no more.
//
// The package reads files and parses front matter: the standard library
// and the YAML decoder, and nothing else.
package skill

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sdougbrown/poo/prompt"
)

// SectionName is the name of the prompt section that lists the skills.
const SectionName = "skills"

// DefaultDir is the directory a session reads a person's skills from
// when the settings ask for the default: the one the rest of the family
// keeps its skills in.
const DefaultDir = "~/.agents/skills"

// Skill is one skill the catalog names.
type Skill struct {
	// Name is the skill's name: the directory it lives in.
	Name string
	// Description is what the skill does, as its front matter says.
	Description string
	// Path is the SKILL.md the model reads for the guidance.
	Path string
}

// Set is the skills one session offers, in name order.
type Set struct {
	skills []Skill
	dirs   []string
}

// Dirs returns the directories the set was read from, in the order it
// read them: the places a session tells its read tool it may look.
func (s *Set) Dirs() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.dirs...)
}

// Len is how many skills the set holds.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.skills)
}

// Skills returns the skills, in name order.
func (s *Set) Skills() []Skill {
	if s == nil {
		return nil
	}
	return append([]Skill(nil), s.skills...)
}

// Template returns the catalog's section of the system prompt as a
// template.
//
// The template renders with the set's data, so a prompt directory that
// replaces the section renders with it too.
func (s *Set) Template() *prompt.Template {
	return prompt.MustCompileTrimmed(SectionName, skillsText, func(prompt.Env) any {
		return skillsData{Skills: s.Skills()}
	})
}

// Section renders the catalog's section of the system prompt.
func (s *Set) Section() string {
	return s.Template().Render(prompt.Env{})
}

// Summary renders the catalog's section for human reading, replacing the
// XML skill entries with a compact list of names and descriptions.
func (s *Set) Summary() string {
	skills := s.Skills()
	if len(skills) == 0 {
		return ""
	}
	intro, _, _ := strings.Cut(skillsText, "<available_skills>")
	intro = strings.TrimSpace(intro)
	var b strings.Builder
	if intro != "" {
		b.WriteString(intro)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Available skills (%d):", len(skills))
	for _, sk := range skills {
		b.WriteByte('\n')
		if sk.Description != "" {
			fmt.Fprintf(&b, "- %s: %s", sk.Name, sk.Description)
		} else {
			fmt.Fprintf(&b, "- %s", sk.Name)
		}
	}
	return b.String()
}

//go:embed skills.md
var skillsText string

// skillsData is the data the catalog's section template renders with.
type skillsData struct {
	Skills []Skill
}

// newSet returns the set of the skills it is given, in name order.
//
// A name two directories define is the first one's: the paths are read
// in the order the session offers them, so a directory read first
// shadows the one behind it.
func newSet(skills []Skill, dirs []string) *Set {
	seen := make(map[string]bool, len(skills))
	kept := make([]Skill, 0, len(skills))
	for _, s := range skills {
		if s.Name == "" || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		kept = append(kept, s)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })
	return &Set{skills: kept, dirs: append([]string(nil), dirs...)}
}

// dirName is the name a skill's directory gives it.
func dirName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), string(filepath.Separator))
}
