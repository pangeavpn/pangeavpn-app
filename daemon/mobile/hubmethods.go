package mobile

// Ways the app may reach the hub. Ports apps/desktop/src/shared/hubMethods.ts.

// hubMethods are independent switches; at least one is always enabled.
type hubMethods struct {
	// DirectIP: cached hub IP, then a DoH-resolved IP with no SNI.
	DirectIP bool `json:"directIp"`
	// Shadowsocks: hub traffic through the local Shadowsocks proxy.
	Shadowsocks bool `json:"shadowsocks"`
	// Fronted: the envelope relayed by an edge worker on shared CDN space.
	Fronted bool `json:"fronted"`
	// Normal: plain HTTPS, the only method that puts the hub's name in clear.
	Normal bool `json:"normal"`
	// Rev records which default changes this blob has already seen.
	Rev int `json:"rev"`
}

// hubMethodOrder is the attempt order. DirectIP needs no lookup; Fronted hands
// a third party our timing (never content) so it sits after our own paths.
var hubMethodOrder = []string{"directIp", "shadowsocks", "fronted", "normal"}

// hubMethodsRev is bumped when a default changes in a way existing installs
// should inherit, since an old default on disk looks like a deliberate choice.
const hubMethodsRev = 1

// rev1Defaults are the methods whose default flipped on at rev 1.
var rev1Defaults = []string{"shadowsocks", "fronted"}

// defaultHubMethods leaves Normal off: it is the only method whose SNI names
// the hub in cleartext.
func defaultHubMethods() hubMethods {
	return hubMethods{
		DirectIP:    true,
		Shadowsocks: true,
		Fronted:     true,
		Normal:      false,
		Rev:         hubMethodsRev,
	}
}

func (m hubMethods) get(method string) bool {
	switch method {
	case "directIp":
		return m.DirectIP
	case "shadowsocks":
		return m.Shadowsocks
	case "fronted":
		return m.Fronted
	case "normal":
		return m.Normal
	default:
		return false
	}
}

func (m hubMethods) with(method string, enabled bool) hubMethods {
	out := m
	switch method {
	case "directIp":
		out.DirectIP = enabled
	case "shadowsocks":
		out.Shadowsocks = enabled
	case "fronted":
		out.Fronted = enabled
	case "normal":
		out.Normal = enabled
	}
	return out
}

// enabled lists the switched-on methods in attempt order.
func (m hubMethods) enabled() []string {
	out := make([]string, 0, len(hubMethodOrder))
	for _, method := range hubMethodOrder {
		if m.get(method) {
			out = append(out, method)
		}
	}
	return out
}

// applyHubMethod flips one switch, refusing to disable the last one so the
// caller can say why nothing moved rather than silently correcting it.
func applyHubMethod(current hubMethods, method string, enabled bool) (hubMethods, bool) {
	if !isHubMethod(method) {
		return current, false
	}
	if current.get(method) == enabled {
		return current, true
	}
	if !enabled && len(current.enabled()) == 1 {
		return current, false
	}
	return current.with(method, enabled), true
}

func isHubMethod(value string) bool {
	for _, method := range hubMethodOrder {
		if method == value {
			return true
		}
	}
	return false
}

// normalizeHubMethods re-applies changed defaults once for anything stored
// below the current rev, then guarantees at least one method is on.
func (m hubMethods) normalize() hubMethods {
	out := m
	if out.Rev < hubMethodsRev {
		defaults := defaultHubMethods()
		for _, method := range rev1Defaults {
			out = out.with(method, defaults.get(method))
		}
	}
	out.Rev = hubMethodsRev
	if len(out.enabled()) == 0 {
		out.DirectIP = true
	}
	return out
}
