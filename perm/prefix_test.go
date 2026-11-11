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
// prefix: it matches the host's own paths and its subdomains, and
// nothing past the host's edge or on the other scheme — the adjacent
// host an attacker registers, and the cleartext downgrade, match
// nothing.
func TestAPrefixWithoutASlashStaysInsideTheHost(t *testing.T) {
	rule := Rule{Tool: "fetch", CommandPrefix: "https://internal.example.com", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	for _, url := range []string{
		"https://internal.example.com/",
		"https://internal.example.com/index.html",
		"https://sub.internal.example.com/index.html",
		"https://internal.example.com:0443/index.html",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if !rule.matchesCommand(info) {
			t.Errorf("the prefix does not match %s, want the host and its subdomains", url)
		}
	}
	for _, url := range []string{
		"https://internal.example.com.evil.com/index.html",
		"https://internal.example.com:8443/index.html",
		"http://internal.example.com/index.html",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if rule.matchesCommand(info) {
			t.Errorf("the prefix matches %s, want the host's edge and the scheme to hold", url)
		}
	}
}

// TestAHostnamePrefixCoversBothSchemes covers the schemeless prefix: a
// prefix that names no scheme is a hostname rule, and the call's own
// URL decides the scheme — one hostname covers the cleartext and the
// secure URL, with the host's spellings and its subdomains matching
// either way. An adjacent host and a non-http scheme match nothing.
func TestAHostnamePrefixCoversBothSchemes(t *testing.T) {
	rule := Rule{Tool: "fetch", CommandPrefix: "internal.example.com", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	matching := []string{
		"https://internal.example.com/page",
		"http://internal.example.com/page",
		"https://internal.example.com./page",
		"HTTPS://INTERNAL.EXAMPLE.COM/page",
		"https://internal.example.com:0443/page",
		"https://sub.internal.example.com/page",
		"http://sub.internal.example.com:0080/page",
	}
	for _, url := range matching {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if !rule.matchesCommand(info) {
			t.Errorf("the hostname prefix does not match %s, want the host under either scheme", url)
		}
	}
	for _, url := range []string{
		"https://internal.example.com.evil.com/page",
		"https://other.example.com/page",
		"ftp://internal.example.com/page",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if rule.matchesCommand(info) {
			t.Errorf("the hostname prefix matches %s, want the adjacent host and the foreign scheme to hold out", url)
		}
	}
}

// TestAHostnamePrefixCarriesItsPathAndPort covers the schemeless
// prefix's authority: a path on the hostname stays inside it, and an
// explicit port holds — the same rules an explicit-scheme prefix
// answers to, under the call's scheme.
func TestAHostnamePrefixCarriesItsPathAndPort(t *testing.T) {
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	path := Rule{Tool: "fetch", CommandPrefix: "internal.example.com/secret", Action: ActionDeny}
	info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "https://internal.example.com/secret/page"}`)}
	if !path.matchesCommand(info) {
		t.Errorf("the hostname-and-path prefix does not match the path under it")
	}
	info = poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "http://internal.example.com/secret/page"}`)}
	if !path.matchesCommand(info) {
		t.Errorf("the hostname-and-path prefix does not match the path under the other scheme")
	}
	info = poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "https://internal.example.com/"}`)}
	if path.matchesCommand(info) {
		t.Errorf("the hostname-and-path prefix matches a path above it")
	}
	port := Rule{Tool: "fetch", CommandPrefix: "internal.example.com:8443", Action: ActionDeny}
	info = poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "https://internal.example.com:8443/page"}`)}
	if !port.matchesCommand(info) {
		t.Errorf("the hostname-and-port prefix does not match its port")
	}
	info = poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "https://internal.example.com/page"}`)}
	if port.matchesCommand(info) {
		t.Errorf("the hostname-and-port prefix matches the default port")
	}
}

// TestAHostnamePrefixRefusesUserinfo covers the authority's grammar: a
// hostname rule names a host, so a prefix whose authority carries
// userinfo names nothing a URL rule can mean, and it matches nothing
// rather than widening to the bare host behind the userinfo.
func TestAHostnamePrefixRefusesUserinfo(t *testing.T) {
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	schemeless := Rule{Tool: "fetch", CommandPrefix: "attacker@internal.example.com", Action: ActionDeny}
	for _, url := range []string{
		"https://internal.example.com/page",
		"https://attacker@internal.example.com/page",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if schemeless.matchesCommand(info) {
			t.Errorf("the userinfo prefix matches %s, want a rule that names no host to match nothing", url)
		}
	}
	explicit := Rule{Tool: "fetch", CommandPrefix: "https://attacker@internal.example.com", Action: ActionDeny}
	info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "https://internal.example.com/page"}`)}
	if explicit.matchesCommand(info) {
		t.Errorf("the explicit userinfo prefix matches the bare host, want no canonical widening")
	}
	info = poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "https://attacker@internal.example.com/page"}`)}
	if !explicit.matchesCommand(info) {
		t.Errorf("the explicit userinfo prefix does not match its own spelling, want the lexical fallback")
	}
}

// TestAShortSchemelessPrefixNamesAHost pins the grammar's edge: a
// schemeless prefix that used to match URL values lexically — "http"
// was a string prefix of every absolute http(s) URL — now names the
// host it spells, and an ordinary URL matches nothing under it.
func TestAShortSchemelessPrefixNamesAHost(t *testing.T) {
	rule := Rule{Tool: "fetch", CommandPrefix: "http", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	for _, url := range []string{
		"https://example.com/page",
		"http://example.com/page",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if rule.matchesCommand(info) {
			t.Errorf("the prefix \"http\" matches %s, want only the host it names", url)
		}
	}
}

// TestAPrefixWithASchemeKeepsItsScheme covers the refusal branch: a
// prefix that names a scheme the canonical form does not take is
// refused, so no http(s) URL canonicalizes against it. The prefix
// keeps the lexical comparison, which an absolute http(s) URL never
// satisfies; a value that is itself an ftp URL matches it as text,
// the way any non-http(s) value does.
func TestAPrefixWithASchemeKeepsItsScheme(t *testing.T) {
	rule := Rule{Tool: "fetch", CommandPrefix: "ftp://example.com", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "fetch", PrimaryArg: "url"}
	for _, url := range []string{
		"https://example.com/page",
		"http://example.com/page",
	} {
		info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "` + url + `"}`)}
		if rule.matchesCommand(info) {
			t.Errorf("the ftp prefix matches %s, want a refused prefix to match no absolute http(s) URL", url)
		}
	}
	info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"url": "ftp://example.com/file"}`)}
	if !rule.matchesCommand(info) {
		t.Errorf("the ftp prefix does not match the ftp URL, want the lexical fallback")
	}
}

// TestAPrefixStaysLexicalForNonURLs covers the rules over a command or
// a path: no canonicalization, the text compares as it is. A hostname
// prefix keeps the lexical fallback for a value that is no URL.
func TestAPrefixStaysLexicalForNonURLs(t *testing.T) {
	rule := Rule{Tool: "bash", CommandPrefix: "git status", Action: ActionDeny}
	spec := poo.ToolSpec{Name: "bash", PrimaryArg: "command"}
	info := poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"command": "git status && make test"}`)}
	if !rule.matchesCommand(info) {
		t.Errorf("the prefix does not match the command, want the lexical match")
	}
	host := Rule{Tool: "bash", CommandPrefix: "example.com", Action: ActionDeny}
	info = poo.ToolCallInfo{Spec: spec, Arguments: []byte(`{"command": "example.com/build && report"}`)}
	if !host.matchesCommand(info) {
		t.Errorf("the hostname prefix does not match a non-URL value, want the lexical fallback")
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
