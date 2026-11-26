// Command hang is the MCP server that never speaks: it starts, writes
// its own pid to the file the environment names, and then hangs
// before the handshake, so a test can watch a launch time out and
// prove the process did not survive the failure.
package main

import (
	"os"
	"strconv"
	"time"
)

func main() {
	if path := os.Getenv("POO_FIXTURE_PIDFILE"); path != "" {
		_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	// A sleeper, never a bare select: the runtime calls a program whose
	// every goroutine waits on a select a deadlock, and a crashed
	// process is not a hung one.
	for {
		time.Sleep(time.Hour)
	}
}
