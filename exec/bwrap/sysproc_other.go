//go:build unix && !linux

package bwrap

import "syscall"

// sysProcAttr puts the sandbox in its own process group, so the whole group can
// be signalled at once.
//
// The parent-death signal is Linux's, and bubblewrap with it: this build carries
// the group alone, so a teardown signals the tree and nothing ties the child to
// poo's own death.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
