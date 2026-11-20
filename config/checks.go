package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/sdougbrown/poo/protect"
)

// The checks the settings must pass.
//
// The generated check (settings_gen.go) carries the rules that cross
// fields and that the umpire spec v1.1.0 can express:
//
//   - the decide thresholds need a decider model: a `requires` rule
//     with the `has_decider` condition;
//   - `system_prompt.base_file` and `system_prompt.replace` are
//     mutually exclusive: a `disables` rule;
//   - the values the settings allow, such as the actions and the
//     ratios: `check` rules.
//
// The rules below are the ones the spec cannot carry. They are here
// because the settings' leaves are not the shapes the spec's fields
// have:
//
//   - a permission rule matches exactly one of a tool and a class, and
//     a command prefix names the tool it matches. A rule is an object in
//     a list, not a leaf, so the rules are checked by hand.
//   - the asker names are the ones the settings know. The spec's
//     `contains` tests a chain for one name, never every entry against a
//     set of names, so the rule is checked by hand.
//   - the roles are the ones the settings know. The roles are a map,
//     not a leaf, so the rule is checked by hand.
//   - the protected paths compile, and the files the settings name are
//     there. The rules depend on the filesystem, so they are checked
//     by hand.

// Check runs the checks the settings must pass.
//
// The generated check knows the rules that cross fields, and the
// hand-written ones know the rules that depend on the shape of a value
// or on the files the settings name.
//
// The check never changes the settings: it reads them and reports what
// it finds.
func (s *Settings) Check() error {
	if err := s.checkAvailability(); err != nil {
		return err
	}
	return s.checkValues()
}

// checkAvailability runs the generated check.
//
// A field the settings set but the check does not allow is a failure
// that names the field: the operator set it, and the other settings
// do not allow it there. The generated check's own account of the
// field is the failure's explanation.
//
// The check judges the settings for a runtime that has every
// capability. Settings are one file an operator shares between front
// doors, so the front door without a wire client must not refuse a
// chain that names the wire asker. The build judges the same settings
// with what it has, and warns about what that leaves out.
func (s *Settings) checkAvailability() error {
	fields := fieldsOf(s)
	conditions := conditionsOf(s)
	conditions.HasWireClient = true
	availability := Check(fields, conditions, SettingsFields{})

	value := reflect.ValueOf(fields)
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		status, ok := statusOf(availability, name)
		if !ok {
			continue
		}
		path := fieldPath(typ.Field(i))
		if value.Field(i).IsZero() {
			if status.Required {
				return &Error{
					Code:    CodeInvalid,
					Field:   path,
					Message: "the settings must have this field",
				}
			}
			continue
		}
		switch {
		case !status.Enabled:
			challenge := Challenge(name, fields, conditions, SettingsFields{})
			return &Error{
				Code:      CodeUnavailable,
				Field:     path,
				Message:   "the settings do not allow this field here",
				Challenge: challenge.Explanations,
			}
		case !status.Fair:
			challenge := Challenge(name, fields, conditions, SettingsFields{})
			return &Error{
				Code:      CodeInvalid,
				Field:     path,
				Message:   "the settings do not allow this value",
				Challenge: challenge.Explanations,
			}
		}
	}
	return nil
}

// checkValues runs the checks the generated check cannot run.
func (s *Settings) checkValues() error {
	if err := s.checkRoles(); err != nil {
		return err
	}
	if err := s.checkRules(); err != nil {
		return err
	}
	if err := s.checkAskers(); err != nil {
		return err
	}
	code := s.Tools.Codemode
	if code.Engine != nil && *code.Engine == "quickjs" && code.ExecSteps != nil && *code.ExecSteps != 0 {
		return &Error{
			Code:    CodeInvalid,
			Field:   "tools.codemode.exec_steps",
			Message: "quickjs does not support the exec_steps limit; use timeout_s",
		}
	}
	return s.checkFiles()
}

// checkRoles checks that the settings name the roles they know.
func (s *Settings) checkRoles() error {
	for name := range s.Roles {
		switch name {
		case "chat", "decider":
		default:
			return &Error{
				Code:    CodeInvalid,
				Field:   "roles." + name,
				Message: "the settings do not have this role",
			}
		}
	}
	return nil
}

// checkRules checks the permission rules.
//
// A rule matches on the tool's name or on the tool's class, never on
// both, and a command matcher names the tool it matches: a prefix or a
// substring on a class rule would have no argument to match, and a
// rule that matches nothing is a rule the operator thinks is doing
// something.
func (s *Settings) checkRules() error {
	for i, rule := range s.Permission.Rules {
		where := fmt.Sprintf("permission.rules[%d]", i)
		switch {
		case rule.Tool == "" && rule.Class == "":
			return &Error{
				Code:    CodeInvalid,
				Field:   where,
				Message: "the rule matches neither a tool nor a class",
			}
		case rule.Tool != "" && rule.Class != "":
			return &Error{
				Code:    CodeInvalid,
				Field:   where,
				Message: "the rule matches both a tool and a class",
			}
		}
		if rule.Class != "" {
			switch rule.Class {
			case "read_only", "side_effecting":
			default:
				return &Error{
					Code:    CodeInvalid,
					Field:   where + ".class",
					Message: fmt.Sprintf("the class %q is not one the tools have", rule.Class),
				}
			}
		}
		if rule.CommandPrefix != "" && rule.Tool == "" {
			return &Error{
				Code:    CodeInvalid,
				Field:   where + ".command_prefix",
				Message: "a command prefix is only valid on a rule that names a tool",
			}
		}
		if len(rule.CommandContains) > 0 && rule.Tool == "" {
			return &Error{
				Code:    CodeInvalid,
				Field:   where + ".command_contains",
				Message: "a command substring is only valid on a rule that names a tool",
			}
		}
		if rule.CommandPrefix != "" && len(rule.CommandContains) > 0 {
			return &Error{
				Code:    CodeInvalid,
				Field:   where,
				Message: "a rule carries a command prefix or command substrings, never both",
			}
		}
		for _, substring := range rule.CommandContains {
			if substring == "" {
				return &Error{
					Code:    CodeInvalid,
					Field:   where + ".command_contains",
					Message: "a command substring is empty, and a substring that matches everywhere matches everything",
				}
			}
		}
		switch rule.Action {
		case "allow", "ask", "deny":
		default:
			return &Error{
				Code:    CodeInvalid,
				Field:   where + ".action",
				Message: fmt.Sprintf("the action %q is not one of allow, ask, deny", rule.Action),
			}
		}
	}
	return nil
}

// checkAskers checks the asker chain.
//
// The chain names the askers it holds, in order. A chain the settings
// name with no askers is a chain with no askers: every ask goes to the
// fallback. What a known asker needs is a rule in the schema, on
// permission.askers; a name with no rule at all is a typo.
func (s *Settings) checkAskers() error {
	for i, name := range s.Permission.Askers {
		if name == "decide" || name == "wire" {
			continue
		}
		return &Error{
			Code:    CodeInvalid,
			Field:   fmt.Sprintf("permission.askers[%d]", i),
			Message: fmt.Sprintf("the asker %q is not one the settings know", name),
		}
	}
	return nil
}

// checkFiles checks the files the settings name.
//
// A path in the settings is relative to the working directory, the
// same as every other path a command line takes — except the ones the
// load resolved: the prompt's base file and the finish schema arrive
// beside the settings file.
func (s *Settings) checkFiles() error {
	if len(s.Tools.ProtectedPaths) > 0 {
		if _, err := protect.Compile(s.Tools.ProtectedPaths); err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "tools.protected_paths",
				Message: "the protected paths do not compile",
				Err:     err,
			}
		}
	}
	if s.SystemPrompt.BaseFile != "" {
		if _, err := os.Stat(s.SystemPrompt.BaseFile); err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "system_prompt.base_file",
				Message: "the file is not there",
				Err:     err,
			}
		}
	}
	if value := s.Session.UserPromptFile; value != nil && *value != "" {
		path, err := expandHome(*value)
		if err == nil {
			_, err = os.Stat(path)
		}
		if err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "session.user_prompt_file",
				Message: "the file is not there",
				Err:     err,
			}
		}
	}
	if s.Finish.SchemaFile != "" {
		data, err := os.ReadFile(s.Finish.SchemaFile)
		if err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "finish.schema_file",
				Message: "the file is not there",
				Err:     err,
			}
		}
		if !json.Valid(data) {
			return &Error{
				Code:    CodeInvalid,
				Field:   "finish.schema_file",
				Message: "the file is not JSON",
			}
		}
	}
	return nil
}

// statusOf returns the availability of the named field.
//
// The availability and the fields have the same field names, so the
// name is the link between them.
func statusOf(availability SettingsAvailability, name string) (FieldStatus, bool) {
	field := reflect.ValueOf(availability).FieldByName(name)
	if !field.IsValid() {
		return FieldStatus{}, false
	}
	status, ok := field.Interface().(FieldStatus)
	return status, ok
}

// fieldPath returns the name the settings give a field.
//
// The generated fields carry the settings' own names in their tags, so
// the failure names the key the operator wrote.
func fieldPath(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	return name
}
