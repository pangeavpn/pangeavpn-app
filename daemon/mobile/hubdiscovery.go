package mobile

// hubRoutes are the ways back into the hub that a server list advertised.
type hubRoutes struct {
	shadowsocks []hubShadowsocksCreds
	reality     []hubRealityCreds
	fronted     []string
}

// discoverHubRoutes collects every node's control-plane credentials; the
// relays repeat per region, so the first region that names them speaks for all.
func discoverHubRoutes(servers []serverInfo) hubRoutes {
	var routes hubRoutes
	for _, server := range servers {
		if ss := server.ControlPlaneShadowsocks; ss != nil {
			routes.shadowsocks = append(routes.shadowsocks, hubShadowsocksCreds{
				RemoteHost: ss.RemoteHost, RemotePort: ss.RemotePort, Method: ss.Method, Password: ss.Password,
			})
		}
		if re := server.ControlPlaneReality; re != nil {
			routes.reality = append(routes.reality, hubRealityCreds{
				RemoteHost: re.RemoteHost, RemotePort: re.RemotePort, UUID: re.UUID,
				PublicKey: re.PublicKey, ShortID: re.ShortID, ServerName: re.ServerName,
			})
		}
		if routes.fronted == nil && len(server.FrontedEndpoints) > 0 {
			routes.fronted = server.FrontedEndpoints
		}
	}
	return routes
}
