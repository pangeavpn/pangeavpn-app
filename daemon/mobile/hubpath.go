//go:build android

package mobile

// The ways the envelope reaches the hub. Ports ensureHub in
// apps/desktop/src/main/pangeaApiClient.ts.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	keyHubIP       = "hubIp"
	keyFronted     = "frontedEndpoints"
	keyHubSS       = "hubShadowsocks"
	probeRoute     = "/api/client/regions"
	hubProbeTimout = 8 * time.Second
)

// hubPath is one resolved way to POST the envelope.
type hubPath struct {
	kind   string
	url    string
	host   string
	client *http.Client
}

// newDirectIPPath dials the address with no SNI. The envelope is sealed end to
// end, so the transport certificate carries no trust here.
func newDirectIPPath(ip string) *hubPath {
	return &hubPath{
		kind: "directIp",
		url:  "https://" + ip + "/v1/secure",
		host: hubHost,
		client: &http.Client{
			Timeout: hubProbeTimout,
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
	return &hubPath{
		kind: "fronted",
		url:  "https://" + host + "/v1/secure",
		host: host,
		client: &http.Client{
			Timeout:   hubProbeTimout,
			Transport: &http.Transport{DialContext: protectedDialer(hubProbeTimout).DialContext},
		},
	}
}

// newNormalPath is the only path that puts the hub's name on the wire.
func newNormalPath() *hubPath {
	return &hubPath{
		kind: "normal",
		url:  "https://" + hubHost + "/v1/secure",
		host: hubHost,
		client: &http.Client{
			Timeout:   hubProbeTimout,
			Transport: &http.Transport{DialContext: protectedDialer(hubProbeTimout).DialContext},
		},
	}
}

// newShadowsocksPath routes through the local mixed inbound, which answers
// HTTP CONNECT.
func newShadowsocksPath(port int) *hubPath {
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	return &hubPath{
		kind: "shadowsocks",
		url:  "https://" + hubHost + "/v1/secure",
		host: hubHost,
		client: &http.Client{
			Timeout: hubProbeTimout,
			Transport: &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				DialContext:     protectedDialer(hubProbeTimout).DialContext,
				TLSClientConfig: &tls.Config{ServerName: hubHost},
			},
		},
	}
}

// postEnvelope sends one sealed envelope over this path.
func (p *hubPath) postEnvelope(envJSON []byte) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodPost, p.url, bytes.NewReader(envJSON))
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}

// probe proves a path end to end by round-tripping a real secure request.
// Never returns an error so ensureHub can fall through to the next method.
func (p *hubPath) probe() bool {
	env, aesKey, err := encryptRequest(http.MethodGet, probeRoute, map[string]string{}, nil)
	if err != nil {
		return false
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return false
	}
	body, status, err := p.postEnvelope(envJSON)
	if err != nil || status < 200 || status >= 300 {
		return false
	}
	var encResp encryptedResponse
	if err := json.Unmarshal(body, &encResp); err != nil {
		return false
	}
	_, err = decryptResponse(aesKey, encResp)
	return err == nil
}

// ensureHubPath finds a working way to reach the hub, trying each enabled
// method in order and caching whatever wins for the process lifetime.
func ensureHubPath() error {
	hubMu.Lock()
	ready := activeHubPath != nil
	hubMu.Unlock()
	if ready {
		return nil
	}

	mu.Lock()
	methods := settings.HubMethods.normalize()
	logs := wgLogs
	mu.Unlock()

	for _, method := range methods.enabled() {
		var path *hubPath
		switch method {
		case "directIp":
			path = tryDirectIPPaths()
		case "shadowsocks":
			path = tryShadowsocksPath()
		case "fronted":
			path = tryFrontedPath()
		case "normal":
			path = tryNormalPath()
		}
		if path == nil {
			continue
		}
		if logs != nil {
			logs.Add(state.LogInfo, state.SourceDaemon, "reached the hub over "+path.kind)
		}
		hubMu.Lock()
		activeHubPath = path
		hubMu.Unlock()
		return nil
	}

	// Fail closed: falling back to the domain would leak the SNI the user
	// switched that method off to avoid.
	if !methods.Normal {
		return fmt.Errorf("hub unreachable: every enabled connection method failed, and the normal (cleartext domain) method is switched off")
	}
	return fmt.Errorf("hub unreachable: every connection method failed")
}

// tryDirectIPPaths tries the last known good IP first, since it needs no
// lookup at all, then a DoH-resolved one.
func tryDirectIPPaths() *hubPath {
	if cached := storedValue(keyHubIP); cached != "" {
		if path := newDirectIPPath(cached); path.probe() {
			return path
		}
	}
	ip, err := resolveViaDoH(hubHost)
	if err != nil {
		return nil
	}
	path := newDirectIPPath(ip)
	if !path.probe() {
		return nil
	}
	setStoredValue(keyHubIP, ip)
	return path
}

// tryShadowsocksPath walks every cached node: one whose key has rotated must
// not end the search.
func tryShadowsocksPath() *hubPath {
	cached := loadHubShadowsocks()
	if len(cached) == 0 {
		return nil
	}
	mu.Lock()
	logs := wgLogs
	mu.Unlock()

	for index, creds := range cached {
		manager := shadowsocks.NewProxyManager(logs)
		port, err := manager.Start(context.Background(), state.ShadowsocksProfile{
			RemoteHost: creds.RemoteHost,
			RemotePort: creds.RemotePort,
			Method:     creds.Method,
			Password:   creds.Password,
		})
		if err != nil || port == 0 {
			continue
		}
		path := newShadowsocksPath(port)
		if path.probe() {
			hubMu.Lock()
			hubSSProxy = manager
			hubMu.Unlock()
			if promoted := promoteCreds(cached, index); promoted != nil {
				saveHubShadowsocks(promoted)
			}
			return path
		}
		_ = manager.Stop(context.Background())
	}
	return nil
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

func tryNormalPath() *hubPath {
	path := newNormalPath()
	if !path.probe() {
		return nil
	}
	return path
}

// resetHubPath forces the next request to rediscover a route.
func resetHubPath() {
	hubMu.Lock()
	proxy := hubSSProxy
	activeHubPath = nil
	hubSSProxy = nil
	hubMu.Unlock()
	if proxy != nil {
		_ = proxy.Stop(context.Background())
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

func loadFrontedEndpoints() []string {
	var stored []string
	if raw := storedValue(keyFronted); raw != "" {
		_ = json.Unmarshal([]byte(raw), &stored)
	}
	return restoreFrontedEndpoints(stored)
}

func saveFrontedEndpoints(list []string) {
	if b, err := json.Marshal(list); err == nil {
		setStoredValue(keyFronted, string(b))
	}
}

func loadHubShadowsocks() []hubShadowsocksCreds {
	var stored []hubShadowsocksCreds
	if raw := storedValue(keyHubSS); raw != "" {
		_ = json.Unmarshal([]byte(raw), &stored)
	}
	return restoreCachedCreds(stored)
}

func saveHubShadowsocks(list []hubShadowsocksCreds) {
	if b, err := json.Marshal(list); err == nil {
		setStoredValue(keyHubSS, string(b))
	}
}

// rememberHubDiscovery caches what a hub response advertised, so a later start
// has more than one way back in.
func rememberHubDiscovery(fronted []string, creds []hubShadowsocksCreds) {
	if merged := mergeFrontedEndpoints(loadFrontedEndpoints(), fronted); merged != nil {
		saveFrontedEndpoints(merged)
	}
	if merged := mergeAdvertisedCreds(loadHubShadowsocks(), creds); merged != nil {
		saveHubShadowsocks(merged)
	}
}
