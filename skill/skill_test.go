package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sdougbrown/poo/skill"
)

// writeSkill puts a SKILL.md in dir/name.
func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("making %s: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, skill.SkillFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the skill: %v", err)
	}
}

// front returns a skill file with the given front matter.
func front(fields string) string {
	return "---\n" + fields + "---\n\nThe guidance.\n"
}

// TestLoadReadsTheSkillsInNameOrder covers the scan: each subdirectory
// with a SKILL.md is a skill, named by its directory, and the set is in
// name order whatever order the directories read in.
func TestLoadReadsTheSkillsInNameOrder(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "zeta", front("description: the last one\n"))
	writeSkill(t, dir, "alpha", front("description: the first one\n"))
	// A directory with no SKILL.md is not a skill.
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatalf("making the notes directory: %v", err)
	}

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	got := set.Skills()
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("the skills are %v, want alpha then zeta", got)
	}
	if got[0].Description != "the first one" {
		t.Errorf("alpha's description is %q", got[0].Description)
	}
	if !strings.HasSuffix(got[0].Path, filepath.Join("alpha", skill.SkillFileName)) {
		t.Errorf("alpha's path is %q", got[0].Path)
	}
}

// TestLoadHidesASkillTheModelMayNotInvoke covers `disable-model-invocation`:
// the skill stays the person's, and the model is never told it exists.
func TestLoadHidesASkillTheModelMayNotInvoke(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "hidden", front("description: a secret\ndisable-model-invocation: true\n"))
	writeSkill(t, dir, "shown", front("description: a public one\n"))

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	if got := set.Skills(); len(got) != 1 || got[0].Name != "shown" {
		t.Errorf("the skills are %v, want only shown", got)
	}
	if section := set.Section(); strings.Contains(section, "a secret") {
		t.Errorf("the catalog names a skill the model may not invoke:\n%s", section)
	}
}

// TestLoadFollowsALinkToASkill covers a skills directory that holds
// links rather than copies, which is how one shared set serves several
// harnesses: the link is the skill.
func TestLoadFollowsALinkToASkill(t *testing.T) {
	shared := t.TempDir()
	writeSkill(t, shared, "review", front("description: Reviews a diff\n"))
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(shared, "review"), filepath.Join(dir, "review")); err != nil {
		t.Skipf("linking the skill: %v", err)
	}

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	got := set.Skills()
	if len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("the skills are %v, want the linked review", got)
	}
}

// TestLoadSkipsASkillWithNoDescription covers a SKILL.md that says
// nothing the catalog can print: it is not offered, and it is not an
// error either.
func TestLoadSkipsASkillWithNoDescription(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "empty", front("name: empty\n"))
	writeSkill(t, dir, "plain", "No front matter at all.\n")

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	if got := set.Len(); got != 0 {
		t.Errorf("the set holds %d skills, want none: %v", got, set.Skills())
	}
}

// TestLoadFailsOnFrontMatterThatDoesNotParse covers a directory offered
// on purpose whose file cannot be read for what it is.
func TestLoadFailsOnFrontMatterThatDoesNotParse(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "broken", "---\ndescription: [unclosed\n---\n")
	if _, err := skill.Load(dir); err == nil {
		t.Fatal("the load accepted front matter that does not parse")
	}

	unclosed := t.TempDir()
	writeSkill(t, unclosed, "open", "---\nA horizontal rule, and then prose.\n")
	set, err := skill.Load(unclosed)
	if err != nil {
		t.Fatalf("a file that opens with a rule failed the load: %v", err)
	}
	if got := set.Len(); got != 0 {
		t.Errorf("the set holds %d skills, want none: a file with no front matter is not a skill", got)
	}
}

// TestLoadShadowsANameTheEarlierDirectoryDefines covers the order the
// directories are read in: the first one offering a name wins, so a
// directory read first shadows the one behind it.
func TestLoadShadowsANameTheEarlierDirectoryDefines(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeSkill(t, first, "shared", front("description: the first copy\n"))
	writeSkill(t, second, "shared", front("description: the second copy\n"))

	set, err := skill.Load(first, second)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	got := set.Skills()
	if len(got) != 1 || got[0].Description != "the first copy" {
		t.Errorf("the skills are %v, want the first copy alone", got)
	}
}

// TestLoadKeepsADescriptionToOneLine covers the catalog's line: a
// description written for a person browsing skills runs to paragraphs,
// and the prompt carries the first sentence.
func TestLoadKeepsADescriptionToOneLine(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "long", front("description: >-\n  Does the first thing. Then it does the second thing, which\n  would be a second line and a second paragraph.\n"))

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	got := set.Skills()
	if len(got) != 1 {
		t.Fatalf("the set holds %d skills, want one", len(got))
	}
	if got[0].Description != "Does the first thing." {
		t.Errorf("the description is %q, want the first sentence", got[0].Description)
	}
}

// TestLoadIgnoresADirectoryThatIsNotThere covers a path with nothing at
// it: the loader holds no skills for it and does not fail, because the
// caller decides what a missing directory means.
func TestLoadIgnoresADirectoryThatIsNotThere(t *testing.T) {
	set, err := skill.Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	if got := set.Len(); got != 0 {
		t.Errorf("the set holds %d skills, want none", got)
	}
}

// TestLoadCutsALongSingleSentence covers the catalog's width: a
// description that is one long sentence is cut at a word, and the cut is
// marked.
func TestLoadCutsALongSingleSentence(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("word ", 60) + "final"
	writeSkill(t, dir, "wide", front("description: "+long+"\n"))

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	got := set.Skills()
	if len(got) != 1 {
		t.Fatalf("the set holds %d skills, want one", len(got))
	}
	description := got[0].Description
	if !strings.HasSuffix(description, "\u2026") {
		t.Errorf("the cut description does not end with an ellipsis: %q", description)
	}
	if runes := len([]rune(description)); runes > 201 {
		t.Errorf("the description is %d runes, want 200 plus the ellipsis", runes)
	}
	// The cut lands on a word: no half word before the ellipsis.
	trimmed := strings.TrimSuffix(description, "\u2026")
	if strings.HasSuffix(trimmed, "wor") || !strings.HasSuffix(trimmed, "word") {
		t.Errorf("the cut landed inside a word: %q", description)
	}
}

// TestLoadCutsNonLatinTextWhereItCanBeRead covers a description whose
// characters take more than one byte: the cut lands on a character
// boundary, so the catalog never carries half a rune.
func TestLoadCutsNonLatinTextWhereItCanBeRead(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "wide", front("description: "+strings.Repeat("\u4e16", 210)+"\n"))

	set, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("loading the skills: %v", err)
	}
	got := set.Skills()
	if len(got) != 1 {
		t.Fatalf("the set holds %d skills, want one", len(got))
	}
	if !utf8.ValidString(got[0].Description) {
		t.Errorf("the description is not valid UTF-8: %q", got[0].Description)
	}
	if runes := len([]rune(got[0].Description)); runes > 201 {
		t.Errorf("the description is %d runes, want 200 plus the ellipsis", runes)
	}
}
