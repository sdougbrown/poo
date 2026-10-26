package bash

import (
	"github.com/sdougbrown/poo/exec"
)

// The bash tool is one caller of the exec contract: a binary that links
// an executor needs the contract without the tool, so the constructor,
// the registry and the environment helpers are re-exported here.
type (
	// Masker expands the protected paths to mask.
	Masker = exec.Masker
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
