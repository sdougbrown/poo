package prompt

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// A prompt directory holds a binary's own prose. Each file is named
// after the section it replaces, and the files map onto the builder's
// sections in one call.

// Sources are the sections a prompt directory may replace, with the
// data their templates render with.
//
// A nil value renders with the environment, which is what every section
// whose text names no field of its own uses. The map's names are also
// the names a directory may carry: a file that names anything else is
// an error.
type Sources map[string]func(Env) any

// Dir is a prompt directory, compiled and ready to apply.
type Dir struct {
	// files are the sections the directory carries, defaults first.
	files []dirFile
}

// dirFile is one file of a prompt directory.
type dirFile struct {
	// section is the section the file replaces.
	section string
	// variant is the alternative's name, or "" for the default text.
	variant string
	// template is the file's compiled prose.
	template *Template
}

// Load reads a prompt directory from fsys.
//
// A file is named after the section it replaces: base.md replaces the
// base section, environment.md the environment, and tools/bash.md the
// bash tool's section. A directory in the path names the section's
// prefix, so tools/bash.md and tools/edit.md are the sections
// "tools.bash" and "tools.edit".
//
// A variant's text is a sibling whose name carries the variant:
// tools/bash.small-local.md replaces the bash section's "small-local"
// alternative and leaves its default text alone.
//
// The data each file's template renders with is the section's entry in
// sources, so a replacement renders like the default it replaces. A
// file that names a section the map does not hold is an error that
// names the file, and so is a file that is not a .md file.
func Load(fsys fs.FS, sources Sources) (*Dir, error) {
	var files []dirFile
	err := fs.WalkDir(fsys, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("prompt: %s: %w", p, err)
		}
		if entry.IsDir() {
			return nil
		}
		section, variant, err := sectionOf(p)
		if err != nil {
			return err
		}
		data, ok := sources[section]
		if !ok {
			return fmt.Errorf("prompt: %s: unknown section %q", p, section)
		}
		text, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("prompt: %s: %w", p, err)
		}
		tmpl, err := Compile(section, string(text), data)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		files = append(files, dirFile{section: section, variant: variant, template: tmpl})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The defaults come first, so a directory that carries a section and
	// one of its alternatives applies them in the order the builder
	// expects, whatever order the files are walked in.
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].variant == "" && files[j].variant != ""
	})
	return &Dir{files: files}, nil
}

// Apply replaces the sections the directory carries in a builder.
//
// A file with no variant replaces the section's default text and keeps
// the alternatives the section offers; a variant file replaces that
// alternative. A section the builder does not hold is added, so a
// directory may bring a section of its own.
func (d *Dir) Apply(b *Builder) {
	for _, file := range d.files {
		if file.variant == "" {
			b.SetTemplateDefault(file.section, file.template)
			continue
		}
		b.SetTemplateVariant(file.section, file.variant, file.template)
	}
}

// sectionOf maps a file's path onto a section name and a variant name.
//
// The path's directories name the section's prefix, and the file's name
// names the section's last part. A dot in the file's name names a
// variant: "tools/bash.small-local.md" is the bash section's
// "small-local" alternative.
func sectionOf(p string) (section, variant string, err error) {
	if path.Ext(p) != ".md" {
		return "", "", fmt.Errorf("prompt: %s: not a .md file", p)
	}
	name := path.Base(p)
	stem := strings.TrimSuffix(name, ".md")
	leaf, variant, _ := strings.Cut(stem, ".")
	if leaf == "" {
		return "", "", fmt.Errorf("prompt: %s: no section name", p)
	}
	if variant == "" && strings.HasSuffix(stem, ".") {
		return "", "", fmt.Errorf("prompt: %s: no variant name", p)
	}
	if dir := path.Dir(p); dir != "." {
		section = strings.ReplaceAll(dir, "/", ".") + "." + leaf
	} else {
		section = leaf
	}
	return section, variant, nil
}
