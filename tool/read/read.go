// Package read implements the read tool: a line-based view of a file.
//
// The tool shows the model an explicit range of a file, so it can resume
// from a known position instead of re-reading the whole thing.
package read

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
)

// Defaults for the read tool.
const (
	// DefaultLimit is the number of lines a read shows when the model
	// does not ask for a limit.
	DefaultLimit = 2000
	// MaxLineChars is the number of characters a single line may show.
	// Longer lines are truncated, so a minified file cannot flood the
	// context.
	MaxLineChars = 2000
	// OutputCapMax is the largest read output the model ever sees.
	OutputCapMax = 70000
	// CharsPerToken is the plan's estimate of characters per token.
	CharsPerToken = 3.5
	// BinarySniffBytes is how much of a file is read to decide whether it
	// is binary.
	BinarySniffBytes = 8 << 10
)

// OutputCap returns the cap on a read's output.
//
// The cap is context-relative: min(70000, floor(0.10 × context_limit × 3.5))
// characters.
func OutputCap(contextLimit int) int {
	cap := int(0.10 * float64(contextLimit) * CharsPerToken)
	if cap > OutputCapMax {
		return OutputCapMax
	}
	if cap < 0 {
		return 0
	}
	return cap
}

// Tool is the read tool.
type Tool struct {
	// OutputCap is the cap on the output. Zero means the cap is taken from
	// the run's settings.
	OutputCap int
}

// New returns a read tool.
func New() *Tool { return &Tool{} }

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "read",
		Description: "Read a range of lines from a file.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "The path of the file to read."},
    "offset": {"type": "integer", "description": "The first line to show, 1-indexed."},
    "limit": {"type": "integer", "description": "How many lines to show."}
  },
  "required": ["path"]
}`),
		Class:      poo.ClassReadOnly,
		PrimaryArg: "path",
	}
}

// args are the tool's arguments.
type args struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// prepared is a validated read call.
type prepared struct {
	path   string
	offset int
	limit  int
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string {
	return fmt.Sprintf("%s (offset %d, limit %d)", p.path, p.offset, p.limit)
}

// Prepare validates the arguments.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("read: %v", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return nil, poo.InvalidArgsf("read: path must not be empty")
	}
	if a.Offset < 0 {
		return nil, poo.InvalidArgsf("read: offset must not be negative")
	}
	if a.Limit < 0 {
		return nil, poo.InvalidArgsf("read: limit must not be negative")
	}
	offset := a.Offset
	if offset == 0 {
		offset = 1
	}
	limit := a.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	return &prepared{path: a.Path, offset: offset, limit: limit}, nil
}

// decodeStrict decodes one JSON value and nothing else.
//
// A second value, or anything after the first, is a malformed call: the
// model sent more than it meant to, and taking the first object would
// silently run the wrong thing.
func decodeStrict(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("the arguments hold more than one JSON value")
		}
		return err
	}
	return nil
}

// Call reads the file.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*prepared)
	if !ok {
		return poo.ToolResult{}, errors.New("read: prepared value is not a read call")
	}

	path := p.path
	if !filepath.IsAbs(path) {
		path = filepath.Join(env.WorkingDir, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("read: %v", err), IsError: true}, nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("read: %v", err), IsError: true}, nil
	}
	if info.IsDir() {
		return poo.ToolResult{Text: fmt.Sprintf("read: %s is a directory", path), IsError: true}, nil
	}

	// A NUL byte in the first 8 KiB means the file is binary.
	sniff := make([]byte, BinarySniffBytes)
	n, err := f.Read(sniff)
	if err != nil && !errors.Is(err, io.EOF) {
		return poo.ToolResult{Text: fmt.Sprintf("read: %v", err), IsError: true}, nil
	}
	if idx := indexNUL(sniff[:n]); idx >= 0 {
		return poo.ToolResult{
			Text:    fmt.Sprintf("read: %s is a binary file (%d bytes); use grep or a command instead", path, info.Size()),
			IsError: true,
		}, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("read: %v", err), IsError: true}, nil
	}

	cap := t.OutputCap
	if cap <= 0 {
		cap = env.Settings.ReadOutputCapChars
	}
	if cap <= 0 {
		cap = OutputCapMax
	}

	body, total, first, last, capped, err := readRange(f, p.offset, p.limit, cap)
	if err != nil {
		return poo.ToolResult{Text: fmt.Sprintf("read: %v", err), IsError: true}, nil
	}

	header := fmt.Sprintf("[%s · lines %d–%d of %d]", p.path, first, last, total)
	if first == 0 {
		// The range is empty: the offset is past the end of the file.
		header = fmt.Sprintf("[%s · no lines at offset %d of %d]", p.path, p.offset, total)
	}
	if capped {
		header = fmt.Sprintf("[%s · lines %d–%d of %d · capped]", p.path, first, last, total)
	}
	text := header + "\n" + body

	return poo.ToolResult{Text: text}, nil
}

// readRange reads the requested lines, honouring the output cap.
//
// It returns the body, the total number of lines, the first and last
// line shown, and whether the cap ended the read early. The file is read
// once, so a large file costs one pass.
//
// The cap and the line cap are counted in characters, and a cut never
// splits a rune. A line longer than the line cap is read to its end and
// truncated, so a minified file is readable and costs a bounded buffer.
func readRange(f *os.File, offset, limit, cap int) (string, int, int, int, bool, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, 0, false, err
	}
	br := bufio.NewReader(f)

	var b strings.Builder
	line := 0
	total := 0
	first := 0
	last := 0
	capped := false
	// room is how much of the cap is left, in characters.
	room := cap

	for {
		text, eof, err := readLine(br, MaxLineChars)
		if err != nil {
			return "", 0, 0, 0, false, err
		}
		if eof && text == "" {
			// The file ended.
			break
		}
		line++
		total = line
		if line < offset {
			if eof {
				break
			}
			continue
		}
		if first > 0 && line-first >= limit {
			// The range is complete, but the rest of the file is still
			// counted, so the header can name the total.
			if eof {
				break
			}
			continue
		}
		if first == 0 {
			first = line
		}
		if capped {
			// The cap already ended the body, so the rest is only
			// counted.
			if eof {
				break
			}
			continue
		}

		chunk := text + "\n"
		chars := utf8.RuneCountInString(chunk)
		if chars > room {
			// The cap ends the read. The first line is still shown, in
			// part, so the model can make progress.
			if room > 0 {
				chunk = truncateRunes(chunk, room)
				b.WriteString(chunk)
				last = line
			}
			capped = true
			room = 0
			if eof {
				break
			}
			continue
		}
		b.WriteString(chunk)
		room -= chars
		last = line

		if eof {
			break
		}
	}
	if first == 0 {
		// The file has fewer lines than the offset.
		return "", total, 0, 0, false, nil
	}
	return b.String(), total, first, last, capped, nil
}

// readLine reads one line, keeping at most limit characters.
//
// The rest of the line is discarded rather than held in memory, so a
// minified file costs a bounded buffer. It reports whether the file
// ended with the line.
func readLine(br *bufio.Reader, limit int) (string, bool, error) {
	// A rune is at most four bytes, so this budget holds enough whole
	// characters to satisfy the limit.
	budget := limit*4 + 4
	var b strings.Builder
	for {
		chunk, err := br.ReadSlice('\n')
		if b.Len() < budget {
			room := budget - b.Len()
			if room > len(chunk) {
				room = len(chunk)
			}
			b.Write(chunk[:room])
		}
		switch {
		case err == nil:
			text := strings.TrimSuffix(b.String(), "\n")
			return truncateRunes(trimPartialRune(text), limit), false, nil
		case errors.Is(err, bufio.ErrBufferFull):
			// The line is longer than the buffer. Keep reading.
			continue
		case errors.Is(err, io.EOF):
			return truncateRunes(trimPartialRune(b.String()), limit), true, nil
		default:
			return "", false, err
		}
	}
}

// trimPartialRune drops an incomplete rune from the end of a string.
func trimPartialRune(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

// truncateRunes keeps a string within a character budget, without
// splitting a rune.
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}

// indexNUL returns the index of the first NUL byte, or -1.
func indexNUL(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}
