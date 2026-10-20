package eval

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
)

// The harness builds the environments its subprocesses run in.
//
// The catalog names one key variable per model. The harness knows every
// key variable the catalog names, and it removes all of them from the
// environments it builds — except the one the running model needs. A run
// against a model with no key must not carry another model's credential
// into the poo process, and no credential may reach the check, whose
// output the report keeps.

// keyEnvs returns the environment variables the catalog names as key
// variables.
//
// The names are sorted and de-duplicated, so the result does not depend
// on the catalog's order.
func keyEnvs(catalog []Model) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range catalog {
		name := strings.TrimSpace(m.APIKeyEnv)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// scrubbedEnv returns the environment a subprocess runs in.
//
// The base environment is the harness's own, minus every key variable
// the catalog names, plus the one key variable the running model needs.
// The key is passed by name on the command line, so the value reaches
// the process and nothing else.
func scrubbedEnv(base []string, keyEnvs []string, keep string) []string {
	remove := map[string]bool{}
	for _, name := range keyEnvs {
		remove[name] = true
	}
	if keep != "" {
		delete(remove, keep)
	}

	out := make([]string, 0, len(base))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if ok && remove[name] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// pooEnv returns the environment the poo subprocess runs in.
//
// The selected model's key variable stays, so the binary can read it,
// and every other key variable the catalog names is gone.
func (r *Runner) pooEnv(m Model) []string {
	return scrubbedEnv(os.Environ(), r.keyEnvs, m.APIKeyEnv)
}

// checkEnv returns the environment a check script runs in.
//
// Every key variable is gone: the check's output goes into the report,
// and a check that prints its environment must not print a credential.
func (r *Runner) checkEnv(extra ...string) []string {
	return append(scrubbedEnv(os.Environ(), r.keyEnvs, ""), extra...)
}

// randomName returns a short random name.
//
// The name is used for the scratch directories a run needs, so two runs
// never share one.
func randomName() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("eval: reading randomness: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
