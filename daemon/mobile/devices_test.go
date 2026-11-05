//go:build android || ios

package mobile

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRenameDeviceRequiresSignIn(t *testing.T) {
	initForTest(t, nil)
	if _, err := RenameDevice("d1", "Phone"); err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("RenameDevice = %v, want not authenticated", err)
	}
}

func TestRenameDeviceValidatesBeforeTheHub(t *testing.T) {
	cases := []struct{ id, name, want string }{
		{"d1", "   ", "1-40 characters"},
		{"d1", strings.Repeat("😀", 21), "1-40 characters"},
		{"  ", "Phone", "missing device id"},
	}
	for _, tc := range cases {
		initForTest(t, map[string]string{keyLicense: "lk"})
		if _, err := RenameDevice(tc.id, tc.name); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("RenameDevice(%q, %q) = %v, want %q", tc.id, tc.name, err, tc.want)
		}
		if currentHubRoute() != nil {
			t.Fatal("an invalid rename reached for the hub")
		}
	}
}

var oldHubList = map[string]any{"devices": []any{}}

func newAccountNumberHub(t *testing.T) (namesVectors, *identityKeyPair, *fakeHub) {
	t.Helper()
	v := loadNamesVectors(t)
	ident := mustIdentity(t)
	initForTest(t, map[string]string{keyLicense: v.AccountNumberInputs[0], keyIdentity: ident.marshal()})
	return v, ident, newFakeHub(t)
}

func v2HubList(nameKey any) map[string]any {
	return map[string]any{"devices": []any{}, "nameKey": nameKey}
}

func TestRenameDeviceOnAnOldHubSendsThePlainName(t *testing.T) {
	_, _, h := newSignedInHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, oldHubList))
	h.handle("/api/device/rename", replyWith(http.StatusOK, map[string]any{"ok": true, "friendlyName": "Work Phone"}))

	got, err := RenameDevice("d1", "  Work Phone \n")
	if err != nil || got != "Work Phone" {
		t.Fatalf("RenameDevice = %q, %v", got, err)
	}
	reqs := h.requestsTo("/api/device/rename")
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost {
		t.Fatalf("rename requests = %+v", reqs)
	}
	if string(reqs[0].Body) != `{"deviceId":"d1","friendlyName":"Work Phone"}` {
		t.Fatalf("body = %s", reqs[0].Body)
	}
	if reqs[0].Headers["X-License-Key"] != "lk" {
		t.Fatalf("headers = %v, want the license", reqs[0].Headers)
	}

	h.handle("/api/device/rename", replyWith(http.StatusOK, map[string]any{"ok": true}))
	if got, err := RenameDevice("d1", " Tablet "); err != nil || got != "Tablet" {
		t.Fatalf("a reply without a name = %q, %v; want the trimmed name", got, err)
	}
}

func TestRenameDeviceSurfacesHubCodes(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{404, "DEVICE_NOT_FOUND"},
		{400, "APP_UPDATE_REQUIRED"},
		{400, "BAD_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			_, _, h := newSignedInHub(t)
			h.handle("/api/device/list", replyWith(http.StatusOK, oldHubList))
			h.handle("/api/device/rename", replyCode(tc.status, tc.code))
			_, err := RenameDevice("d1", "Phone")
			if got := hubErrorCode(err); got != tc.code {
				t.Fatalf("hubErrorCode(%v) = %q, want %q", err, got, tc.code)
			}
		})
	}
}

func TestRenameDeviceOnAV2HubSendsOnlyTheSealedName(t *testing.T) {
	v, _, h := newAccountNumberHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, v2HubList(v.NameKey.Sealed)))
	h.handle("/api/device/rename", replyWith(http.StatusOK, map[string]any{"ok": true, "sealedName": "zk2:ignored"}))

	got, err := RenameDevice(v.DeviceID, "  Work Phone \n")
	if err != nil || got != "Work Phone" {
		t.Fatalf("RenameDevice = %q, %v", got, err)
	}
	reqs := h.requestsTo("/api/device/rename")
	if len(reqs) != 1 || len(h.requestsTo("/api/device/list")) != 1 {
		t.Fatalf("requests = %+v, want one list and one rename", h.requests())
	}
	body := bodyFields(t, reqs[0])
	name, ok := openName(mustHex(t, v.NameKeyHex), v.DeviceID, body["sealedName"])
	if len(body) != 2 || body["deviceId"] != v.DeviceID || !ok || name != "Work Phone" {
		t.Fatalf("body = %s", reqs[0].Body)
	}
	if strings.Contains(string(reqs[0].Body), "Work Phone") {
		t.Fatal("the plaintext name reached the hub")
	}
}

func TestRenameDeviceCreatesTheNameKeyWhenTheAccountHasNone(t *testing.T) {
	v, _, h := newAccountNumberHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, v2HubList(nil)))
	h.handle("/api/device/rename", replyWith(http.StatusOK, map[string]any{"ok": true}))

	if _, err := RenameDevice(v.DeviceID, "Work Phone"); err != nil {
		t.Fatalf("RenameDevice: %v", err)
	}
	body := bodyFields(t, h.requestsTo("/api/device/rename")[0])
	k := unwrapNameKey(numberWrapKey(v.AccountNumber), body["nameKey"])
	if name, ok := openName(k, v.DeviceID, body["sealedName"]); len(body) != 3 || !ok || name != "Work Phone" {
		t.Fatalf("body = %v", body)
	}
}

func TestRenameDeviceRetriesOnceWhenAnotherDeviceMadeTheKey(t *testing.T) {
	v, _, h := newAccountNumberHub(t)
	var lists, renames atomic.Int32
	h.handle("/api/device/list", func(fakeRequest) (int, any) {
		if lists.Add(1) == 1 {
			return http.StatusOK, v2HubList(nil)
		}
		return http.StatusOK, v2HubList(v.NameKey.Sealed)
	})
	h.handle("/api/device/rename", func(req fakeRequest) (int, any) {
		if renames.Add(1) == 1 {
			return replyCode(http.StatusConflict, "NAME_KEY_EXISTS")(req)
		}
		return http.StatusOK, map[string]any{"ok": true}
	})

	if got, err := RenameDevice(v.DeviceID, "Work Phone"); err != nil || got != "Work Phone" {
		t.Fatalf("RenameDevice = %q, %v", got, err)
	}
	reqs := h.requestsTo("/api/device/rename")
	if len(reqs) != 2 || lists.Load() != 2 {
		t.Fatalf("renames = %d, lists = %d; want two of each", len(reqs), lists.Load())
	}
	if _, sent := bodyFields(t, reqs[0])["nameKey"]; !sent {
		t.Fatalf("first body = %s, want a new key", reqs[0].Body)
	}
	retry := bodyFields(t, reqs[1])
	name, ok := openName(mustHex(t, v.NameKeyHex), v.DeviceID, retry["sealedName"])
	if _, sent := retry["nameKey"]; sent || !ok || name != "Work Phone" {
		t.Fatalf("retry body = %s, want the name under the account's key", reqs[1].Body)
	}
}

func TestRenameDeviceRetriesOnlyOnce(t *testing.T) {
	v, _, h := newAccountNumberHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, v2HubList(nil)))
	h.handle("/api/device/rename", replyCode(http.StatusConflict, "NAME_KEY_EXISTS"))

	if _, err := RenameDevice(v.DeviceID, "Work Phone"); hubErrorCode(err) != "NAME_KEY_EXISTS" {
		t.Fatalf("RenameDevice = %v, want the conflict", err)
	}
	if n := len(h.requestsTo("/api/device/rename")); n != 2 {
		t.Fatalf("%d renames, want 2", n)
	}
}

func TestRenameDeviceAsksALegacyTokenForTheAccountNumber(t *testing.T) {
	_, _, h := newSignedInHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, v2HubList(nil)))
	if _, err := RenameDevice("d1", "Phone"); err == nil || err.Error() != "Sign in with your account number to rename devices" {
		t.Fatalf("RenameDevice = %v, want the account-number error", err)
	}
	if len(h.requestsTo("/api/device/rename")) != 0 {
		t.Fatal("a legacy-token rename reached the hub")
	}
}

func TestListDevicesMarksThisDevice(t *testing.T) {
	_, ident, h := newSignedInHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, map[string]any{"devices": []map[string]any{
		{"id": "d1", "identityPubkey": "someone-else", "friendlyName": "Laptop", "createdAt": "2026-01-01", "status": "active"},
		{"id": "d2", "identityPubkey": ident.pubB64, "friendlyName": nil, "createdAt": "2026-02-01", "status": "active"},
	}}))

	raw, err := ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	var devices []deviceInfo
	if err := json.Unmarshal([]byte(raw), &devices); err != nil || len(devices) != 2 {
		t.Fatalf("ListDevices = %s (%v)", raw, err)
	}
	if devices[0].Current || !devices[1].Current {
		t.Fatalf("current flags = %v, %v; want only d2", devices[0].Current, devices[1].Current)
	}
	if strings.Contains(raw, "identityPubkey") || strings.Contains(raw, ident.pubB64) || strings.Contains(raw, "someone-else") {
		t.Fatalf("ListDevices leaks identity keys: %s", raw)
	}
}

func TestListDevicesWithoutAnIdentityMarksNothing(t *testing.T) {
	initForTest(t, map[string]string{keyLicense: "lk"})
	h := newFakeHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, map[string]any{"devices": []map[string]any{
		{"id": "d1", "identityPubkey": "", "createdAt": "2026-01-01", "status": "active"},
	}}))

	raw, err := ListDevices()
	if err != nil || strings.Contains(raw, `"current":true`) {
		t.Fatalf("ListDevices = %s, %v", raw, err)
	}
}

func TestListDevicesShowsV2NamesInFriendlyName(t *testing.T) {
	v, ident, h := newAccountNumberHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, map[string]any{"nameKey": v.NameKey.Sealed, "devices": []map[string]any{
		{"id": v.DeviceID, "identityPubkey": ident.pubB64, "friendlyName": "Server Opened", "label": "Swift Fox", "sealedName": v.SealedNames[1].Sealed, "createdAt": "2026-01-01", "status": "active"},
		{"id": v.OtherDeviceID, "friendlyName": "Calm Owl", "label": "Calm Owl", "sealedName": nil, "createdAt": "2026-02-01", "status": "active"},
		{"id": "5e6d7a8b-9c0d-4b8a-9c2f-3f1c9a527d4e", "friendlyName": nil, "label": nil, "sealedName": nil, "createdAt": "2026-03-01", "status": "active"},
	}}))

	raw, err := ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	var devices []deviceInfo
	if err := json.Unmarshal([]byte(raw), &devices); err != nil || len(devices) != 3 {
		t.Fatalf("ListDevices = %s (%v)", raw, err)
	}
	if *devices[0].FriendlyName != v.SealedNames[1].Plain || *devices[1].FriendlyName != "Calm Owl" || *devices[2].FriendlyName != "5e6d7a8b" {
		t.Fatalf("ListDevices = %s", raw)
	}
	if !devices[0].Current || strings.Contains(raw, "zk2:") || strings.Contains(raw, "sealedName") || strings.Contains(raw, "label") {
		t.Fatalf("ListDevices = %s", raw)
	}
}

func TestListDevicesShowsLabelsToALegacyToken(t *testing.T) {
	v := loadNamesVectors(t)
	_, _, h := newSignedInHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, map[string]any{"nameKey": v.NameKey.Sealed, "devices": []map[string]any{
		{"id": v.DeviceID, "friendlyName": "Server Opened", "label": "Swift Fox", "sealedName": v.SealedNames[0].Sealed, "createdAt": "2026-01-01", "status": "active"},
	}}))

	raw, err := ListDevices()
	if err != nil || !strings.Contains(raw, `"friendlyName":"Swift Fox"`) {
		t.Fatalf("ListDevices = %s, %v", raw, err)
	}
}

func TestHubRequestsCarryTheAppHeader(t *testing.T) {
	platform := strings.ToLower(platformName)
	for version, want := range map[string]string{"": platform + "/unknown", " 1.4.0 ": platform + "/1.4.0"} {
		t.Run(want, func(t *testing.T) {
			SetClientInfo(version, "")
			t.Cleanup(func() { SetClientInfo("", "") })
			_, _, h := newSignedInHub(t)
			h.handle("/api/device/list", replyWith(http.StatusOK, oldHubList))
			if _, err := ListDevices(); err != nil {
				t.Fatalf("ListDevices: %v", err)
			}
			if got := h.requestsTo("/api/device/list")[0].Headers["X-Pangea-App"]; got != want {
				t.Fatalf("X-Pangea-App = %q, want %q", got, want)
			}
		})
	}
}
