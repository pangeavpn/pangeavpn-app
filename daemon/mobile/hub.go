//go:build android

package mobile

// Control plane: requests sealed by securechannel.go, sent over hubpath.go's
// route. Mirrors apps/desktop/src/main/pangeaApiClient.ts.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// errSubscriptionExpired is distinct so the host shows a top-up prompt rather
// than treating it as a failed sign-in.
var errSubscriptionExpired = errors.New("SUBSCRIPTION_EXPIRED: this account's subscription has expired")

type dohProvider struct {
	url    string
	accept string
}

var dohProviders = []dohProvider{
	{"https://1.1.1.1/dns-query", "application/dns-json"},
	{"https://8.8.8.8/resolve", "application/dns-json"},
	{"https://9.9.9.9:5053/dns-query", "application/dns-json"},
	{"https://94.140.14.14/dns-query", "application/dns-json"},
}

var (
	hubMu sync.Mutex
	// activeHubPath is the route that last proved itself; nil forces a
	// rediscovery on the next request.
	activeHubPath *hubPath
	hubTrack      hubTracker
	dohHTTP       *http.Client
)

func protectedDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: protectingControl}
}

func getDohClient() *http.Client {
	hubMu.Lock()
	defer hubMu.Unlock()
	if dohHTTP == nil {
		dohHTTP = &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{DialContext: protectedDialer(5 * time.Second).DialContext},
		}
	}
	return dohHTTP
}

func tryDoHProvider(ctx context.Context, p dohProvider, hostname string) (string, bool) {
	sep := "?"
	if strings.Contains(p.url, "?") {
		sep = "&"
	}
	reqURL := fmt.Sprintf("%s%sname=%s&type=A", p.url, sep, hostname)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", p.accept)

	resp, err := getDohClient().Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var data dohResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDoHResponseBytes)).Decode(&data); err != nil {
		return "", false
	}
	return pickDoHAddress(data.Answer)
}

func resolveViaDoH(hostname string) (string, error) {
	ip, ok := raceStaggered(len(dohProviders), dohStagger, func(ctx context.Context, index int) (string, bool) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return tryDoHProvider(ctx, dohProviders[index], hostname)
	})
	if !ok {
		return "", errors.New("DoH resolution failed for all providers")
	}
	return ip, nil
}

// ensureHub finds a working route to the hub across every enabled method.
func ensureHub() error {
	return ensureHubPath()
}

// hubFetch encrypts one request through the secure channel and returns the
// decrypted inner response body and status.
func hubFetch(path, method string, headers map[string]string, body []byte) ([]byte, int, error) {
	// Twice: a route dropped between ensureHub and the read is not a failure.
	for attempt := 0; attempt < 2; attempt++ {
		if err := ensureHub(); err != nil {
			return nil, 0, err
		}
		hubMu.Lock()
		route := activeHubPath
		hubMu.Unlock()
		if route == nil {
			continue
		}

		sealed, err := sealRequest(method, path, headers, body)
		if err != nil {
			return nil, 0, fmt.Errorf("seal request: %w", err)
		}
		inner, err := sendOnRoute(route, sealed)
		if err != nil {
			hubRequestFailed(route)
			return nil, 0, err
		}
		hubRequestSucceeded()
		return inner.Body, inner.Status, nil
	}
	return nil, 0, errors.New("hub unreachable: no working connection method")
}

func sendOnRoute(route *hubPath, sealed sealedRequest) (innerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hubRequestTimeout)
	defer cancel()
	respBytes, status, err := route.postEnvelope(ctx, sealed.route, sealed.envelope)
	if err != nil {
		return innerResponse{}, fmt.Errorf("hub request failed: %w", err)
	}
	return openHubReply(sealed, respBytes, status)
}

// hubRequest wraps hubFetch with the X-License-Key header and JSON
// encode/decode of the request/response bodies.
func hubRequest(method, route string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	headers := map[string]string{"Content-Type": "application/json"}
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	if lk != "" {
		headers["X-License-Key"] = lk
	}

	respBody, status, err := hubFetch(route, method, headers, bodyBytes)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		text := string(respBody)
		// Checked before the auth branch: a lapsed subscription is also a 403,
		// but signing the user out would discard the token they still need.
		if strings.Contains(text, "SUBSCRIPTION_EXPIRED") {
			return errSubscriptionExpired
		}
		if status == 401 || status == 403 || strings.Contains(text, "DEVICE_NOT_REGISTERED") {
			return fmt.Errorf("hub auth error (%d): %s", status, text)
		}
		return fmt.Errorf("hub error (%d): %s", status, text)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode hub response: %w", err)
		}
	}
	return nil
}

type tokenLoginResponse struct {
	VpnAccessToken string `json:"vpnAccessToken"`
	User           struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Servers []serverInfo `json:"servers"`
	// FrontedEndpoints is absent on hubs that predate edge relays.
	FrontedEndpoints []string `json:"frontedEndpoints"`
}

// captureHubDiscovery caches the relays and control-plane credentials a
// response advertised, so a later start has more than one way back in.
func captureHubDiscovery(fronted []string, servers []serverInfo) {
	routes := discoverHubRoutes(servers)
	if len(fronted) > 0 {
		routes.fronted = fronted
	}
	rememberHubDiscovery(routes)
}

func tokenLogin(token, identityPub string) (*tokenLoginResponse, error) {
	reqBody := map[string]string{"vpnAccessToken": strings.TrimSpace(token)}
	if identityPub != "" {
		reqBody["identityPubkey"] = identityPub
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	respBody, status, err := hubFetch("/api/client/token-login", http.MethodPost,
		map[string]string{"Content-Type": "application/json"}, bodyBytes)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("token login failed (%d): %s", status, string(respBody))
	}

	var out tokenLoginResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode token-login response: %w", err)
	}
	captureHubDiscovery(out.FrontedEndpoints, out.Servers)
	return &out, nil
}

type deviceRegisterResponse struct {
	DeviceID     string  `json:"deviceId"`
	AssignedIP   string  `json:"assignedIp"`
	FriendlyName *string `json:"friendlyName"`
}

func registerDevice(identityPub, friendlyName string) (*deviceRegisterResponse, error) {
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	body := map[string]string{"licenseKey": lk, "identityPubkey": identityPub}
	if friendlyName != "" {
		body["friendlyName"] = friendlyName
	}
	var out deviceRegisterResponse
	if err := hubRequest(http.MethodPost, "/api/device/register", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func fetchServers(identityPub string) ([]serverInfo, error) {
	route := "/api/client/regions"
	if identityPub != "" {
		route += "?identityPubkey=" + url.QueryEscape(identityPub)
	}
	var out []serverInfo
	if err := hubRequest(http.MethodGet, route, nil, &out); err != nil {
		return nil, err
	}
	captureHubDiscovery(nil, out)
	return out, nil
}

func listDevicesHub() ([]deviceInfo, error) {
	var out struct {
		Devices []deviceInfo `json:"devices"`
	}
	if err := hubRequest(http.MethodGet, "/api/device/list", nil, &out); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func removeDeviceHub(deviceID string) error {
	return hubRequest(http.MethodPost, "/api/device/remove", map[string]string{"deviceId": deviceID}, nil)
}

func getSubscriptionHub() (*subscriptionInfo, error) {
	var out struct {
		Subscription *subscriptionInfo `json:"subscription"`
	}
	if err := hubRequest(http.MethodGet, "/api/client/subscription", nil, &out); err != nil {
		return nil, err
	}
	return out.Subscription, nil
}

type registerResponse struct {
	ServerPubkey string `json:"serverPubkey"`
	AssignedIP   string `json:"assignedIP"`
	DNS          string `json:"dns"`
}

func registerServer(identityPub, wgPub, region string) (*registerResponse, error) {
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	body := map[string]string{
		"licenseKey":     lk,
		"identityPubkey": identityPub,
		"wgPubkey":       wgPub,
		"region":         region,
	}
	var out registerResponse
	if err := hubRequest(http.MethodPost, "/api/register", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
