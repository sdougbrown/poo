package bash_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/tool/bash"
)

// TestTheRegistryBuildsTheExecutorItHolds covers the registry: the
// name selects the constructor, and the executor comes back.
func TestTheRegistryBuildsTheExecutorItHolds(t *testing.T) {
	registry := bash.NewRegistry()
	registry.Register("local", func(bash.Options) (bash.Executor, error) {
		return bash.NewLocal(), nil
	})

	exec, err := registry.New("local", bash.Options{})
	if err != nil {
		t.Fatalf("building the executor: %v", err)
	}
	if exec.Name() != "local" {
		t.Errorf("the executor is named %q, want local", exec.Name())
	}
}

// TestAnUnknownExecutorIsATypedError covers the plan's build option: a
// name the binary does not link is a configuration error, and never a
// fallback to another executor.
func TestAnUnknownExecutorIsATypedError(t *testing.T) {
	registry := bash.NewRegistry()
	registry.Register("local", func(bash.Options) (bash.Executor, error) {
		return bash.NewLocal(), nil
	})

	exec, err := registry.New("bwrap", bash.Options{})
	if err == nil {
		t.Fatal("an executor the registry does not hold was built")
	}
	if exec != nil {
		t.Errorf("the failed build returned %v, want nothing", exec)
	}
	var unknown *bash.ErrUnknownExecutor
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
