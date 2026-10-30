//go:build android || ios

package mobile

// TestHubMethod and HubStatus. Ports testHubMethod in
// apps/desktop/src/main/pangeaApiClient.ts: nothing is persisted or promoted.

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

var hubTestBusy atomic.Bool

// hubTestResolve finds the hub's address for a directIp test; tests replace it
// to stay off the network.
var hubTestResolve = func() (string, error) { return resolveViaDoH(hubHost) }

// TestHubMethod probes one method on its own, never touching the route in use.
func TestHubMethod(method string) (string, error) {
	return runHubTest(method, &hubTestBusy, testHubMethodOnce)
}

// HubStatus reports the switches and the route this process is using now.
func HubStatus() string {
	mu.Lock()
	methods := settings.HubMethods.normalize()
	mu.Unlock()
	hubMu.Lock()
	route := activeHubPath
	hubMu.Unlock()
	if route == nil {
		return encodeHubStatus(hubStatusFor(methods, "", ""))
	}
	return encodeHubStatus(hubStatusFor(methods, route.kind, route.detail))
}

func testHubMethodOnce(method string) hubTestResult {
	if result, borrowed := borrowActiveRoute(method); borrowed {
		return result
	}
	mu.Lock()
	logs := wgLogs
	mu.Unlock()
	switch method {
	case "directIp":
		return testDirectMethod(storedValue(keyHubIP), hubTestResolve)
	case "reality":
		creds := loadHubReality()
		nodes := make([]string, len(creds))
		for i, c := range creds {
			nodes[i] = c.RemoteHost
		}
		return testProxyMethod(method, nodes, func(i int) (hubProxy, int, error) { return startRealityHubProxy(creds[i], logs) }, logs)
	case "shadowsocks":
		creds := loadHubShadowsocks()
		nodes := make([]string, len(creds))
		for i, c := range creds {
			nodes[i] = c.RemoteHost
		}
		return testProxyMethod(method, nodes, func(i int) (hubProxy, int, error) { return startShadowsocksHubProxy(creds[i], logs) }, logs)
	case "fronted":
		return testFrontedMethod(loadFrontedEndpoints())
	case "normal":
		return testHostPaths(normalHubHosts(), newNormalPath)
	}
	return hubTestResult{}
}

// borrowActiveRoute probes the live route with roundTrips, not probe: probe
// closes a failing path, which would kill the route while it stays installed.
func borrowActiveRoute(method string) (hubTestResult, bool) {
	hubMu.Lock()
	active := activeHubPath
	hubMu.Unlock()
	if active == nil || active.kind != method {
		return hubTestResult{}, false
	}
	if active.roundTrips() {
		return hubTestResult{OK: true, Detail: active.detail}, true
	}
	return hubTestResult{}, true
}

// testDirectMethod tries the cached address, then a DoH answer, and unlike
// tryDirectIPPaths never stores the one that worked.
func testDirectMethod(cached string, resolve func() (string, error)) hubTestResult {
	tried := false
	if isIPv4Literal(cached) {
		tried = true
		if probeOnce(newDirectIPPath(cached)) {
			return hubTestResult{OK: true, Detail: cached}
		}
	}
	if ip, err := resolve(); err == nil && ip != "" && ip != cached {
		tried = true
		if probeOnce(newDirectIPPath(ip)) {
			return hubTestResult{OK: true, Detail: ip}
		}
	}
	if !tried {
		return hubTestResult{Unavailable: hubTestNoAddress}
	}
	return hubTestResult{}
}

func testFrontedMethod(hosts []string) hubTestResult {
	if len(hosts) == 0 {
		return hubTestResult{Unavailable: hubTestNoRelay}
	}
	return testHostPaths(hosts, newFrontedPath)
}

func testHostPaths(hosts []string, newPath func(string) *hubPath) hubTestResult {
	for _, host := range hosts {
		if probeOnce(newPath(host)) {
			return hubTestResult{OK: true, Detail: host}
		}
	}
	return hubTestResult{}
}

// testProxyMethod starts each node's proxy in turn and always stops it again;
// unlike tryRealityPath it promotes and saves nothing.
func testProxyMethod(kind string, nodes []string, start func(int) (hubProxy, int, error), logs *state.LogStore) hubTestResult {
	if len(nodes) == 0 {
		return hubTestResult{Unavailable: hubTestNoCredentials}
	}
	for i, node := range nodes {
		manager, port, err := start(i)
		if err != nil || port == 0 || manager == nil {
			logAdd(logs, state.LogWarn, fmt.Sprintf("hub test: %s node %s did not start: %v", kind, node, err))
			if manager != nil {
				_ = manager.Stop(context.Background())
			}
			continue
		}
		username, password := manager.Credentials()
		path := newProxyPath(kind, node, port, username, password)
		path.proxy = manager
		if probeOnce(path) {
			return hubTestResult{OK: true, Detail: node}
		}
	}
	return hubTestResult{}
}

// probeOnce proves a throwaway path and closes it whatever the outcome.
func probeOnce(path *hubPath) bool {
	ok := path.roundTrips()
	path.close()
	return ok
}
