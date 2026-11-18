// Package schematext renders a tool's schema findings for the model.
//
// Two layers refuse a call that misses its schema: the session's own
// check, and a registered CLI's check of its manifest. Both hand the
// model the same kind of refusal, so both render through one renderer.
// The vocabulary is the schema the call missed: the accepted argument
// names, an enum's values, a bound's limit.
package schematext

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sdougbrown/cli-registry/schema"
)

// IssuesText renders schema issues for the model.
//
// A path is an RFC 6901 pointer into the arguments, so the model can
// see which argument it got wrong. The schema sits beside the issues.
// The common refusals render in the schema's own vocabulary: the
// accepted argument names, an enum's values, a bound's limit. An issue
// the renderer has no words for keeps the library's message, or its
// code when the message is empty. A refusal never ends in a bare
// pointer.
func IssuesText(name string, schema map[string]any, issues []schema.StructuralIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		if msg := issueMessage(name, schema, issue); msg != "" {
			parts = append(parts, msg)
			continue
		}
		path := issue.Path
		if path == "" {
			path = "arguments"
		}
		message := issue.Message
		if message == "" {
			message = issue.Code
		}
		parts = append(parts, path+": "+message)
	}
	return strings.Join(parts, "; ")
}

// issueMessage renders one issue in the schema's vocabulary, or an
// empty string when the renderer has no words for it.
func issueMessage(name string, schema map[string]any, issue schema.StructuralIssue) string {
	segments := splitPointer(issue.Path)
	last := ""
	if len(segments) > 0 {
		last = segments[len(segments)-1]
	}
	// A clause names the argument it rejects: the leaf name at the top
	// level, the pointer's path once the argument sits inside an object
	// or an array, so two same-kind misses stay mappable to each
	// argument.
	subject := ""
	if arg := clauseSubject(segments); arg != "" {
		subject = fmt.Sprintf("%q ", arg)
	}
	switch issue.Code {
	case "required":
		if last == "" {
			return ""
		}
		return fmt.Sprintf("%q is required", last)
	case "additionalProperties":
		parent := schemaPointerAt(schema, segments[:len(segments)-1])
		accepted := PropertyNames(parent)
		if accepted == "" {
			return ""
		}
		// The accepted list is the parent's vocabulary, so a nested
		// argument names the parent it belongs under.
		who := name
		if parentSegments := segments[:len(segments)-1]; len(parentSegments) > 0 {
			who = name + "." + strings.Join(parentSegments, ".")
		}
		return fmt.Sprintf("unknown argument %q; %s accepts: %s", last, who, accepted)
	case "enum":
		enum, _ := schemaPointerAt(schema, segments)["enum"].([]any)
		if len(enum) == 0 {
			return ""
		}
		return subject + "must be one of: " + joinValues(enum)
	case "const":
		value, ok := schemaPointerAt(schema, segments)["const"]
		if !ok {
			return ""
		}
		return subject + "must be " + valueText(value)
	case "discriminator":
		parent := schemaPointerAt(schema, segments[:len(segments)-1])
		if values := discriminatorValues(parent, last); len(values) > 0 {
			return subject + "must be one of: " + joinValues(values)
		}
		return ""
	case "type":
		node := schemaPointerAt(schema, segments)
		kind, _ := node["type"].(string)
		if kind == "" {
			// A tagged oneOf reports a type miss for a value that is
			// not an object; the branches are all objects.
			if _, tagged := node["oneOf"]; tagged {
				return subject + "must be an object"
			}
			return ""
		}
		return subject + "must be " + article(kind) + " " + kind
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum",
		"minLength", "maxLength", "minItems", "maxItems":
		bound, ok := schemaPointerAt(schema, segments)[issue.Code]
		if !ok {
			return ""
		}
		return subject + limitMessage(issue.Code, bound)
	}
	return ""
}

// PropertyNames lists a schema object's properties in name order,
// required ones marked, or an empty string when there are none.
//
// Names sort by their base name; a required mark is appended after
// the sort. Two renderers that share a schema then list the same
// arguments in the same order.
func PropertyNames(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return ""
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	required := map[string]bool{}
	for _, name := range ToNames(schema["required"]) {
		required[name] = true
	}
	for i, name := range names {
		if required[name] {
			names[i] += "*"
		}
	}
	return strings.Join(names, ", ")
}

// ToNames reads a JSON array of strings, tolerating anything else.
func ToNames(value any) []string {
	names, _ := value.([]any)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if s, ok := name.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// clauseSubject names the argument a clause rejects: the leaf name at
// the top level, the pointer's path once the argument sits inside an
// object or an array.
func clauseSubject(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	if len(segments) == 1 {
		return segments[0]
	}
	return strings.Join(segments, "/")
}

// splitPointer splits an RFC 6901 pointer into its unescaped segments.
func splitPointer(path string) []string {
	if path == "" {
		return nil
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, segment := range segments {
		segments[i] = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
	}
	return segments
}

// schemaPointerAt resolves a pointer's segments to the subschema they
// name, walking object properties and array items, or nil when the
// walk leaves the schema.
func schemaPointerAt(schema map[string]any, segments []string) map[string]any {
	current := schema
	for _, segment := range segments {
		current = resolveRef(current, schema)
		if _, err := strconv.Atoi(segment); err == nil {
			items, ok := current["items"].(map[string]any)
			if !ok {
				return nil
			}
			current = items
			continue
		}
		properties, ok := current["properties"].(map[string]any)
		if !ok {
			return nil
		}
		next, ok := properties[segment].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return resolveRef(current, schema)
}

// resolveRef follows one local reference into the schema's $defs, or
// returns the node as it is. A reference is followed once: a chain of
// references stays where the first hop lands, so a cycle cannot loop.
func resolveRef(node, root map[string]any) map[string]any {
	ref, ok := node["$ref"].(string)
	if !ok {
		return node
	}
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return node
	}
	defs, _ := root["$defs"].(map[string]any)
	def, ok := defs[strings.TrimPrefix(ref, prefix)].(map[string]any)
	if !ok {
		return node
	}
	return def
}

// discriminatorValues lists the consts a tagged oneOf's branches
// declare for the discriminator key, or nil when the schema does not
// take that shape.
func discriminatorValues(parent map[string]any, key string) []any {
	if parent == nil || key == "" {
		return nil
	}
	branches, _ := parent["oneOf"].([]any)
	values := make([]any, 0, len(branches))
	for _, branch := range branches {
		bm, _ := branch.(map[string]any)
		props, _ := bm["properties"].(map[string]any)
		ps, _ := props[key].(map[string]any)
		if c, ok := ps["const"]; ok {
			values = append(values, c)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// joinValues renders a JSON array's values the way a model reads them.
func joinValues(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, valueText(value))
	}
	return strings.Join(parts, ", ")
}

// valueText renders one JSON value in message prose: strings quoted,
// everything else as the encoder writes it.
func valueText(value any) string {
	if s, ok := value.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(value)
}

// article is the indefinite article a type name takes.
func article(kind string) string {
	switch kind[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// limitMessage renders a bound violation, naming the bound the schema
// declares.
func limitMessage(code string, bound any) string {
	switch code {
	case "minimum":
		return fmt.Sprintf("is below the minimum of %s", valueText(bound))
	case "maximum":
		return fmt.Sprintf("is above the maximum of %s", valueText(bound))
	case "exclusiveMinimum":
		return fmt.Sprintf("must be greater than %s", valueText(bound))
	case "exclusiveMaximum":
		return fmt.Sprintf("must be less than %s", valueText(bound))
	case "minLength":
		return fmt.Sprintf("is shorter than the minimum length of %s", valueText(bound))
	case "maxLength":
		return fmt.Sprintf("is longer than the maximum length of %s", valueText(bound))
	case "minItems":
		return fmt.Sprintf("has fewer than %s items", valueText(bound))
	case "maxItems":
		return fmt.Sprintf("has more than %s items", valueText(bound))
	}
	return ""
}
