package mobile

import "testing"

func TestIsIPv4Literal(t *testing.T) {
	tests := map[string]bool{
		"1.2.3.4":         true,
		"0.0.0.0":         true,
		"255.255.255.255": true,
		"256.1.1.1":       false,
		"01.2.3.4":        false,
		"1.2.3":           false,
		"1.2.3.4.5":       false,
		"node.example":    false,
		"":                false,
		" 1.2.3.4":        false,
	}
	for in, want := range tests {
		if got := isIPv4Literal(in); got != want {
			t.Errorf("isIPv4Literal(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsIPLiteralAcceptsIPv6(t *testing.T) {
	for _, in := range []string{"2001:db8::1", "[2001:db8::1]", "::1", "1:2:3:4:5:6:7:8"} {
		if !isIPLiteral(in) {
			t.Errorf("isIPLiteral(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"example.com", "1:2:3", "2001:db8::g", "1::2::3"} {
		if isIPLiteral(in) {
			t.Errorf("isIPLiteral(%q) = true, want false", in)
		}
	}
}
