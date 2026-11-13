//go:build linux

package bwrap

import "syscall"

// sysProcAttr puts the sandbox in its own process group, so the whole group can
// be signalled at once.
//
// The parent-death signal belongs to the sandbox's tie to poo: --die-with-parent
// ties the sandbox to bubblewrap, so a poo that dies without running a teardown
// takes the sandbox with it instead of leaving it to init. Only Linux carries
// that signal, which is also the only platform bubblewrap runs on.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
