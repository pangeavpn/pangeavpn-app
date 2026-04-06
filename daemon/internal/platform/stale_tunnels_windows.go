//go:build windows

package platform

import (
	"context"
	"fmt"
	"strings"
)

// CleanupStaleTunnelArtifacts removes duplicated Windows tunnel artifacts for
// the provided tunnel names. It only resets a tunnel when Windows already has
// multiple matching artifacts or a numbered alias like "name 12", so normal
// reconnects keep reusing the same clean adapter.
func CleanupStaleTunnelArtifacts(ctx context.Context, tunnelNames []string) ([]string, error) {
	if len(normalizeTunnelNames(tunnelNames)) == 0 {
		return nil, nil
	}

	script := buildStaleTunnelCleanupScript(tunnelNames)
	output, err := runHiddenCommand(
		ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
	if err != nil {
		return nil, fmt.Errorf("powershell stale tunnel cleanup failed: %w (%s)", err, strings.TrimSpace(output))
	}

	trimmed := strings.TrimSpace(output)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil, nil
	}

	actions, parseErr := parseJSONStrings(trimmed)
	if parseErr != nil {
		return nil, fmt.Errorf("parse stale tunnel cleanup output failed: %w (%s)", parseErr, trimmed)
	}
	return actions, nil
}

func buildStaleTunnelCleanupScript(tunnelNames []string) string {
	targets := normalizeTunnelNames(tunnelNames)
	quotedTargets := make([]string, 0, len(targets))
	for _, name := range targets {
		quotedTargets = append(quotedTargets, fmt.Sprintf("'%s'", psSingleQuote(name)))
	}

	targetArray := "@()"
	if len(quotedTargets) > 0 {
		targetArray = "@(" + strings.Join(quotedTargets, ", ") + ")"
	}

	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		"$targets=" + targetArray,
		"$actions=@()",
		"function Match-PangeaTunnelTarget([string]$value) {",
		"  if ([string]::IsNullOrWhiteSpace($value)) { return $null }",
		"  $normalized=$value.Trim().ToLowerInvariant()",
		"  foreach ($target in $targets) {",
		"    if ($normalized -eq $target) { return $target }",
		"    if ($normalized -match ('^' + [regex]::Escape($target) + '\\s+\\d+$')) { return $target }",
		"  }",
		"  return $null",
		"}",
		"$adapters=@(Get-NetAdapter -IncludeHidden -ErrorAction SilentlyContinue | Where-Object {",
		"  $target=Match-PangeaTunnelTarget([string]$_.Name)",
		"  if ($null -eq $target) { return $false }",
		"  $desc=[string]$_.InterfaceDescription",
		"  if ([string]::IsNullOrWhiteSpace($desc)) { return $false }",
		"  $descLower=$desc.ToLowerInvariant()",
		"  return $descLower -like 'wireguard*' -or $descLower -like 'wintun*'",
		"})",
		"$groups=@{}",
		"foreach ($adapter in $adapters) {",
		"  $target=Match-PangeaTunnelTarget([string]$adapter.Name)",
		"  if ($null -eq $target) { continue }",
		"  if (-not $groups.ContainsKey($target)) { $groups[$target]=New-Object System.Collections.ArrayList }",
		"  [void]$groups[$target].Add($adapter)",
		"}",
		"$profileRoot='HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\NetworkList\\Profiles'",
		"$signatureRoots=@(",
		"  'HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\NetworkList\\Signatures\\Managed',",
		"  'HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\NetworkList\\Signatures\\Unmanaged'",
		")",
		"foreach ($target in $targets) {",
		"  $group=@()",
		"  if ($groups.ContainsKey($target)) { $group=@($groups[$target]) }",
		"  $hasNumbered=$false",
		"  foreach ($adapter in $group) {",
		"    $name=[string]$adapter.Name",
		"    if (-not [string]::IsNullOrWhiteSpace($name) -and $name.Trim().ToLowerInvariant() -match ('^' + [regex]::Escape($target) + '\\s+\\d+$')) {",
		"      $hasNumbered=$true",
		"      break",
		"    }",
		"  }",
		"  if (-not $hasNumbered -and $group.Count -le 1) { continue }",
		"  $removedProfileGuids=@()",
		"  if (Test-Path $profileRoot) {",
		"    foreach ($profileKey in Get-ChildItem $profileRoot -ErrorAction SilentlyContinue) {",
		"      $profileName=[string](Get-ItemProperty -LiteralPath $profileKey.PSPath -Name ProfileName -ErrorAction SilentlyContinue).ProfileName",
		"      if ((Match-PangeaTunnelTarget $profileName) -ne $target) { continue }",
		"      $removedProfileGuids += [string]$profileKey.PSChildName",
		"      try {",
		"        Remove-Item -LiteralPath $profileKey.PSPath -Recurse -Force -ErrorAction Stop",
		"        $actions += ('removed network profile ' + $profileName)",
		"      } catch { }",
		"    }",
		"  }",
		"  if ($removedProfileGuids.Count -gt 0) {",
		"    foreach ($signatureRoot in $signatureRoots) {",
		"      if (-not (Test-Path $signatureRoot)) { continue }",
		"      foreach ($signatureKey in Get-ChildItem $signatureRoot -ErrorAction SilentlyContinue) {",
		"        $profileGuid=[string](Get-ItemProperty -LiteralPath $signatureKey.PSPath -Name ProfileGuid -ErrorAction SilentlyContinue).ProfileGuid",
		"        if ([string]::IsNullOrWhiteSpace($profileGuid)) { continue }",
		"        $normalizedGuid=$profileGuid.Trim('{}').ToLowerInvariant()",
		"        $shouldRemove=$false",
		"        foreach ($removedProfileGuid in $removedProfileGuids) {",
		"          if ($normalizedGuid -eq ([string]$removedProfileGuid).Trim('{}').ToLowerInvariant()) {",
		"            $shouldRemove=$true",
		"            break",
		"          }",
		"        }",
		"        if (-not $shouldRemove) { continue }",
		"        try {",
		"          Remove-Item -LiteralPath $signatureKey.PSPath -Recurse -Force -ErrorAction Stop",
		"          $actions += ('removed network signature ' + [string]$signatureKey.PSChildName)",
		"        } catch { }",
		"      }",
		"    }",
		"  }",
		"  foreach ($adapter in ($group | Sort-Object InterfaceIndex, Name)) {",
		"    $oldName=[string]$adapter.Name",
		"    $interfaceIndex=[int]$adapter.InterfaceIndex",
		"    $newName=('PangeaVPN-Stale-' + $target + '-' + $interfaceIndex)",
		"    if ($newName.Length -gt 127) { $newName=$newName.Substring(0, 127) }",
		"    $finalName=$oldName",
		"    if (-not [string]::Equals($oldName, $newName, [System.StringComparison]::OrdinalIgnoreCase)) {",
		"      try {",
		"        Rename-NetAdapter -Name $oldName -NewName $newName -IncludeHidden -Confirm:$false -ErrorAction Stop | Out-Null",
		"        $finalName=$newName",
		"        $actions += ('renamed tunnel adapter ' + $oldName + ' -> ' + $newName)",
		"      } catch { }",
		"    }",
		"    try {",
		"      Disable-NetAdapter -Name $finalName -IncludeHidden -Confirm:$false -ErrorAction Stop | Out-Null",
		"      $actions += ('disabled tunnel adapter ' + $finalName)",
		"    } catch { }",
		"  }",
		"}",
		"@($actions | Select-Object -Unique) | ConvertTo-Json -Compress",
	}

	return strings.Join(parts, "; ")
}
