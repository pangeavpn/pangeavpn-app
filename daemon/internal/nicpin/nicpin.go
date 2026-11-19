// Package nicpin lets the daemon dial a shared destination (a CDN edge) off-tunnel by
// pinning its own sockets to the physical NIC, never by a route every app would follow.
package nicpin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing/common/control"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// ErrUnsupported is returned where sockets are protected another way (mobile).
var ErrUnsupported = errors.New("pinning sockets to the physical network is not supported here")

// Pin is one acquired pinning. Release it once the sockets using it are closed.
type Pin struct {
	// Server is the destination as an address literal, so the dial needs no lookup.
	Server string
	// Addrs are every address the host resolved to; each is reachable through the pin.
	Addrs []netip.Addr
	// Interface is the NIC to bind to; empty on Linux, which uses Mark instead.
	Interface string
	Mark      uint32
	index     int
	release   func()
	once      sync.Once
}

// Release undoes the pinning; safe to call more than once and on nil.
func (p *Pin) Release() {
	if p == nil || p.release == nil {
		return
	}
	p.once.Do(p.release)
}

// Control applies the pin to a plain net.Dialer's socket.
func (p *Pin) Control() control.Func {
	if p.Mark != 0 {
		return control.RoutingMark(p.Mark)
	}
	return control.BindToInterface(nil, p.Interface, p.index)
}

var (
	lookupIP = platform.CachedLookupIP
	// physical is replaced by tests; production asks the OS for the default-route NIC.
	physical = platform.PhysicalDefaultRoute
)

const resolveTimeout = 5 * time.Second

// Acquire pins the daemon's sockets for host to the physical NIC. It fails rather than
// leave a socket unpinned: unpinned, the dial would loop into the tunnel or be blocked.
func Acquire(ctx context.Context, host string) (*Pin, error) {
	addrs, err := resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	pin, err := acquire(addrs)
	if err != nil {
		return nil, fmt.Errorf("pin %s to the physical network: %w", host, err)
	}
	pin.Addrs = addrs
	pin.Server = addrs[0].String()
	return pin, nil
}

// resolve returns the public IPv4 addresses of host; the lock blocks IPv6 outright.
func resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil, errors.New("no host to pin")
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
		defer cancel()
		found, err := lookupIP(lookupCtx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}
		ips = found
	}
	var out []netip.Addr
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if addr.Is4() && platform.DaemonEgressAddr(addr) && !containsAddr(out, addr) {
			out = append(out, addr)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no public IPv4 address to pin", host)
	}
	return out, nil
}

func containsAddr(addrs []netip.Addr, addr netip.Addr) bool {
	for _, a := range addrs {
		if a == addr {
			return true
		}
	}
	return false
}

// physicalInterface names the default-route NIC and its index, refusing a tunnel.
func physicalInterface() (name string, index int, gateway string, err error) {
	name, gateway, err = physical()
	if err != nil {
		return "", 0, "", err
	}
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return "", 0, "", fmt.Errorf("look up %s: %w", name, err)
	}
	if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
		return "", 0, "", fmt.Errorf("%s is not an up, non-loopback interface", name)
	}
	return ifc.Name, ifc.Index, gateway, nil
}
