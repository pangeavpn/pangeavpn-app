//go:build android || ios

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

// hubFetch encrypts one request through the secure channel and returns the
// decrypted inner response body and status.
func hubFetch(path, method string, headers map[string]string, body []byte) ([]byte, int, error) {
	return hubFetchSince(currentWipeEpoch(), path, method, headers, body)
}

// hubFetchSince drops the answer, undecoded, once a Wipe has run since wipe.
func hubFetchSince(wipe uint64, path, method string, headers map[string]string, body []byte) ([]byte, int, error) {
	// Twice: a route dropped between ensureHubPath and the read is not a failure.
	for attempt := 0; attempt < 2; attempt++ {
		if err := ensureHubPath(wipe); err != nil {
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
		if currentWipeEpoch() != wipe {
			return nil, 0, errSessionEnded
		}
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

// hubRequest is hubRequestAs for the session signed in right now.
func hubRequest(method, route string, body any, out any) error {
	return hubRequestAs(captureScope(), method, route, body, out)
}

// hubRequestAs wraps hubFetch with the scope's X-License-Key header and JSON
// encode/decode, dropping the answer undecoded once the scope's session ends.
func hubRequestAs(scope callScope, method, route string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if scope.licenseKey != "" {
		headers["X-License-Key"] = scope.licenseKey
	}

	respBody, status, err := hubFetchSince(scope.wipe, route, method, headers, bodyBytes)
	if err != nil {
		return err
	}
	// Before the status: a refusal of an ended session must not sign out its successor.
	if scope.sessionEnded() {
		return errSessionEnded
	}
	if status < 200 || status >= 300 {
		return hubStatusError(status, respBody)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return undecodableAnswer(err)
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
func captureHubDiscovery(wipe uint64, fronted []string, servers []serverInfo) {
	routes := discoverHubRoutes(servers)
	if len(fronted) > 0 {
		routes.fronted = fronted
	}
	rememberHubDiscovery(wipe, routes)
}

func tokenLogin(wipe uint64, token, identityPub string) (*tokenLoginResponse, error) {
	reqBody := map[string]string{"vpnAccessToken": strings.TrimSpace(token)}
	if identityPub != "" {
		reqBody["identityPubkey"] = identityPub
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	respBody, status, err := hubFetchSince(wipe, "/api/client/token-login", http.MethodPost,
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
	captureHubDiscovery(wipe, out.FrontedEndpoints, out.Servers)
	return &out, nil
}

type deviceRegisterResponse struct {
	DeviceID     string  `json:"deviceId"`
	AssignedIP   string  `json:"assignedIp"`
	FriendlyName *string `json:"friendlyName"`
}

func registerDevice(scope callScope, identityPub, friendlyName string) (*deviceRegisterResponse, error) {
	body := map[string]string{"licenseKey": scope.licenseKey, "identityPubkey": identityPub}
	if friendlyName != "" {
		body["friendlyName"] = friendlyName
	}
	var out deviceRegisterResponse
	if err := hubRequestAs(scope, http.MethodPost, "/api/device/register", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func fetchServers(scope callScope, identityPub string) ([]serverInfo, error) {
	route := "/api/client/regions"
	if identityPub != "" {
		route += "?identityPubkey=" + url.QueryEscape(identityPub)
	}
	var out []serverInfo
	if err := hubRequestAs(scope, http.MethodGet, route, nil, &out); err != nil {
		return nil, err
	}
	captureHubDiscovery(scope.wipe, nil, out)
	rememberServers(scope, out, identityPub)
	return out, nil
}

// rememberServers keeps the last good list for a launch the hub cannot answer.
func rememberServers(scope callScope, servers []serverInfo, identityPub string) {
	if scope.licenseKey == "" {
		return
	}
	if raw, ok := encodeServerCache(servers, cacheOwner(scope.licenseKey, identityPub)); ok {
		storeForSession(scope, keyServers, raw)
	}
}

func listDevicesHub(scope callScope) ([]hubDevice, error) {
	var out struct {
		Devices []hubDevice `json:"devices"`
	}
	if err := hubRequestAs(scope, http.MethodGet, "/api/device/list", nil, &out); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func removeDeviceHub(scope callScope, deviceID string) error {
	return hubRequestAs(scope, http.MethodPost, "/api/device/remove", map[string]string{"deviceId": deviceID}, nil)
}

// renameDeviceHub returns the name the hub stored, nil when it did not say.
func renameDeviceHub(scope callScope, deviceID, name string) (*string, error) {
	var out struct {
		FriendlyName *string `json:"friendlyName"`
	}
	body := map[string]string{"deviceId": deviceID, "friendlyName": name}
	if err := hubRequestAs(scope, http.MethodPost, "/api/device/rename", body, &out); err != nil {
		return nil, err
	}
	return out.FriendlyName, nil
}

// deregisterDeviceHub sends the license in the body: this route never reads
// the X-License-Key header.
func deregisterDeviceHub(scope callScope, identityPub string) error {
	body := map[string]string{"licenseKey": scope.licenseKey, "identityPubkey": identityPub}
	return hubRequestAs(scope, http.MethodPost, "/api/device/deregister", body, nil)
}

func getSubscriptionHub(scope callScope) (*hubSubscription, error) {
	var out struct {
		Subscription *hubSubscription `json:"subscription"`
	}
	if err := hubRequestAs(scope, http.MethodGet, "/api/client/subscription", nil, &out); err != nil {
		return nil, err
	}
	return out.Subscription, nil
}
