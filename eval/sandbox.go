package eval

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// The sandbox modes a run can use.
const (
	// SandboxNone runs poo directly. A model can reach anything the
	// harness's user can, including the task set and its checks.
	SandboxNone = "none"
	// SandboxBwrap runs poo under bubblewrap. The run sees the system's
	// read-only /usr and /etc, its own workdir at /work and its log
	// directory at /log, and nothing else of the filesystem. The network
	// is shared, so the model's endpoint stays reachable.
	SandboxBwrap = "bwrap"
)

// The paths a sandboxed run sees.
const (
	sandboxWork = "/work"
	sandboxLog  = "/log"
	sandboxPoo  = "/poo/poo"
)

// checkSandbox reports whether a sandbox mode can be used.
func checkSandbox(mode string) error {
	switch mode {
	case "", SandboxNone:
		return nil
	case SandboxBwrap:
		if _, err := exec.LookPath("bwrap"); err != nil {
			return fmt.Errorf("eval: the bwrap sandbox needs bubblewrap installed: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("eval: unknown sandbox %q, want %s or %s", mode, SandboxNone, SandboxBwrap)
	}
}

// bwrapArgs returns the bubblewrap arguments that run poo with the given
// arguments inside the sandbox.
//
// The workdir and the log's directory are the only writable paths, and
// poo's --working-dir and --log are rewritten to name them as the
// sandbox sees them.
func bwrapArgs(poo, workdir, logPath string, args []string) []string {
	inner := make([]string, len(args))
	copy(inner, args)
	for i := 0; i+1 < len(inner); i++ {
		switch inner[i] {
		case "--working-dir":
			inner[i+1] = sandboxWork
		case "--log":
			inner[i+1] = filepath.Join(sandboxLog, filepath.Base(logPath))
		}
	}
	out := []string{
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/sbin", "/sbin",
		"--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib64", "/lib64",
		"--ro-bind", "/etc", "/etc",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--ro-bind", poo, sandboxPoo,
		"--bind", workdir, sandboxWork,
		"--bind", filepath.Dir(logPath), sandboxLog,
		"--chdir", sandboxWork,
		"--unshare-all",
		"--share-net",
		"--die-with-parent",
		"--new-session",
		sandboxPoo,
	}
	return append(out, inner...)
}
