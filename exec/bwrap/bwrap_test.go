package bwrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
)

// The plan's bwrap list: what the sandbox contains, what it does not,
// and what happens when the sandbox cannot be created.

// newExecutor returns a bwrap executor, or skips the test with the
// reason logged.
//
// A host without bubblewrap, or without unprivileged user namespaces,
// cannot run the sandboxed executor. The test says so rather than
// failing: the sandbox is Linux-only, and the rest of the build runs
// everywhere.
func newExecutor(t *testing.T, opts bwrap.Options) *bwrap.Executor {
	t.Helper()
	exec, err := bwrap.New(opts)
	if err != nil {
		t.Skipf("skip: the bwrap executor is unavailable: %v", err)
	}
	return exec
}

// call runs a command through the tool and returns the output.
func call(t *testing.T, exec *bwrap.Executor, command, dir string) string {
	t.Helper()
	tool := bash.New(bash.WithExecutor(exec))
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshalling the arguments: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing %q: %v", command, err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	if err != nil {
		t.Fatalf("calling %q: %v", command, err)
	}
	var obs map[string]any
	if err := json.Unmarshal(res.Structured, &obs); err != nil {
		t.Fatalf("decoding the observation %q: %v", res.Structured, err)
	}
	text, _ := obs["output"].(string)
	return text
}

// TestAFileOutsideTheWorkingDirectoryIsInvisible covers the plan's
// filesystem: the command sees the system directories and its working
// directory, and nothing else.
func TestAFileOutsideTheWorkingDirectoryIsInvisible(t *testing.T) {
	exec := newExecutor(t, bwrap.Options{})
	workdir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("the host's file"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	got := call(t, exec, fmt.Sprintf("cat %s 2>&1; echo ---; cat /etc/hostname 2>&1 | head -1", secret), workdir)
	if strings.Contains(got, "the host's file") {
		t.Errorf("a file outside the working directory is readable: %q", got)
	}
	if !strings.Contains(got, "No such file") {
		t.Errorf("the file outside the working directory did not fail: %q", got)
	}
	// The system directories are there: the command can run at all.
	if !strings.Contains(got, "---") {
		t.Errorf("the command did not run: %q", got)
	}
}

// TestTheHomeDirectoryIsNotMounted covers the plan's filesystem: $HOME
// is absent, or holds only the directories a bind needed, so the
// operator's files are not the command's.
func TestTheHomeDirectoryIsNotMounted(t *testing.T) {
	exec := newExecutor(t, bwrap.Options{})
	workdir := t.TempDir()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("skip: the home directory is unknown: %v", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Skipf("skip: the home directory cannot be read: %v", err)
	}
	marker := ""
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			marker = entry.Name()
			break
		}
	}

	// The sandbox creates the directories a bind needs. When the working
	// directory is under the home, that is the one entry the home may
	// hold; every other entry of the real home must be absent.
	allowed := ""
	if rel, err := filepath.Rel(home, workdir); err == nil && !strings.HasPrefix(rel, "..") {
		allowed = strings.Split(rel, string(filepath.Separator))[0]
	}
	command := fmt.Sprintf("if [ -e %q ]; then echo present; ls -A %q; else echo absent; fi", home, home)
	if marker != "" {
		command += fmt.Sprintf("; cat %q 2>&1", filepath.Join(home, marker))
	}
	got := call(t, exec, command, workdir)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if lines[0] == "present" {
		if allowed == "" {
			t.Errorf("the home directory is mounted: %q", got)
		} else {
			for _, line := range lines[1:] {
				if strings.HasPrefix(line, "cat:") {
					continue
				}
				if strings.TrimSpace(line) != allowed {
					t.Errorf("the home directory holds %q, want only %q: %q", line, allowed, got)
				}
			}
		}
	}
	if marker != "" && !strings.Contains(got, "No such file") {
		t.Errorf("a file of the home directory is readable: %q", got)
	}
}

// fakeMasker expands to a fixed list of paths.
type fakeMasker struct {
	paths []string
	err   error
}

func (m fakeMasker) Expand(root string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.paths, nil
}

// TestAProtectedPathReadsAsEmpty covers the plan's protected paths: a
// path inside the working directory is masked, so the command cannot
// read it.
func TestAProtectedPathReadsAsEmpty(t *testing.T) {
	workdir := t.TempDir()
	secret := filepath.Join(workdir, "secret.txt")
	if err := os.WriteFile(secret, []byte("the protected text"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	dir := filepath.Join(workdir, "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("making the directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inside.txt"), []byte("the protected text"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	exec := newExecutor(t, bwrap.Options{
		Masker: fakeMasker{paths: []string{secret, dir}},
	})

	got := call(t, exec, `echo "file=[$(cat secret.txt)]"; echo "dir=[$(ls -A secrets)]"`, workdir)
	if strings.Contains(got, "the protected text") {
		t.Errorf("a protected path is readable: %q", got)
	}
	if !strings.Contains(got, "file=[]") {
		t.Errorf("the protected file did not read as empty: %q", got)
	}
	if !strings.Contains(got, "dir=[]") {
		t.Errorf("the protected directory did not read as empty: %q", got)
	}
}

// TestAMaskerFailureStopsTheCommand covers the fail-closed rule: a
// masker that cannot say what to mask is an error, not a command that
// runs with the paths visible.
func TestAMaskerFailureStopsTheCommand(t *testing.T) {
	workdir := t.TempDir()
	exec := newExecutor(t, bwrap.Options{
		Masker: fakeMasker{err: errors.New("the matcher failed")},
	})
	tool := bash.New(bash.WithExecutor(exec))
	raw, err := json.Marshal(map[string]string{"command": "echo hello"})
	if err != nil {
		t.Fatalf("marshalling the arguments: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: workdir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	if _, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: workdir},
		Env:        os.Environ(),
	}); err == nil {
		t.Fatal("the command ran with a failing masker")
	}
}

// TestTheNetworkIsCutWhenTheSettingsSaySo covers the plan's network
// rule: the network is shared unless it is cut, and a cut network
// cannot reach the host's.
func TestTheNetworkIsCutWhenTheSettingsSaySo(t *testing.T) {
	// A listener on the loopback: the host's network is the one that
	// holds it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skip: the loopback listener failed: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	command := fmt.Sprintf(
		"if timeout 10 bash -c 'exec 3<>/dev/tcp/127.0.0.1/%d' 2>/dev/null; then echo connected; else echo refused; fi",
		port)

	shared := newExecutor(t, bwrap.Options{})
	if got := call(t, shared, command, t.TempDir()); !strings.Contains(got, "connected") {
		t.Errorf("the shared network did not reach the listener: %q", got)
	}

	off := false
	cut := newExecutor(t, bwrap.Options{Network: &off})
	if got := call(t, cut, command, t.TempDir()); !strings.Contains(got, "refused") {
		t.Errorf("the cut network reached the listener: %q", got)
	}
}

// TestASetsidEscapedDescendantDiesWithTheCall covers the plan's
// isolation limit: the local executor cannot stop a descendant that
// left the process group, and the sandbox can, because the namespace
// ends with the shell.
func TestASetsidEscapedDescendantDiesWithTheCall(t *testing.T) {
	exec := newExecutor(t, bwrap.Options{})
	workdir := t.TempDir()

	got := call(t, exec, "readlink /proc/self/ns/pid > ns.txt; setsid sleep 300 & echo $! > pid.txt; echo done", workdir)
	if !strings.Contains(got, "done") {
		t.Fatalf("the command did not run: %q", got)
	}

	ns := readFile(t, filepath.Join(workdir, "ns.txt"))
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(processesInNamespace(ns)) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the namespace %s still holds %v", ns, processesInNamespace(ns))
}

// TestTheExecutorIsUnavailableWithoutBubblewrap covers the plan's fail
// closed rule: a missing binary is an error, never a fallback to the
// local executor.
func TestTheExecutorIsUnavailableWithoutBubblewrap(t *testing.T) {
	_, err := bwrap.New(bwrap.Options{Binary: filepath.Join(t.TempDir(), "no-such-bwrap")})
	if err == nil {
		t.Fatal("the executor was built without bubblewrap")
	}
	var unavailable *bwrap.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("the failure is %v, want an unavailable executor", err)
	}
	if unavailable.Code() != bwrap.CodeUnavailable {
		t.Errorf("the code is %q, want %q", unavailable.Code(), bwrap.CodeUnavailable)
	}
	if !strings.Contains(err.Error(), bwrap.CodeUnavailable) {
		t.Errorf("the message %q does not name the code", err)
	}
}

// TestTheExecutorIsUnavailableWhenTheNamespacesFail covers the plan's
// fail closed rule: a host that does not allow the namespaces is an
// error, never a fallback to the local executor.
func TestTheExecutorIsUnavailableWhenTheNamespacesFail(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "bwrap")
	script := "#!/bin/sh\necho 'bwrap: Creating new namespace failed: Operation not permitted' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatalf("writing the fake binary: %v", err)
	}

	_, err := bwrap.New(bwrap.Options{Binary: fake})
	if err == nil {
		t.Fatal("the executor was built with a binary that cannot create the namespaces")
	}
	var unavailable *bwrap.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("the failure is %v, want an unavailable executor", err)
	}
	if unavailable.Code() != bwrap.CodeUnavailable {
		t.Errorf("the code is %q, want %q", unavailable.Code(), bwrap.CodeUnavailable)
	}
}

// TestABadReadWriteBindIsUnavailableAtConstruction covers the probe: a
// read-write bind that the sandbox cannot make is an executor that cannot
// run, not a failure a command discovers later.
func TestABadReadWriteBindIsUnavailableAtConstruction(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-directory")
	_, err := bwrap.New(bwrap.Options{RWBinds: []string{missing}})
	if err == nil {
		t.Fatal("the executor was built with a read-write bind that does not exist")
	}
	var unavailable *bwrap.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("the failure is %v, want an unavailable executor", err)
	}
	if unavailable.Code() != bwrap.CodeUnavailable {
		t.Errorf("the code is %q, want %q", unavailable.Code(), bwrap.CodeUnavailable)
	}
}

// TestTheExecutorIsNamedBwrap covers the executor's name: the settings
// name it, and the name is what the registry holds.
func TestTheExecutorIsNamedBwrap(t *testing.T) {
	exec := newExecutor(t, bwrap.Options{})
	if exec.Name() != "bwrap" {
		t.Errorf("the executor is named %q, want bwrap", exec.Name())
	}
}

// readFile returns a file's contents, trimmed.
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return strings.TrimSpace(string(raw))
}

// processesInNamespace returns the pids of the processes in a pid
// namespace.
//
// The namespace is an identity, not a pattern: a process is in it when
// its namespace link is the same one.
func processesInNamespace(ns string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		link, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
		if err != nil {
			continue
		}
		if link == ns {
			pids = append(pids, pid)
		}
	}
	return pids
}
