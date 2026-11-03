package mobile

// Hostnames for the hub's own domain. Ports apps/desktop/src/shared/hubHosts.ts.

const (
	hubHost = "api.pangeavpn.org"
	// hubMirrorHost sits behind a CDN with no SNI routing, so only the normal
	// method may use it; direct IP always presents hubHost.
	hubMirrorHost = "api.pangeavpn.it"
)

// normalHubHosts is tried in order by the normal method. Primary first: the
// mirror costs a round trip and puts a second name on the wire.
func normalHubHosts() []string {
	return []string{hubHost, hubMirrorHost}
}
