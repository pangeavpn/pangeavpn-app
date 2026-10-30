package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestMarkCurrentDevicesFlagsOnlyThisInstall(t *testing.T) {
	rows := []hubDevice{
		{ID: "d1", IdentityPubkey: strPtr("pub-a"), FriendlyName: strPtr("Phone"), CreatedAt: "2026-01-01", Status: "active"},
		{ID: "d2", IdentityPubkey: strPtr("pub-b"), CreatedAt: "2026-01-02", Status: "active"},
		{ID: "d3", IdentityPubkey: nil, CreatedAt: "2026-01-03", Status: "active"},
	}
	cases := []struct {
		name  string
		myPub string
		want  []bool
	}{
		{"matching key", "pub-b", []bool{false, true, false}},
		{"no identity", "", []bool{false, false, false}},
		{"unknown key", "pub-z", []bool{false, false, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := markCurrentDevices(rows, tc.myPub)
			if len(got) != len(rows) {
				t.Fatalf("got %d rows, want %d", len(got), len(rows))
			}
			for i, row := range got {
				if row.Current != tc.want[i] {
					t.Fatalf("row %s current = %v, want %v", row.ID, row.Current, tc.want[i])
				}
				if row.ID != rows[i].ID || row.CreatedAt != rows[i].CreatedAt || row.Status != rows[i].Status {
					t.Fatalf("row %d = %+v, lost fields of %+v", i, row, rows[i])
				}
			}
			if got[0].FriendlyName == nil || *got[0].FriendlyName != "Phone" || got[1].FriendlyName != nil {
				t.Fatalf("friendly names = %v, %v", got[0].FriendlyName, got[1].FriendlyName)
			}
		})
	}
}

func TestMarkCurrentDevicesGivesAnEmptyListNotNull(t *testing.T) {
	b, err := json.Marshal(markCurrentDevices(nil, "pub"))
	if err != nil || string(b) != "[]" {
		t.Fatalf("got %s, %v, want []", b, err)
	}
}

func TestDeviceJSONNeverCarriesTheIdentityKey(t *testing.T) {
	rows := []hubDevice{{ID: "d1", IdentityPubkey: strPtr("c2VjcmV0LWlkZW50aXR5"), Status: "active"}}
	b, err := json.Marshal(markCurrentDevices(rows, "c2VjcmV0LWlkZW50aXR5"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "identityPubkey") || strings.Contains(out, "c2VjcmV0LWlkZW50aXR5") {
		t.Fatalf("device JSON leaks the identity key: %s", out)
	}
	if !strings.Contains(out, `"current":true`) {
		t.Fatalf("device JSON lacks current: %s", out)
	}
}
