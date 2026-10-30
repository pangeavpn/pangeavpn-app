//go:build android || ios

package mobile

import (
	"net/http"
	"net/url"
	"testing"
)

func TestDirectIPPathBracketsAnIPv6Address(t *testing.T) {
	tests := map[string]string{
		"203.0.113.30": "203.0.113.30",
		"2001:db8::30": "[2001:db8::30]",
	}
	for ip, wantHost := range tests {
		path := newDirectIPPath(ip)
		parsed, err := url.Parse(path.base + probeRoute)
		if err != nil {
			t.Fatalf("%s: parse %q: %v", ip, path.base+probeRoute, err)
		}
		if parsed.Host != wantHost || parsed.Hostname() != ip {
			t.Errorf("%s: URL host %q (hostname %q), want %q", ip, parsed.Host, parsed.Hostname(), wantHost)
		}
		if path.detail != ip || path.host != hubHost {
			t.Errorf("%s: detail=%q host=%q, want the address logged and the hub named", ip, path.detail, path.host)
		}
	}
}

func TestProxyPathPointsAtTheLoopbackProxy(t *testing.T) {
	path := newProxyPath("reality", "203.0.113.20", 1080, "user", "secret")
	transport, ok := path.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", path.client.Transport)
	}
	req, err := http.NewRequest(http.MethodPost, path.base+probeRoute, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	proxy, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if proxy.Host != "127.0.0.1:1080" || proxy.User.Username() != "user" {
		t.Fatalf("proxy = %v, want user@127.0.0.1:1080", proxy)
	}
}
