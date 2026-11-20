package quickjs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fastschema/qjs"
)

// TestTheGuestMountCannotOpenHostFiles covers the boundary behind the
// wrapper's globals deletion: the qjs option every run builds mounts a
// path under the null device, so a file operation must never open a host
// file. The wrapper removes `std` before the model's script runs, so this
// probe drives the mount directly, where `std` still exists.
//
// A trap here is the pass: the inert mount makes any open trap the
// runtime. A clean run of the probe means the mount became usable — most
// plausibly by repointing CWD at a real directory — and fails the test.
func TestTheGuestMountCannotOpenHostFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(filepath.Join(dir, "input.txt"))
	probes := map[string]string{
		"relative read":  `std.open("input.txt", "r")`,
		"absolute read":  `std.open(` + string(quoted) + `, "r")`,
		"relative write": `std.open("output.txt", "w")`,
	}
	for name, open := range probes {
		t.Run(name, func(t *testing.T) {
			trapped, err := probe(t, `
const f = `+open+`;
if (f !== null) throw new Error("the mount opened a host file");
0`)
			if err != nil && strings.Contains(err.Error(), "the mount opened a host file") {
				t.Fatal("the mount opened a host file")
			}
			if !trapped && err == nil {
				t.Fatal("the file operation did not fail; the mount may be usable")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(dir, "output.txt")); !os.IsNotExist(err) {
		t.Fatalf("the write probe reached the workspace: %v", err)
	}
}

// probe runs one script on a guest built the way every run builds one.
// It reports whether the run trapped the runtime, and the eval error
// when it did not.
func probe(t *testing.T, code string) (trapped bool, err error) {
	t.Helper()
	vm, err := newVM(t.Context())
	if err != nil {
		t.Fatalf("building the guest: %v", err)
	}
	defer vm.Close()
	defer func() {
		trapped = recover() != nil
	}()
	_, err = vm.Context().Eval("probe.js", qjs.Code(code))
	return trapped, err
}
