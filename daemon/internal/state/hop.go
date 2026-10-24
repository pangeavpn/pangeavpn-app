package state

import (
	"errors"
	"fmt"
)

var errSameRegion = errors.New("hop entryRegion and exitRegion must differ")

func errHopIncomplete(field string) error {
	return fmt.Errorf("hop is missing %s: a partial hop would egress at the entry node", field)
}

// DefaultCloakProxyMethod and DefaultNaiveBridgePort name the single-hop
// target: the node's own WireGuard listener, addressed per protocol.
const (
	DefaultCloakProxyMethod = "wireguard"
	DefaultNaiveBridgePort  = 9000
)

// IsMultihop reports whether the transport terminates on an entry node
// separate from the exit node holding the WireGuard peer.
func (p Profile) IsMultihop() bool { return p.Hop != nil }

// EntryRegion and ExitRegion label the session for status. Both are empty on
// a single-hop profile, where one node is both.
func (p Profile) EntryRegion() string {
	if p.Hop == nil {
		return ""
	}
	return p.Hop.EntryRegion
}

func (p Profile) ExitRegion() string {
	if p.Hop == nil {
		return ""
	}
	return p.Hop.ExitRegion
}

// ApplyHop returns a copy whose transport sub-profiles carry the destination
// each should request on the remote node; every transport must go through here.
func ApplyHop(p Profile) Profile {
	out := p

	out.Cloak = p.Cloak
	out.Cloak.ProxyMethod = cloakProxyMethod(p.Hop)

	if p.Reality != nil {
		reality := *p.Reality
		reality.TargetPort = singBoxTargetPort(p.Hop)
		out.Reality = &reality
	}
	if p.Hysteria2 != nil {
		hysteria2 := *p.Hysteria2
		hysteria2.TargetPort = singBoxTargetPort(p.Hop)
		out.Hysteria2 = &hysteria2
	}
	if p.Shadowsocks != nil {
		shadowsocks := *p.Shadowsocks
		shadowsocks.TargetPort = singBoxTargetPort(p.Hop)
		// The hop port is a loopback service on the entry, same as the local
		// WireGuard listener it replaces.
		shadowsocks.TargetHost = loopbackHost
		out.Shadowsocks = &shadowsocks
	}
	if p.AnyTLS != nil {
		anytls := *p.AnyTLS
		anytls.TargetPort = singBoxTargetPort(p.Hop)
		// Same as Shadowsocks: the hop port is a loopback service on the entry.
		anytls.TargetHost = loopbackHost
		out.AnyTLS = &anytls
	}
	if p.Naive != nil {
		naive := *p.Naive
		naive.BridgePort = naiveBridgePort(p.Hop)
		out.Naive = &naive
	}

	return out
}

const loopbackHost = "127.0.0.1"

func singBoxTargetPort(hop *HopProfile) int {
	if hop != nil && hop.SingBoxPort > 0 {
		return hop.SingBoxPort
	}
	return DefaultWireGuardPort
}

func cloakProxyMethod(hop *HopProfile) string {
	if hop != nil && hop.CloakProxyMethod != "" {
		return hop.CloakProxyMethod
	}
	return DefaultCloakProxyMethod
}

func naiveBridgePort(hop *HopProfile) int {
	if hop != nil && hop.NaiveBridgePort > 0 {
		return hop.NaiveBridgePort
	}
	return DefaultNaiveBridgePort
}

// ValidateHop rejects a hop the hub did not fully specify: a partial hop
// would fall back to defaults and silently egress at the entry node.
func ValidateHop(p Profile) error {
	if p.Hop == nil {
		return nil
	}
	if p.Hop.SingBoxPort <= 0 {
		return errHopIncomplete("singBoxPort")
	}
	if p.Hop.CloakProxyMethod == "" {
		return errHopIncomplete("cloakProxyMethod")
	}
	if p.Naive != nil && p.Hop.NaiveBridgePort <= 0 {
		return errHopIncomplete("naiveBridgePort")
	}
	if p.Hop.EntryRegion == "" || p.Hop.ExitRegion == "" {
		return errHopIncomplete("entryRegion/exitRegion")
	}
	if p.Hop.EntryRegion == p.Hop.ExitRegion {
		return errSameRegion
	}
	return nil
}
