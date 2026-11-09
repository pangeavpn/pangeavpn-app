package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	signedScheme      = "Pangea-HMAC"
	signatureMaxSkew  = 60 * time.Second
	maxSignedBodySize = 1 << 20
)

// The signed form never puts the token on the wire, so a process squatting on
// the port while the daemon restarts captures nothing it can reuse.
func RequireToken(expectedToken string, next http.Handler) http.Handler {
	nonces := &nonceCache{seen: map[string]time.Time{}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, expectedToken, nonces) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, expectedToken string, nonces *nonceCache) bool {
	scheme, value, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found {
		return false
	}
	value = strings.TrimSpace(value)
	switch {
	case strings.EqualFold(scheme, "Bearer"):
		return value != "" && subtle.ConstantTimeCompare([]byte(value), []byte(expectedToken)) == 1
	case strings.EqualFold(scheme, signedScheme):
		return verifySigned(r, value, expectedToken, nonces)
	}
	return false
}

func verifySigned(r *http.Request, value, token string, nonces *nonceCache) bool {
	params := map[string]string{}
	for part := range strings.SplitSeq(value, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
			params[k] = v
		}
	}
	ts, nonce, sig := params["ts"], params["nonce"], params["sig"]
	millis, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(nonce) != 32 || sig == "" {
		return false
	}
	if skew := time.Since(time.UnixMilli(millis)); skew > signatureMaxSkew || skew < -signatureMaxSkew {
		return false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxSignedBodySize+1))
	if err != nil || len(body) > maxSignedBodySize {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	want := requestSignature(token, r.Method, r.RequestURI, ts, nonce, body)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return false
	}
	return nonces.claim(nonce, time.Now())
}

func requestSignature(token, method, uri, ts, nonce string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(method + "\n" + uri + "\n" + ts + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:])))
	return hex.EncodeToString(mac.Sum(nil))
}

type nonceCache struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	lastPrune time.Time
}

// claim refuses a nonce already used. Entries outlive the skew window on both
// sides, so a replay is either remembered here or rejected by its timestamp.
func (c *nonceCache) claim(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.lastPrune) > signatureMaxSkew {
		for n, at := range c.seen {
			if now.Sub(at) > 2*signatureMaxSkew {
				delete(c.seen, n)
			}
		}
		c.lastPrune = now
	}
	if _, used := c.seen[nonce]; used {
		return false
	}
	c.seen[nonce] = now
	return true
}
