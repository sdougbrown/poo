// The compiled argument schemas of the session's tools, and the check
// that holds a model's call to them.
//
// A tool's spec carries its argument schema for the model. Here the
// same schema is the enforced contract. The authority is the closed
// vocabulary the cli-registry manifests already answer to. The
// built-in tools and the registered ones are judged by one set of
// rules.
package poo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sdougbrown/cli-registry/schema"
)

// argSchemas holds the compiled input schemas of the session's tools,
// by tool name.
//
// The map is written once at registration and read on every call, so
// it needs no lock: the writes end before the runs begin.
type argSchemas struct {
	schemas map[string]*schema.ValueSchema
}

// compileArgSchemas compiles every tool's input schema.
//
// A tool with no schema opts out. A schema that does not compile is a
// registration failure. The vocabulary is closed on purpose, so a tool
// that reaches past it is a tool bug. The failure names the keyword
// the tool used.
func compileArgSchemas(tools map[string]Tool) (*argSchemas, error) {
	compiled := &argSchemas{schemas: map[string]*schema.ValueSchema{}}
	for name, tool := range tools {
		spec := tool.Spec()
		if len(spec.Schema) == 0 {
			continue
		}
		var raw any
		if err := json.Unmarshal(spec.Schema, &raw); err != nil {
			return nil, fmt.Errorf("poo: the tool %q has a schema that is not JSON: %v", name, err)
		}
		valueSchema, err := schema.Compile(raw)
		if err != nil {
			var definitionErr *schema.CompileError
			if errors.As(err, &definitionErr) {
				return nil, fmt.Errorf("poo: the tool %q has a schema that does not compile: %s", name, definitionIssuesText(definitionErr.Issues))
			}
			return nil, fmt.Errorf("poo: the tool %q has a schema that does not compile: %v", name, err)
		}
		compiled.schemas[name] = valueSchema
	}
	return compiled, nil
}

// validate holds one call's arguments to its tool's compiled schema.
//
// The failures are the model's to correct, so they come back the way a
// tool's own Prepare refuses: invalid arguments, with the issues as
// the text. A tool that never registered a schema validates nothing.
func (a *argSchemas) validate(name string, raw json.RawMessage) error {
	if a == nil {
		return nil
	}
	compiled, ok := a.schemas[name]
	if !ok {
		return nil
	}
	var value any = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return InvalidArgsf("%s: the arguments: %v", name, err)
		}
	}
	issues := compiled.Validate(value)
	if len(issues) == 0 {
		return nil
	}
	return InvalidArgsf("%s: %s", name, structuralIssuesText(issues))
}

// structuralIssuesText renders the schema's findings for the model.
func structuralIssuesText(issues []schema.StructuralIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		path := issue.Path
		if path == "" {
			path = "arguments"
		}
		parts = append(parts, path+": "+issue.Message)
	}
	return strings.Join(parts, "; ")
}

// definitionIssuesText renders a schema's own problems for the tool's
// author.
func definitionIssuesText(issues []schema.DefinitionIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Path+": "+issue.Message)
	}
	return strings.Join(parts, "; ")
}
