//go:build !windows

package auth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const (
	macTrustGroup   = "admin"
	linuxTrustGroup = "pangeavpn"
)

// ensureTokenReadACL enforces 0600 on an fd we already hold, never a path,
// so it can't be tricked into chmod'ing through a symlink swapped in later.
// It then grants read access to the platform trust group (macOS "admin",
// Linux "pangeavpn") so the desktop app, not just root, can read the token.
func ensureTokenReadACL(path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod token file: %w", err)
	}

	groupName := trustGroupName()
	gid, found, err := lookupGroupGID(groupName)
	if err != nil {
		return fmt.Errorf("resolve %s group: %w", groupName, err)
	}
	if !found {
		log.Printf("auth: group %q does not exist; token file left root-only (0600) - the desktop app will not be able to connect until the installer creates it and adds the user", groupName)
		return nil
	}
	if err := f.Chown(0, gid); err != nil {
		if errors.Is(err, syscall.EPERM) {
			log.Printf("auth: not privileged to grant group %q read access; token file left owner-only (0600)", groupName)
			return nil
		}
		return fmt.Errorf("chown token file to %s: %w", groupName, err)
	}
	if err := f.Chmod(0o640); err != nil {
		return fmt.Errorf("chmod token file: %w", err)
	}
	return nil
}

func trustGroupName() string {
	if runtime.GOOS == "darwin" {
		return macTrustGroup
	}
	return linuxTrustGroup
}

// lookupGroupGID reads /etc/group directly instead of os/user, which needs
// cgo on darwin to resolve names and would silently fail in cgo-free builds.
func lookupGroupGID(name string) (gid int, found bool, err error) {
	f, err := os.Open("/etc/group")
	if err != nil {
		return 0, false, fmt.Errorf("open /etc/group: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), ":", 4)
		if len(fields) < 3 || fields[0] != name {
			continue
		}
		id, convErr := strconv.Atoi(fields[2])
		if convErr != nil {
			return 0, false, fmt.Errorf("parse gid for group %s: %w", name, convErr)
		}
		return id, true, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, false, fmt.Errorf("read /etc/group: %w", err)
	}
	return 0, false, nil
}

// readTrustedTokenFile refuses to adopt a token file unless it is owned by
// root and not writable by group/others, and never follows a symlink to get
// there, so a planted file (or a symlink to a sensitive path) is rejected.
func readTrustedTokenFile(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat token file: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("cannot determine token file owner")
	}
	if stat.Uid != 0 {
		return "", fmt.Errorf("token file is not owned by root")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("token file is writable by group or others")
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	return string(data), nil
}
