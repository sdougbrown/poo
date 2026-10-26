package bash

import (
	"github.com/sdougbrown/poo/exec"
)

// The executor and the child environment are the exec package's.
//
// The bash tool is one caller of the contract: a binary that links an
// executor needs the contract without the tool, and the names below
// keep the tool's API whole.
type (
	// Executor runs the command a bash call asks for.
	Executor = exec.Executor
	// Command is what an executor runs.
	Command = exec.Command
	// Process is a command that is running.
	Process = exec.Process
	// Masker expands the protected paths to mask.
	Masker = exec.Masker
	// Options are what a constructor is given.
	Options = exec.Options
	// Sandbox holds a sandbox's options.
	Sandbox = exec.Sandbox
	// Constructor builds an executor.
	Constructor = exec.Constructor
	// Registry holds the executors a binary links.
	Registry = exec.Registry
	// ErrUnknownExecutor is the error for an executor name no linked
	// executor answers to.
	ErrUnknownExecutor = exec.ErrUnknownExecutor
	// Scrub describes how a child environment is built.
	Scrub = exec.Scrub
)

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return exec.NewRegistry() }

// ChildEnv returns the environment for the child process.
func ChildEnv(base []string, scrub []string) []string { return exec.ChildEnv(base, scrub) }

// ScrubEnv builds a child environment from a base.
func ScrubEnv(base []string, scrub Scrub) []string { return exec.ScrubEnv(base, scrub) }
