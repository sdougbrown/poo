//go:build unix

package repl

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup puts the child in a process group of its own, so a
// signal aimed at the repl's group does not reach the server: the
// server is left to the repl's own teardown.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
