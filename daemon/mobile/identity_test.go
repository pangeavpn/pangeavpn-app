//go:build android || ios

package mobile

import "testing"

func TestParseIdentityRoundTrips(t *testing.T) {
	ident := mustIdentity(t)
	got, err := parseIdentity(ident.marshal())
	if err != nil {
		t.Fatalf("parseIdentity: %v", err)
	}
	if got.publicKey() != ident.pubB64 || string(got.priv.Bytes()) != string(ident.priv.Bytes()) {
		t.Fatal("parsed identity differs from the stored one")
	}
}

func TestParseIdentityTreatsEmptyAsAbsent(t *testing.T) {
	got, err := parseIdentity("")
	if err != nil || got != nil {
		t.Fatalf("parseIdentity(\"\") = %v, %v; want nil, nil", got, err)
	}
	if got.publicKey() != "" {
		t.Fatal("a missing identity has a public key")
	}
}

func TestParseIdentityReportsCorruptValues(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":       "garbage",
		"bad base64":     `{"priv":"***","pub":"x"}`,
		"wrong key size": `{"priv":"AAAA","pub":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseIdentity(raw)
			if err == nil || got != nil {
				t.Fatalf("parseIdentity(%q) = %v, %v; want an error", raw, got, err)
			}
		})
	}
}
