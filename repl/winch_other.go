//go:build !unix

package repl

import "os"

// hasResizeSignals is false where the terminal reports no size change:
// the layout keeps the size it started with and never defers paints.
const hasResizeSignals = false

// resizeSignals does nothing where the terminal reports no size change:
// the layout keeps the size it started with.
func resizeSignals(ch chan<- os.Signal) {}
