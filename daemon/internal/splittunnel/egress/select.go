package egress

import (
	"net/netip"
	"strings"
)

// IF_TYPE values from ipifcons.h.
const (
	ifTypePPP              = 23
	ifTypeSoftwareLoopback = 24
	ifTypePropVirtual      = 53
	ifTypeTunnel           = 131
	ifTypeWwanpp           = 243
	ifTypeWwanpp2          = 244
)

// winRoute is one IPv4 0.0.0.0/0 row of GetIPForwardTable2 with its interface's state.
type winRoute struct {
	LUID     uint64
	Index    uint32
	Name     string
	IfType   uint32
	Up       bool
	Loopback bool
	NextHop  netip.Addr
	Metric   uint64 // route metric + interface metric
}

func selectWindowsDefault(rows []winRoute) (winRoute, bool) {
	var best winRoute
	found := false
	for _, r := range rows {
		if !r.Up || r.Loopback || r.Index == 0 {
			continue
		}
		switch r.IfType {
		case ifTypeSoftwareLoopback, ifTypePropVirtual, ifTypeTunnel:
			continue
		}
		if windowsVirtualName(r.Name) {
			continue
		}
		if r.NextHop.IsLoopback() || r.NextHop.IsMulticast() {
			continue
		}
		// An on-link default belongs to a tunnel unless the link itself is point-to-point.
		onLink := !r.NextHop.IsValid() || r.NextHop.IsUnspecified()
		if onLink && r.IfType != ifTypePPP && r.IfType != ifTypeWwanpp && r.IfType != ifTypeWwanpp2 {
			continue
		}
		if !found || r.Metric < best.Metric || (r.Metric == best.Metric && r.LUID < best.LUID) {
			best, found = r, true
		}
	}
	return best, found
}

// windowsVirtualName mirrors platform.PhysicalDefaultRoute, for adapters that report a physical type.
func windowsVirtualName(alias string) bool {
	lower := strings.ToLower(alias)
	for _, prefix := range []string{"pangea", "wg", "tun", "tap", "loopback"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// ribRoute is one darwin routing-socket entry reduced to what selection needs.
type ribRoute struct {
	Index         int
	DefaultV4     bool
	Up            bool
	Gateway       bool
	IfScope       bool
	RejectOrBlack bool
	NextHop       netip.Addr
	Mirror        bool // RTF_IFSCOPE|RTF_PROTO2: a scoped default this package installed
}

func darwinVirtualName(name string) bool {
	for _, prefix := range []string{"utun", "ipsec", "lo", "gif", "stf", "awdl", "llw", "bridge"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// selectDarwinDefault prefers the unscoped default (the primary service) over interface-scoped ones.
func selectDarwinDefault(routes []ribRoute, iface func(index int) (name string, up bool)) (int, string, bool) {
	scopedIndex, scopedName := 0, ""
	for _, r := range routes {
		if !r.DefaultV4 || !r.Up || !r.Gateway || r.RejectOrBlack || r.Mirror || r.Index <= 0 {
			continue
		}
		name, up := iface(r.Index)
		if name == "" || !up || darwinVirtualName(name) {
			continue
		}
		if !r.IfScope {
			return r.Index, name, true
		}
		if scopedIndex == 0 {
			scopedIndex, scopedName = r.Index, name
		}
	}
	return scopedIndex, scopedName, scopedIndex != 0
}

// scopedDefault is an interface-scoped IPv4 default route.
type scopedDefault struct {
	Index   int
	Name    string
	NextHop netip.Addr
}

// darwinMirrors lists the scoped defaults this package installed whose interface still has a name.
func darwinMirrors(routes []ribRoute, iface func(index int) (name string, up bool)) []scopedDefault {
	var out []scopedDefault
	for _, r := range routes {
		if !r.Mirror || !r.DefaultV4 {
			continue
		}
		if name, _ := iface(r.Index); name != "" {
			out = append(out, scopedDefault{Index: r.Index, Name: name, NextHop: r.NextHop})
		}
	}
	return out
}

// mirrorPlan is what keeping the primary default's scoped copy takes: Primary is its interface
// index (0 without one), Add the copy to install, Remove the mirrors that no longer match it.
type mirrorPlan struct {
	Primary int
	Add     *scopedDefault
	Remove  []scopedDefault
}

func planDarwinMirror(routes []ribRoute, iface func(index int) (name string, up bool)) mirrorPlan {
	var want scopedDefault
	for _, r := range routes {
		if !r.DefaultV4 || !r.Up || !r.Gateway || r.RejectOrBlack || r.IfScope || r.Mirror || r.Index <= 0 || !r.NextHop.Is4() {
			continue
		}
		if name, up := iface(r.Index); name != "" && up && !darwinVirtualName(name) {
			want = scopedDefault{Index: r.Index, Name: name, NextHop: r.NextHop}
			break
		}
	}
	plan := mirrorPlan{Primary: want.Index}
	inPlace := false
	for _, m := range darwinMirrors(routes, iface) {
		if want.Index != 0 && m.Index == want.Index && m.NextHop == want.NextHop {
			inPlace = true
			continue
		}
		plan.Remove = append(plan.Remove, m)
	}
	// Any other scoped default on that interface holds the key, so an add could only fail with EEXIST.
	taken := false
	for _, r := range routes {
		taken = taken || (r.DefaultV4 && r.IfScope && !r.Mirror && r.Index == want.Index)
	}
	if want.Index != 0 && !inPlace && !taken {
		plan.Add = &want
	}
	return plan
}

const (
	linuxTableMain  = 254
	linuxRTNUnicast = 1
)

// linuxRoute is one AF_INET RTM_NEWROUTE record.
type linuxRoute struct {
	DstLen   uint8
	Table    uint32
	Type     uint8
	Oif      int
	Priority uint32
}

func linuxVirtualName(name string) bool {
	for _, prefix := range []string{"lo", "pangea", "wg", "tun", "tap"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func selectLinuxDefault(routes []linuxRoute, iface func(index int) (name string, up bool)) (int, string, bool) {
	var best linuxRoute
	bestName := ""
	for _, r := range routes {
		if r.DstLen != 0 || r.Table != linuxTableMain || r.Type != linuxRTNUnicast || r.Oif <= 0 {
			continue
		}
		name, up := iface(r.Oif)
		if name == "" || !up || linuxVirtualName(name) {
			continue
		}
		if bestName == "" || r.Priority < best.Priority || (r.Priority == best.Priority && r.Oif < best.Oif) {
			best, bestName = r, name
		}
	}
	return best.Oif, bestName, bestName != ""
}
