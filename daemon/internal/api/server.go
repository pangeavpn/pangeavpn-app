package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/auth"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// rateLimiter implements a simple token bucket rate limiter for localhost API access.
type rateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newRateLimiter(maxTokens float64, refillRate float64) *rateLimiter {
	return &rateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens = rl.tokens + elapsed*rl.refillRate
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastRefill = now

	if rl.tokens < 1 {
		return false
	}
	rl.tokens--
	return true
}

// rateLimitMiddleware wraps a handler with rate limiting (~500 req/min).
func rateLimitMiddleware(limiter *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxBodySize is the maximum allowed request body size (1 MB).
const maxBodySize = 1 << 20

type connectRequest struct {
	ProfileID string `json:"profileId"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func NewHandler(token string, service *Service) http.Handler {
	limiter := newRateLimiter(500, 8.33) // 500 burst, refill ~8.33/s (~500/min)
	mux := http.NewServeMux()

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.Handle("/status", auth.RequireBearer(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, service.Status(r.Context()))
	})))

	mux.Handle("/connect", auth.RequireBearer(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req connectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.ProfileID == "" {
			writeError(w, http.StatusBadRequest, "profileId is required")
			return
		}

		err := service.Connect(r.Context(), req.ProfileID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	})))

	mux.Handle("/disconnect", auth.RequireBearer(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		err := service.Disconnect(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	})))

	mux.Handle("/logs", auth.RequireBearer(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		since := int64(0)
		sinceRaw := r.URL.Query().Get("since")
		if sinceRaw != "" {
			value, err := strconv.ParseInt(sinceRaw, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "since must be unix milliseconds")
				return
			}
			since = value
		}

		writeJSON(w, http.StatusOK, service.Logs(since))
	})))

	mux.Handle("/config", auth.RequireBearer(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, service.Config())
		case http.MethodPost:
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			var cfg state.Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if err := service.UpdateConfig(cfg); err != nil {
				log.Printf("config update rejected: %v", err)
				writeError(w, http.StatusBadRequest, "invalid config")
				return
			}
			writeJSON(w, http.StatusOK, okResponse{OK: true})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})))

	return rateLimitMiddleware(limiter, mux)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
