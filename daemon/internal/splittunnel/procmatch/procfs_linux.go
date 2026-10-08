//go:build linux

package procmatch

import (
	"encoding/binary"
	"errors"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// procFS reads a /proc tree (or a fake one in tests) relative to an open root directory.
type procFS struct {
	root   string
	rootfd int
	dents  []byte
	fdents []byte
	buf    []byte
	link   []byte
	// mntNS is the daemon's own mount namespace ("" when unreadable, which skips sameImage).
	mntNS string

	fdReads   uint64
	statReads uint64
}

func openProcFS(root string) (*procFS, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	p := &procFS{
		root:   root,
		rootfd: fd,
		dents:  make([]byte, 32<<10),
		fdents: make([]byte, 16<<10),
		buf:    make([]byte, 4<<10),
		link:   make([]byte, unix.PathMax),
	}
	if n, err := unix.Readlinkat(fd, "self/ns/mnt", p.link); err == nil {
		p.mntNS = string(p.link[:n])
	}
	return p, nil
}

func (p *procFS) close() {
	if p.rootfd >= 0 {
		unix.Close(p.rootfd)
		p.rootfd = -1
	}
}

// goneErr reports errors meaning the process behind a /proc entry no longer exists.
func goneErr(err error) bool {
	return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH)
}

// listPIDs appends the numeric entries of the root directory to pids.
func (p *procFS) listPIDs(pids []int) ([]int, error) {
	if _, err := unix.Seek(p.rootfd, 0, 0); err != nil {
		return pids, err
	}
	for {
		n, err := unix.Getdents(p.rootfd, p.dents)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return pids, err
		}
		if n <= 0 {
			return pids, nil
		}
		walkDirents(p.dents[:n], func(name []byte) {
			if pid, ok := atoiBytes(name); ok && pid > 0 {
				pids = append(pids, pid)
			}
		})
	}
}

// walkDirents calls fn with each linux_dirent64 name; the slice ends before the NUL.
func walkDirents(b []byte, fn func(name []byte)) {
	for len(b) >= 19 {
		reclen := int(binary.NativeEndian.Uint16(b[16:]))
		if reclen < 19 || reclen > len(b) {
			return
		}
		name := b[19:reclen]
		for i, c := range name {
			if c == 0 {
				name = name[:i]
				break
			}
		}
		fn(name)
		b = b[reclen:]
	}
}

func atoiBytes(b []byte) (int, bool) {
	if len(b) == 0 || len(b) > 10 {
		return 0, false
	}
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func pidName(pid int, file string) string {
	if file == "" {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + "/" + file
}

// readAt reads a small file below dirfd into p.buf, growing it up to limit.
func (p *procFS) readAt(dirfd int, name string, limit int) ([]byte, error) {
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	return p.readFD(fd, limit)
}

// readRegular reads a file a process controls. Only a regular file outside /proc and /sys is
// opened: opening a FIFO or a device, or reading a kernel file, can block or have side effects.
func (p *procFS) readRegular(dirfd int, name string, limit int) ([]byte, error) {
	pfd, err := unix.Openat(dirfd, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(pfd)
	var st unix.Stat_t
	if err := unix.Fstat(pfd, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, unix.EINVAL
	}
	var sfs unix.Statfs_t
	if err := unix.Fstatfs(pfd, &sfs); err != nil {
		return nil, err
	}
	switch sfs.Type {
	case unix.PROC_SUPER_MAGIC, unix.SYSFS_MAGIC, unix.DEBUGFS_MAGIC, unix.TRACEFS_MAGIC:
		return nil, unix.EINVAL
	}
	// Reopening the O_PATH descriptor reaches the inode just checked, whatever the path holds now.
	fd, err := unix.Open("/proc/self/fd/"+strconv.Itoa(pfd), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	return p.readFD(fd, limit)
}

func (p *procFS) readFD(fd, limit int) ([]byte, error) {
	n := 0
	for {
		if n == len(p.buf) {
			if len(p.buf) >= limit {
				return p.buf[:n], nil
			}
			nb := make([]byte, min(2*len(p.buf), limit))
			copy(nb, p.buf[:n])
			p.buf = nb
		}
		m, err := unix.Read(fd, p.buf[n:])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		if m == 0 {
			return p.buf[:n], nil
		}
		n += m
	}
}

// stat reads ppid and start time through dirfd (a pinned /proc/<pid>) or the root.
func (p *procFS) stat(dirfd, pid int) (ppid int, start int64, err error) {
	p.statReads++
	name := "stat"
	if dirfd < 0 {
		dirfd, name = p.rootfd, pidName(pid, "stat")
	}
	b, err := p.readAt(dirfd, name, 4<<10)
	if err != nil {
		return 0, 0, err
	}
	ppid, start, ok := parseProcStat(b)
	if !ok {
		return 0, 0, unix.EINVAL
	}
	return ppid, start, nil
}

func (p *procFS) fsuid(dirfd, pid int) (uint32, error) {
	name := "status"
	if dirfd < 0 {
		dirfd, name = p.rootfd, pidName(pid, "status")
	}
	b, err := p.readAt(dirfd, name, 16<<10)
	if err != nil {
		return 0, err
	}
	uid, ok := parseStatusFSUID(b)
	if !ok {
		return 0, unix.EINVAL
	}
	return uid, nil
}

// exeLink reads the exe link, which names the image without touching the image's filesystem
// (a stat through it can hang on a dead NFS or FUSE server).
func (p *procFS) exeLink(dirfd, pid int) (string, error) {
	name := "exe"
	if dirfd < 0 {
		dirfd, name = p.rootfd, pidName(pid, "exe")
	}
	n, err := unix.Readlinkat(dirfd, name, p.link)
	if err != nil {
		return "", err
	}
	return string(p.link[:n]), nil
}

// matchPath maps an exe link onto the match path: " (deleted)" stripped, and a flatpak
// sandbox's /app/... mapped onto its host deployment.
func (p *procFS) matchPath(dirfd, pid int, link string) string {
	exe := cleanExeLink(link)
	if len(exe) > 5 && exe[:5] == "/app/" && !p.flatpakInfoBlocks(dirfd, pid) {
		name := "root/.flatpak-info"
		at := dirfd
		if at < 0 {
			at, name = p.rootfd, pidName(pid, name)
		}
		if info, err := p.readRegular(at, name, 64<<10); err == nil {
			exe = flatpakMatchPath(exe, flatpakAppPath(info))
		}
	}
	// The exe link and /.flatpak-info are both the process's own view: another mount namespace
	// can show any binary at an excluded app's path, so there the path must be that very file.
	if p.foreignMountNS(dirfd, pid) && !p.sameImage(dirfd, pid, exe) {
		return ""
	}
	return exe
}

func (p *procFS) foreignMountNS(dirfd, pid int) bool {
	if p.mntNS == "" {
		return false
	}
	name := "ns/mnt"
	if dirfd < 0 {
		dirfd, name = p.rootfd, pidName(pid, name)
	}
	n, err := unix.Readlinkat(dirfd, name, p.link)
	return err != nil || string(p.link[:n]) != p.mntNS
}

// sameImage reports whether the process's image is the file at hostPath in this namespace.
// Cached attributes only, so neither stat can wait on a dead NFS or FUSE server.
func (p *procFS) sameImage(dirfd, pid int, hostPath string) bool {
	if hostPath == "" {
		return false
	}
	name := "exe"
	if dirfd < 0 {
		dirfd, name = p.rootfd, pidName(pid, name)
	}
	var img, host unix.Statx_t
	if unix.Statx(dirfd, name, unix.AT_STATX_DONT_SYNC, unix.STATX_INO, &img) != nil {
		return false
	}
	if unix.Statx(unix.AT_FDCWD, hostPath, unix.AT_STATX_DONT_SYNC, unix.STATX_INO, &host) != nil {
		return false
	}
	return img.Ino == host.Ino && img.Dev_major == host.Dev_major && img.Dev_minor == host.Dev_minor
}

// flatpakInfoBlocks reports a /.flatpak-info on a FUSE or network mount, whose read could wait
// forever under the classifier lock; a real sandbox writes it to a local filesystem.
func (p *procFS) flatpakInfoBlocks(dirfd, pid int) bool {
	name := "mountinfo"
	if dirfd < 0 {
		dirfd, name = p.rootfd, pidName(pid, name)
	}
	b, err := p.readAt(dirfd, name, 1<<20)
	if err != nil {
		return !errors.Is(err, unix.ENOENT)
	}
	fstype, ok := mountFSType(b, "/.flatpak-info")
	return ok && blockingFSType(fstype)
}

func (p *procFS) exePath(dirfd, pid int) (string, error) {
	link, err := p.exeLink(dirfd, pid)
	if err != nil {
		return "", err
	}
	return p.matchPath(dirfd, pid, link), nil
}

// openPID opens /proc/<pid> and checks it still is the process that started at start, so
// later reads through the descriptor can never reach a process that reused the pid.
func (p *procFS) openPID(pid int, start int64) (int, error) {
	fd, err := unix.Openat(p.rootfd, pidName(pid, ""), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	_, s, err := p.stat(fd, pid)
	if err != nil {
		unix.Close(fd)
		return -1, err
	}
	if s != start {
		unix.Close(fd)
		return -1, unix.ESRCH
	}
	return fd, nil
}

// socketInodes calls fn with the inode of every socket the process behind dirfd holds.
func (p *procFS) socketInodes(dirfd int, fn func(inode uint32)) error {
	fd, err := unix.Openat(dirfd, "fd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	for {
		n, err := unix.Getdents(fd, p.fdents)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n <= 0 {
			return nil
		}
		ents := p.fdents[:n]
		for len(ents) >= 19 {
			reclen := int(binary.NativeEndian.Uint16(ents[16:]))
			if reclen < 19 || reclen > len(ents) {
				break
			}
			name := ents[19:reclen]
			ents = ents[reclen:]
			if len(name) == 0 || name[0] < '0' || name[0] > '9' {
				continue
			}
			p.fdReads++
			m, ok := readlinkAt(fd, &name[0], p.link)
			if !ok {
				continue
			}
			if ino, ok := parseSocketLink(p.link[:m]); ok {
				fn(ino)
			}
		}
	}
}

// readlinkAt avoids a string allocation per fd: name points at a NUL-terminated dirent name.
func readlinkAt(dirfd int, name *byte, buf []byte) (int, bool) {
	n, _, errno := unix.Syscall6(unix.SYS_READLINKAT, uintptr(dirfd), uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	if errno != 0 {
		return 0, false
	}
	return int(n), true
}
