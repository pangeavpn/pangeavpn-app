package mobile

import "testing"

func TestIsUtunName(t *testing.T) {
	cases := map[string]bool{
		"utun3":  true,
		"utun12": true,
		"utun":   false,
		"en0":    false,
		"":       false,
		"utunX":  false,
		"utun1x": false,
	}
	for name, want := range cases {
		if got := isUtunName(name); got != want {
			t.Errorf("isUtunName(%q) = %v, want %v", name, got, want)
		}
	}
}
