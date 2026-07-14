//go:build android

package mobile

// Control-plane transport: DoH-resolved direct-IP HTTPS with no SNI, then the
// secure-channel envelope from securechannel.go POSTed to /v1/secure. Mirrors
// apps/desktop/src/main/pangeaApiClient.ts with directIpOnly always on and no
// system DNS anywhere (Android's pure-Go resolver is unreliable).

import (
	"bytes"
	"context"
	"crypto/tls"
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

const hubHost = "api.pangeavpn.org"

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
	hubMu      sync.Mutex
	resolvedIP string
	hubHTTP    *http.Client
	dohHTTP    *http.Client
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

type dohAnswer struct {
	Data string `json:"data"`
}

type dohResponse struct {
	Answer []dohAnswer `json:"Answer"`
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
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", false
	}
	for _, a := range data.Answer {
		if a.Data != "" {
			return a.Data, true
		}
	}
	return "", false
}

func resolveViaDoH(hostname string) (string, error) {
	for _, p := range dohProviders {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ip, ok := tryDoHProvider(ctx, p, hostname)
		cancel()
		if ok {
			return ip, nil
		}
	}
	return "", errors.New("DoH resolution failed for all providers")
}

// ensureHub resolves and caches the hub's direct IP for the process lifetime.
func ensureHub() error {
	hubMu.Lock()
	ready := resolvedIP != ""
	hubMu.Unlock()
	if ready {
		return nil
	}

	ip, err := resolveViaDoH(hubHost)
	if err != nil {
		return fmt.Errorf("hub unreachable: %w", err)
	}

	hubMu.Lock()
	resolvedIP = ip
	hubHTTP = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:     protectedDialer(15 * time.Second).DialContext,
			TLSClientConfig: &tls.Config{ServerName: "", InsecureSkipVerify: true},
		},
	}
	hubMu.Unlock()
	return nil
}

// hubFetch encrypts one request through the secure channel and returns the
// decrypted inner response body and status.
func hubFetch(path, method string, headers map[string]string, body []byte) ([]byte, int, error) {
	if err := ensureHub(); err != nil {
		return nil, 0, err
	}

	env, aesKey, err := encryptRequest(method, path, headers, body)
	if err != nil {
		return nil, 0, fmt.Errorf("encrypt request: %w", err)
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return nil, 0, err
	}

	hubMu.Lock()
	ip := resolvedIP
	client := hubHTTP
	hubMu.Unlock()

	req, err := http.NewRequest(http.MethodPost, "https://"+ip+"/v1/secure", bytes.NewReader(envJSON))
	if err != nil {
		return nil, 0, err
	}
	req.Host = hubHost
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		hubMu.Lock()
		resolvedIP = ""
		hubMu.Unlock()
		return nil, 0, fmt.Errorf("hub request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read hub response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("secure channel error (%d): %s", resp.StatusCode, string(respBytes))
	}

	var encResp encryptedResponse
	if err := json.Unmarshal(respBytes, &encResp); err != nil {
		return nil, 0, fmt.Errorf("decode secure response: %w", err)
	}
	inner, err := decryptResponse(aesKey, encResp)
	if err != nil {
		return nil, 0, err
	}
	return inner.Body, inner.Status, nil
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
