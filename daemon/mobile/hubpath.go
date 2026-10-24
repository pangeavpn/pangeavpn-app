//go:build android

package mobile

// The ways the envelope reaches the hub. Ports ensureHub in
// apps/desktop/src/main/pangeaApiClient.ts.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reality"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	keyHubIP       = "hubIp"
	keyFronted     = "frontedEndpoints"
	keyHubSS       = "hubShadowsocks"
	keyHubReality  = "hubReality"
	probeRoute     = "/api/client/regions"
	hubProbeTimout = 8 * time.Second
	// hubRequestTimeout is for real requests, which may outlast a probe.
	hubRequestTimeout = 15 * time.Second
	// hubProxyStartTimeout covers the REALITY handshake Start proves first.
	hubProxyStartTimeout = 15 * time.Second
	maxHubResponseBytes  = 8 << 20
)

// hubResolveMu makes a concurrent caller join the search already running
// instead of starting proxies of its own.
var hubResolveMu sync.Mutex

// hubPath is one resolved way to POST an envelope.
type hubPath struct {
	kind string
	// detail is the address, relay or node the path reached, for the log.
	detail string
	base   string
	host   string
	client *http.Client
	// proxy is the local proxy this path rides on, stopped along with it.
	proxy hubProxy
}

// close stops the path's proxy and drops its idle connections.
func (p *hubPath) close() {
	p.client.CloseIdleConnections()
	if p.proxy != nil {
		_ = p.proxy.Stop(context.Background())
	}
}

// hubProxy is a local proxy carrying hub traffic; the live one is kept so a
// later reset can stop it.
type hubProxy interface {
	Stop(ctx context.Context) error
	Credentials() (string, string)
}

// newDirectIPPath dials the address with no SNI. The envelope is sealed end to
// end, so the transport certificate carries no trust here.
func newDirectIPPath(ip string) *hubPath {
	return &hubPath{
		kind:   "directIp",
		detail: ip,
		base:   "https://" + ip,
		host:   hubHost,
		client: &http.Client{
			Timeout: hubRequestTimeout,
			Transport: &http.Transport{
				DialContext:     protectedDialer(hubProbeTimout).DialContext,
				TLSClientConfig: &tls.Config{ServerName: "", InsecureSkipVerify: true},
			},
		},
	}
}

// newFrontedPath validates the relay's certificate normally: it is a real CDN
// host, and it only ever carries a sealed envelope.
func newFrontedPath(host string) *hubPath {
	return newValidatedPath("fronted", host)
}

// newNormalPath is the only path that puts a hub name on the wire.
func newNormalPath(host string) *hubPath {
	return newValidatedPath("normal", host)
}

func newValidatedPath(kind, host string) *hubPath {
	return &hubPath{
		kind:   kind,
		detail: host,
		base:   "https://" + host,
		host:   host,
		client: &http.Client{
			Timeout:   hubRequestTimeout,
			Transport: &http.Transport{DialContext: protectedDialer(hubProbeTimout).DialContext},
		},
	}
}

// newProxyPath goes through a local mixed inbound, which answers HTTP CONNECT
// only with its per-session credentials.
func newProxyPath(kind, node string, port int, username, password string) *hubPath {
	proxyURL := &url.URL{
		Scheme: "http",
		Host:   "127.0.0.1:" + strconv.Itoa(port),
		User:   url.UserPassword(username, password),
	}
	return &hubPath{
		kind:   kind,
		detail: node,
		base:   "https://" + hubHost,
		host:   hubHost,
		client: &http.Client{
			Timeout: hubRequestTimeout,
			Transport: &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				DialContext:     protectedDialer(hubProbeTimout).DialContext,
				TLSClientConfig: &tls.Config{ServerName: hubHost},
			},
		},
	}
}

// postEnvelope sends one sealed envelope to route over this path.
func (p *hubPath) postEnvelope(ctx context.Context, route string, envJSON []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+route, bytes.NewReader(envJSON))
	if err != nil {
		return nil, 0, err
	}
	req.Host = p.host
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHubResponseBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > maxHubResponseBytes {
		return nil, 0, errors.New("hub response too large")
	}
	return body, resp.StatusCode, nil
}

// probe proves a path end to end by round-tripping a real secure request,
// closing it on failure so the search can fall through to the next method.
func (p *hubPath) probe() bool {
	if p.roundTrips() {
		return true
	}
	p.close()
	return false
}

func (p *hubPath) roundTrips() bool {
	sealed, err := sealRequest(http.MethodGet, probeRoute, map[string]string{}, nil)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), hubProbeTimout)
	defer cancel()
	body, status, err := p.postEnvelope(ctx, sealed.route, sealed.envelope)
	if err != nil {
		return false
	}
	_, err = openHubReply(sealed, body, status)
	return err == nil
}

// ensureHubPath finds a working way to reach the hub, trying each enabled
// method in order and caching whatever wins until it stops working.
func ensureHubPath() error {
	hubResolveMu.Lock()
	defer hubResolveMu.Unlock()

	// A settings change mid-search discards the result, so allow one rerun.
	for attempt := 0; attempt < 2; attempt++ {
		hubMu.Lock()
		if activeHubPath != nil {
			hubMu.Unlock()
			return nil
		}
		if wait := hubTrack.retryIn(time.Now()); wait > 0 {
			hubMu.Unlock()
			return fmt.Errorf("hub unreachable: retrying in %ds", int(wait.Seconds())+1)
		}
		generation := hubTrack.generation
		hubMu.Unlock()

		path, err := searchHub()
		hubMu.Lock()
		if hubTrack.generation != generation {
			hubMu.Unlock()
			if path != nil {
				path.close()
			}
			continue
		}
		if err != nil {
			hubTrack.searchFailed(time.Now())
			hubMu.Unlock()
			return err
		}
		activeHubPath = path
		hubMu.Unlock()
		return nil
	}
	return errors.New("hub connection settings changed during the search; try again")
}

// searchHub walks the enabled methods, spending the dead drop once when every
// method is out of addresses that work.
func searchHub() (*hubPath, error) {
	mu.Lock()
	current := settings
	logs := wgLogs
	mu.Unlock()
	methods := current.HubMethods.normalize()

	try := func(method string) (*hubPath, bool) {
		path := tryHubMethod(method, logs)
		return path, path != nil
	}
	var reseed func() bool
	if current.DeadDrop {
		reseed = func() bool { return reseedFromDeadDrop(logs) }
	}
	path, ok := searchHubMethods(methods.enabled(), try, reseed)
	if !ok {
		// Fail closed: falling back to the domain would leak the SNI the user
		// switched that method off to avoid.
		if !methods.Normal {
			return nil, errors.New("hub unreachable: every enabled connection method failed, and the normal (cleartext domain) method is switched off")
		}
		return nil, errors.New("hub unreachable: every connection method failed")
	}
	logAdd(logs, state.LogInfo, fmt.Sprintf("reached the hub over %s (%s)", path.kind, path.detail))
	return path, nil
}

func tryHubMethod(method string, logs *state.LogStore) *hubPath {
	switch method {
	case "directIp":
		return tryDirectIPPaths()
	case "reality":
		return tryRealityPath(logs)
	case "shadowsocks":
		return tryShadowsocksPath(logs)
	case "fronted":
		return tryFrontedPath()
	case "normal":
		return tryNormalPaths()
	}
	return nil
}

// tryDirectIPPaths tries the last known good IP first, since it needs no
// lookup at all, then a DoH-resolved one.
func tryDirectIPPaths() *hubPath {
	cached := storedValue(keyHubIP)
	if isIPv4Literal(cached) {
		if path := newDirectIPPath(cached); path.probe() {
			return path
		}
	}
	ip, err := resolveViaDoH(hubHost)
	if err != nil || ip == cached {
		return nil
	}
	path := newDirectIPPath(ip)
	if !path.probe() {
		return nil
	}
	setStoredValue(keyHubIP, ip)
	return path
}

// tryRealityPath walks every cached node: one whose user has rotated must not
// end the search.
func tryRealityPath(logs *state.LogStore) *hubPath {
	cached := loadHubReality()
	for index, creds := range cached {
		manager := reality.NewProxyManager(logs)
		ctx, cancel := context.WithTimeout(context.Background(), hubProxyStartTimeout)
		port, err := manager.Start(ctx, state.RealityProfile{
			RemoteHost: creds.RemoteHost,
			RemotePort: creds.RemotePort,
			UUID:       creds.UUID,
			PublicKey:  creds.PublicKey,
			ShortID:    creds.ShortID,
			ServerName: creds.ServerName,
		})
		cancel()
		if path := adoptProxyPath("reality", creds.RemoteHost, manager, port, err, logs); path != nil {
			if promoted := promoteEntry(cached, index); promoted != nil {
				saveHubReality(promoted)
			}
			return path
		}
	}
	return nil
}

func tryShadowsocksPath(logs *state.LogStore) *hubPath {
	cached := loadHubShadowsocks()
	for index, creds := range cached {
		manager := shadowsocks.NewProxyManager(logs)
		ctx, cancel := context.WithTimeout(context.Background(), hubProxyStartTimeout)
		port, err := manager.Start(ctx, state.ShadowsocksProfile{
			RemoteHost: creds.RemoteHost,
			RemotePort: creds.RemotePort,
			Method:     creds.Method,
			Password:   creds.Password,
		})
		cancel()
		if path := adoptProxyPath("shadowsocks", creds.RemoteHost, manager, port, err, logs); path != nil {
			if promoted := promoteCreds(cached, index); promoted != nil {
				saveHubShadowsocks(promoted)
			}
			return path
		}
	}
	return nil
}

// adoptProxyPath probes through a freshly started proxy, keeping it as the
// live route on success and stopping it otherwise.
func adoptProxyPath(kind, node string, manager hubProxy, port int, startErr error, logs *state.LogStore) *hubPath {
	if startErr != nil || port == 0 {
		logAdd(logs, state.LogWarn, fmt.Sprintf("%s hub node %s did not start: %v", kind, node, startErr))
		_ = manager.Stop(context.Background())
		return nil
	}
	username, password := manager.Credentials()
	path := newProxyPath(kind, node, port, username, password)
	path.proxy = manager
	if !path.probe() {
		logAdd(logs, state.LogWarn, fmt.Sprintf("%s hub node %s did not reach the hub", kind, node))
		return nil
	}
	return path
}

func tryFrontedPath() *hubPath {
	cached := loadFrontedEndpoints()
	for index, host := range cached {
		path := newFrontedPath(host)
		if path.probe() {
			if promoted := promoteFrontedEndpoint(cached, index); promoted != nil {
				saveFrontedEndpoints(promoted)
			}
			return path
		}
	}
	return nil
}

func tryNormalPaths() *hubPath {
	for _, host := range normalHubHosts() {
		if path := newNormalPath(host); path.probe() {
			return path
		}
	}
	return nil
}

// invalidateHubPath is a deliberate change of plan: drop the route, discard
// any search still running, and lift the failure cooldown.
func invalidateHubPath() {
	hubMu.Lock()
	path := activeHubPath
	activeHubPath = nil
	hubTrack.invalidate()
	hubMu.Unlock()
	if path != nil {
		path.close()
	}
}

// hubRequestFailed drops route after repeated failures, but only while it is
// still the active one: a stale error must not tear down a newer route.
func hubRequestFailed(route *hubPath) {
	hubMu.Lock()
	if activeHubPath != route || !hubTrack.requestFailed() {
		hubMu.Unlock()
		return
	}
	activeHubPath = nil
	hubMu.Unlock()
	route.close()
}

func hubRequestSucceeded() {
	hubMu.Lock()
	hubTrack.requestSucceeded()
	hubMu.Unlock()
}

func logAdd(logs *state.LogStore, level state.LogLevel, message string) {
	if logs != nil {
		logs.Add(level, state.SourceDaemon, message)
	}
}

func storedValue(key string) string {
	mu.Lock()
	s := store
	mu.Unlock()
	if s == nil {
		return ""
	}
	return s.Get(key)
}

func setStoredValue(key, value string) {
	mu.Lock()
	s := store
	mu.Unlock()
	if s != nil {
		s.Set(key, value)
	}
}

func loadStoredJSON(key string, out any) {
	if raw := storedValue(key); raw != "" {
		_ = json.Unmarshal([]byte(raw), out)
	}
}

func saveStoredJSON(key string, value any) {
	if b, err := json.Marshal(value); err == nil {
		setStoredValue(key, string(b))
	}
}

func loadFrontedEndpoints() []string {
	var stored []string
	loadStoredJSON(keyFronted, &stored)
	return seedFrontedEndpoints(stored)
}

func saveFrontedEndpoints(list []string) { saveStoredJSON(keyFronted, list) }

func loadHubShadowsocks() []hubShadowsocksCreds {
	var stored []hubShadowsocksCreds
	loadStoredJSON(keyHubSS, &stored)
	return seedHubShadowsocks(stored)
}

func saveHubShadowsocks(list []hubShadowsocksCreds) { saveStoredJSON(keyHubSS, list) }

func loadHubReality() []hubRealityCreds {
	var stored []hubRealityCreds
	loadStoredJSON(keyHubReality, &stored)
	return seedHubReality(stored)
}

func saveHubReality(list []hubRealityCreds) { saveStoredJSON(keyHubReality, list) }

// rememberHubDiscovery caches what a hub response advertised, so a later start
// has more than one way back in.
func rememberHubDiscovery(routes hubRoutes) {
	if merged := mergeFrontedEndpoints(loadFrontedEndpoints(), routes.fronted); merged != nil {
		saveFrontedEndpoints(merged)
	}
	if merged := mergeAdvertisedCreds(loadHubShadowsocks(), routes.shadowsocks); merged != nil {
		saveHubShadowsocks(merged)
	}
	if merged := mergeAdvertisedHubReality(loadHubReality(), routes.reality); merged != nil {
		saveHubReality(merged)
	}
}
