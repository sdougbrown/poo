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
// there, and a page that reads it is reading credentials. Loopback
// and the private ranges are fetchable when the settings allow
// private hosts, since they are the operator's own network.
func allowedClass(ip net.IP, allowPrivate bool) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	if !allowPrivate && (ip.IsLoopback() || ip.IsPrivate()) {
		return false
	}
	return true
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
		if !allowedClass(ip, allowPrivate) {
			return &refusedError{address: ip.String()}
		}
	}
	return nil
}

// addresses resolves a host to its addresses, taking an IP literal at
// its word: a literal needs no resolver, and the resolver could not
// answer for it anyway.
func addresses(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
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
// The check runs per address and not per URL, and the dial goes to an
// address the check approved: a dial of the name would resolve it a
// second time, and a name whose answers change between the check and
// the dial would have the guard approve one address and reach another.
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

// refusedError is the error a refused address fails with. It names the
// address and the setting that lifts the refusal, so a model whose
// target is legitimate can correct the settings and carry on.
type refusedError struct {
	address string
}

func (e *refusedError) Error() string {
	return "fetch: " + e.address + " is a loopback or private address, and tools.fetch.allow_private_hosts is false"
}
