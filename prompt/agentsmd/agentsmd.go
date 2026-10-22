// Package agentsmd loads a project's instructions for the prompt's
// project section.
//
// The loader reads AGENTS.md, or CLAUDE.md when the directory holds no
// AGENTS.md, from one directory: the working directory the session
// runs in. It never walks up to a parent, so the instructions it loads
// are the ones that directory holds and not a parent's.
//
// The text is capped at MaxBytes. A longer file is cut at a rune
// boundary and a notice says that the file was cut.
//
// The package imports the standard library only.
package agentsmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// MaxBytes is the largest number of bytes of a project's instructions
// the loader returns.
//
// The cap keeps a runaway file out of the prompt. The notice that
// marks a cut file is added on top of it.
const MaxBytes = 64 << 10

// filenames are the files the loader looks for, in the order it looks
// for them. The first one the directory holds is the one it reads.
var filenames = []string{"AGENTS.md", "CLAUDE.md"}

// Load returns the project instructions of a directory.
//
// The loader looks for AGENTS.md and then CLAUDE.md in the directory
// itself, and the first file it finds is the one it reads. It never
// walks up to a parent: a directory with no file of its own holds no
// instructions, even when a parent has some.
//
// A directory with neither file holds no instructions, and the loader
// returns an empty text and no error.
//
// The text is at most MaxBytes long. A longer file is cut at MaxBytes
// and a notice says how much of the file is shown.
//
// A file that cannot be read is an error. The caller asked for the
// project's instructions, and an empty section would say the project
// has none. A path that is not a regular file is an error too: the
// loader does not read a pipe and wait for it.
func Load(dir string) (string, error) {
	for _, name := range filenames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("agentsmd: %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("agentsmd: %s is not a regular file", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("agentsmd: reading %s: %w", path, err)
		}
		// The cap is read, and one byte past it says the file was
		// longer. A runaway file is never held whole.
		data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
		closeErr := f.Close()
		if err != nil {
			return "", fmt.Errorf("agentsmd: reading %s: %w", path, err)
		}
		if closeErr != nil {
			return "", fmt.Errorf("agentsmd: reading %s: %w", path, closeErr)
		}
		return capText(data, name), nil
	}
	return "", nil
}

// capText cuts a text that is longer than the cap.
//
// The cut is at a rune boundary, so the text stays valid UTF-8, and
// the notice names the file it was cut from.
func capText(data []byte, name string) string {
	if len(data) <= MaxBytes {
		return string(data)
	}
	cut := MaxBytes
	for cut > 0 && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return string(data[:cut]) + fmt.Sprintf("\n\n[%s truncated: only the first %d bytes are shown]", name, MaxBytes)
}
