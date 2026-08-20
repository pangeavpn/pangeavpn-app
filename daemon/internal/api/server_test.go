package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceErrorResponseClassifiesOnlyTransportExhaustion(t *testing.T) {
	retryable := serviceErrorResponse(errors.Join(errors.New("details"), ErrTransportExhausted))
	if retryable.Error != "transport_exhausted" {
		t.Fatalf("retryable error code = %q, want transport_exhausted", retryable.Error)
	}

	ordinary := serviceErrorResponse(errors.New("preflight failed"))
	if ordinary.Error != "" {
		t.Fatalf("ordinary error code = %q, want empty", ordinary.Error)
	}
}

func TestTransportMemoryClearRoute(t *testing.T) {
	const token = "0123456789abcdef"

	newHandler := func(t *testing.T) (http.Handler, *fakeTransportMemory) {
		t.Helper()
		svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
		mem := &fakeTransportMemory{entries: map[string]string{"wifi-a": "reality"}}
		svc.SetTransportMemory(mem)
		return NewHandler(token, svc), mem
	}

	t.Run("clears an authenticated post", func(t *testing.T) {
		handler, mem := newHandler(t)
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/transport-memory/clear", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got, ok := mem.Lookup("wifi-a"); ok {
			t.Fatalf("Lookup = %q after clear, want a miss", got)
		}
	})

	t.Run("refuses an unauthenticated post", func(t *testing.T) {
		handler, mem := newHandler(t)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/transport-memory/clear", nil))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if _, ok := mem.Lookup("wifi-a"); !ok {
			t.Fatal("memory was cleared by an unauthenticated caller")
		}
	})

	t.Run("refuses a GET", func(t *testing.T) {
		handler, mem := newHandler(t)
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/transport-memory/clear", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if _, ok := mem.Lookup("wifi-a"); !ok {
			t.Fatal("memory was cleared by a GET")
		}
	})
}
