// Package urlnorm holds the canonical form of a URL.
//
// The permission engine matches a rule's prefix against a call's URL,
// and the fetch tool keys its doom-loop count by one. Both need the
// same answer for the same URL however it was spelled, so the one
// canonical form lives here and both read it.
package urlnorm

import (
	"net/url"
	"path"
	"strings"
)

// Canonical returns the canonical form of an absolute http(s) URL. The
// scheme and the host are case-folded, a trailing dot on the host is
// removed, the scheme's default port is removed, and the path is
// cleaned of its dot segments: the server normalizes them, so a rule
// that spells one path must not be evaded by a URL that spells it
// another way. A value that is not an absolute http(s) URL is reported
// so, and the caller compares lexically instead.
func Canonical(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if port := parsed.Port(); port != "" && port != defaultPort(parsed.Scheme) {
		host = host + ":" + port
	}
	cleaned := parsed.Path
	if cleaned != "" {
		cleaned = path.Clean(cleaned)
	}
	canonical := parsed.Scheme + "://" + host + cleaned
	if parsed.RawQuery != "" {
		canonical += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		canonical += "#" + parsed.Fragment
	}
	return canonical, true
}

// WithinHost reports whether the canonical value is at or under the
// canonical prefix: the same scheme, the same host or a subdomain of
// it. A cleartext http URL is never within an https prefix, and the
// other way around.
func WithinHost(value, prefix string) bool {
	parsedValue, err := url.Parse(value)
	if err != nil {
		return false
	}
	parsedPrefix, err := url.Parse(prefix)
	if err != nil {
		return false
	}
	if parsedValue.Scheme != parsedPrefix.Scheme {
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
