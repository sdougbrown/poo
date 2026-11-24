package poo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/feed"
)

// The scope schema is validated twice: the manifest loader refuses one it
// cannot read, and the session refuses a declared one it cannot honour. A
// schema the loader accepts and the session refuses would be a feed that
// loads and then fails its first subscription, so the two validators are
// pinned together here.

// TestTheScopeSchemaValidatorsAgree covers the pin: the manifest loader and
// the session's declaration check accept and refuse the same schema, one
// written as a manifest and the other as the declaration the build hands the
// session.
func TestTheScopeSchemaValidatorsAgree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []scopeField
		want   bool
	}{
		{
			name:   "a field with a pattern",
			fields: []scopeField{{name: "url", typ: "string", pattern: `^https://.*$`}},
			want:   true,
		},
		{
			name:   "a field with no pattern",
			fields: []scopeField{{name: "repo", typ: "string"}},
			want:   true,
		},
		{
			name:   "a field with no type",
			fields: []scopeField{{name: "url"}},
		},
		{
			name:   "a field name the pattern refuses",
			fields: []scopeField{{name: "url-two"}},
		},
		{
			name:   "a field name that does not start with a letter",
			fields: []scopeField{{name: "1url"}},
		},
		{
			name:   "a field that is not a string",
			fields: []scopeField{{name: "url", typ: "number"}},
		},
		{
			name:   "a pattern that does not compile",
			fields: []scopeField{{name: "url", pattern: `(https://`}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded := loadScopeSchema(t, tc.fields)
			declared := declareScopeSchema(t, tc.fields)
			if loaded != tc.want {
				t.Fatalf("the manifest loader accepted the schema: %v, want %v", loaded, tc.want)
			}
			if declared != loaded {
				t.Fatalf("the session declared the schema: %v, the manifest loader: %v; the two validators must agree", declared, loaded)
			}
		})
	}
}

// scopeField is one field of a scope schema, spelled the way both
// validators take it.
//
// The manifest names a field by its key and its type beside it, and the
// declaration names all three on the field. An empty type is a schema that
// names none, which both refuse.
type scopeField struct {
	name    string
	typ     string
	pattern string
}

// loadScopeSchema reports whether the manifest loader accepts the schema.
func loadScopeSchema(t *testing.T, fields []scopeField) bool {
	t.Helper()
	scope := map[string]any{}
	for _, field := range fields {
		entry := map[string]any{}
		if field.typ != "" {
			entry["type"] = field.typ
		}
		if field.pattern != "" {
			entry["pattern"] = field.pattern
		}
		scope[field.name] = entry
	}
	encoded, err := json.Marshal(map[string]any{
		"version": 1,
		"feeds": map[string]any{
			"git-feedback": map[string]any{
				"command":  []string{"true"},
				"optional": true,
				"scope":    scope,
			},
		},
	})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	_, err = feed.Load(path)
	return err == nil
}

// declareScopeSchema reports whether the session accepts the schema as a
// feed's declaration.
func declareScopeSchema(t *testing.T, fields []scopeField) bool {
	t.Helper()
	spec := poo.FeedSpec{Name: "git-feedback", Optional: true, Run: waitingFeed}
	for _, field := range fields {
		spec.Scope = append(spec.Scope, poo.FeedScopeField{Name: field.name, Type: field.typ, Pattern: field.pattern})
	}
	s, err := poo.New(testOptions(t, scripted(nil), &echoTool{}, &recordingSink{}), poo.WithFeeds(spec))
	if s != nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return err == nil
}
