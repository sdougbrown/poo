package depcheck_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestAReadOnlyBinaryLinksNoToolItDidNotLink covers the composition
// gate: a binary built from the exported factory, the search profile,
// read, grep and the openai provider links what it composes, and not the
// tools it left out.
//
// The subject is the read-only binary the conformance suite runs
// (wire/conformance/testdata/readonly).
func TestAReadOnlyBinaryLinksNoToolItDidNotLink(t *testing.T) {
	root := moduleRoot(t)
	modulePath := modulePathOf(t, root)
	touchGoFiles(t, root)

	deps := listDeps(t, root, "./wire/conformance/testdata/readonly")

	for _, excluded := range []string{"tool/bash", "tool/edit", "tool/cliregistry", "exec/bwrap"} {
		if slices.Contains(deps, modulePath+"/"+excluded) {
			t.Errorf("the read-only binary links %s", modulePath+"/"+excluded)
		}
	}
	// The closure the gate is about: a subject that stopped importing
	// what it composes would pass the exclusions by linking nothing.
	for _, included := range []string{
		"config",
		"server",
		"profile/search",
		"tool/read",
		"tool/grep",
		"provider/openai",
	} {
		if !slices.Contains(deps, modulePath+"/"+included) {
			t.Errorf("the read-only binary does not link %s", modulePath+"/"+included)
		}
	}
}

// listDeps returns the import paths the package's closure holds.
func listDeps(t *testing.T, root, pkg string) []string {
	t.Helper()

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg)
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("listing the deps of %s: %v\n%s", pkg, err, out)
	}
	return strings.Fields(string(out))
}
