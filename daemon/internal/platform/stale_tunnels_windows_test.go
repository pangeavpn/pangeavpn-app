//go:build windows

package platform

import (
	"strings"
	"testing"
)

func TestBuildStaleTunnelCleanupScript_TargetedReset(t *testing.T) {
	script := buildStaleTunnelCleanupScript([]string{"vps-1"})
	if script == "" {
		t.Fatal("expected non-empty powershell script")
	}

	for _, snippet := range []string{
		"$targets=@('vps-1')",
		"Match-PangeaTunnelTarget",
		"^' + [regex]::Escape($target) + '\\s+\\d+$'",
		"Get-NetAdapter -IncludeHidden",
		"Rename-NetAdapter -Name $oldName -NewName $newName -IncludeHidden",
		"Disable-NetAdapter -Name $finalName -IncludeHidden",
		"HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\NetworkList\\Profiles",
		"NetworkList\\Signatures\\Managed",
		"NetworkList\\Signatures\\Unmanaged",
		"if (-not $hasNumbered -and $group.Count -le 1) { continue }",
		"$descLower -like 'wireguard*' -or $descLower -like 'wintun*'",
	} {
		if !strings.Contains(script, snippet) {
			t.Fatalf("expected script to contain %q, got: %s", snippet, script)
		}
	}
}

func TestBuildStaleTunnelCleanupScript_NormalizesTargets(t *testing.T) {
	script := buildStaleTunnelCleanupScript([]string{" VPS-1 ", "vps-1", "prod_tunnel"})

	if strings.Count(script, "'vps-1'") != 1 {
		t.Fatalf("expected normalized target list to contain vps-1 once, got: %s", script)
	}
	if !strings.Contains(script, "'prod_tunnel'") {
		t.Fatalf("expected normalized target list to contain prod_tunnel, got: %s", script)
	}
}
