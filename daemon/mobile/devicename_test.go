package mobile

import (
	"strings"
	"testing"
)

func TestValidateDeviceNameMatchesTheHub(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"trims like JS", " Work Phone ", "Work Phone", true},
		{"trims tabs and newlines", "\tPhone\n", "Phone", true},
		{"empty", "", "", false},
		{"blank", "   ", "", false},
		{"64 ascii", strings.Repeat("a", 64), strings.Repeat("a", 64), true},
		{"65 ascii", strings.Repeat("a", 65), "", false},
		{"64 two-byte runes count as 64 units", strings.Repeat("é", 64), strings.Repeat("é", 64), true},
		{"32 emoji are 64 units", strings.Repeat("😀", 32), strings.Repeat("😀", 32), true},
		{"33 emoji are 66 units", strings.Repeat("😀", 33), "", false},
		{"C0 control", "a\x00b", "", false},
		{"C1 control", "a\u0085b", "", false},
		{"DEL", "a\x7fb", "", false},
		{"right-to-left override", "Phone‮gpj.exe", "", false},
		{"left-to-right embedding", "a‪b", "", false},
		{"pop directional formatting", "a‬b", "", false},
		{"right-to-left isolate", "a⁧b", "", false},
		{"pop directional isolate", "a⁩b", "", false},
		{"left-to-right mark", "a‎b", "", false},
		{"right-to-left mark", "a‏b", "", false},
		{"arabic letter mark", "a؜b", "", false},
		{"ZWJ family emoji", "Family \U0001F468‍\U0001F469‍\U0001F467", "Family \U0001F468‍\U0001F469‍\U0001F467", true},
		{"zero-width non-joiner", "a‌b", "a‌b", true},
		{"arabic name", "هاتف", "هاتف", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateDeviceName(tc.raw)
			if tc.ok {
				if err != nil || got != tc.want {
					t.Fatalf("got %q, %v, want %q", got, err, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("got %q, want an error", got)
			}
		})
	}
}

func TestValidateDeviceNameExplainsTheLimit(t *testing.T) {
	for _, raw := range []string{"", strings.Repeat("a", 65)} {
		if _, err := validateDeviceName(raw); err == nil || !strings.Contains(err.Error(), "1-64 characters") {
			t.Fatalf("validateDeviceName(%q) = %v, want it to mention 1-64 characters", raw, err)
		}
	}
}
