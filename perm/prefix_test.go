package perm

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// TestAPrefixMatchesTheHostSpelledTheWaysURLsAre covers the URL rules:
// the same host with a default port, a trailing dot on the FQDN, or a
// different case matches the prefix, and a different port does not.
func TestAPrefixMatchesTheHostSpelledTheWaysURLsAre(t *testing.T) {
	rule := Rule{Tool: "fetch", CommandPrefix: "https://docs.example.com/", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	matching := []string{
		"https://docs.example.com/index.html",
		"https://docs.example.com:443/index.html",
		"https://docs.example.com./index.html",
		"HTTPS://DOCS.EXAMPLE.COM/index.html",
	}
	for _, url := range matching {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if !rule.matchesCommand(info) {
			t.Errorf("the prefix does not match %s, want the same host in one spelling", url)
		}
	}
	for _, url := range []string{
		"https://docs.example.com:8443/index.html",
		"https://other.example.com/index.html",
		"https://docs.example.com.evil.test/index.html",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if rule.matchesCommand(info) {
			t.Errorf("the prefix matches %s, want a different address", url)
		}
	}
}

// TestAPrefixWithoutASlashStaysInsideTheHost covers the no-slash
// prefix: it matches the host's own paths, and nothing past the
// host's edge — the adjacent host an attacker registers matches
// nothing.
func TestAPrefixWithoutASlashStaysInsideTheHost(t *testing.T) {
	rule := Rule{Tool: "fetch", CommandPrefix: "https://internal.example.com", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	for _, url := range []string{
		"https://internal.example.com/",
		"https://internal.example.com/index.html",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if !rule.matchesCommand(info) {
			t.Errorf("the prefix does not match %s, want the host's own paths", url)
		}
	}
	for _, url := range []string{
		"https://internal.example.com.evil.com/index.html",
		"https://internal.example.com:8443/index.html",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if rule.matchesCommand(info) {
			t.Errorf("the prefix matches %s, want the host's edge to hold", url)
		}
	}
}

// TestAPrefixStaysLexicalForNonURLs covers the rules over a command or
// a path: no canonicalization, the text compares as it is.
func TestAPrefixStaysLexicalForNonURLs(t *testing.T) {
	rule := Rule{Tool: "bash", CommandPrefix: "git status", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "bash", PrimaryArg: "command"}
	info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"command": "git status && make test"}`)}
	if !rule.matchesCommand(info) {
		t.Errorf("the prefix does not match the command, want the lexical match")
	}
}

// TestAPrefixOnAToolWithNoPrimaryArgumentMatchesNothing covers the
// shape the config refuses at the build: a policy built by hand gets a
// rule that matches nothing, rather than one that matches by accident.
func TestAPrefixOnAToolWithNoPrimaryArgumentMatchesNothing(t *testing.T) {
	rule := Rule{Tool: "jobs", CommandPrefix: "anything", Action: ActionDeny}
	info := poo.ToolCallInfo{Spec: poo.ToolSpec{Name: "jobs"}, Arguments: []byte(`{}`)}
	if rule.matchesCommand(info) {
		t.Errorf("the prefix matched a tool with no primary argument")
	}
}
