//go:build windows

package wg

import (
	"encoding/binary"
	"hash"
	"strings"

	"golang.org/x/crypto/blake2s"
	"golang.org/x/sys/windows"
	"golang.org/x/text/unicode/norm"
)

const windowsRequestedGUIDLabel = "PangeaVPN WireGuard Windows Requested GUID v2"

func requestedWindowsTunnelGUID(tunnelName string) *windows.GUID {
	b2, _ := blake2s.New256(nil)
	b2.Write([]byte(windowsRequestedGUIDLabel))
	writeWindowsGUIDHashString(b2, strings.TrimSpace(tunnelName))

	sum := b2.Sum(nil)
	return &windows.GUID{
		Data1: binary.LittleEndian.Uint32(sum[0:4]),
		Data2: binary.LittleEndian.Uint16(sum[4:6]),
		Data3: binary.LittleEndian.Uint16(sum[6:8]),
		Data4: [8]byte{sum[8], sum[9], sum[10], sum[11], sum[12], sum[13], sum[14], sum[15]},
	}
}

func writeWindowsGUIDHashString(h hash.Hash, value string) {
	bytes := norm.NFC.Bytes([]byte(value))
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(bytes)))
	h.Write(size[:])
	h.Write(bytes)
}
