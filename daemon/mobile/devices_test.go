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

func TestRenameDeviceReturnsTheStoredName(t *testing.T) {
	_, _, h := newSignedInHub(t)
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
		{503, "DEVICE_NAMES_UNAVAILABLE"},
		{400, "BAD_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			_, _, h := newSignedInHub(t)
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
