//go:build android || ios

package mobile

// Prepare, Start, Stop and State: one tunnel's provisioning and data plane.

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Prepare provisions a fresh WireGuard config against serverId and stashes
// it in memory for the next Start call.
func Prepare(serverID string) (string, error) {
	mu.Lock()
	lk := licenseKey
	ident := identity
	srvs := servers
	mu.Unlock()
	if lk == "" {
		return "", errors.New("mobile: not authenticated")
	}
	if ident == nil {
		return "", errors.New("mobile: no device identity")
	}

	var target *serverInfo
	for i := range srvs {
		if srvs[i].ID == serverID {
			target = &srvs[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("mobile: unknown server %q", serverID)
	}

	wgPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate wireguard key: %w", err)
	}
	wgPubB64 := base64.StdEncoding.EncodeToString(wgPriv.PublicKey().Bytes())

	reg, err := registerServer(ident.pubB64, wgPubB64, serverID)
	if err != nil {
		return "", err
	}
	if reg.ServerPubkey == "" || reg.AssignedIP == "" || reg.DNS == "" {
		return "", errors.New("mobile: server returned an incomplete response; device may have been removed")
	}

	serverPubRaw, err := base64.StdEncoding.DecodeString(reg.ServerPubkey)
	if err != nil {
		return "", fmt.Errorf("decode server public key: %w", err)
	}

	mu.Lock()
	current := settings
	mu.Unlock()
	dnsServers := resolveDNS(reg.DNS, current.CustomDNS)

	mu.Lock()
	prepared = &preparedTunnel{
		wgPrivateKeyRaw: wgPriv.Bytes(),
		serverPubKeyRaw: serverPubRaw,
		profile:         buildProfile(target),
		serverID:        serverID,
		serverName:      target.Name,
	}
	mu.Unlock()

	b, err := json.Marshal(tunnelConfigJSON{
		Address:      reg.AssignedIP,
		PrefixLength: 32,
		DNS:          dnsServers,
		MTU:          normalizeMTU(current.MTU),
		ServerID:     serverID,
		ServerName:   target.Name,
	})
	if err != nil {
		return "", fmt.Errorf("marshal tunnel config: %w", err)
	}
	return string(b), nil
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

// State returns the current Status JSON, with live byte counters when
// connected.
func State() string {
	mu.Lock()
	snapshot := currentStatus
	t := activeTunnel
	mu.Unlock()

	if t != nil {
		rx, tx := tunnelBytes(t.dev)
		snapshot.BytesIn = rx
		snapshot.BytesOut = tx
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
