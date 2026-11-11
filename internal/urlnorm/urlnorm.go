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
	"strconv"
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
	if port := parsed.Port(); port != "" {
		// The port is compared the way the dialer reads it: a spelling
		// with leading zeros names the same port, and a rule that
		// spells the port without them matches it.
		if n, err := strconv.Atoi(port); err == nil && strconv.Itoa(n) != defaultPort(parsed.Scheme) {
			host = host + ":" + strconv.Itoa(n)
		}
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

// CanonicalPrefix returns the canonical form of a rule's prefix under
// the scheme of the canonical value it will be matched against.
//
// A prefix that names no scheme of its own is a hostname rule. The
// call's own URL decides the scheme, so one hostname covers both.
//
// A prefix that names a scheme of its own canonicalizes as itself,
// and its scheme must hold. A cleartext URL never matches an https
// prefix, or the other way around. A scheme the canonical form does
// not take is refused, and so is a prefix whose authority carries
// userinfo. A hostname rule names a host; userinfo names nothing a
// URL rule can mean. A refused prefix matches nothing canonically,
// and the caller keeps the lexical comparison. That never widens the
// rule past what the operator typed.
func CanonicalPrefix(canonical, prefix string) (string, bool) {
	scheme, _, ok := strings.Cut(canonical, "://")
	if !ok || (scheme != "http" && scheme != "https") {
		return "", false
	}
	authority := prefix
	if _, rest, found := strings.Cut(prefix, "://"); found {
		authority = rest
	}
	// The authority ends at the first slash, question mark, or hash.
	// Past one of those, an @ is a path, a query, or a fragment — not
	// userinfo.
	authority, _, _ = strings.Cut(authority, "/")
	authority, _, _ = strings.Cut(authority, "?")
	authority, _, _ = strings.Cut(authority, "#")
	if strings.Contains(authority, "@") {
		return "", false
	}
	if strings.Contains(prefix, "://") {
		return Canonical(prefix)
	}
	return Canonical(scheme + "://" + prefix)
}

// WithinHost reports whether the canonical value is at or under the
// canonical prefix: the same scheme, the same host or a subdomain of
// it. A path on the prefix holds its segment boundary: /api matches
// /api and /api/v2, and never /api-internal. The boundary holds on
// the host and its subdomains alike. A path that names no segment is
// the whole host: the scheme-plus-host spelling and the root path are
// one resource. A cleartext http URL is never within an https prefix,
// and the other way around. The query and the fragment take no part
// in the comparison: the prefix speaks to the host and its path.
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
		return pathWithin(parsedValue.Path, parsedPrefix.Path)
	}
	return strings.HasSuffix(valueHost, "."+prefixHost) && pathWithin(parsedValue.Path, parsedPrefix.Path)
}

// pathWithin reports whether the value's path sits at or under the
// prefix path: the same path, or a continuation at a segment boundary.
// A prefix that names no path, or only the root, covers every path on
// the host. The value's empty path is the bare-host spelling of the
// root, so it joins them.
func pathWithin(valuePath, prefixPath string) bool {
	if valuePath == "" {
		valuePath = "/"
	}
	if prefixPath == "" || prefixPath == "/" {
		return strings.HasPrefix(valuePath, "/")
	}
	return valuePath == prefixPath || strings.HasPrefix(valuePath, prefixPath+"/")
}

// defaultPort is the port a scheme implies, and the one a canonical
// URL leaves out.
func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
