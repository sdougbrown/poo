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

// guardedDial wraps a dialer so every address it dials is checked.
//
// The check runs per address and not per URL: a name with several
// records is checked record by record, and a redirect to a refused
// address is refused too, since a redirect dials as well.
func guardedDial(dialer *net.Dialer, allowPrivate bool) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("fetch: the address %q is malformed: %w", address, err)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			// A name the resolver cannot turn into addresses is not
			// the guard's business: the dial reports it.
			return dialer.DialContext(ctx, network, address)
		}
		for _, ip := range ips {
			if !allowedClass(ip.IP, allowPrivate) {
				return nil, &refusedError{address: ip.IP.String()}
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
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
