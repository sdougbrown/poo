// The dial guard refuses the network addresses a fetch must not
// reach: loopback, link-local (the cloud metadata service lives
// there) and the private ranges. The refusal is the default, and the
// settings' allow_private_hosts lifts it.
package fetch

import (
	"context"
	"fmt"
	"net"
)

// allowedClass reports whether an address is fetchable.
//
// Link-local is never fetchable: the cloud metadata service lives
// there, and a page that reads it is reading credentials. The
// reserved networks are never fetchable either. Loopback and the
// private ranges are fetchable when the settings allow private
// hosts, since they are the operator's own network.
func allowedClass(ip net.IP, allowPrivate bool) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	if isReservedNetwork(ip) {
		return false
	}
	if !allowPrivate && (ip.IsLoopback() || ip.IsPrivate()) {
		return false
	}
	return true
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
// in the environment makes the dial the proxy's address. The target's
// host then goes unchecked. The request's own host is checked
// here instead, before the fetch and at every redirect.
func checkHost(ctx context.Context, host string, allowPrivate bool) error {
	ips, err := addresses(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if !allowedClass(ip, allowPrivate) {
			return &refusedError{address: ip.String()}
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
// A request the environment sends through a proxy marks its context,
// and the dial of the proxy's own address is exempt: the proxy is the
// operator's infrastructure, and the target's host is checked before
// the fetch and at every redirect.
func guardedDial(dialer *net.Dialer, allowPrivate bool) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("fetch: the address %q is malformed: %w", address, err)
		}
		ips, err := addresses(ctx, host)
		if err != nil {
			// A name that resolves to nothing has no address to
			// approve, and dialing the name would trust the resolver
			// the guard just refused to trust. The failure is the
			// call's.
			return nil, err
		}
		if ctx.Value(proxiedKey{}) != nil {
			allowPrivate = true
		}
		var lastErr error
		for _, ip := range ips {
			if !allowedClass(ip, allowPrivate) {
				return nil, &refusedError{address: ip.String()}
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
}

// proxiedKey marks a request's context when the environment names a
// proxy for it, so the dial of the proxy's own address is exempt.
type proxiedKey struct{}

// refusedError is the error a refused address fails with. It names the
// address and the setting that lifts the refusal, so a model whose
// target is legitimate can correct the settings and carry on.
type refusedError struct {
	address string
}

func (e *refusedError) Error() string {
	return "fetch: " + e.address + " is a loopback or private address, and tools.fetch.allow_private_hosts is false"
}
