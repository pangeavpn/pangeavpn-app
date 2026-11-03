//go:build android || ios

package mobile

import (
	"encoding/json"
	"net/http"
	"strings"
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
		{"d1", "   ", "1-64 characters"},
		{"d1", strings.Repeat("😀", 33), "1-64 characters"},
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

func newAccountNumberHub(t *testing.T) (*identityKeyPair, *fakeHub) {
	t.Helper()
	ident := mustIdentity(t)
	initForTest(t, map[string]string{keyLicense: zk1Credential, keyIdentity: ident.marshal()})
	return ident, newFakeHub(t)
}

func TestRenameDeviceSendsOnlyTheSealedName(t *testing.T) {
	_, h := newAccountNumberHub(t)
	h.handle("/api/device/rename", replyWith(http.StatusOK, map[string]any{"ok": true, "encryptedName": "zk1:ignored"}))

	got, err := RenameDevice("d1", "  Work Phone \n")
	if err != nil || got != "Work Phone" {
		t.Fatalf("RenameDevice = %q, %v", got, err)
	}
	reqs := h.requestsTo("/api/device/rename")
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost {
		t.Fatalf("rename requests = %+v", reqs)
	}
	body := bodyFields(t, reqs[0])
	name, ok := openDeviceName(deviceNameKey(zk1Credential), body["encryptedName"])
	if len(body) != 2 || body["deviceId"] != "d1" || !ok || name != "Work Phone" {
		t.Fatalf("body = %s", reqs[0].Body)
	}
	if strings.Contains(string(reqs[0].Body), "Work Phone") {
		t.Fatal("the plaintext name reached the hub")
	}
}

func TestRenameDeviceRefusesALegacyToken(t *testing.T) {
	_, _, h := newSignedInHub(t)
	if _, err := RenameDevice("d1", "Phone"); err == nil || !strings.Contains(err.Error(), "Sign in with your account number") {
		t.Fatalf("RenameDevice = %v, want the account-number error", err)
	}
	if len(h.requestsTo("/api/device/rename")) != 0 {
		t.Fatal("a legacy-token rename reached the hub")
	}
}

func TestRenameDeviceSurfacesHubCodes(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{404, "DEVICE_NOT_FOUND"},
		{400, "DEVICE_NAME_NOT_ENCRYPTED"},
		{400, "BAD_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			_, h := newAccountNumberHub(t)
			h.handle("/api/device/rename", replyCode(tc.status, tc.code))
			_, err := RenameDevice("d1", "Phone")
			if got := hubErrorCode(err); got != tc.code {
				t.Fatalf("hubErrorCode(%v) = %q, want %q", err, got, tc.code)
			}
		})
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

func TestListDevicesOpensEncryptedNames(t *testing.T) {
	_, h := newAccountNumberHub(t)
	h.handle("/api/device/list", replyWith(http.StatusOK, map[string]any{"devices": []map[string]any{
		{"id": "d1", "friendlyName": nil, "encryptedName": zk1Sealed, "createdAt": "2026-01-01", "status": "active"},
		{"id": "d2", "friendlyName": "Swift Fox", "encryptedName": nil, "createdAt": "2026-02-01", "status": "active"},
	}}))

	raw, err := ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	var devices []deviceInfo
	if err := json.Unmarshal([]byte(raw), &devices); err != nil || len(devices) != 2 {
		t.Fatalf("ListDevices = %s (%v)", raw, err)
	}
	if *devices[0].FriendlyName != zk1Plaintext || *devices[1].FriendlyName != "Swift Fox" || strings.Contains(raw, "zk1:") {
		t.Fatalf("ListDevices = %s", raw)
	}
}

func TestLoginSealsTheNameOnlyForAnAccountNumber(t *testing.T) {
	for _, credential := range []string{zk1Credential, "lk"} {
		t.Run(credential, func(t *testing.T) {
			initForTest(t, nil)
			h := newFakeHub(t)
			h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{"vpnAccessToken": credential, "user": map[string]string{}}))
			h.handle("/api/device/register", replyWith(http.StatusOK, map[string]string{"deviceId": "d1", "assignedIp": "10.0.0.2"}))

			if _, err := Login(credential); err != nil {
				t.Fatalf("Login: %v", err)
			}
			body := bodyFields(t, h.requestsTo("/api/device/register")[0])
			if _, plain := body["friendlyName"]; plain {
				t.Fatalf("register sent a plaintext name: %v", body)
			}
			name, ok := openDeviceName(deviceNameKey(zk1Credential), body["encryptedName"])
			if credential == zk1Credential && (!ok || name != platformName) {
				t.Fatalf("register body = %v, want the sealed platform name", body)
			}
			if _, sealed := body["encryptedName"]; credential == "lk" && sealed {
				t.Fatalf("a legacy token sealed a name: %v", body)
			}
		})
	}
}
