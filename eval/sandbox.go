package eval

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// The sandbox modes a run can use.
const (
	// SandboxNone runs poo directly. A model can reach anything the
	// runner's user can, including the task set and its checks.
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

// The executors a run's bash tool may use.
//
// The executor is a layer under the sandbox: the sandbox wraps the
// whole poo process, and the executor wraps the commands the model
// runs. A run may use both, and a run may use the executor alone.
const (
	// ExecutorLocal runs the model's commands in poo's own process
	// space.
	ExecutorLocal = "local"
	// ExecutorBwrap runs the model's commands in a bubblewrap
	// sandbox, bound to the task's workdir.
	ExecutorBwrap = "bwrap"
)

// checkExecutor reports whether an executor can be used.
//
// The executor is chosen by the binary that runs, not by the runner:
// the runner only says which one the run wants. The check here is for
// the one thing the runner knows: a sandboxed executor needs
// bubblewrap on the host.
func checkExecutor(name string) error {
	switch name {
	case "", ExecutorLocal:
		return nil
	case ExecutorBwrap:
		if _, err := exec.LookPath("bwrap"); err != nil {
			return fmt.Errorf("eval: the bwrap executor needs bubblewrap installed: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("eval: unknown executor %q, want %s or %s", name, ExecutorLocal, ExecutorBwrap)
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
		// /etc/resolv.conf is often a link into systemd-resolved's
		// directory, so the resolver needs that directory too. It is
		// skipped on a machine without it.
		"--ro-bind-try", "/run/systemd/resolve", "/run/systemd/resolve",
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
