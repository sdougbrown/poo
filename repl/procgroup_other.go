//go:build !unix

package repl

import "os/exec"

// ownProcessGroup is the platforms without process groups: the child
// runs in the repl's, and the repl's teardown ends it.
func ownProcessGroup(cmd *exec.Cmd) {}
