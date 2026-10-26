package bash

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Constructor builds an executor.
type Constructor func(Options) (Executor, error)

// Registry holds the executors a binary links.
//
// A binary links the executors it wants: it registers them here, and
// the settings pick one by name. A binary that links only one executor
// cannot run a command outside it.
//
// A name no linked executor answers to is a configuration error, never
// a fallback to another executor: a command that was meant to run in a
// sandbox must not run unconfined because the binary forgot to link
// one.
type Registry struct {
	mu    sync.RWMutex
	ctors map[string]Constructor
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{ctors: map[string]Constructor{}}
}

// Register adds a constructor under a name.
//
// Registering the same name twice replaces the earlier one.
func (r *Registry) Register(name string, ctor Constructor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctors == nil {
		r.ctors = map[string]Constructor{}
	}
	r.ctors[name] = ctor
}

// Names returns the names the registry holds, in order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.ctors))
	for name := range r.ctors {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// New builds the executor the name selects.
//
// A name the registry does not hold is an ErrUnknownExecutor.
func (r *Registry) New(name string, opts Options) (Executor, error) {
	r.mu.RLock()
	ctor, ok := r.ctors[name]
	linked := make([]string, 0, len(r.ctors))
	for linkedName := range r.ctors {
		linked = append(linked, linkedName)
	}
	r.mu.RUnlock()
	if !ok {
		slices.Sort(linked)
		return nil, &ErrUnknownExecutor{Name: name, Linked: linked}
	}
	return ctor(opts)
}

// ErrUnknownExecutor is the error for an executor name no linked
// executor answers to.
//
// The error is typed, so a caller can tell a name the binary does not
// link from an executor that could not be built.
type ErrUnknownExecutor struct {
	// Name is the name that was asked for.
	Name string
	// Linked names the executors the binary does link.
	Linked []string
}

func (e *ErrUnknownExecutor) Error() string {
	if len(e.Linked) == 0 {
		return fmt.Sprintf("bash: no executor named %q, and this binary links none", e.Name)
	}
	return fmt.Sprintf("bash: no executor named %q; this binary links %s",
		e.Name, strings.Join(e.Linked, ", "))
}
