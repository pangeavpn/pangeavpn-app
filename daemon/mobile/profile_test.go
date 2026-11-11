package mobile

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
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

	if profile.Reality != nil || profile.Hysteria2 != nil || profile.Shadowsocks != nil ||
		profile.AnyTLS != nil || profile.Snowflake != nil || profile.Naive != nil {
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
			name:       "a non-address remoteIp is ignored",
			info:       naiveInfo{RemoteHost: "203.0.113.20", RemoteIP: "naive.example.net"},
			nodeIP:     "198.51.100.5",
			wantHost:   "203.0.113.20",
			wantServer: "203.0.113.20",
		},
		{
			name:       "an IPv6 remoteIp is dialled",
			info:       naiveInfo{RemoteHost: "naive.example.net", RemoteIP: "2001:db8::5"},
			nodeIP:     "198.51.100.5",
			wantHost:   "2001:db8::5",
			wantServer: "naive.example.net",
		},
		{
			name:       "serverName is never blank",
			info:       naiveInfo{RemoteHost: "  "},
			nodeIP:     "198.51.100.5",
			wantHost:   "198.51.100.5",
			wantServer: "198.51.100.5",
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

// The hub's AnyTLS block is pin-only: no insecure flag, no relay target.
func TestBuildProfileMapsAnyTLS(t *testing.T) {
	raw := `{"id":"a","name":"A","region":"r","country":"C",
	         "cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"},
	         "anytls":{"remoteHost":"198.51.100.8","remotePort":8446,"password":"at-pass",
	                   "serverName":"cover.example","pinSha256":"pin=="}}`
	got := buildProfile(decodeServer(t, raw)).AnyTLS
	if got == nil {
		t.Fatal("anytls profile missing")
	}
	want := state.AnyTLSProfile{
		RemoteHost: "198.51.100.8", RemotePort: 8446, Password: "at-pass",
		ServerName: "cover.example", PinSHA256: "pin==",
	}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}

// Same dial-host and SNI rules as apps/desktop/src/shared/anytlsProfile.ts.
func TestBuildProfileAnyTLSHostAndServerName(t *testing.T) {
	tests := []struct {
		name, block, wantHost, wantSNI string
	}{
		{"remoteIp wins", `"remoteHost":"node.example","remoteIp":"198.51.100.9"`, "198.51.100.9", "node.example"},
		{"non-address remoteIp ignored", `"remoteHost":"node.example","remoteIp":"node.example"`, "198.51.100.5", "node.example"},
		{"literal remoteHost dialed, never the SNI", `"remoteHost":"198.51.100.7"`, "198.51.100.7", ""},
		{"explicit serverName kept", `"remoteHost":"node.example","serverName":"cover.example"`, "198.51.100.5", "cover.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"id":"a","name":"A","region":"r","country":"C",
			         "cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"},
			         "anytls":{` + tt.block + `,"remotePort":8446,"password":"pw"}}`
			got := buildProfile(decodeServer(t, raw)).AnyTLS
			if got.RemoteHost != tt.wantHost || got.ServerName != tt.wantSNI {
				t.Fatalf("host=%q sni=%q, want host=%q sni=%q", got.RemoteHost, got.ServerName, tt.wantHost, tt.wantSNI)
			}
		})
	}
}

// Port hopping beats single-port UDP blocks; dropping the ranges here would
// quietly pin Android to one port.
func TestBuildProfileCarriesHysteria2PortRanges(t *testing.T) {
	raw := `{"id":"a","name":"A","region":"r","country":"C",
	         "cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"},
	         "hysteria2":{"remoteHost":"node.example","remotePort":8444,"password":"pw","obfsPassword":"ob",
	                      "remotePorts":["20000:30000","40000:40010"]}}`
	profile := buildProfile(decodeServer(t, raw))
	if !sameStrings(profile.Hysteria2.RemotePorts, []string{"20000:30000", "40000:40010"}) {
		t.Fatalf("remotePorts %v", profile.Hysteria2.RemotePorts)
	}
}

func TestDiscoverHubRoutesReadsEveryNode(t *testing.T) {
	raw := `[{"id":"a","name":"A","region":"r","country":"C","cloak":{"remoteHost":"198.51.100.5","uid":"u","publicKey":"p"},
	          "controlPlaneShadowsocks":{"remoteHost":"198.51.100.5","remotePort":8489,"method":"2022-blake3-aes-128-gcm","password":"pw"},
	          "controlPlaneReality":{"remoteHost":"198.51.100.5","remotePort":443,"uuid":"cf550715-b9c8-4a58-a610-dc5cc73e36f4",
	                                 "publicKey":"8rifnTuJS517L1ysYdVaSvCntor5nkC3dn1XGqWMYlg","shortId":"355adc938875db2a",
	                                 "serverName":"swdist.apple.com"},
	          "frontedEndpoints":["relay.example.com"]},
	         {"id":"b","name":"B","region":"r","country":"C","cloak":{"remoteHost":"198.51.100.6","uid":"u","publicKey":"p"},
	          "frontedEndpoints":["relay.example.com"]}]`
	var servers []serverInfo
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		t.Fatal(err)
	}
	got := discoverHubRoutes(servers)
	if len(got.shadowsocks) != 1 || got.shadowsocks[0].RemotePort != 8489 {
		t.Fatalf("shadowsocks %+v", got.shadowsocks)
	}
	if len(got.reality) != 1 || got.reality[0].ServerName != "swdist.apple.com" {
		t.Fatalf("reality %+v", got.reality)
	}
	if !sameStrings(got.fronted, []string{"relay.example.com"}) {
		t.Fatalf("fronted %v", got.fronted)
	}
}

// tectonicBlock is /api/register's tectonic block with node swapped in.
func tectonicBlock(node string) string {
	return `{"node":` + node + `,"cover_domain":"cover.example","key_id":"` +
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 20)) + `","device_key":"` +
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)) + `",
	  "pins":["pin=="],"borrowed_sni":["www.example.org"],"cdn":{"host":"cdn.example","path":"/t","edge":"198.51.100.40"},
	  "shaping":[{"name":"chrome"}]}`
}

func decodeTectonicBlock(t *testing.T, raw string) *tectonicInfo {
	t.Helper()
	info, err := decodeTectonic(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("decode tectonic: %v", err)
	}
	return info
}

func TestBuildTectonicMapsTheHubBlock(t *testing.T) {
	got := buildTectonic(decodeTectonicBlock(t, tectonicBlock(`"203.0.113.7:8443"`)), "198.51.100.5")
	if got == nil {
		t.Fatal("tectonic profile missing")
	}
	if got.Node != "203.0.113.7:8443" || got.CoverDomain != "cover.example" || got.LocalPort != 0 {
		t.Fatalf("node=%q cover=%q port=%d", got.Node, got.CoverDomain, got.LocalPort)
	}
	if !bytes.Equal(got.KeyID, bytes.Repeat([]byte{1}, 20)) || !bytes.Equal(got.DeviceKey, bytes.Repeat([]byte{2}, 32)) {
		t.Fatalf("keyId=%x deviceKey=%x, want the decoded credential", got.KeyID, got.DeviceKey)
	}
	if !sameStrings(got.Pins, []string{"pin=="}) || !sameStrings(got.BorrowedSNI, []string{"www.example.org"}) {
		t.Fatalf("pins=%v borrowedSni=%v", got.Pins, got.BorrowedSNI)
	}
	if got.CDN == nil || *got.CDN != (state.TectonicCDNRoute{Host: "cdn.example", Path: "/t", Edge: "198.51.100.40"}) {
		t.Fatalf("cdn = %+v", got.CDN)
	}
	if string(got.Shaping) != `[{"name":"chrome"}]` || got.SNIMode != "" || got.SNI != "" {
		t.Fatalf("shaping=%s sniMode=%q sni=%q, want shaping as sent and Tectonic left to pick", got.Shaping, got.SNIMode, got.SNI)
	}
}

// Same dial-host rule as apps/desktop/src/shared/tectonicProfile.ts: an IP, never a name.
func TestBuildTectonicDialsAnAddress(t *testing.T) {
	tests := []struct {
		name, node, nodeIP, want string
	}{
		{"address and port kept", `"203.0.113.7:8443"`, "198.51.100.5", "203.0.113.7:8443"},
		{"no port means 443", `"203.0.113.7"`, "198.51.100.5", "203.0.113.7:443"},
		{"hostname falls back to the node IP, keeping the port", `"node.example:8443"`, "198.51.100.5", "198.51.100.5:8443"},
		{"bare hostname falls back to the node IP on 443", `"node.example"`, "198.51.100.5", "198.51.100.5:443"},
		{"bracketed IPv6", `"[2001:db8::7]:8443"`, "198.51.100.5", "[2001:db8::7]:8443"},
		{"bare IPv6", `"2001:db8::7"`, "198.51.100.5", "[2001:db8::7]:443"},
		{"bad port reads as none", `"203.0.113.7:99999"`, "198.51.100.5", "203.0.113.7:443"},
		{"empty node uses the node IP", `""`, "198.51.100.5", "198.51.100.5:443"},
		{"no address anywhere drops it", `"node.example"`, "cloak.example", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTectonic(decodeTectonicBlock(t, tectonicBlock(tt.node)), tt.nodeIP)
			switch {
			case tt.want == "" && got != nil:
				t.Fatalf("got node %q, want Tectonic dropped", got.Node)
			case tt.want != "" && (got == nil || got.Node != tt.want):
				t.Fatalf("got %+v, want node %q", got, tt.want)
			}
		})
	}
}

func TestBuildTectonicDropsAnUnusableCredential(t *testing.T) {
	for name, raw := range map[string]string{
		"no key id":          `{"node":"203.0.113.7","device_key":"AgI="}`,
		"no device key":      `{"node":"203.0.113.7","key_id":"AQE="}`,
		"key id not base64":  `{"node":"203.0.113.7","key_id":"!!","device_key":"AgI="}`,
		"device key garbled": `{"node":"203.0.113.7","key_id":"AQE=","device_key":"not base64"}`,
	} {
		if got := buildTectonic(decodeTectonicBlock(t, raw), "198.51.100.5"); got != nil {
			t.Errorf("%s: got %+v, want nil", name, got)
		}
	}
	if got := buildTectonic(nil, "198.51.100.5"); got != nil {
		t.Fatalf("no block = %+v, want nil", got)
	}
}

func TestBuildTectonicOmitsEmptyOptionalBlocks(t *testing.T) {
	raw := `{"node":"203.0.113.7","key_id":"AQE=","device_key":"AgI=","cdn":{"host":" ","path":"/t"},"shaping":[]}`
	got := buildTectonic(decodeTectonicBlock(t, raw), "198.51.100.5")
	if got == nil || got.CDN != nil || got.Shaping != nil {
		t.Fatalf("got %+v, want no CDN route and no shaping", got)
	}
}

func TestDecodeTectonicReportsAMalformedBlock(t *testing.T) {
	for _, raw := range []string{"", "null", "  "} {
		if info, err := decodeTectonic(json.RawMessage(raw)); info != nil || err != nil {
			t.Fatalf("%q = %+v, %v; want nothing", raw, info, err)
		}
	}
	if _, err := decodeTectonic(json.RawMessage(`{"pins":"not a list"}`)); err == nil {
		t.Fatal("a malformed block decoded without error")
	}
}
