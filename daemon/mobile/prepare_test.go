//go:build android || ios

package mobile

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

var testServerPub = bytes.Repeat([]byte{9}, 32)

func registerAnswer(extra map[string]any) map[string]any {
	answer := map[string]any{
		"serverPubkey": base64.StdEncoding.EncodeToString(testServerPub),
		"assignedIP":   "10.66.0.2",
		"dns":          "10.66.0.1",
	}
	for key, value := range extra {
		answer[key] = value
	}
	return answer
}

// signedInWith signs in with srvs as the known list and a fake hub online.
func signedInWith(t *testing.T, srvs ...serverInfo) *fakeHub {
	t.Helper()
	initForTest(t, map[string]string{keyLicense: "lk", keyIdentity: mustIdentity(t).marshal()})
	h := newFakeHub(t)
	mu.Lock()
	servers = srvs
	mu.Unlock()
	return h
}

func answerRegister(h *fakeHub, extra map[string]any) {
	h.handle("/api/register", func(fakeRequest) (int, any) { return http.StatusOK, registerAnswer(extra) })
}

func registerBody(t *testing.T, h *fakeHub) map[string]json.RawMessage {
	t.Helper()
	got := h.requestsTo("/api/register")
	if len(got) != 1 {
		t.Fatalf("got %d register requests, want 1", len(got))
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(got[0].Body, &body); err != nil {
		t.Fatalf("decode register body %s: %v", got[0].Body, err)
	}
	return body
}

func preparedNow() *preparedTunnel {
	mu.Lock()
	defer mu.Unlock()
	return prepared
}

func enablePostQuantum(t *testing.T) {
	t.Helper()
	if _, err := SetConfig(`{"postQuantum":true}`); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
}

func TestPrepareSendsNoOfferWhenPostQuantumIsOff(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "fra-1", Name: "Frankfurt"})
	answerRegister(h, nil)

	if _, err := Prepare("fra-1"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	body := registerBody(t, h)
	if _, ok := body["pq"]; ok {
		t.Fatalf("register body carries an offer: %v", body)
	}
	if _, ok := body["entryRegion"]; ok {
		t.Fatalf("single-hop register body names an entry: %v", body)
	}
	if string(body["region"]) != `"fra-1"` || string(body["licenseKey"]) != `"lk"` {
		t.Fatalf("register body = %v, want region fra-1 and the license", body)
	}
	if p := preparedNow(); p == nil || p.presharedKeyRaw != nil {
		t.Fatalf("prepared %+v, want a config with no pre-shared key", p)
	}
}

func TestPrepareKeysThePeerWhenTheNodeAnswers(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "fra-1", Name: "Frankfurt"})
	enablePostQuantum(t)
	nodePSK := make(chan []byte, 1)
	h.handle("/api/register", func(req fakeRequest) (int, any) {
		var body struct {
			PQ *pqOffer `json:"pq"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil || body.PQ == nil {
			t.Errorf("register body %s carries no offer (%v)", req.Body, err)
			return http.StatusInternalServerError, nil
		}
		ciphertext, shared, err := pq.Encapsulate(body.PQ.Algorithm, body.PQ.KEMPublicKey)
		if err != nil {
			t.Errorf("node could not encapsulate: %v", err)
			return http.StatusInternalServerError, nil
		}
		psk, _ := pq.DerivePresharedKey(shared)
		nodePSK <- psk
		return http.StatusOK, registerAnswer(map[string]any{
			"pq": map[string]string{"algorithm": pq.Algorithm, "kemCiphertext": ciphertext},
		})
	})

	if _, err := Prepare("fra-1"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	want := <-nodePSK
	if p := preparedNow(); p == nil || !bytes.Equal(p.presharedKeyRaw, want) {
		t.Fatalf("prepared %+v, want the node's key %x", p, want)
	}
}

func TestPrepareStaysUnkeyedWhenTheNodeIgnoresTheOffer(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "fra-1", Name: "Frankfurt"})
	enablePostQuantum(t)
	answerRegister(h, nil)

	if _, err := Prepare("fra-1"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, ok := registerBody(t, h)["pq"]; !ok {
		t.Fatal("register body carries no offer with post-quantum on")
	}
	if p := preparedNow(); p == nil || p.presharedKeyRaw != nil {
		t.Fatalf("prepared %+v, want an unkeyed config", p)
	}
}

func TestPrepareFailsOnABrokenAnswer(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "fra-1", Name: "Frankfurt"})
	enablePostQuantum(t)
	previous := &preparedTunnel{serverID: "old"}
	stagePrepared(previous)
	answerRegister(h, map[string]any{"pq": map[string]string{}})

	_, err := Prepare("fra-1")
	if err == nil || !strings.HasPrefix(err.Error(), "mobile: post-quantum key exchange failed: ") {
		t.Fatalf("Prepare = %v, want a post-quantum failure", err)
	}
	if p := preparedNow(); p != previous {
		t.Fatal("a failed Prepare replaced the prepared config")
	}
}

func TestPrepareWhileRunningLeavesTheTunnelAlone(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "A", Name: "Alpha"}, serverInfo{ID: "B", Name: "Bravo"})
	answerRegister(h, nil)
	running := &tunnelRuntime{serverID: "A"}
	mu.Lock()
	activeTunnel = running
	mu.Unlock()
	log := recordPushes()

	if _, err := Prepare("B"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	mu.Lock()
	gotActive, gotPrepared := activeTunnel, prepared
	mu.Unlock()
	if gotActive != running || gotPrepared == nil || gotPrepared.serverID != "B" {
		t.Fatalf("active kept=%v prepared=%+v, want the tunnel untouched and B prepared", gotActive == running, gotPrepared)
	}
	if pushes := log.all(); len(pushes) != 0 {
		t.Fatalf("Prepare pushed %+v", pushes)
	}
}

func TestPrepareFailureKeepsThePreviousConfig(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "B", Name: "Bravo"})
	previous := &preparedTunnel{serverID: "A", entryServerID: "C"}
	stagePrepared(previous)
	h.handle("/api/register", func(fakeRequest) (int, any) {
		return http.StatusBadGateway, map[string]string{"error": "Node unreachable", "code": "NODE_UNREACHABLE"}
	})

	if _, err := Prepare("B"); err == nil || !strings.Contains(err.Error(), "hub error (502)") {
		t.Fatalf("Prepare = %v, want the hub's 502", err)
	}
	if p := preparedNow(); p != previous || p.entryServerID != "C" {
		t.Fatalf("prepared = %+v, want the previous config kept", p)
	}
}

func TestPrepareScrubsTheConfigItReplaces(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "B", Name: "Bravo"})
	answerRegister(h, nil)
	previous := keyedTunnel("A")
	stagePrepared(previous)

	if _, err := Prepare("B"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !scrubbed(previous) {
		t.Fatal("the replaced config's keys were left in memory")
	}
	if p := preparedNow(); p == previous || bytes.Equal(p.wgPrivateKeyRaw, make([]byte, 32)) {
		t.Fatal("the new config was scrubbed instead")
	}
}

func TestAStartHoldsThePreparedKeysUntilItEnds(t *testing.T) {
	signedInWith(t)
	held := keyedTunnel("A")
	stagePrepared(held)
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}

	Logout()
	if scrubbed(held) {
		t.Fatal("the keys were scrubbed while a start was still configuring WireGuard with them")
	}
	finishStart(attempt)
	if !scrubbed(held) {
		t.Fatal("the keys outlived the start that held them")
	}
}

func TestAFinishedStartKeepsTheConfigStillPrepared(t *testing.T) {
	signedInWith(t)
	kept := keyedTunnel("A")
	stagePrepared(kept)
	attempt, err := claimStart()
	if err != nil {
		t.Fatalf("claimStart: %v", err)
	}
	finishStart(attempt)
	if scrubbed(kept) {
		t.Fatal("a finished start scrubbed the config a later Start reuses")
	}
}

func TestPrepareRefetchesAnUnknownServerOnce(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "A", Name: "Alpha"})
	h.handle("/api/client/regions", func(fakeRequest) (int, any) {
		return http.StatusOK, []serverInfo{{ID: "A", Name: "Alpha"}, {ID: "B", Name: "Bravo"}}
	})
	answerRegister(h, nil)

	if _, err := Prepare("B"); err != nil {
		t.Fatalf("Prepare of a server the stale list lacks: %v", err)
	}
	if got := len(h.requestsTo("/api/client/regions")); got != 1 {
		t.Fatalf("got %d region fetches, want 1", got)
	}
	if p := preparedNow(); p == nil || p.serverID != "B" || p.serverName != "Bravo" {
		t.Fatalf("prepared = %+v, want B", p)
	}
	mu.Lock()
	known := len(servers)
	mu.Unlock()
	if known != 2 {
		t.Fatalf("server list has %d entries after the refetch, want 2", known)
	}

	_, err := Prepare("Z")
	if err == nil || err.Error() != `mobile: unknown server "Z"` {
		t.Fatalf("Prepare(Z) = %v, want unknown server", err)
	}
	if got := len(h.requestsTo("/api/client/regions")); got != 2 {
		t.Fatalf("got %d region fetches after Z, want exactly one more", got)
	}
}

func signedIn() bool {
	mu.Lock()
	defer mu.Unlock()
	return licenseKey != ""
}

func TestPrepareSignsOutOnARefusedSession(t *testing.T) {
	refused := func(fakeRequest) (int, any) {
		return http.StatusUnauthorized, map[string]string{"error": "Invalid license key", "code": "INVALID_LICENSE"}
	}
	for _, route := range []string{"/api/register", "/api/client/regions"} {
		t.Run(route, func(t *testing.T) {
			h := signedInWith(t)
			if route == "/api/register" {
				mu.Lock()
				servers = []serverInfo{{ID: "A", Name: "Alpha"}}
				mu.Unlock()
			}
			h.handle(route, refused)

			if _, err := Prepare("A"); hubErrorCode(err) != "INVALID_LICENSE" {
				t.Fatalf("Prepare = %v, want the hub's INVALID_LICENSE", err)
			}
			if signedIn() || storedValue(keyLicense) != "" {
				t.Fatal("a refused session stayed signed in")
			}
		})
	}
}

func TestPrepareKeepsTheSessionOnAnExpiredSubscription(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "A", Name: "Alpha"})
	h.handle("/api/register", func(fakeRequest) (int, any) {
		return http.StatusForbidden, map[string]string{"error": "Subscription expired", "code": "SUBSCRIPTION_EXPIRED"}
	})

	if _, err := Prepare("A"); err == nil || !strings.HasPrefix(err.Error(), "SUBSCRIPTION_EXPIRED") {
		t.Fatalf("Prepare = %v, want the expired-subscription error", err)
	}
	if !signedIn() || storedValue(keyLicense) != "lk" {
		t.Fatal("an expired subscription signed the user out")
	}
}

func TestConcurrentPreparesAreSerialised(t *testing.T) {
	h := signedInWith(t, serverInfo{ID: "A", Name: "Alpha"})
	var inFlight, most atomic.Int32
	h.handle("/api/register", func(fakeRequest) (int, any) {
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			seen := most.Load()
			if now <= seen || most.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		return http.StatusOK, registerAnswer(nil)
	})

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Prepare("A"); err != nil {
				t.Errorf("Prepare: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := most.Load(); got != 1 {
		t.Fatalf("up to %d registrations overlapped, want 1", got)
	}
}
