//go:build windows

package platform

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// Detached cleanup under the daemon's LocalSystem token — no UAC prompt. Waits
// for the GUI to exit, then removes the service, app, and all daemon state.
func SpawnSelfUninstall(appPID int) error {
	script := buildWindowsUninstallScript(appPID)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: detachedProcess | createNewProcessGroup}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn self-uninstall: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func buildWindowsUninstallScript(appPID int) string {
	wait := ""
	if appPID > 0 {
		wait = fmt.Sprintf("try { Wait-Process -Id %d -Timeout 30 } catch {}", appPID)
	}
	return strings.Join([]string{
		"$ErrorActionPreference='SilentlyContinue'",
		wait,
		"$state = Join-Path $env:SystemDrive 'ProgramData\\PangeaVPN'",
		"& sc.exe stop PangeaDaemon | Out-Null",
		"for ($i=0; $i -lt 60; $i++) { $q=(& sc.exe query PangeaDaemon 2>$null | Out-String); if (($q -eq '') -or ($q -match 'STOPPED') -or ($q -match '1060')) { break }; Start-Sleep -Milliseconds 500 }",
		"& sc.exe delete PangeaDaemon | Out-Null",
		"$keys=@('HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall','HKLM:\\SOFTWARE\\WOW6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall')",
		"$u=Get-ChildItem $keys -ErrorAction SilentlyContinue | ForEach-Object { Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue } | Where-Object { $_.DisplayName -eq 'PangeaVPN' } | Select-Object -First 1",
		"if ($u -and $u.UninstallString) { $exe=($u.UninstallString -replace '\"','').Trim(); if (Test-Path -LiteralPath $exe) { Start-Process -FilePath $exe -ArgumentList '/S' -Wait } }",
		"Remove-Item -LiteralPath $state -Recurse -Force -ErrorAction SilentlyContinue",
		"Remove-Item -LiteralPath 'HKLM:\\Software\\Classes\\AppUserModelId\\com.pangea.pangeavpn' -Recurse -Force -ErrorAction SilentlyContinue",
	}, "\n")
}

func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
