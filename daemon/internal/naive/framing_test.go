package naive

import (
	"bytes"
	"testing"
)

func TestWriteFrameThenReadFrameRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("wireguard datagram bytes")

	if err := WriteFrame(&buf, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("ReadFrame = %q, want %q", got, payload)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	var buf bytes.Buffer
	oversized := make([]byte, 65536) // one over the 2-byte length prefix's max

	if err := WriteFrame(&buf, oversized); err == nil {
		t.Fatalf("WriteFrame should reject a 65536-byte payload, got nil error")
	}
}
