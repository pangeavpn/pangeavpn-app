package mobile

import (
	"encoding/json"
	"testing"
)

// hubServerJSON is a hub /api/servers entry carrying every transport, in the
// wire shape ServerInfo declares in apps/desktop/src/shared/ipc.ts.
const hubServerJSON = `{
  "id": "lon-1",
  "name": "London",
  "region": "eu-west",
  "country": "GB",
  "load": 42,
  "cloak": {"remoteHost": "203.0.113.10", "uid": "uid-1", "publicKey": "ck-pub", "serverName": "www.example.com"},
  "naive": {"remoteHost": "naive.example.net", "remoteIp": "203.0.113.15", "remotePort": 443,
            "username": "nv-user", "password": "nv-pass"},
  "reality": {"remoteHost": "lon1.example.net", "remoteIp": "203.0.113.11", "remotePort": 8443,
              "uuid": "uuid-1", "publicKey": "re-pub", "shortId": "abcd", "flow": "xtls-rprx-vision",
              "serverName": "www.cloudflare.com"},
  "hysteria2": {"remoteHost": "lon1.example.net", "remoteIp": "203.0.113.12", "remotePort": 8444,
                "password": "h2-pass", "obfsPassword": "h2-obfs", "pinSha256": "pin=="},
  "shadowsocks": {"remoteHost": "lon1.example.net", "remoteIp": "203.0.113.13", "remotePort": 8388,
                  "method": "2022-blake3-aes-128-gcm", "password": "ss-pass",
                  "targetHost": "127.0.0.1", "targetPort": 51820, "udpOverTcp": true},
  "controlPlaneShadowsocks": {"remoteHost": "203.0.113.14", "remotePort": 8389,
                              "method": "2022-blake3-aes-128-gcm", "password": "cp-pass"},
  "snowflake": {"brokerURL": "https://broker.example", "bridgeFingerprint": "fp-1",
                "frontDomains": ["cdn.example"], "ampCacheURL": "https://amp.example",
                "iceServers": ["stun:stun.example:3478"]}
}`

func decodeServer(t *testing.T, raw string) *serverInfo {
	t.Helper()
	var server serverInfo
	if err := json.Unmarshal([]byte(raw), &server); err != nil {
		t.Fatalf("decode server: %v", err)
	}
	return &server
}

func TestBuildProfileMapsEveryTransport(t *testing.T) {
	profile := buildProfile(decodeServer(t, hubServerJSON))

	if profile.ID != "auto-lon-1" || profile.Name != "London" {
		t.Fatalf("got id=%q name=%q", profile.ID, profile.Name)
	}
	if profile.Cloak.RemoteHost != "203.0.113.10" || profile.Cloak.RemotePort != 443 {
		t.Fatalf("cloak endpoint wrong: %+v", profile.Cloak)
	}
	if profile.Cloak.EncryptionMethod != "plain" {
		t.Fatalf("cloak encryption %q, want plain", profile.Cloak.EncryptionMethod)
	}

	if profile.Naive == nil {
		t.Fatal("naive profile missing")
	}
	if profile.Naive.RemoteHost != "203.0.113.15" {
		t.Fatalf("naive should dial the IP, got %q", profile.Naive.RemoteHost)
	}
	if profile.Naive.ServerName != "naive.example.net" {
		t.Fatalf("naive SNI should stay the domain, got %q", profile.Naive.ServerName)
	}
	if profile.Naive.Username != "nv-user" || profile.Naive.Password != "nv-pass" {
		t.Fatalf("naive credentials wrong: %+v", profile.Naive)
	}

	if profile.Reality == nil {
		t.Fatal("reality profile missing")
	}
	if profile.Reality.RemoteHost != "203.0.113.11" {
		t.Fatalf("reality should dial the IP, got %q", profile.Reality.RemoteHost)
	}
	if profile.Reality.ServerName != "www.cloudflare.com" || profile.Reality.ShortID != "abcd" {
		t.Fatalf("reality fields wrong: %+v", profile.Reality)
	}

	if profile.Hysteria2 == nil {
		t.Fatal("hysteria2 profile missing")
	}
	if profile.Hysteria2.RemoteHost != "203.0.113.12" {
		t.Fatalf("hysteria2 should dial the IP, got %q", profile.Hysteria2.RemoteHost)
	}
	if profile.Hysteria2.PinSHA256 != "pin==" {
		t.Fatalf("hysteria2 pin lost: %+v", profile.Hysteria2)
	}

	if profile.Shadowsocks == nil {
		t.Fatal("shadowsocks profile missing")
	}
	if profile.Shadowsocks.RemoteHost != "203.0.113.13" {
		t.Fatalf("shadowsocks should dial the IP, got %q", profile.Shadowsocks.RemoteHost)
	}
	if profile.Shadowsocks.Method != "2022-blake3-aes-128-gcm" || !profile.Shadowsocks.UDPOverTCP {
		t.Fatalf("shadowsocks fields wrong: %+v", profile.Shadowsocks)
	}

	if profile.Snowflake == nil {
		t.Fatal("snowflake profile missing")
	}
	if profile.Snowflake.BrokerURL != "https://broker.example" || profile.Snowflake.BridgeFingerprint != "fp-1" {
		t.Fatalf("snowflake fields wrong: %+v", profile.Snowflake)
	}
}

// A cloak-only node is the common case and must not fabricate other blocks;
// nil is what makes StarterFor skip them.
func TestBuildProfileLeavesAbsentTransportsNil(t *testing.T) {
	raw := `{"id":"a","name":"A","region":"r","country":"C",
	         "cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"}}`
	profile := buildProfile(decodeServer(t, raw))

	if profile.Reality != nil || profile.Hysteria2 != nil ||
		profile.Shadowsocks != nil || profile.Snowflake != nil || profile.Naive != nil {
		t.Fatalf("absent transports should stay nil, got %+v", profile)
	}
}

// Ports the cases in apps/desktop/src/shared/naiveEndpoint.ts: the engine is
// handed an address to dial plus the name to present, so it never resolves.
func TestBuildNaiveEndpointSplit(t *testing.T) {
	cases := []struct {
		name       string
		info       naiveInfo
		nodeIP     string
		wantHost   string
		wantServer string
	}{
		{
			name:       "per-transport ip wins",
			info:       naiveInfo{RemoteHost: "naive.example.net", RemoteIP: "203.0.113.15"},
			nodeIP:     "198.51.100.5",
			wantHost:   "203.0.113.15",
			wantServer: "naive.example.net",
		},
		{
			name:       "literal remoteHost is dialled as given",
			info:       naiveInfo{RemoteHost: "203.0.113.20"},
			nodeIP:     "198.51.100.5",
			wantHost:   "203.0.113.20",
			wantServer: "203.0.113.20",
		},
		{
			name:       "domain falls back to the node",
			info:       naiveInfo{RemoteHost: "naive.example.net"},
			nodeIP:     "198.51.100.5",
			wantHost:   "198.51.100.5",
			wantServer: "naive.example.net",
		},
		{
			name:       "explicit serverName overrides the host",
			info:       naiveInfo{RemoteHost: "naive.example.net", ServerName: "www.example.com"},
			nodeIP:     "198.51.100.5",
			wantHost:   "198.51.100.5",
			wantServer: "www.example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildNaive(&tc.info, tc.nodeIP)
			if got.RemoteHost != tc.wantHost {
				t.Errorf("remoteHost %q, want %q", got.RemoteHost, tc.wantHost)
			}
			if got.ServerName != tc.wantServer {
				t.Errorf("serverName %q, want %q", got.ServerName, tc.wantServer)
			}
		})
	}
}

// Without a per-transport remoteIp the node address stands in, so a domain is
// still never resolved client-side.
func TestBuildProfileFallsBackToNodeIP(t *testing.T) {
	raw := `{"id":"a","name":"A","region":"r","country":"C",
	         "cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"},
	         "reality":{"remoteHost":"node.example","remotePort":443,"uuid":"x","publicKey":"y","shortId":"z"},
	         "shadowsocks":{"remoteHost":"node.example","remotePort":8388,"method":"m","password":"pw"}}`
	profile := buildProfile(decodeServer(t, raw))

	if profile.Reality.RemoteHost != "198.51.100.5" {
		t.Fatalf("reality host %q, want node IP", profile.Reality.RemoteHost)
	}
	if profile.Shadowsocks.RemoteHost != "198.51.100.5" {
		t.Fatalf("shadowsocks host %q, want node IP", profile.Shadowsocks.RemoteHost)
	}
}

// Hysteria2 verifies its cert against serverName, so an absent one falls back
// to the domain rather than the IP it dials.
func TestBuildProfileHysteria2ServerNameFallback(t *testing.T) {
	raw := `{"id":"a","name":"A","region":"r","country":"C",
	         "cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"},
	         "hysteria2":{"remoteHost":"node.example","remoteIp":"198.51.100.9","remotePort":8444,
	                      "password":"pw","obfsPassword":"ob"}}`
	profile := buildProfile(decodeServer(t, raw))

	if profile.Hysteria2.ServerName != "node.example" {
		t.Fatalf("serverName %q, want the node domain", profile.Hysteria2.ServerName)
	}
	if profile.Hysteria2.RemoteHost != "198.51.100.9" {
		t.Fatalf("remoteHost %q, want the IP", profile.Hysteria2.RemoteHost)
	}
}

