package bash

import (
	"context"
	"io"
)

// Executor runs the command a bash call asks for.
//
// The tool does not spawn processes itself: it hands the command to an
// executor, and the executor decides where the command runs. A command
// that runs in a sandbox never sees the credentials the host tools
// use, because the sandbox is another process with another environment.
//
// An executor is chosen when the binary is built (which executors it
// links) and when the session is set up (which one it uses).
type Executor interface {
	// Name is the executor's name, as the settings name it.
	Name() string
	// Start runs the command and writes its output to out.
	//
	// The output of stdout and stderr is one stream, so it arrives in
	// the order the command produced it. A writer that stops accepting
	// the output does not stop the command: the executor keeps
	// reading and discards what the writer will not take.
	//
	// The context ends the command: when it is done, the executor
	// stops the command the way Kill does.
	Start(ctx context.Context, c Command, out io.Writer) (Process, error)
}

// Command is what an executor runs.
type Command struct {
	// Argv is the command and its arguments, e.g.
	// ["/bin/bash", "-c", "ls -l"].
	Argv []string
	// Dir is the directory the command runs in.
	Dir string
	// Env is the command's environment, already scrubbed. A nil
	// environment means the command inherits the executor's own.
	Env []string
}

// Process is a command that is running.
type Process interface {
	// Wait waits for the command to finish and for everything it
	// started to die. It returns the command's exit code; a command
	// that was killed has none, and its code is negative.
	Wait() (exitCode int, err error)
	// Kill stops the command and everything it started.
	Kill()
}

// Masker expands the protected paths to mask.
//
// A sandbox asks it about every directory the sandbox mounts, not only the
// working directory, so a path the operator protects inside a read-only or
// read-write bind is hidden as well. The paths it returns are absolute; the
// executor masks the ones a bind reaches.
type Masker interface {
	// Expand returns the protected paths at or under root.
	Expand(root string) ([]string, error)
}

// Options are what a constructor is given.
type Options struct {
	// Masker expands the protected paths to mask. It is nil when the
	// session protects nothing.
	Masker Masker
}
