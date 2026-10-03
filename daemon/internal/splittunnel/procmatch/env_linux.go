//go:build linux

package procmatch

import (
	"os"
	"path/filepath"
	"strings"
)

func hostSettings() envSettings {
	s := envSettings{resolve: resolveSymlinks}
	if exe, err := os.Readlink("/proc/1/exe"); err == nil {
		s.initExe = strings.TrimSuffix(exe, " (deleted)")
	}
	return s
}

// resolveSymlinks leaves snap rules alone: their symlinks end at /usr/bin/snap.
func resolveSymlinks(raw string, kind RuleKind) []string {
	if strings.HasPrefix(raw, "/snap/") {
		return nil
	}
	p := raw
	if kind != RuleFile && p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil
	}
	return []string{real}
}
