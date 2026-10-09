package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

func TestPostQuantumRoutes(t *testing.T) {
	const token = "0123456789abcdef"
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	handler := NewHandler(token, svc)

	call := func(t *testing.T, route, body string, authed bool) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+route, strings.NewReader(body))
		if authed {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var payload map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		return rec.Code, payload
	}

	t.Run("offer then finish yields the responder's key", func(t *testing.T) {
		code, offer := call(t, "/pq/offer", "", true)
		if code != http.StatusOK {
			t.Fatalf("offer status = %d", code)
		}
		id, _ := offer["id"].(string)
		publicKey, _ := offer["kemPublicKey"].(string)
		if id == "" || offer["algorithm"] != pq.Algorithm {
			t.Fatalf("offer = %v", offer)
		}

		ciphertext, shared, err := pq.Encapsulate(pq.Algorithm, publicKey)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := pq.DerivePresharedKey(shared)

		body, _ := json.Marshal(map[string]string{"id": id, "algorithm": pq.Algorithm, "kemCiphertext": ciphertext})
		code, finish := call(t, "/pq/finish", string(body), true)
		if code != http.StatusOK {
			t.Fatalf("finish status = %d: %v", code, finish)
		}
		if finish["presharedKey"] != base64.StdEncoding.EncodeToString(want) {
			t.Fatalf("presharedKey = %v, want %s", finish["presharedKey"], base64.StdEncoding.EncodeToString(want))
		}

		code, _ = call(t, "/pq/finish", string(body), true)
		if code != http.StatusBadRequest {
			t.Fatalf("second finish status = %d, want 400", code)
		}
	})

	t.Run("encapsulate answers a valid key and refuses a bad one", func(t *testing.T) {
		_, offer := call(t, "/pq/offer", "", true)
		body, _ := json.Marshal(map[string]string{"algorithm": pq.Algorithm, "kemPublicKey": offer["kemPublicKey"].(string)})
		code, reply := call(t, "/pq/encapsulate", string(body), true)
		if code != http.StatusOK {
			t.Fatalf("status = %d: %v", code, reply)
		}
		ct, _ := base64.StdEncoding.DecodeString(reply["kemCiphertext"].(string))
		ss, _ := base64.StdEncoding.DecodeString(reply["sharedSecret"].(string))
		if len(ct) != pq.CiphertextSize || len(ss) != 32 {
			t.Fatalf("ciphertext %d bytes, shared secret %d bytes", len(ct), len(ss))
		}

		code, _ = call(t, "/pq/encapsulate", `{"algorithm":"ml-kem-768","kemPublicKey":"AAAA"}`, true)
		if code != http.StatusBadRequest {
			t.Fatalf("bad key status = %d, want 400", code)
		}
		code, _ = call(t, "/pq/encapsulate", `{"algorithm":"ml-kem-768","kemPublicKey":`, true)
		if code != http.StatusBadRequest {
			t.Fatalf("bad json status = %d, want 400", code)
		}
	})

	t.Run("every route needs the token and POST", func(t *testing.T) {
		for _, route := range []string{"/pq/offer", "/pq/finish", "/pq/encapsulate"} {
			if code, _ := call(t, route, "{}", false); code != http.StatusUnauthorized {
				t.Fatalf("%s unauthenticated status = %d, want 401", route, code)
			}
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+route, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s GET status = %d, want 405", route, rec.Code)
			}
		}
	})
}
