//go:build windows

package platform

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestBuildWindowsUninstallScript(t *testing.T) {
	script := buildWindowsUninstallScript(1234)
	for _, want := range []string{
		"Wait-Process -Id 1234",
		"sc.exe stop PangeaDaemon",
		"sc.exe delete PangeaDaemon",
		"DisplayName -eq 'PangeaVPN'",
		"ArgumentList '/S' -Wait",
		"ProgramData\\PangeaVPN",
		"AppUserModelId\\com.pangea.pangeavpn",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

func TestBuildWindowsUninstallScriptNoPID(t *testing.T) {
	if strings.Contains(buildWindowsUninstallScript(0), "Wait-Process") {
		t.Error("a non-positive pid must not emit a Wait-Process line")
	}
}

func TestEncodePowerShellRoundTrips(t *testing.T) {
	const in = "Remove-Item 'x'"
	raw, err := base64.StdEncoding.DecodeString(encodePowerShell(in))
	if err != nil {
		t.Fatalf("not base64: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	if got := string(utf16.Decode(units)); got != in {
		t.Errorf("round-trip = %q, want %q", got, in)
	}
}
