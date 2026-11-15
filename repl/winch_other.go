//go:build !unix

package repl

import "os"

// resizeSignals does nothing where the terminal reports no size change:
// the layout keeps the size it started with.
func resizeSignals(ch chan<- os.Signal) {}
