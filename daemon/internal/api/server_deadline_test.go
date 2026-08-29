package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A handler that outlives the server's WriteTimeout loses its response; the
// extension is what lets a long connect cascade still answer the client.
func TestExtendWriteDeadline_OutlivesServerWriteTimeout(t *testing.T) {
	handler := func(extend bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if extend {
				extendWriteDeadline(w)
			}
			time.Sleep(300 * time.Millisecond)
			_, _ = io.WriteString(w, "ok")
		}
	}
	get := func(h http.Handler) (string, error) {
		srv := httptest.NewUnstartedServer(h)
		srv.Config.WriteTimeout = 100 * time.Millisecond
		srv.Start()
		defer srv.Close()
		resp, err := srv.Client().Get(srv.URL)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return string(body), err
	}

	if body, err := get(handler(false)); err == nil && body == "ok" {
		t.Fatal("precondition: the bare WriteTimeout should have cut the slow response off")
	}
	body, err := get(handler(true))
	if err != nil || body != "ok" {
		t.Fatalf("extended deadline: body=%q err=%v, want ok", body, err)
	}
}
