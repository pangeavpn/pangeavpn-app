package mobile

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf16"
)

// maxDeviceNameUnits is the hub's limit, counted in UTF-16 code units.
const maxDeviceNameUnits = 64

var errDeviceNameLength = errors.New("mobile: device name must be 1-64 characters")

// validateDeviceName mirrors the hub: JS trim, then 1..64 UTF-16 code units.
// Control characters are refused too, which the hub would store verbatim.
func validateDeviceName(raw string) (string, error) {
	trimmed := strings.TrimFunc(raw, isJSTrimSpace)
	units := len(utf16.Encode([]rune(trimmed)))
	if units < 1 || units > maxDeviceNameUnits {
		return "", errDeviceNameLength
	}
	if strings.IndexFunc(trimmed, unicode.IsControl) >= 0 {
		return "", errors.New("mobile: device name must not contain control characters")
	}
	if strings.IndexFunc(trimmed, isBidiControl) >= 0 {
		return "", errors.New("mobile: device name must not contain text direction controls")
	}
	return trimmed, nil
}

// isBidiControl spots the marks that can make a name read as something else.
// Not all of Cf: emoji sequences need U+200D.
func isBidiControl(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x200E || r == 0x200F || r == 0x061C
}

// isJSTrimSpace is what String.prototype.trim removes: unlike Go's TrimSpace
// it keeps U+0085 and drops the byte order mark.
func isJSTrimSpace(r rune) bool {
	return r == 0xFEFF || (r != '\u0085' && unicode.IsSpace(r))
}
