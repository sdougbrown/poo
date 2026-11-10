// Package urlnorm holds the canonical form of a URL.
//
// The permission engine matches a rule's prefix against a call's URL,
// and the fetch tool keys its doom-loop count by one. Both need the
// same answer for the same URL however it was spelled, so the one
// canonical form lives here and both read it.
package urlnorm

import (
	"net/url"
	"strings"
)

// Canonical returns the canonical form of an absolute http(s) URL. The
// scheme and the host are case-folded, a trailing dot on the host is
// removed, and the scheme's default port is removed. A value that is
// not one is reported so, and the caller compares lexically instead.
func Canonical(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if port := parsed.Port(); port != "" && port != defaultPort(parsed.Scheme) {
		host = host + ":" + port
	}
	canonical := parsed.Scheme + "://" + host + parsed.Path
	if parsed.RawQuery != "" {
		canonical += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		canonical += "#" + parsed.Fragment
	}
	return canonical, true
}

// WithinHost reports whether the canonical value is at or under the
// canonical prefix's host: the same host, or a subdomain of it.
//
// A lexical prefix comparison has no boundary at the host's edge, so a
// prefix for one host matches an attacker's adjacent one. The host
// comparison here is the boundary; the path stays lexical, the way the
// rule's own documentation says.
func WithinHost(value, prefix string) bool {
	parsedValue, err := url.Parse(value)
	if err != nil {
		return false
	}
	parsedPrefix, err := url.Parse(prefix)
	if err != nil {
		return false
	}
	valueHost, prefixHost := parsedValue.Host, parsedPrefix.Host
	if valueHost == prefixHost {
		return strings.HasPrefix(parsedValue.Path, parsedPrefix.Path)
	}
	return strings.HasSuffix(valueHost, "."+prefixHost)
}

// defaultPort is the port a scheme implies, and the one a canonical
// URL leaves out.
func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
