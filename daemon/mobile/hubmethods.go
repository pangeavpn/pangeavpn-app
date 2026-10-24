package mobile

// Ways the app may reach the hub. Ports apps/desktop/src/shared/hubMethods.ts.

// hubMethods are independent switches; at least one is always enabled.
type hubMethods struct {
	// DirectIP: cached hub IP, then a DoH-resolved IP with no SNI.
	DirectIP bool `json:"directIp"`
	// Reality: hub traffic through a local REALITY proxy, to a node user pinned to the hub.
	Reality bool `json:"reality"`
	// Shadowsocks: hub traffic through the local Shadowsocks proxy.
	Shadowsocks bool `json:"shadowsocks"`
	// Fronted: the envelope relayed by an edge worker on shared CDN space.
	Fronted bool `json:"fronted"`
	// Normal: plain HTTPS, the only method that puts the hub's name in clear.
	Normal bool `json:"normal"`
	// Rev records which default changes this blob has already seen.
	Rev int `json:"rev"`
}

// hubMethodOrder: directIp needs no lookup, REALITY passes for TLS where SS-2022
// is flagged as random, fronted only leaks timing, normal names the hub in clear.
var hubMethodOrder = []string{"directIp", "reality", "shadowsocks", "fronted", "normal"}

// hubMethodsRev is bumped when a default changes in a way existing installs
// should inherit, since an old default on disk looks like a deliberate choice.
const hubMethodsRev = 2

// revDefaults are the methods whose default flipped on at each rev.
var revDefaults = []struct {
	rev     int
	methods []string
}{
	{rev: 1, methods: []string{"shadowsocks", "fronted"}},
	{rev: 2, methods: []string{"reality"}},
}

// defaultHubMethods leaves Normal off: it is the only method whose SNI names
// the hub in cleartext.
func defaultHubMethods() hubMethods {
	return hubMethods{
		DirectIP:    true,
		Reality:     true,
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
	case "reality":
		return m.Reality
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
	case "reality":
		out.Reality = enabled
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

// normalize re-applies each changed default once for anything stored below
// its rev, then guarantees at least one method is on.
func (m hubMethods) normalize() hubMethods {
	out := m
	defaults := defaultHubMethods()
	for _, change := range revDefaults {
		if out.Rev >= change.rev {
			continue
		}
		for _, method := range change.methods {
			out = out.with(method, defaults.get(method))
		}
	}
	out.Rev = hubMethodsRev
	// directIp alone cannot reach the hub without a cached IP or DoH, so an
	// all-off blob gets the whole default set back.
	if len(out.enabled()) == 0 {
		return defaults
	}
	return out
}
