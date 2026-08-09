package api

import (
	"errors"
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
