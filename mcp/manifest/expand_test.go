package manifest_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/mcp/manifest"
)

// TestParseTemplateRejectsWhatNoLaunchCouldResolve covers the forms that are
// not templates: an unterminated reference, a reference with no name, a
// session reference to a value the session scope does not carry, and a default
// on a reference that reads the session.
func TestParseTemplateRejectsWhatNoLaunchCouldResolve(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "unterminated", source: "${AVENOR_BIN", want: "unterminated"},
		{name: "no name", source: "${}", want: "names no variable"},
		{name: "unknown session value", source: "${session:runid}", want: "session:runid"},
		{name: "unknown scope", source: "${scope:id}", want: "scope"},
		{name: "default on a session value", source: "${session:id:-none}", want: "takes no default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := manifest.ParseTemplate(tt.source); err == nil {
				t.Fatalf("parsed %q, want a failure", tt.source)
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the failure is %v, want it to hold %q", err, tt.want)
			}
		})
	}
}

// TestTemplateResolve covers what a manifest value stands for at launch: the
// host's value, the default it wrote, the session's own ID, a literal dollar,
// and the failure an unset variable without a default is.
func TestTemplateResolve(t *testing.T) {
	host := func(name string) (string, bool) {
		switch name {
		case "AVENOR_BIN":
			return "/opt/avenor", true
		case "EMPTY":
			return "", true
		}
		return "", false
	}
	tests := []struct {
		name      string
		source    string
		sessionID string
		want      string
		fails     string
	}{
		{name: "literal", source: "avenor mcp serve", sessionID: "s1", want: "avenor mcp serve"},
		{name: "host value", source: "${AVENOR_BIN} mcp", sessionID: "s1", want: "/opt/avenor mcp"},
		{name: "empty host value", source: "${EMPTY}", sessionID: "s1", want: ""},
		{name: "default unread", source: "${AVENOR_BIN:-avenor}", sessionID: "s1", want: "/opt/avenor"},
		{name: "default read", source: "${AVENOR_URL:-http://127.0.0.1:8787}", sessionID: "s1", want: "http://127.0.0.1:8787"},
		{name: "empty default", source: "x${AVENOR_URL:-}y", sessionID: "s1", want: "xy"},
		{name: "session id", source: "--session=${session:id}", sessionID: "01J8", want: "--session=01J8"},
		{name: "literal dollar", source: "$$5 and $$", sessionID: "s1", want: "$5 and $"},
		{name: "trailing dollar", source: "cost$", sessionID: "s1", want: "cost$"},
		{name: "empty value", source: "", sessionID: "s1", want: ""},
		{name: "unset without default", source: "${AVENOR_URL}", sessionID: "s1", fails: "AVENOR_URL"},
		{name: "no session", source: "${session:id}", sessionID: "", fails: "session:id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template, err := manifest.ParseTemplate(tt.source)
			if err != nil {
				t.Fatalf("parsing %q: %v", tt.source, err)
			}
			got, err := template.Resolve(host, tt.sessionID)
			if tt.fails != "" {
				if err == nil {
					t.Fatalf("resolved %q to %q, want a failure naming %s", tt.source, got, tt.fails)
				}
				if !strings.Contains(err.Error(), tt.fails) {
					t.Errorf("the failure is %v, want it to name %q", err, tt.fails)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolving %q: %v", tt.source, err)
			}
			if got != tt.want {
				t.Errorf("%q resolved to %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}
