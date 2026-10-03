//go:build windows

package procmatch

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// GetFinalPathNameByHandleW flags; x/sys does not name them.
const (
	fileNameNormalized = 0x0
	volumeNameDOS      = 0x0
)

func hostSettings() envSettings {
	s := envSettings{resolve: resolveWindows}
	for _, v := range []string{"SystemRoot", "windir"} {
		if p := os.Getenv(v); p != "" {
			s.systemRoots = append(s.systemRoots, p)
		}
	}
	for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramFiles(Arm)", "ProgramW6432", "ProgramData"} {
		if p := os.Getenv(v); p != "" {
			s.programDirs = append(s.programDirs, p)
		}
	}
	if d := os.Getenv("SystemDrive"); d != "" {
		s.usersRoots = append(s.usersRoots, d+`\Users`)
	}
	if p := os.Getenv("PUBLIC"); p != "" {
		s.usersRoots = append(s.usersRoots, filepath.Dir(p))
	}
	return s
}

func resolveWindows(raw string, kind RuleKind) []string {
	p := raw
	if kind != RuleFile && len(p) > len(`c:\`) {
		p = strings.TrimSuffix(p, `\`)
	}
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return nil
	}
	h, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(h)
	if final, ok := finalPath(h); ok {
		return []string{final}
	}
	return nil
}

func finalPath(h windows.Handle) (string, bool) {
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameDOS)
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			return windows.UTF16ToString(buf[:n]), true
		}
		if n > 32768 {
			return "", false
		}
		buf = make([]uint16, n+1)
	}
}
