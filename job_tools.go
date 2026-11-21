package poo

import "fmt"

// coreToolNames is the set of tool names the core owns: the job tools
// it registers below, and the finish tool the consumer links.
//
// A nested call may not run any of them: the job tools are the run's own
// way of reaching its jobs, and the finish tool would settle the run out
// from under the script that called it.
var coreToolNames = map[string]struct{}{
	FinishToolName:    {},
	JobsToolName:      {},
	CancelJobToolName: {},
}

// nestedRefusal reports why a codemode script may not call a tool by
// name, or "" when it may.
//
// The run's own tools are outside a script's reach: the finish tool would
// settle the run the script runs in, and the job tools drive it. A tool
// that only runs in the background is out too, because a nested call runs
// in the turn its parent runs in. The tools a script is offered and the
// tools a nested batch may call both come from this one rule, so the two
// cannot drift.
func nestedRefusal(name string, spec ToolSpec) string {
	if _, core := coreToolNames[name]; core {
		return fmt.Sprintf("the tool %q cannot run as a nested call", name)
	}
	if spec.Background == BackgroundAlways {
		return fmt.Sprintf("the tool %q cannot run as a nested call", name)
	}
	return ""
}

// nestedCallable reports whether a codemode script may call a tool by
// name.
func nestedCallable(name string, spec ToolSpec) bool {
	return nestedRefusal(name, spec) == ""
}

// registerJobTools adds the core's own job tools and checks that every
// linked tool may be registered.
//
// The job tools are registered whenever a tool declares that it may run
// in the background, and whenever the session runs a feed: a model that
// can start a job needs a way to see them and a way to stop them, and a
// feed is listed and stopped through the same two tools.
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
	if !hasBackground && len(s.opts.Feeds) == 0 {
		return nil
	}
	if _, taken := s.opts.Tools[JobsToolName]; taken {
		return fmt.Errorf("poo: the tool %q is the core's own", JobsToolName)
	}
	if _, taken := s.opts.Tools[CancelJobToolName]; taken {
		return fmt.Errorf("poo: the tool %q is the core's own", CancelJobToolName)
	}
	if s.opts.Tools == nil {
		s.opts.Tools = make(map[string]Tool, 2)
	}
	s.opts.Tools[JobsToolName] = &JobTool{session: s, name: JobsToolName}
	s.opts.Tools[CancelJobToolName] = &JobTool{session: s, name: CancelJobToolName}
	s.opts.ToolOrder = append(s.opts.ToolOrder, JobsToolName, CancelJobToolName)

	// The tool schemas compile once, at registration. A schema that
	// does not compile is a tool bug, and the construction fails
	// naming it. The compiled schemas hold every call to the contract
	// the model was given.
	schemas, err := compileArgSchemas(s.opts.Tools)
	if err != nil {
		return err
	}
	s.argSchemas = schemas
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
	return shownSpec(tool)
}

// shownSpec returns the spec the model sees for a tool.
//
// A tool that may run in the background is offered the reserved
// background argument, so the schema the model reads, the schema the
// session validates against, and the vocabulary a refusal renders are
// one and the same. A tool with no schema opts out of all of it.
func shownSpec(tool Tool) ToolSpec {
	spec := tool.Spec()
	if spec.Background == BackgroundAllowed && len(spec.Schema) > 0 {
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
