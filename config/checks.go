package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/sdougbrown/poo/tool/protect"
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
//   - the decide asker needs a decider model. The rule is a membership
//     test in a list of names, and the generator's `in` and `notIn`
//     operators compare a list field with a scalar, so the generated
//     code does not compile. The rule is checked by hand.
//   - the hard ratio is above the notice ratio. The spec's expressions
//     compare a field with a constant, never with another field. The
//     rule is checked by hand.
//   - a permission rule matches exactly one of a tool and a class, and
//     a command prefix belongs to a bash rule. A rule is an object in
//     a list, not a leaf, so the rules are checked by hand.
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
func (s *Settings) checkAvailability() error {
	fields := fieldsOf(s)
	conditions := conditionsOf(s)
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
				Code:     CodeUnavailable,
				Field:    path,
				Message:  "the settings do not allow this field here",
				Challenge: challenge.Explanations,
			}
		case !status.Fair:
			challenge := Challenge(name, fields, conditions, SettingsFields{})
			return &Error{
				Code:     CodeInvalid,
				Field:    path,
				Message:  "the settings do not allow this value",
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
	if err := s.checkContext(); err != nil {
		return err
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
// both, and a command prefix belongs to a bash rule: a prefix on
// another rule would never match, and a rule that matches nothing is
// a rule the operator thinks is doing something.
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
		if rule.CommandPrefix != "" && rule.Tool != "bash" {
			return &Error{
				Code:    CodeInvalid,
				Field:   where + ".command_prefix",
				Message: "a command prefix is only valid for a bash rule",
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
// The chain names the askers it holds, in order. The decide asker
// needs a decider model: a chain that asks a model that is not there
// would wait for an answer that cannot come.
func (s *Settings) checkAskers() error {
	for i, name := range s.Permission.Askers {
		where := fmt.Sprintf("permission.askers[%d]", i)
		switch name {
		case "decide":
			if s.Roles["decider"] == "" {
				return &Error{
					Code:    CodeUnavailable,
					Field:   where,
					Message: "the decide asker needs a decider model: roles.decider names one",
				}
			}
		case "wire":
		default:
			return &Error{
				Code:    CodeInvalid,
				Field:   where,
				Message: fmt.Sprintf("the asker %q is not one the settings know", name),
			}
		}
	}
	return nil
}

// checkContext checks the context window's ratios.
//
// The notice ratio is when the model is told that the context is
// filling up, and the hard ratio is when the run stops. The notice
// comes first: a hard ratio at or below the notice ratio would stop
// the run before the model was ever told.
func (s *Settings) checkContext() error {
	notice, hard := s.Context.NoticeRatio, s.Context.HardRatio
	if notice != nil && hard != nil && *hard <= *notice {
		return &Error{
			Code:  CodeInvalid,
			Field: "context.hard_ratio",
			Message: fmt.Sprintf("the hard ratio %g is not above the notice ratio %g",
				*hard, *notice),
		}
	}
	return nil
}

// checkFiles checks the files the settings name.
//
// A path in the settings is relative to the working directory, the
// same as every other path a command line takes.
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
