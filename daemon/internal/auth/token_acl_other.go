//go:build !windows

package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	accessGroup   = "pangeavpn"
	macAdminGroup = "admin"
)

// Works on an fd we already hold, never a path, so it can't be tricked into
// chmod'ing through a symlink swapped in later.
func ensureTokenReadACL(path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod token file: %w", err)
	}

	groupName, gid, found, err := tokenReadGroup()
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

// The pkg creates pangeavpn on macOS; installs that predate it keep the
// admin-only access they always had.
func tokenReadGroup() (name string, gid int, found bool, err error) {
	if runtime.GOOS == "darwin" {
		if gid, ok := directoryGroupGID(accessGroup); ok {
			return accessGroup, gid, true, nil
		}
		gid, found, err = lookupGroupGID(macAdminGroup)
		return macAdminGroup, gid, found, err
	}
	gid, found, err = lookupGroupGID(accessGroup)
	return accessGroup, gid, found, err
}

// directoryGroupGID asks Directory Services, where macOS keeps groups made with
// dseditgroup; its /etc/group lists only the built-in ones.
func directoryGroupGID(name string) (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/dscl", ".", "-read", "/Groups/"+name, "PrimaryGroupID").Output()
	if err != nil {
		return 0, false
	}
	field, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "PrimaryGroupID:")
	if !ok {
		return 0, false
	}
	gid, err := strconv.Atoi(strings.TrimSpace(field))
	return gid, err == nil && gid > 0
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
