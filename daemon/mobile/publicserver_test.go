package mobile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// serverWithEveryTransport carries every transport block and a distinct secret in each.
func serverWithEveryTransport(t *testing.T) serverInfo {
	t.Helper()
	load := 12
	return serverInfo{
		ID: "fra", Name: "Frankfurt", Region: "eu-central", Country: "DE", Load: &load, Multihop: true,
		Cloak:       cloakInfo{RemoteHost: "1.2.3.4", UID: "secret-cloak-uid", PublicKey: "secret-cloak-pk", ServerName: "cdn.example"},
		Naive:       &naiveInfo{RemoteHost: "n.example", RemotePort: 443, Username: "secret-naive-user", Password: "secret-naive-pass"},
		Reality:     &realityInfo{RemoteHost: "1.2.3.4", RemotePort: 443, UUID: "secret-reality-uuid", PublicKey: "secret-reality-pk", ShortID: "secret-sid"},
		Hysteria2:   &hysteria2Info{RemoteHost: "h.example", RemotePort: 443, Password: "secret-hy2-pass", ObfsPassword: "secret-obfs", RemotePorts: []string{"20000:30000"}},
		Shadowsocks: &shadowsocksInfo{RemoteHost: "1.2.3.4", RemotePort: 8388, Method: "2022-blake3-aes-256-gcm", Password: "secret-ss-pass"},
		AnyTLS:      &anytlsInfo{RemoteHost: "1.2.3.4", RemotePort: 8443, Password: "secret-anytls-pass"},
		ControlPlaneShadowsocks: &controlPlaneShadowsocksInfo{RemoteHost: "1.2.3.4", RemotePort: 8389, Method: "aes-256-gcm",
			Password: "secret-cp-ss-pass"},
		ControlPlaneReality: &controlPlaneRealityInfo{RemoteHost: "1.2.3.4", RemotePort: 9443, UUID: "secret-cp-uuid",
			PublicKey: "secret-cp-pk", ShortID: "secret-cp-sid", ServerName: "cp.example"},
		Snowflake:        &snowflakeInfo{BrokerURL: "https://broker.example", BridgeFingerprint: "secret-fingerprint"},
		FrontedEndpoints: []string{"secret-relay.example"},
	}
}

// everyBlockRunnable stands in for a build that can run anything it is given.
func everyBlockRunnable(p *state.Profile, kind string) bool {
	switch kind {
	case "cloak":
		return true
	case "reality":
		return p.Reality != nil
	case "hysteria2":
		return p.Hysteria2 != nil
	case "shadowsocks":
		return p.Shadowsocks != nil
	case "anytls":
		return p.AnyTLS != nil
	case "naive":
		return p.Naive != nil
	case "snowflake":
		return p.Snowflake != nil
	}
	return false
}

func TestPublicServerJSONCarriesNoCredentials(t *testing.T) {
	b, err := json.Marshal(toPublicServers([]serverInfo{serverWithEveryTransport(t)}, everyBlockRunnable))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, leak := range []string{"password", "Password", "uuid", "uid", "publicKey", "controlPlane", "secret", "remoteHost", "1.2.3.4", "fronted"} {
		if strings.Contains(out, leak) {
			t.Errorf("public server JSON contains %q: %s", leak, out)
		}
	}
	want := `[{"id":"fra","name":"Frankfurt","region":"eu-central","country":"DE","load":12,"multihop":true,` +
		`"cloak":true,"reality":true,"hysteria2":true,"shadowsocks":true,"anytls":true,"naive":true,"snowflake":true}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestPublicServerFlagsNeedTheBlockAndTheBuild(t *testing.T) {
	bare := serverInfo{ID: "lon", Name: "London", Region: "eu-west", Country: "GB", Cloak: cloakInfo{RemoteHost: "5.6.7.8"}}
	got := toPublicServers([]serverInfo{bare}, everyBlockRunnable)[0]
	if !got.Cloak || got.Reality || got.Hysteria2 || got.Shadowsocks || got.AnyTLS || got.Naive || got.Snowflake || got.Multihop {
		t.Fatalf("a server with only cloak = %+v", got)
	}

	cannotRun := func(_ *state.Profile, kind string) bool { return kind == "cloak" }
	got = toPublicServers([]serverInfo{serverWithEveryTransport(t)}, cannotRun)[0]
	if !got.Cloak || got.Reality || got.Naive || got.Snowflake || !got.Multihop {
		t.Fatalf("advertised but not runnable = %+v", got)
	}

	alwaysYes := func(*state.Profile, string) bool { return true }
	got = toPublicServers([]serverInfo{bare}, alwaysYes)[0]
	if got.Reality || got.Naive || got.Snowflake {
		t.Fatalf("a block the hub never advertised was flagged: %+v", got)
	}
}

func TestPublicServersIsAnEmptyListNotNull(t *testing.T) {
	b, _ := json.Marshal(toPublicServers(nil, everyBlockRunnable))
	if string(b) != "[]" {
		t.Fatalf("got %s, want []", b)
	}
	session, _ := json.Marshal(sessionJSON{Servers: toPublicServers(nil, everyBlockRunnable)})
	if string(session) != `{"email":"","name":"","servers":[],"cached":false}` {
		t.Fatalf("session = %s", session)
	}
}
