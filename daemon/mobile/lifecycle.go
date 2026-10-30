//go:build android || ios

package mobile

// Prepare, Start, Stop and State: one tunnel's provisioning and data plane.

import (
	"crypto/ecdh"
	"crypto/mlkem"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// prepareMu serialises registrations: the hub keeps only the key registered
// last, so overlapping calls could stash one it has already replaced.
var prepareMu sync.Mutex

// Prepare provisions a fresh WireGuard config against serverId and stashes
// it in memory for the next Start call.
func Prepare(serverID string) (string, error) {
	return PrepareVia(serverID, "")
}

// PrepareVia provisions exitServerID carried through entryServerID; "" is a
// single hop. The hub moves the peer here, so a running tunnel stops carrying traffic.
func PrepareVia(exitServerID, entryServerID string) (string, error) {
	prepareMu.Lock()
	defer prepareMu.Unlock()

	mu.Lock()
	lk, ident, current, logs := licenseKey, identity, settings, wgLogs
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: not authenticated")
	}
	if ident == nil {
		return "", errors.New("mobile: no device identity")
	}

	exit, entry, err := lookupRoute(lk, ident.pubB64, exitServerID, entryServerID)
	if err != nil {
		return "", err
	}
	if entry != nil {
		if err := validateHopEntry(exit.ID, entry); err != nil {
			return "", err
		}
	}
	got, err := provision(ident.pubB64, exit, entry, current.PostQuantum, logs)
	if err != nil {
		if isSignOutCode(err) {
			signOutLocally()
		}
		return "", err
	}

	mu.Lock()
	// A sign-out or account switch mid-call must not get this config back.
	if licenseKey != lk || identity.publicKey() != ident.pubB64 {
		mu.Unlock()
		return "", errors.New("mobile: the session changed while preparing")
	}
	prepared = got.tunnel
	mu.Unlock()
	return tunnelConfig(got, current)
}

// lookupRoute finds both servers, reloading the list once when either is
// missing: the host may offer a server this process has not seen.
func lookupRoute(lk, identityPub, exitID, entryID string) (*serverInfo, *serverInfo, error) {
	mu.Lock()
	known := servers
	mu.Unlock()
	if exit, entry, missing := routeServers(known, exitID, entryID); missing == "" {
		return exit, entry, nil
	}

	fresh, _, err := loadServers(lk, identityPub)
	if err != nil {
		return nil, nil, err
	}
	exit, entry, missing := routeServers(fresh, exitID, entryID)
	if missing != "" {
		return nil, nil, fmt.Errorf("mobile: unknown server %q", missing)
	}
	return exit, entry, nil
}

// provisioned is one registration's result, not yet published to prepared.
type provisioned struct {
	tunnel *preparedTunnel
	reg    *registerResponse
}

// provision registers a fresh key for the route. It changes no global state,
// so a failure keeps the previous prepared config.
func provision(identityPub string, exit, entry *serverInfo, postQuantum bool, logs *state.LogStore) (provisioned, error) {
	wgPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return provisioned{}, fmt.Errorf("generate wireguard key: %w", err)
	}
	wgPubB64 := base64.StdEncoding.EncodeToString(wgPriv.PublicKey().Bytes())
	dk := offerPostQuantum(postQuantum, logs)

	entryID, entryName := "", ""
	if entry != nil {
		entryID, entryName = entry.ID, entry.Name
	}
	reg, err := registerServer(identityPub, wgPubB64, exit.ID, entryID, dk)
	if err != nil {
		return provisioned{}, err
	}
	if reg.ServerPubkey == "" || reg.AssignedIP == "" || reg.DNS == "" {
		return provisioned{}, errors.New("mobile: server returned an incomplete response; device may have been removed")
	}
	serverPubRaw, err := base64.StdEncoding.DecodeString(reg.ServerPubkey)
	if err != nil {
		return provisioned{}, fmt.Errorf("decode server public key: %w", err)
	}
	profile := buildProfile(exit)
	if entry != nil {
		if profile, err = hopProfile(exit, entry, reg.Hop); err != nil {
			return provisioned{}, err
		}
	}
	psk, err := presharedKeyFrom(dk, reg.PQ)
	if err != nil {
		return provisioned{}, fmt.Errorf("mobile: post-quantum key exchange failed: %w", err)
	}
	if dk != nil && psk == nil {
		logAdd(logs, state.LogWarn, "server did not answer the post-quantum offer; the peer is not post-quantum keyed")
	}

	tunnel := &preparedTunnel{
		wgPrivateKeyRaw: wgPriv.Bytes(),
		serverPubKeyRaw: serverPubRaw,
		presharedKeyRaw: psk,
		profile:         profile,
		serverID:        exit.ID,
		serverName:      exit.Name,
		entryServerID:   entryID,
		entryServerName: entryName,
	}
	return provisioned{tunnel: tunnel, reg: reg}, nil
}

// offerPostQuantum keeps the decapsulation key on Prepare's stack, so it is
// single-use by construction. A failure only costs the pre-shared key.
func offerPostQuantum(enabled bool, logs *state.LogStore) *mlkem.DecapsulationKey768 {
	if !enabled {
		return nil
	}
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		logAdd(logs, state.LogWarn, "post-quantum offer unavailable; registering without a pre-shared key: "+err.Error())
		return nil
	}
	return dk
}

func tunnelConfig(got provisioned, current config) (string, error) {
	b, err := json.Marshal(tunnelConfigJSON{
		Address:         got.reg.AssignedIP,
		PrefixLength:    32,
		DNS:             resolveDNS(got.reg.DNS, current.CustomDNS),
		MTU:             normalizeMTU(current.MTU),
		ServerID:        got.tunnel.serverID,
		ServerName:      got.tunnel.serverName,
		EntryServerID:   got.tunnel.entryServerID,
		EntryServerName: got.tunnel.entryServerName,
	})
	if err != nil {
		return "", fmt.Errorf("marshal tunnel config: %w", err)
	}
	return string(b), nil
}

// MultihopEntries returns the ids of servers that can relay to exitServerID as
// a JSON array, lightest first, from the last fetched list.
func MultihopEntries(exitServerID string) string {
	mu.Lock()
	known := servers
	mu.Unlock()
	b, err := json.Marshal(multihopEntries(known, exitServerID))
	if err != nil {
		return "[]"
	}
	return string(b)
}

// Start brings up the tunnel on the host's TUN fd. Android hands the fd over;
// iOS keeps it and Go works on a dup.
func Start(tunFd int) error {
	return startTunnel(tunFd)
}

// Stop tears down the running tunnel, if any. The prepared config survives, so
// Start can run again without another Prepare.
func Stop() {
	stopTunnel()
}

// StopForSwitch tears the tunnel down for an immediate Start on the prepared
// server, reporting connecting rather than disconnected.
func StopForSwitch() {
	stopTunnelForSwitch()
}

// State returns the current Status JSON, with live counters and liveness
// when connected.
func State() string {
	mu.Lock()
	snapshot := currentStatus
	t := activeTunnel
	mu.Unlock()

	if t != nil {
		// One UAPI read per poll: the iOS extension runs in a tight memory budget.
		snapshot.BytesIn, snapshot.BytesOut, snapshot.LastHandshake = tunnelStats(t.dev)
		snapshot.PostQuantum = t.postQuantum
	}

	b, err := json.Marshal(snapshot)
	if err != nil {
		return "{}"
	}
	return string(b)
}

type registerResponse struct {
	ServerPubkey string `json:"serverPubkey"`
	AssignedIP   string `json:"assignedIP"`
	DNS          string `json:"dns"`
	// PQ stays raw so absent, null and malformed remain distinguishable.
	PQ json.RawMessage `json:"pq"`
	// Hop echoes the route the hub set up; only an entryRegion request gets one.
	Hop *state.HopProfile `json:"hop"`
}

func registerServer(identityPub, wgPub, region, entryRegion string, dk *mlkem.DecapsulationKey768) (*registerResponse, error) {
	mu.Lock()
	lk := licenseKey
	mu.Unlock()
	body := map[string]any{
		"licenseKey":     lk,
		"identityPubkey": identityPub,
		"wgPubkey":       wgPub,
		"region":         region,
	}
	if entryRegion != "" {
		body["entryRegion"] = entryRegion
	}
	if dk != nil {
		body["pq"] = newPQOffer(dk)
	}
	var out registerResponse
	if err := hubRequest(http.MethodPost, "/api/register", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
