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
		{"40 ascii", strings.Repeat("a", 40), strings.Repeat("a", 40), true},
		{"41 ascii", strings.Repeat("a", 41), "", false},
		{"40 two-byte runes count as 40 units", strings.Repeat("é", 40), strings.Repeat("é", 40), true},
		{"20 emoji are 40 units", strings.Repeat("😀", 20), strings.Repeat("😀", 20), true},
		{"21 emoji are 42 units", strings.Repeat("😀", 21), "", false},
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
	for _, raw := range []string{"", strings.Repeat("a", 41)} {
		if _, err := validateDeviceName(raw); err == nil || !strings.Contains(err.Error(), "1-40 characters") {
			t.Fatalf("validateDeviceName(%q) = %v, want it to mention 1-40 characters", raw, err)
		}
	}
}
