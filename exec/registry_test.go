package exec_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/exec"
)

// localStub is an executor the registry tests build: the registry
// holds constructors, and what the constructor returns is the test's
// own.
type localStub struct{}

// Name is the name the stub answers to.
func (localStub) Name() string { return "local" }

// Start runs nothing: the stub is only built, never started.
func (localStub) Start(context.Context, exec.Command, io.Writer) (exec.Process, error) {
	return nil, nil
}

// TestTheRegistryBuildsTheExecutorItHolds covers the registry: the
// name selects the constructor, and the executor comes back.
func TestTheRegistryBuildsTheExecutorItHolds(t *testing.T) {
	registry := exec.NewRegistry()
	registry.Register("local", func(exec.Options) (exec.Executor, error) {
		return localStub{}, nil
	})

	built, err := registry.New("local", exec.Options{})
	if err != nil {
		t.Fatalf("building the executor: %v", err)
	}
	if built.Name() != "local" {
		t.Errorf("the executor is named %q, want local", built.Name())
	}
}

// TestAnUnknownExecutorIsATypedError covers the plan's build option: a
// name the binary does not link is a configuration error, and never a
// fallback to another executor.
func TestAnUnknownExecutorIsATypedError(t *testing.T) {
	registry := exec.NewRegistry()
	registry.Register("local", func(exec.Options) (exec.Executor, error) {
		return localStub{}, nil
	})

	built, err := registry.New("bwrap", exec.Options{})
	if err == nil {
		t.Fatal("an executor the registry does not hold was built")
	}
	if built != nil {
		t.Errorf("the failed build returned %v, want nothing", built)
	}
	var unknown *exec.ErrUnknownExecutor
	if !errors.As(err, &unknown) {
		t.Fatalf("the failure is %v, want an unknown executor", err)
	}
	if unknown.Name != "bwrap" {
		t.Errorf("the error names %q, want bwrap", unknown.Name)
	}
	if len(unknown.Linked) != 1 || unknown.Linked[0] != "local" {
		t.Errorf("the error lists %v, want the executors the binary links", unknown.Linked)
	}
	if !strings.Contains(err.Error(), "bwrap") {
		t.Errorf("the message %q does not name the executor", err)
	}
}
