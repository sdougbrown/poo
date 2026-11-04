package exec

import (
	"os"
	"path/filepath"
	"strings"
)

// Scrub describes how a child environment is built.
//
// The environment a command gets is the session's environment minus the
// names the operator asked to keep out of it. The rules are the ones
// the plan names: the variables an endpoint's key is read from, the
// globs the operator listed, and the names the operator wants to pass
// through and nothing else.
type Scrub struct {
	// Names are the exact names of the variables to remove.
	Names []string
	// Globs are the glob patterns of the names to remove. A pattern
	// is matched against the name of the variable, so "AWS_*" removes
	// every variable whose name starts with AWS_.
	Globs []string
	// Passthrough limits the environment to exactly these names when
	// it is not empty. A name that is scrubbed as well is still gone.
	Passthrough []string
}

// Empty reports whether the rules change nothing.
func (s Scrub) Empty() bool {
	return len(s.Names) == 0 && len(s.Globs) == 0 && len(s.Passthrough) == 0
}

// ChildEnv returns the environment for the child process.
//
// A nil base means the parent's own environment. The scrubbed names are
// removed from whichever base is used, so an API key never reaches the
// shell.
func ChildEnv(base []string, scrub []string) []string {
	if len(scrub) == 0 && base == nil {
		return nil
	}
	return ScrubEnv(base, Scrub{Names: scrub})
}

// ScrubEnv builds a child environment from a base.
//
// The base loses every scrubbed name and every name a scrubbed glob
// matches. With a passthrough list, the environment holds exactly the
// names on it and nothing else.
//
// A nil base means the parent's own environment.
func ScrubEnv(base []string, scrub Scrub) []string {
	if base == nil {
		base = os.Environ()
	}
	if scrub.Empty() {
		return base
	}

	keep := passthroughSet(scrub.Passthrough)
	out := make([]string, 0, len(base))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if keep != nil && !keep[name] {
			continue
		}
		if scrubbed(name, scrub) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Lookup returns a lookup over an environment's NAME=value entries.
//
// A feed's or a CLI's environment references are resolved through it,
// so a name the environment does not hold is one the reference cannot
// have. A nil environment means the host's own, the way it does for a
// child that inherits one.
func Lookup(environ []string) func(string) (string, bool) {
	if environ == nil {
		environ = os.Environ()
	}
	values := make(map[string]string, len(environ))
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		values[name] = value
	}
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

// passthroughSet returns the names a passthrough list holds, or nil
// when the list is empty and everything is kept.
func passthroughSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// scrubbed reports whether a variable name is scrubbed.
func scrubbed(name string, scrub Scrub) bool {
	for _, scrubbed := range scrub.Names {
		if name == scrubbed {
			return true
		}
	}
	for _, pattern := range scrub.Globs {
		// A pattern that does not compile matches nothing: a name is
		// never a pattern, so a bad pattern cannot remove it by
		// accident.
		if matched, err := filepath.Match(pattern, name); err == nil && matched {
			return true
		}
	}
	return false
}
