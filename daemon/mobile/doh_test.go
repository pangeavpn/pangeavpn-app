package mobile

import (
	"strings"
	"testing"
)

// A CNAME often comes first in the answer list; dialing it would reach
// whatever host the name points at, not an address.
func TestPickDoHAddressSkipsNonAddressRecords(t *testing.T) {
	answers := []dohAnswer{
		{Type: 5, Data: "api.pangeavpn.org.cdn.example."},
		{Type: 1, Data: "x@evil.example"},
		{Type: 1, Data: "203.0.113.7"},
	}
	got, ok := pickDoHAddress(answers)
	if !ok || got != "203.0.113.7" {
		t.Fatalf("got (%q, %v), want the A record's address", got, ok)
	}
}

func TestPickDoHAddressRejectsAnswersWithNoAddress(t *testing.T) {
	if got, ok := pickDoHAddress([]dohAnswer{{Type: 5, Data: "alias.example."}, {Type: 1, Data: ""}}); ok {
		t.Fatalf("got %q, want no address", got)
	}
}

func TestClipForErrorBoundsAndCleansTheBody(t *testing.T) {
	got := clipForError([]byte("bad\r\nline\x00" + strings.Repeat("x", 1000)))
	if len(got) > maxErrorBodyBytes+len("…") {
		t.Fatalf("got %d bytes, want at most %d", len(got), maxErrorBodyBytes)
	}
	if strings.ContainsAny(got, "\r\n\x00") {
		t.Fatalf("control characters survived: %q", got[:20])
	}
	if short := clipForError([]byte("SUBSCRIPTION_EXPIRED")); short != "SUBSCRIPTION_EXPIRED" {
		t.Fatalf("a short body must pass through, got %q", short)
	}
}
