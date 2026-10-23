package bwrap

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The helper is the sandbox's init's child: it starts the command, waits
// for it, and reports the wait status to the executor. A shell cannot tell
// a signal death from an exit of 128 plus the signal, so the wait status is
// the only reliable report.
//
// It re-execs the running binary, because a separate helper binary would
// have to be installed beside every poo build.
func init() {
	if len(os.Args) < 2 || os.Args[1] != helperArg || os.Getenv(statusFDEnv) == "" {
		return
	}
	os.Exit(runHelper())
}

// runHelper runs the command and reports its wait status.
func runHelper() int {
	fd, err := strconv.Atoi(os.Getenv(statusFDEnv))
	if err != nil || fd < 3 {
		fmt.Fprintf(os.Stderr, "poo bwrap helper: bad status descriptor %q\n", os.Getenv(statusFDEnv))
		return 127
	}
	status := os.NewFile(uintptr(fd), "bwrap-status")
	// The command must not be able to write its own status: it would be
	// reporting on itself. The descriptor is closed when the command is
	// executed.
	syscall.CloseOnExec(fd)
	// Bubblewrap's status descriptor is the helper's too. The command has
	// no business with it.
	syscall.CloseOnExec(jsonStatusFD)
	_ = syscall.Close(jsonStatusFD)

	argv := os.Args[2:]
	if len(argv) == 0 {
		fmt.Fprint(os.Stderr, "poo bwrap helper: no command\n")
		writeStatus(status, statusReport{Exit: intPtr(127)})
		return 127
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withoutEnv(os.Environ(), statusFDEnv)
	// The command gets a process group of its own, so the helper can
	// signal the whole tree with one call.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "poo bwrap helper: %v\n", err)
		writeStatus(status, statusReport{Exit: intPtr(127)})
		return 127
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(signals)

	var escalation <-chan time.Time
	for {
		select {
		case <-done:
			report := waitReport(cmd.ProcessState)
			writeStatus(status, report)
			switch {
			case report.Signal != nil:
				return 128 + *report.Signal
			case report.Exit != nil:
				return *report.Exit
			default:
				return 1
			}
		case sig := <-signals:
			if s, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(-cmd.Process.Pid, s)
			}
			if escalation == nil {
				escalation = time.After(KillGrace)
			}
		case <-escalation:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			escalation = nil
		}
	}
}

// waitReport turns a finished process's wait status into the report the
// executor reads.
func waitReport(state *os.ProcessState) statusReport {
	if state == nil {
		return statusReport{Exit: intPtr(1)}
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok {
		return statusReport{Exit: intPtr(state.ExitCode())}
	}
	if status.Signaled() {
		signal := int(status.Signal())
		return statusReport{Signal: &signal}
	}
	code := status.ExitStatus()
	return statusReport{Exit: &code}
}

// writeStatus writes the report and closes the descriptor.
func writeStatus(status *os.File, report statusReport) {
	if status == nil {
		return
	}
	raw, err := json.Marshal(report)
	if err == nil {
		_, _ = status.Write(append(raw, '\n'))
	}
	_ = status.Close()
}

// withoutEnv returns an environment without the named variables.
func withoutEnv(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, dropName := range names {
			if name == dropName {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// intPtr returns a pointer to a copy of an int.
func intPtr(v int) *int { return &v }
