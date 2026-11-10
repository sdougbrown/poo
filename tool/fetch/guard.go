// The dial guard refuses the network addresses a fetch must not
// reach: loopback, link-local (the cloud metadata service lives
// there) and the reserved ranges. The refusal is the default, and the
// settings' allow_private_hosts lifts it.
package fetch

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
)

// allowedClass reports whether an address is fetchable, and whether a
// refusal of it names the setting that lifts it.
//
// Link-local is never fetchable: the cloud metadata service lives
// there, and a page that reads it is reading credentials. The
// reserved networks are never fetchable either. Loopback and the
// private ranges are fetchable when the settings allow private
// hosts, since they are the operator's own network.
func allowedClass(ip net.IP, allowPrivate bool) (bool, bool) {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false, false
	}
	if isReservedNetwork(ip) {
		return false, false
	}
	if !allowPrivate && (ip.IsLoopback() || ip.IsPrivate()) {
		return false, true
	}
	return true, false
}

// isReservedNetwork reports whether the address is in a range the
// fetch refuses whatever the settings say.
func isReservedNetwork(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 0:
			// 0.0.0.0/8: this network. Linux routes the block to loopback,
			// so an address like 0.1.2.3 reaches a service on localhost.
			return true
		case ip4[0] == 100 && ip4[1]&0xc0 == 64:
			// 100.64.0.0/10: the shared address space (CGNAT).
			return true
		case ip4[0] == 198 && ip4[1]&0xfe == 18:
			// 198.18.0.0/15: the benchmarking range.
			return true
		}
		return false
	}
	// fec0::/10: the deprecated site-local range. A host that still
	// answers on one is as private as the ranges the settings gate.
	return len(ip) == 16 && ip[0] == 0xfe && ip[1]&0xc0 == 0x80
}

// checkHost resolves a host and refuses it when none of its addresses
// is fetchable.
//
// The transport's guard checks what it dials, but a proxy configured
// in the environment makes the dial the proxy's address, so the
// target's host would go unchecked. The request's own host is checked
// here instead, before the fetch and at every redirect.
func checkHost(ctx context.Context, host string, allowPrivate bool) error {
	ips, err := addresses(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if allowed, liftable := allowedClass(ip, allowPrivate); !allowed {
			return &refusedError{address: ip.String(), liftable: liftable}
		}
	}
	return nil
}

// resolve is the host's resolver, a seam the tests point at a fake.
var resolve = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// addresses resolves a host to its addresses, taking an IP literal at
// its word. A literal needs no resolver, and the resolver could not
// answer for it anyway.
func addresses(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("fetch: resolving %q: %w", host, err)
	}
	resolved := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		resolved = append(resolved, ip.IP)
	}
	return resolved, nil
}

// guardedDial wraps a dialer so every address it dials is checked, and
// dials a validated address rather than the name.
//
// The check runs per address and not per URL. The dial goes to an
// address the check approved: a dial of the name resolves it a second
// time. A name whose answers change between the check and the dial
// has the guard approve one address and reach another.
//
// The dial of a proxy the environment names is exempt, whatever the
// scheme the request came in on: the proxy is the operator's
// infrastructure, and the target's host is checked before the fetch
// and at every redirect. The exemption is the proxy's own host string,
// so a redirect that leaves the proxy behind dials its own host, and
// the guard applies there.
func guardedDial(dialer *net.Dialer, allowPrivate bool, proxyHosts []string) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("fetch: the address %q is malformed: %w", address, err)
		}
		if !slices.Contains(proxyHosts, host) {
			ips, err := addresses(ctx, host)
			if err != nil {
				// A name that resolves to nothing has no address to
				// approve, and dialing the name would trust the resolver
				// the guard just refused to trust. The failure is the
				// call's.
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				if allowed, liftable := allowedClass(ip, allowPrivate); !allowed {
					return nil, &refusedError{address: ip.String(), liftable: liftable}
				}
			}
			for _, ip := range ips {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		}
		return dialer.DialContext(ctx, network, address)
	}
}

// proxyHosts returns the hosts the environment's proxies live on, by
// the name each proxy URL carries. The one list covers both schemes:
// a scheme's own proxy is on it, and a shared proxy appears once.
func proxyHosts() []string {
	var hosts []string
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		hosts = append(hosts, parsed.Hostname())
	}
	return hosts
}

// refusedError is the error a refused address fails with. It names
// the address, and the way out when there is one: the loopback and
// private refusals name the setting that lifts them, and the link-local
// and reserved refusals say the address is never fetchable, so an
// operator does not chase a setting that cannot help.
type refusedError struct {
	address  string
	liftable bool
}

func (e *refusedError) Error() string {
	if e.liftable {
		return "fetch: " + e.address + " is a loopback or private address, and tools.fetch.allow_private_hosts is false"
	}
	return "fetch: " + e.address + " is a link-local or reserved address, and the fetch refuses it whatever the settings say"
}
