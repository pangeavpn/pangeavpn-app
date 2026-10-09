package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func RequireBearer(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r.Header.Get("Authorization"))
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(header string) string {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
