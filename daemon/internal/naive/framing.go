package naive

import (
	"encoding/binary"
	"fmt"
	"io"
)

// WriteFrame writes payload as a [2-byte big-endian length][payload] frame,
// the wire contract this design shares with the node-side bridge (see
// docs/superpowers/specs/2026-07-18-naiveproxy-transport-design.md, "Wire
// contract"). Max payload is 65535 bytes, well above WireGuard's ~1420-byte
// typical datagram size.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > 65535 {
		return fmt.Errorf("naive: frame payload too large: %d bytes (max 65535)", len(payload))
	}
	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("naive: write frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("naive: write frame payload: %w", err)
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
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("naive: read frame payload: %w", err)
	}
	return payload, nil
}
