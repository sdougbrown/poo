package config

import "github.com/sdougbrown/poo"

// ToolSpecs returns the specs of the tools the session offers.
//
// The order is the order the profile offers them in, and the specs are
// the tools' own: a name, a class and whether a call may run in the
// background.
func (r *Runtime) ToolSpecs() []poo.ToolSpec {
	specs := make([]poo.ToolSpec, 0, len(r.Options.ToolOrder))
	for _, name := range r.Options.ToolOrder {
		tool, ok := r.Options.Tools[name]
		if !ok || tool == nil {
			continue
		}
		specs = append(specs, tool.Spec())
	}
	return specs
}

// SessionTools returns the tools a session records in its header.
//
// The list derives from the specs the session offers: one source for a
// tool's name and class, so the log cannot drift from the wire.
func (r *Runtime) SessionTools() []poo.SessionTool {
	specs := r.ToolSpecs()
	tools := make([]poo.SessionTool, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, poo.SessionTool{
			Name:       spec.Name,
			Class:      spec.Class,
			Background: spec.Background,
		})
	}
	return tools
}
