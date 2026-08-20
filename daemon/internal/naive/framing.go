package naive

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxSaneFramePayload catches a desynced stream (e.g. plaintext HTTP from a
// failed CONNECT) fast instead of blocking on a length that never arrives;
// real payloads are WireGuard datagrams, ~1420 bytes.
const maxSaneFramePayload = 4096

// WriteFrame writes one [2-byte big-endian length][payload] frame in a single
// Write call. Callers must serialize writes; concurrent use corrupts the stream.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > 65535 {
		return fmt.Errorf("naive: frame payload too large: %d bytes (max 65535)", len(payload))
	}
	frame := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(payload)))
	copy(frame[2:], payload)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("naive: write frame: %w", err)
	}
	return nil
}

// ReadFrame reads one [2-byte big-endian length][payload] frame.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("naive: read frame header: %w", err)
	}
	length := binary.BigEndian.Uint16(header[:])
	if length > maxSaneFramePayload {
		return nil, fmt.Errorf("naive: frame length %d exceeds sane max %d bytes; stream likely desynced", length, maxSaneFramePayload)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("naive: read frame payload: %w", err)
	}
	return payload, nil
}
