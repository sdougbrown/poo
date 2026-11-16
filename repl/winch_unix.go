//go:build unix

package repl

import (
	"os"
	"os/signal"
	"syscall"
)

// resizeSignals subscribes ch to the terminal's size changes.
func resizeSignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGWINCH)
}
