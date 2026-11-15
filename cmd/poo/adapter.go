package main

import (
	"context"
	"errors"
	"io"
	"strings"

	clicontract "github.com/sdougbrown/cli-contract"
)

// The command's result vocabulary: the exit codes a failure takes, and the
// stderr lines that explain it.

// exit is a command's result: the exit code the process ends with. A
// command writes its own stdout and stderr, so the adapter has nothing to
// print for it.
type exit struct{ code int }

const (
	// operationalExitCode is a failure with no code of its own.
	operationalExitCode = 1
	// usageExitCode is a command's refusal of its arguments or its
	// settings, and every failure the library catches before dispatch.
	usageExitCode = 2
)

// pooAdapter routes the declared surface's results and failures.
type pooAdapter struct{}

// ErrorResult has nothing to write: poo's failures go to stderr.
func (pooAdapter) ErrorResult(string, error) any { return nil }

// Write prints nothing: every command writes its own stdout.
func (pooAdapter) Write(io.Writer, any) error { return nil }

// ExitCode answers with the code the command reported, or with the code
// its failure takes. A failure caught before dispatch is the caller's.
func (pooAdapter) ExitCode(_ clicontract.Invocation, result any, handlerErr error) int {
	if handlerErr != nil {
		var usage *clicontract.UsageError
		if errors.As(handlerErr, &usage) {
			return usageExitCode
		}
		return operationalExitCode
	}
	if code, ok := result.(exit); ok {
		return code.code
	}
	return 0
}

// Deadline leaves the context alone: the command that bounds its runtime
// does it through its own --timeout flag.
func (pooAdapter) Deadline(ctx context.Context, _ clicontract.Invocation) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}

// Validate refuses nothing before dispatch: a command checks its flags
// itself, because a flag's check reads more than the flag.
func (pooAdapter) Validate(clicontract.Invocation) error { return nil }

// BeforeHandler prints nothing.
func (pooAdapter) BeforeHandler(clicontract.Invocation, io.Writer) {}

// HandlerDiagnostic keeps the prefix every poo diagnostic carries.
func (pooAdapter) HandlerDiagnostic(_ clicontract.Invocation, err error) string {
	return "poo: " + err.Error()
}

// DispatchDiagnostic keeps the prefix every poo diagnostic carries.
//
// The library builds the failure before dispatch and prefixes it with the
// command path, so a message that already carries the prefix is left
// alone: only the failures cobra raises itself, such as an unknown
// command, arrive bare.
func (pooAdapter) DispatchDiagnostic(_ string, err error) string {
	message := err.Error()
	if strings.HasPrefix(message, "poo:") || strings.HasPrefix(message, "poo ") {
		return message
	}
	return "poo: " + message
}
