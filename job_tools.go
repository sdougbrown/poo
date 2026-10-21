package poo

import (
	"encoding/json"
	"fmt"
)

// registerJobTools adds the core's own job tools and checks that every
// linked tool may be registered.
//
// The job tools are registered whenever a tool declares that it may run
// in the background: a model that can start a job needs a way to see
// them and a way to stop them.
//
// A tool whose own schema defines the reserved background argument
// cannot be registered: the core would not know whether the model meant
// the tool's argument or its own.
func (s *Session) registerJobTools() error {
	hasBackground := false
	for name, tool := range s.opts.Tools {
		if tool == nil {
			return fmt.Errorf("poo: the tool %q is nil", name)
		}
		spec := tool.Spec()
		if spec.Name == "" {
			return fmt.Errorf("poo: the tool %q has no name", name)
		}
		switch spec.Background {
		case BackgroundNever, BackgroundAllowed, BackgroundAlways:
		default:
			return fmt.Errorf("poo: the tool %q has an unknown background mode %q", name, spec.Background)
		}
		if spec.Background != BackgroundNever && schemaDefinesBackground(spec.Schema) {
			return fmt.Errorf("poo: the tool %q defines the reserved %q argument", name, BackgroundArgName)
		}
		if spec.Background != BackgroundNever {
			hasBackground = true
		}
	}
	if !hasBackground {
		return nil
	}
	if _, taken := s.opts.Tools[JobsToolName]; taken {
		return fmt.Errorf("poo: the tool %q is the core's own", JobsToolName)
	}
	if _, taken := s.opts.Tools[CancelJobToolName]; taken {
		return fmt.Errorf("poo: the tool %q is the core's own", CancelJobToolName)
	}
	s.opts.Tools[JobsToolName] = &jobTool{session: s, name: JobsToolName}
	s.opts.Tools[CancelJobToolName] = &jobTool{session: s, name: CancelJobToolName}
	s.opts.ToolOrder = append(s.opts.ToolOrder, JobsToolName, CancelJobToolName)
	return nil
}

// toolSpecFor returns the spec the model sees for a tool.
//
// A tool that may run in the background is offered the reserved
// background argument, so the model knows it may ask for a job.
func (s *Session) toolSpecFor(name string) ToolSpec {
	tool, ok := s.opts.Tools[name]
	if !ok {
		return ToolSpec{Name: name}
	}
	spec := tool.Spec()
	if spec.Background == BackgroundAllowed {
		spec.Schema = withBackgroundSchema(spec.Schema)
	}
	return spec
}

// backgroundModeOf returns a tool's background mode.
func (s *Session) backgroundModeOf(name string) Background {
	tool, ok := s.opts.Tools[name]
	if !ok {
		return BackgroundNever
	}
	return tool.Spec().Background
}

// callBecomesJob reports whether a call runs as a job.
//
// A tool that says always makes every call a job. A tool that says
// allowed makes a job of the calls that ask for one.
func (s *Session) callBecomesJob(name string, asked bool) bool {
	switch s.backgroundModeOf(name) {
	case BackgroundAlways:
		return true
	case BackgroundAllowed:
		return asked
	default:
		return false
	}
}

// stripBackgroundArg removes the reserved background argument from a
// call's arguments.
//
// The core validates the argument and removes it before the tool's own
// decoder sees the arguments, so a strict decoder never sees it. The
// tool.call record keeps the original arguments.
func stripBackgroundArg(raw json.RawMessage) (asked bool, stripped json.RawMessage, err error) {
	asked, stripped, err = backgroundArg(raw)
	if err != nil {
		return false, raw, err
	}
	return asked, stripped, nil
}
