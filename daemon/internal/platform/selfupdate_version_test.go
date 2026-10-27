package platform

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.8.2", "0.8.1", 1},
		{"0.8.1", "0.8.2", -1},
		{"0.8.2", "0.8.2", 0},
		{"v0.8.2", "0.8.2", 0},
		{"0.8.2+build.7", "0.8.2", 0},
		{"0.10.0", "0.9.9", 1},
		{"1.0", "1.0.0", 0},
		{"0.8.2-rc.1", "0.8.2", -1},
		{"0.8.2", "0.8.2-rc.1", 1},
		{"0.8.2-rc.2", "0.8.2-rc.1", 1},
		{"0.8.2-rc.10", "0.8.2-rc.9", 1},
		{"0.8.2-rc.1", "0.8.1", 1},
		{"0.8.2-beta", "0.8.2-alpha", 1},
		{"0.8.2-rc.1", "0.8.2-rc", 1},
		{"0.8.2-1", "0.8.2-rc", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
