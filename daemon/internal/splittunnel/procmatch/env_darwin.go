//go:build darwin

package procmatch

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func hostSettings() envSettings {
	return envSettings{resolve: resolveDarwin}
}

// resolveDarwin adds the symlink-free form and the kernel vnode path, which is what
// proc_pidpath reports (e.g. Safari inside the App cryptex).
func resolveDarwin(raw string, kind RuleKind) []string {
	p := raw
	if kind != RuleFile && p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	var out []string
	if real, err := filepath.EvalSymlinks(p); err == nil {
		out = append(out, real)
	}
	if v, ok := vnodePath(p); ok {
		out = append(out, v)
	}
	return out
}

func vnodePath(p string) (string, bool) {
	fd, err := unix.Open(p, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", false
	}
	defer unix.Close(fd)
	buf := make([]byte, unix.PathMax)
	_, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(fd), unix.F_GETPATH, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", false
	}
	return unix.ByteSliceToString(buf), true
}
