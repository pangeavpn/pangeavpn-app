package snowflake

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteReadFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("wireguard datagram payload")

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

func TestWriteFrameEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, nil); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadFrame = %q, want empty", got)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	var buf bytes.Buffer
	oversized := make([]byte, 65536)
	if err := WriteFrame(&buf, oversized); err == nil {
		t.Fatal("expected error for payload > 65535 bytes")
	}
}

func TestMultipleFramesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payloads := [][]byte{[]byte("first"), []byte("second"), []byte("third")}
	for _, p := range payloads {
		if err := WriteFrame(&buf, p); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}
	for _, want := range payloads {
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("ReadFrame = %q, want %q", got, want)
		}
	}
}

func TestReadFrameTruncatedHeader(t *testing.T) {
	r := strings.NewReader("\x00")
	if _, err := ReadFrame(r); err == nil {
		t.Fatal("expected error reading truncated header")
	}
}

func TestReadFrameTruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x05}) // claims 5 bytes
	buf.WriteString("ab")         // only 2 supplied
	if _, err := ReadFrame(&buf); err == nil {
		t.Fatal("expected error reading truncated payload")
	}
}
