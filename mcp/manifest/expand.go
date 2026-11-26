package manifest

import (
	"fmt"
	"strings"

	"github.com/sdougbrown/poo/exec"
)

// Template is one manifest value with its references parsed: a server's
// command, one of its arguments, or one of its environment values.
//
// The forms are `${NAME}`, `${NAME:-default}`, `${session:id}`, and `$$`
// for a literal dollar sign. A `${NAME}` is the host environment's value
// when it has one, and the default when the template gives one. A
// `${session:id}` is the session's own ID. Anything else is literal
// text.
//
// A template resolves when its server launches, never when the settings
// load: the values a launch resolves are the session's, and a session
// that does not exist yet has none of them.
//
// A template is immutable, so copies of it share their parsed form.
type Template struct {
	// parts are the value's pieces, in the order they are written.
	parts []templatePart
}

// templatePart is one piece of a template: literal text, a host
// environment reference, or the session's own ID.
type templatePart struct {
	// kind is the piece's kind.
	kind templatePartKind
	// text is the literal text, or the name a host reference reads.
	text string
	// defaultValue is what a host reference resolves to when the
	// environment does not hold its name, and is empty when the
	// reference gives no default.
	defaultValue string
	// hasDefault says the reference gave a default, which may be empty.
	hasDefault bool
}

// templatePartKind is what one piece of a template is.
type templatePartKind int

const (
	// templateLiteral is text the manifest wrote as it stands.
	templateLiteral templatePartKind = iota
	// templateHost is `${NAME}`: the host environment's value.
	templateHost
	// templateSession is `${session:id}`: the session's own ID.
	templateSession
)

// ParseTemplate parses one manifest value into the pieces it resolves.
//
// An unterminated `${`, a reference with no name, a `session:` reference
// to a value the session scope does not carry, and a reference that
// takes a default all fail here, when the manifest loads: a value the
// session could never resolve is a manifest that does not load, rather
// than a server that fails at every launch.
func ParseTemplate(source string) (Template, error) {
	var parts []templatePart
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		parts = append(parts, templatePart{kind: templateLiteral, text: text.String()})
		text.Reset()
	}
	for i := 0; i < len(source); i++ {
		if source[i] != '$' {
			text.WriteByte(source[i])
			continue
		}
		// A trailing dollar is the dollar itself.
		if i+1 == len(source) {
			text.WriteByte('$')
			break
		}
		switch source[i+1] {
		case '$':
			text.WriteByte('$')
			i++
		case '{':
			end := strings.IndexByte(source[i+2:], '}')
			if end < 0 {
				return Template{}, fmt.Errorf("the value %q has an unterminated %q", source, "${")
			}
			part, err := referencePart(source[i+2 : i+2+end])
			if err != nil {
				return Template{}, err
			}
			flush()
			parts = append(parts, part)
			i += 2 + end
		default:
			text.WriteByte('$')
		}
	}
	flush()
	return Template{parts: parts}, nil
}

// referencePart returns the piece a `${...}` reference names.
func referencePart(body string) (templatePart, error) {
	name, value, hasDefault := strings.Cut(body, ":-")
	if name == "" {
		return templatePart{}, fmt.Errorf("the reference %q names no variable", "${"+"}")
	}
	if scope, field, reference := strings.Cut(name, ":"); reference {
		if hasDefault {
			return templatePart{}, fmt.Errorf("the reference %q takes no default, and only a host variable does", "${"+name+"}")
		}
		// The accepted names are the exec package's, so the load refuses
		// exactly what a launch would fail to resolve.
		if err := exec.ValidSessionReference(scope, field); err != nil {
			return templatePart{}, fmt.Errorf("the reference %q: %w", "${"+name+"}", err)
		}
		return templatePart{kind: templateSession}, nil
	}
	return templatePart{kind: templateHost, text: name, defaultValue: value, hasDefault: hasDefault}, nil
}

// Resolve returns the value the template stands for.
//
// host looks a variable name up, and a nil host resolves no variable.
// sessionID is the session's own ID. A `${NAME}` the host does not hold
// and the template gives no default for is an error: a command that runs
// with an empty argument or an empty environment value is worse than a
// server that does not launch.
func (t Template) Resolve(host func(string) (string, bool), sessionID string) (string, error) {
	if len(t.parts) == 0 {
		return "", nil
	}
	var out strings.Builder
	for _, part := range t.parts {
		switch part.kind {
		case templateHost:
			if host != nil {
				if value, ok := host(part.text); ok {
					out.WriteString(value)
					continue
				}
			}
			if part.hasDefault {
				out.WriteString(part.defaultValue)
				continue
			}
			return "", fmt.Errorf("the environment variable %q is not set, and the manifest gives it no default", part.text)
		case templateSession:
			if sessionID == "" {
				return "", fmt.Errorf("the reference %q resolves to no session id", "${session:id}")
			}
			out.WriteString(sessionID)
		default:
			out.WriteString(part.text)
		}
	}
	return out.String(), nil
}
