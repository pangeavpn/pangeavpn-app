package mobile

import (
	"encoding/json"
	"errors"
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

func decodeDeviceList(t *testing.T, raw string) hubDeviceList {
	t.Helper()
	var list hubDeviceList
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return list
}

func shownNames(rows []hubDevice) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = "<nil>"
		if row.FriendlyName != nil {
			out[i] = *row.FriendlyName
		}
	}
	return strings.Join(out, "|")
}

func TestDeviceListSpeaksNamesV2OnlyWithANameKeyField(t *testing.T) {
	cases := map[string]bool{
		`{"devices":[]}`:                     false,
		`{"devices":[],"nameKey":null}`:      true,
		`{"devices":[],"nameKey":"wk1:abc"}`: true,
	}
	for raw, want := range cases {
		if got := decodeDeviceList(t, raw).speaksNamesV2(); got != want {
			t.Fatalf("%s speaks v2 = %v, want %v", raw, got, want)
		}
	}
}

func TestShownRowsFollowsTheDisplayRule(t *testing.T) {
	v := loadNamesVectors(t)
	sealed := v.SealedNames[0].Sealed
	rows := []hubDevice{
		{ID: v.DeviceID, SealedName: &sealed, Label: strPtr("Swift Fox"), FriendlyName: strPtr("Server Opened")},
		{ID: v.OtherDeviceID, SealedName: &sealed, Label: strPtr("Calm Owl"), FriendlyName: strPtr("Server Opened")},
		{ID: "d3", FriendlyName: strPtr("Legacy Name")},
		{ID: "d4", Label: strPtr(""), FriendlyName: strPtr("")},
		{ID: "5e6d7a8b-9c0d-4b8a-9c2f-3f1c9a527d4e"},
	}
	list := hubDeviceList{Devices: rows, NameKey: json.RawMessage(`"` + v.NameKey.Sealed + `"`)}
	const fallback = "Calm Owl|Legacy Name|d4|5e6d7a8b"
	cases := []struct{ credential, want string }{
		{v.AccountNumberInputs[0], "Work laptop|" + fallback},
		{"lk", "Swift Fox|" + fallback},
		{"ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ", "Swift Fox|" + fallback},
	}
	for _, tc := range cases {
		if got := shownNames(list.shownRows(tc.credential)); got != tc.want {
			t.Fatalf("%s: shown %q, want %q", tc.credential, got, tc.want)
		}
	}
	list.NameKey = json.RawMessage("null")
	if got := shownNames(list.shownRows(v.AccountNumber)); got != "Swift Fox|"+fallback {
		t.Fatalf("no key: shown %q", got)
	}
	list.NameKey = nil
	if got := shownNames(list.shownRows(v.AccountNumber)); got != "Server Opened|Server Opened|Legacy Name||<nil>" {
		t.Fatalf("old hub: shown %q", got)
	}
	if *rows[0].FriendlyName != "Server Opened" {
		t.Fatal("shownRows changed the hub's rows")
	}
}

func TestSealRenameUsesTheAccountsKey(t *testing.T) {
	v := loadNamesVectors(t)
	list := decodeDeviceList(t, `{"devices":[],"nameKey":"`+v.NameKey.Sealed+`"}`)
	body, err := list.sealRename(v.AccountNumberInputs[1], v.DeviceID, "Work laptop")
	if err != nil || body.DeviceID != v.DeviceID || body.NameKey != "" {
		t.Fatalf("body = %+v, %v", body, err)
	}
	if name, ok := openName(mustHex(t, v.NameKeyHex), v.DeviceID, body.SealedName); !ok || name != "Work laptop" {
		t.Fatalf("opened = %q, %v", name, ok)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "nameKey") || strings.Contains(string(raw), "Work laptop") {
		t.Fatalf("rename body = %s", raw)
	}
}

func TestSealRenameCreatesAKeyWhenTheAccountHasNone(t *testing.T) {
	v := loadNamesVectors(t)
	body, err := decodeDeviceList(t, `{"devices":[],"nameKey":null}`).sealRename(v.AccountNumber, v.DeviceID, "Work laptop")
	if err != nil || !strings.HasPrefix(body.NameKey, nameKeyPrefix) {
		t.Fatalf("body = %+v, %v", body, err)
	}
	k := unwrapNameKey(numberWrapKey(v.AccountNumber), body.NameKey)
	if name, ok := openName(k, v.DeviceID, body.SealedName); !ok || name != "Work laptop" {
		t.Fatalf("opened = %q, %v", name, ok)
	}
}

func TestSealRenameRefusesWhatCannotKeepTheNameSecret(t *testing.T) {
	v := loadNamesVectors(t)
	_, err := decodeDeviceList(t, `{"devices":[],"nameKey":null}`).sealRename("lk", v.DeviceID, "Phone")
	if !errors.Is(err, errRenameNeedsAccountNumber) || err.Error() != "Sign in with your account number to rename devices" {
		t.Fatalf("legacy token = %v", err)
	}
	list := decodeDeviceList(t, `{"devices":[],"nameKey":"`+v.NameKey.Sealed+`"}`)
	if _, err := list.sealRename("ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ", v.DeviceID, "Phone"); !errors.Is(err, errNameKeyUnreadable) {
		t.Fatalf("another number = %v", err)
	}
}
