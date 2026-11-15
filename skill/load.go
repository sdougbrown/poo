package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillFileName is the file a skill's directory holds.
const SkillFileName = "SKILL.md"

// frontMatter is the part of a skill's front matter the catalog reads.
type frontMatter struct {
	// Description is what the skill does, in the author's words.
	Description string `yaml:"description"`
	// DisableModelInvocation keeps the skill out of the model's
	// catalog: the person can still ask for it, and the model is never
	// told it exists.
	DisableModelInvocation bool `yaml:"disable-model-invocation"`
}

// Load reads the skills in the directories it is given, in order.
//
// Each directory is scanned for the skills it holds: one level of
// subdirectories, each with a SKILL.md. A path that is not a directory
// holds no skills and is not an error — the caller decides what a path
// that is not there means.
//
// A SKILL.md whose front matter names no description is not offered: a
// catalog line is the description, and a skill with none has nothing to
// say. A file that cannot be read, or whose front matter does not
// parse, is an error: the directory was offered on purpose.
func Load(dirs ...string) (*Set, error) {
	var found []Skill
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}
		for _, entry := range entries {
			// The entry is stat'd rather than asked whether it is a
			// directory: a skills directory shared across harnesses
			// holds links to the skills, not copies.
			path := filepath.Join(dir, entry.Name())
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				continue
			}
			skill, ok, err := read(path)
			if err != nil {
				return nil, err
			}
			if ok {
				found = append(found, skill)
			}
		}
	}
	return newSet(found), nil
}

// read returns the skill a directory holds, and whether it holds one.
//
// A directory with no SKILL.md is not a skill: a skills directory is a
// place people also keep notes.
func read(dir string) (Skill, bool, error) {
	path := filepath.Join(dir, SkillFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Skill{}, false, nil
		}
		return Skill{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	front, ok, err := parseFrontMatter(data)
	if err != nil {
		return Skill{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if !ok || front.DisableModelInvocation {
		return Skill{}, false, nil
	}
	if strings.TrimSpace(front.Description) == "" {
		return Skill{}, false, nil
	}
	return Skill{
		Name:        dirName(dir),
		Description: oneLine(front.Description),
		Path:        path,
	}, true, nil
}

// parseFrontMatter reads the YAML between the leading --- markers.
//
// The second return value says the file carries front matter at all. A
// file with none is not a skill, and a file whose front matter does not
// parse is a skill that cannot be read.
func parseFrontMatter(data []byte) (frontMatter, bool, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---") {
		return frontMatter{}, false, nil
	}
	rest := strings.TrimPrefix(text, "---")
	rest = strings.TrimPrefix(rest, "\r")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return frontMatter{}, false, fmt.Errorf("the front matter is not closed with ---")
	}
	var front frontMatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &front); err != nil {
		return frontMatter{}, false, fmt.Errorf("the front matter does not parse: %w", err)
	}
	return front, true, nil
}

// oneLine keeps a description to the one line the catalog shows.
//
// A description is written for a person browsing skills, so it runs to
// paragraphs. The catalog takes the first sentence, and a sentence that
// is still long is cut at a word.
func oneLine(description string) string {
	line := strings.Join(strings.Fields(description), " ")
	if i := strings.Index(line, ". "); i >= 0 {
		line = line[:i+1]
	}
	const limit = 200
	if len(line) <= limit {
		return line
	}
	cut := line[:limit]
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}
