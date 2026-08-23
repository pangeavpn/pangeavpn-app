package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/auth"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// sanitizeLog strips CR/LF and other control chars from values that may
// originate from user-controlled input before they are written to logs.
func sanitizeLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

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

// rateLimitMiddleware wraps a handler with rate limiting (~2000 req/min).
func rateLimitMiddleware(limiter *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether hostport (a Host header or an Origin's
// authority, port optional) names the loopback interface.
func isLoopbackHost(hostport string) bool {
	h := hostport
	if hostOnly, _, err := net.SplitHostPort(hostport); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

// hostOriginMiddleware blocks DNS-rebinding pages: a hostname that resolves to
// 127.0.0.1 still fails this check unless it literally names loopback.
func hostOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !isLoopbackHost(u.Host) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// maxBodySize is the maximum allowed request body size (1 MB).
const maxBodySize = 1 << 20

type connectRequest struct {
	ProfileID string `json:"profileId"`
	AllowLAN  bool   `json:"allowLAN,omitempty"`
	Lockdown  bool   `json:"lockdown,omitempty"`
	// PreferredTransport: "cloak", "reality", "hysteria2", "naive",
	// "shadowsocks", "snowflake", "wireguard" (straight to the node, no
	// transport), or "" / "auto" (cascade in autoCascadeOrder: reality, cloak,
	// shadowsocks, hysteria2, then naive; snowflake is gated off this release --
	// see snowflakeReleaseGated, and "wireguard" is never in the cascade). Auto
	// mode keeps only the transports this profile configures, then
	// reorderByMemory may promote whatever last worked on this network.
	// transportCandidates dispatches on this value and rejects anything else.
	PreferredTransport string `json:"preferredTransport,omitempty"`
}

type disconnectRequest struct {
	KeepKillSwitch bool `json:"keepKillSwitch,omitempty"`
}

type engageKillSwitchRequest struct {
	ProfileID string `json:"profileId,omitempty"`
	AllowLAN  bool   `json:"allowLAN,omitempty"`
}

// permitHostsRequest carries control-plane IPs (the Pangea hub) that must stay
// reachable through an engaged lockdown lock. IP literals only — see
// Service.PermitHosts.
type permitHostsRequest struct {
	Hosts []string `json:"hosts,omitempty"`
}

type okResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ssProxyStartRequest carries the control-plane Shadowsocks credentials. This
// is a separate listener from any tunnel transport: its node-side ACL permits
// the hub only, so these credentials cannot reach WireGuard.
type ssProxyStartRequest struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Method     string `json:"method"`
	Password   string `json:"password"`
	UDPOverTCP bool   `json:"udpOverTcp,omitempty"`
}

type ssProxyStartResponse struct {
	OK            bool   `json:"ok"`
	Port          int    `json:"port,omitempty"`
	ProxyUsername string `json:"proxyUsername,omitempty"`
	ProxyPassword string `json:"proxyPassword,omitempty"`
	Error         string `json:"error,omitempty"`
}

// ssProxySupportedMethods mirrors shadowsocks.supportedMethods: the AEAD and
// AEAD-2022 cipher families only, no legacy unauthenticated stream ciphers.
var ssProxySupportedMethods = map[string]bool{
	"aes-128-gcm":                   true,
	"aes-192-gcm":                   true,
	"aes-256-gcm":                   true,
	"chacha20-ietf-poly1305":        true,
	"xchacha20-ietf-poly1305":       true,
	"2022-blake3-aes-128-gcm":       true,
	"2022-blake3-aes-256-gcm":       true,
	"2022-blake3-chacha20-poly1305": true,
}

// validateSSProxyStartRequest enforces the permitHostsRequest contract: only an
// IP literal may be handed to PermitHosts to punch a lockdown hole for.
func validateSSProxyStartRequest(req ssProxyStartRequest) error {
	if net.ParseIP(strings.TrimSpace(req.RemoteHost)) == nil {
		return errors.New("remoteHost must be an IP literal")
	}
	if req.RemotePort < 1 || req.RemotePort > 65535 {
		return errors.New("remotePort must be between 1 and 65535")
	}
	if method := strings.TrimSpace(req.Method); method != "" && !ssProxySupportedMethods[method] {
		return errors.New("method is not a supported cipher")
	}
	return nil
}

func serviceErrorResponse(err error) okResponse {
	if errors.Is(err, ErrTransportExhausted) {
		return okResponse{OK: false, Error: "transport_exhausted"}
	}
	return okResponse{OK: false}
}

// withAuthAndLimit authenticates first, then rate-limits: an unauthenticated
// caller never spends a token from the shared bucket the real UI depends on.
func withAuthAndLimit(token string, limiter *rateLimiter, handler http.HandlerFunc) http.Handler {
	return auth.RequireBearer(token, rateLimitMiddleware(limiter, handler))
}

func NewHandler(token string, service *Service) http.Handler {
	// Sized for the peak, not the average: the client polls status at 4Hz while
	// connecting, on top of the config and kill-switch calls a connect makes.
	limiter := newRateLimiter(2000, 33.3) // 2000 burst, refill ~33/s (~2000/min)
	// /ping is unauthenticated, so it gets its own small bucket: draining it
	// never starves the authenticated routes sharing limiter above.
	pingLimiter := newRateLimiter(20, 1)
	mux := http.NewServeMux()

	mux.Handle("/ping", rateLimitMiddleware(pingLimiter, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	mux.Handle("/status", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, service.Status(r.Context()))
	}))

	mux.Handle("/connect", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
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

		err := service.Connect(r.Context(), req.ProfileID, ConnectOptions{AllowLAN: req.AllowLAN, Lockdown: req.Lockdown, PreferredTransport: req.PreferredTransport})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, serviceErrorResponse(err))
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/disconnect", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req disconnectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		err := service.Disconnect(r.Context(), req.KeepKillSwitch)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/killswitch/clear", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if err := service.ClearKillSwitch(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/transport-memory/clear", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if err := service.ClearTransportMemory(); err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/killswitch/engage", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req engageKillSwitchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		if err := service.EngageKillSwitch(r.Context(), req.ProfileID, req.AllowLAN); err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/killswitch/permit", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req permitHostsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		if err := service.PermitHosts(r.Context(), req.Hosts); err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	// Single unhyphenated segments, matching /killswitch/*. Deliberately not
	// part of Connect: this must work before a profile exists.
	mux.Handle("/ssproxy/start", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req ssProxyStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := validateSSProxyStartRequest(req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		port, err := service.StartShadowsocksProxy(r.Context(), state.ShadowsocksProfile{
			RemoteHost: req.RemoteHost,
			RemotePort: req.RemotePort,
			Method:     req.Method,
			Password:   req.Password,
			UDPOverTCP: req.UDPOverTCP,
		})
		if err != nil {
			// The reason travels: without it the settings pane can only say the
			// hub was unreachable, which sends the next hour after the network.
			writeJSON(w, http.StatusInternalServerError, ssProxyStartResponse{OK: false, Error: err.Error()})
			return
		}

		user, pass := service.ShadowsocksProxyCredentials()
		writeJSON(w, http.StatusOK, ssProxyStartResponse{OK: true, Port: port, ProxyUsername: user, ProxyPassword: pass})
	}))

	mux.Handle("/ssproxy/stop", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if err := service.StopShadowsocksProxy(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, okResponse{OK: false})
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/switch", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
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

		err := service.Switch(r.Context(), req.ProfileID, ConnectOptions{AllowLAN: req.AllowLAN, Lockdown: req.Lockdown, PreferredTransport: req.PreferredTransport})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, serviceErrorResponse(err))
			return
		}

		writeJSON(w, http.StatusOK, okResponse{OK: true})
	}))

	mux.Handle("/logs", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
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
	}))

	mux.Handle("/config", withAuthAndLimit(token, limiter, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, service.Config())
		case http.MethodPost:
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			// A pointer field distinguishes an absent "profiles" key (nil, rejected)
			// from an explicit empty list, so a truncated body can't wipe all profiles.
			var payload struct {
				Profiles *[]state.Profile `json:"profiles"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if payload.Profiles == nil {
				writeError(w, http.StatusBadRequest, "profiles is required")
				return
			}
			cfg := state.Config{Profiles: *payload.Profiles}
			if err := service.UpdateConfig(cfg); err != nil {
				log.Printf("config update rejected: %s", sanitizeLog(err.Error()))
				writeError(w, http.StatusBadRequest, "invalid config")
				return
			}
			writeJSON(w, http.StatusOK, okResponse{OK: true})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}))

	return slowRequestWatchdog(hostOriginMiddleware(mux))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		log.Printf("writeJSON: encode failed: %s", sanitizeLog(err.Error()))
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
