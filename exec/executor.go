// Package exec holds the contract a binary's executor fills: what a
// command is, how it starts and how it ends.
//
// A tool that runs commands takes an Executor rather than spawning
// processes itself, so a binary chooses the executors it links without
// the tool knowing where the command runs. The package also builds the
// child environment, less the names the operator scrubs.
//
// Argument names distinguish three layers: Argv is process argv, Args
// are CLI arguments, and Arguments are tool-call arguments.
package exec

import (
	"context"
	"io"
)

// Executor runs the command a tool asks for.
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
	// The output of stdout and stderr is one stream unless the command
	// asks for them apart, so it arrives in the order the command
	// produced it. A writer that stops accepting the output does not
	// stop the command: the executor keeps reading and discards what
	// the writer will not take.
	//
	// The context ends the command: when it is done, the executor
	// stops the command the way Kill does. A context that has already
	// ended starts nothing: Start returns its error and no process.
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
	// Stderr is where the command's error output goes, when the
	// caller needs the two streams apart. A nil writer keeps them
	// merged into out, interleaved in the order the command wrote
	// them.
	Stderr io.Writer
}

// Process is a command that is running.
type Process interface {
	// Wait waits for the command to finish and for everything it
	// started to die. It returns the command's exit code; a command
	// that was killed has none, and its code is negative.
	Wait() (exitCode int, err error)
	// Kill stops the command and everything it started.
	Kill()
	// Stopped reports that the command did not exit on its own: the
	// executor killed it, which it does when the context ends. Wait has
	// to have returned. A command that exited on its own keeps that
	// outcome, however long the teardown of what it left behind takes.
	Stopped() bool
}

// Masker expands the protected paths to mask.
//
// A sandbox asks it about every directory the sandbox mounts, not only the
// working directory, so a path the operator protects inside a read-only or
// read-write bind is masked as well. The paths it returns are absolute; the
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
	// Sandbox holds the options a sandboxing executor needs.
	//
	// The settings name the sandbox's own options: the network, the
	// binds and the paths to mask. The executor that is a sandbox
	// reads them, and the executor that is not ignores them.
	Sandbox SandboxOptions
}

// SandboxOptions holds a sandbox's options.
//
// The options are plain data, so the executor that is a sandbox reads
// them without the tool package knowing what a sandbox is.
type SandboxOptions struct {
	// Network says whether the sandbox shares the host's network
	// namespace. Nil and true share it; false gives the sandbox its
	// own empty one.
	Network *bool
	// ROBinds are the host paths the sandbox mounts read-only.
	ROBinds []string
	// RWBinds are the host paths the sandbox mounts read-write.
	RWBinds []string
}
