//go:build unix

package repl

import (
	"os"
	"os/signal"
	"syscall"
)

// hasResizeSignals reports whether this platform delivers terminal resize
// signals so the run loop can rebuild the layout after a size change.
const hasResizeSignals = true

// resizeSignals subscribes ch to the terminal's size changes.
func resizeSignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGWINCH)
}
