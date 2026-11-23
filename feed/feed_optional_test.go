package feed_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/cli-registry"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/feed"
)

// theOptionalFeed is one manifest entry a subscription starts: it declares
// the values it watches and references one of them from its environment.
func theOptionalFeed() map[string]any {
	return map[string]any{
		"description": "Feedback on the pull requests of one repository.",
		"command":     []string{"git-feedback", "--watch"},
		"optional":    true,
		"scope": map[string]any{
			"url": map[string]any{"type": "string", "pattern": "^https://.*$"},
		},
		"env": map[string]any{"GIT_FEEDBACK_URL": "scope:url"},
	}
}

// TestAnOptionalFeedLoadsTheScopeASubscriptionSupplies covers the load: an
// optional feed declares the fields a subscription must supply, in name
// order, and the scope reference stays unresolved until the feed starts.
func TestAnOptionalFeedLoadsTheScopeASubscriptionSupplies(t *testing.T) {
	set, err := feed.Load(writeManifest(t, map[string]any{"git-feedback": theOptionalFeed()}))
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	spec := set.Specs()[0]
	if !spec.Optional {
		t.Error("the feed is not optional, and its manifest says it is")
	}
	if len(spec.Scope) != 1 || spec.Scope[0].Name != "url" || spec.Scope[0].Pattern != "^https://.*$" || spec.Scope[0].Type != "string" {
		t.Fatalf("the scope is %+v, want one url field matching ^https://.*$", spec.Scope)
	}
	if len(spec.Env) != 1 || spec.Env[0].Scope != cliregistry.ScopeScope || spec.Env[0].From != "url" {
		t.Fatalf("the env binding is %+v, want a scope:url reference", spec.Env)
	}

	// The reference resolves through the resolver the subscription's
	// values are handed to, and not before: a feed that resolves one
	// without a scope fails, the way a session reference does.
	if _, err := spec.ResolveEnvScoped(func(string) (string, bool) { return "", false }, nil); err == nil {
		t.Fatal("the scope reference resolved with no scope")
	}
	resolved, err := spec.ResolveEnvScoped(
		func(string) (string, bool) { return "", false },
		exec.ScopedResolver("sess-01", map[string]string{"url": "https://example.test/repo"}),
	)
	if err != nil {
		t.Fatalf("resolving the env: %v", err)
	}
	if len(resolved) != 1 || resolved[0] != "GIT_FEEDBACK_URL=https://example.test/repo" {
		t.Errorf("the env is %v, want the subscribed url", resolved)
	}
}

// TestAnOptionalFeedWithoutAScopeLoads covers the other side: a feed a
// subscription starts with no values declares no scope, and nothing about
// its environment changes.
func TestAnOptionalFeedWithoutAScopeLoads(t *testing.T) {
	set, err := feed.Load(writeManifest(t, map[string]any{
		"audit": map[string]any{"command": []string{"audit"}, "optional": true},
	}))
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	spec := set.Specs()[0]
	if !spec.Optional || len(spec.Scope) != 0 {
		t.Errorf("the spec is optional=%v with scope %+v, want an optional feed with no scope", spec.Optional, spec.Scope)
	}
}

// TestTheManifestRefusesAScopeItCannotHonor covers the manifests that would
// start and fail every attempt: a reference to a field the feed does not
// declare, a scope on a feed the session starts itself, a field type that
// cannot arrive as an environment value, a pattern that does not compile,
// a field name that is not a word, and an unknown field in the schema.
func TestTheManifestRefusesAScopeItCannotHonor(t *testing.T) {
	// withScope returns the optional feed with its url field declared as
	// the field says.
	withScope := func(field map[string]any) map[string]any {
		optional := theOptionalFeed()
		optional["scope"] = map[string]any{"url": field}
		return optional
	}
	for _, tc := range []struct {
		name     string
		manifest map[string]any
		wants    string
	}{
		{
			name: "an undeclared scope field",
			manifest: func() map[string]any {
				optional := theOptionalFeed()
				optional["env"] = map[string]any{"GIT_FEEDBACK_URL": "scope:repo"}
				return optional
			}(),
			wants: "scope:repo",
		},
		{
			name: "a bare scope reference",
			manifest: func() map[string]any {
				optional := theOptionalFeed()
				optional["env"] = map[string]any{"GIT_FEEDBACK_URL": "scope:"}
				return optional
			}(),
			wants: "names no scope value",
		},
		{
			name: "a scope on a feed that starts with the session",
			manifest: func() map[string]any {
				optional := theOptionalFeed()
				optional["optional"] = false
				return optional
			}(),
			wants: "is not optional",
		},
		{
			name:     "a field type that cannot arrive",
			manifest: withScope(map[string]any{"type": "integer"}),
			wants:    `want "string"`,
		},
		{
			name:     "a pattern that does not compile",
			manifest: withScope(map[string]any{"type": "string", "pattern": "([a-"}),
			wants:    "does not compile",
		},
		{
			name: "a field name that is not a word",
			manifest: func() map[string]any {
				optional := theOptionalFeed()
				optional["scope"] = map[string]any{"Repo URL": map[string]any{"type": "string"}}
				return optional
			}(),
			wants: "valid field name",
		},
		{
			name:     "an unknown field in the schema",
			manifest: withScope(map[string]any{"type": "string", "required": true}),
			wants:    "unknown field",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := feed.Load(writeManifest(t, map[string]any{"git-feedback": tc.manifest}))
			if err == nil {
				t.Fatalf("the manifest loaded, want the load to fail")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("the failure is %q, want it to name %q", err, tc.wants)
			}
		})
	}
}
