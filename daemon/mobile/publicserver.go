package mobile

import "github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"

// toPublicServers strips every credential. runnable reports whether this
// build can start kind for the profile; the hub must also have advertised it.
func toPublicServers(servers []serverInfo, runnable func(profile *state.Profile, kind string) bool) []publicServer {
	out := make([]publicServer, 0, len(servers))
	for i := range servers {
		s := &servers[i]
		profile := buildProfile(s)
		offers := func(advertised bool, kind string) bool { return advertised && runnable(profile, kind) }
		out = append(out, publicServer{
			ID:          s.ID,
			Name:        s.Name,
			Region:      s.Region,
			Country:     s.Country,
			Load:        s.Load,
			Multihop:    s.Multihop,
			Cloak:       offers(true, "cloak"),
			Reality:     offers(s.Reality != nil, "reality"),
			Hysteria2:   offers(s.Hysteria2 != nil, "hysteria2"),
			Shadowsocks: offers(s.Shadowsocks != nil, "shadowsocks"),
			AnyTLS:      offers(s.AnyTLS != nil, "anytls"),
			Naive:       offers(s.Naive != nil, "naive"),
			Snowflake:   offers(s.Snowflake != nil, "snowflake"),
		})
	}
	return out
}
