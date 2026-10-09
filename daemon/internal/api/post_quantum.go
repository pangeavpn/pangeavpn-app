package api

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

type pqFinishRequest struct {
	ID            string `json:"id"`
	Algorithm     string `json:"algorithm"`
	KEMCiphertext string `json:"kemCiphertext"`
}

type pqFinishResponse struct {
	PresharedKey string `json:"presharedKey"`
}

type pqEncapsulateRequest struct {
	Algorithm    string `json:"algorithm"`
	KEMPublicKey string `json:"kemPublicKey"`
}

type pqEncapsulateResponse struct {
	KEMCiphertext string `json:"kemCiphertext"`
	SharedSecret  string `json:"sharedSecret"`
}

// registerPostQuantumRoutes serves ML-KEM to the app, which has no post-quantum
// crypto of its own; results ride the loopback channel that already carries WireGuard keys.
func registerPostQuantumRoutes(mux *http.ServeMux, token string, limiter *rateLimiter) {
	exchanges := pq.NewExchanges()

	mux.Handle("/pq/offer", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		offer, err := exchanges.Offer()
		if err != nil {
			log.Printf("pq offer failed: %s", sanitizeLog(err.Error()))
			writeError(w, http.StatusInternalServerError, "post-quantum offer failed")
			return
		}
		writeJSON(w, http.StatusOK, offer)
	}))

	mux.Handle("/pq/finish", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req pqFinishRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		key, err := exchanges.Finish(req.ID, pq.Answer{Algorithm: req.Algorithm, KEMCiphertext: req.KEMCiphertext})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, pqFinishResponse{PresharedKey: base64.StdEncoding.EncodeToString(key)})
	}))

	mux.Handle("/pq/encapsulate", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req pqEncapsulateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		ciphertext, shared, err := pq.Encapsulate(req.Algorithm, req.KEMPublicKey)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, pqEncapsulateResponse{
			KEMCiphertext: ciphertext,
			SharedSecret:  base64.StdEncoding.EncodeToString(shared),
		})
	}))
}
