// Package poo is the core of the poo agent harness.
//
// Poo is a modular agent harness written in Go. It ships two things from one
// codebase: a headless CLI that runs a session to completion, and a Go library
// that a wrapping application links into its own binary, injecting its own
// tools, providers and policies.
//
// The core owns the agent loop, the tool registry, the hook points, the event
// stream and the session lifecycle. Everything else is an opt-in package
// wired at compile time, so what a consumer leaves out cannot be reached at
// runtime.
//
// This package depends on the standard library, plus the optional poo/prompt
// and poo/decide packages.
package poo
