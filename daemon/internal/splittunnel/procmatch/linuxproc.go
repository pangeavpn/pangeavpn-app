package procmatch

import (
	"bytes"
	"path"
	"slices"
	"strings"
)

const deletedSuffix = " (deleted)"

// parseProcStat reads ppid (field 4) and starttime (field 22) from /proc/<pid>/stat; the
// command name may contain spaces and parentheses, so fields count from the last ')'.
func parseProcStat(b []byte) (ppid int, start int64, ok bool) {
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, 0, false
	}
	field := 0
	var cur int64
	inField := false
	gotPPID := false
	for _, ch := range b[i+1:] {
		if ch == ' ' || ch == '\n' {
			if inField {
				switch field {
				case 1:
					ppid, gotPPID = int(cur), true
				case 19:
					return ppid, cur, gotPPID
				}
				field++
				inField = false
				cur = 0
			}
			continue
		}
		inField = true
		if field == 1 || field == 19 {
			if ch < '0' || ch > '9' {
				return 0, 0, false
			}
			cur = cur*10 + int64(ch-'0')
		}
	}
	if inField && field == 19 {
		return ppid, cur, gotPPID
	}
	return 0, 0, false
}

// parseStatusFSUID returns the filesystem uid, the fourth value of the Uid: line.
func parseStatusFSUID(b []byte) (uint32, bool) {
	for len(b) > 0 {
		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		rest, ok := bytes.CutPrefix(line, []byte("Uid:"))
		if !ok {
			continue
		}
		f := bytes.Fields(rest)
		if len(f) < 4 {
			return 0, false
		}
		var v uint64
		for _, ch := range f[3] {
			if ch < '0' || ch > '9' {
				return 0, false
			}
			v = v*10 + uint64(ch-'0')
			if v > 1<<32-1 {
				return 0, false
			}
		}
		return uint32(v), len(f[3]) > 0
	}
	return 0, false
}

// parseSocketLink returns the inode of an fd link target "socket:[N]".
func parseSocketLink(b []byte) (uint32, bool) {
	rest, ok := bytes.CutPrefix(b, []byte("socket:["))
	if !ok || len(rest) < 2 || rest[len(rest)-1] != ']' {
		return 0, false
	}
	var v uint64
	for _, ch := range rest[:len(rest)-1] {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		v = v*10 + uint64(ch-'0')
		if v > 1<<32-1 {
			return 0, false
		}
	}
	return uint32(v), true
}

// cleanExeLink turns an exe link target into a match path: binaries replaced by an upgrade
// read "<path> (deleted)".
func cleanExeLink(target string) string {
	return strings.TrimSuffix(target, deletedSuffix)
}

// flatpakAppPath returns [Instance] app-path from a sandbox's /.flatpak-info.
func flatpakAppPath(info []byte) string {
	section := ""
	for _, line := range strings.Split(string(info), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			continue
		}
		if section != "Instance" {
			continue
		}
		if v, ok := strings.CutPrefix(line, "app-path="); ok {
			v = strings.TrimSpace(v)
			if !strings.HasPrefix(v, "/") || strings.IndexByte(v, 0) >= 0 {
				return ""
			}
			return path.Clean(v)
		}
	}
	return ""
}

// flatpakMatchPath maps a sandboxed exe (/app/...) onto the host deployment path so
// <installation>/app/<id>/ rules match across updates.
func flatpakMatchPath(exe, appPath string) string {
	rest, ok := strings.CutPrefix(exe, "/app/")
	if !ok || appPath == "" {
		return exe
	}
	return path.Clean(appPath + "/" + rest)
}

// mountFSType returns the filesystem type of the mount holding target, from a mountinfo listing:
// the deepest mount point on target's path, the last one listed if mounts are stacked.
func mountFSType(mountinfo []byte, target string) (string, bool) {
	best, fstype := -1, ""
	for _, line := range strings.Split(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		sep := slices.Index(fields, "-")
		if sep < 5 || sep+1 >= len(fields) {
			continue
		}
		mnt := unescapeMountinfo(fields[4])
		if !pathWithin(target, mnt) || len(mnt) < best {
			continue
		}
		best, fstype = len(mnt), fields[sep+1]
	}
	return fstype, best >= 0
}

func pathWithin(target, dir string) bool {
	return dir == "/" || target == dir || strings.HasPrefix(target, dir+"/")
}

// unescapeMountinfo undoes the kernel's octal escapes (\040 for a space) in a mountinfo path.
func unescapeMountinfo(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// blockingFSType reports filesystems whose reads wait on a server or a userspace daemon.
func blockingFSType(fstype string) bool {
	if fstype == "fuse" || fstype == "fuseblk" || strings.HasPrefix(fstype, "fuse.") {
		return true
	}
	switch fstype {
	case "nfs", "nfs4", "cifs", "smb3", "smbfs", "9p", "ceph", "afs", "glusterfs", "lustre":
		return true
	}
	return false
}
