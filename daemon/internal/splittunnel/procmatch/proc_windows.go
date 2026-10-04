//go:build windows

package procmatch

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type spiProc struct {
	pid   int
	ppid  int
	start int64
}

type winProcess struct {
	start int64
	ppid  int
	path  string
}

func filetimeValue(ft windows.Filetime) int64 {
	return int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime)
}

// filetimeNow uses the precise clock: a coarse reading can trail the creation time of a
// process started a moment earlier and make its fresh socket look unowned.
func filetimeNow() int64 {
	var ft windows.Filetime
	windows.GetSystemTimePreciseAsFileTime(&ft)
	return filetimeValue(ft)
}

func openProcess(pid int) (windows.Handle, error) {
	if pid <= 0 {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	return windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
}

func handleStart(h windows.Handle) (int64, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return filetimeValue(created), nil
}

func handleImage(h windows.Handle) (string, error) {
	for size := 1024; ; size *= 4 {
		buf := make([]uint16, size)
		n := uint32(size)
		err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
		if err == nil {
			return windows.UTF16ToString(buf[:n]), nil
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || size >= 32768 {
			return "", err
		}
	}
}

func handleParent(h windows.Handle) (int, error) {
	var pbi windows.PROCESS_BASIC_INFORMATION
	var n uint32
	err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), &n)
	if err != nil {
		return 0, err
	}
	return int(pbi.InheritedFromUniqueProcessId), nil
}

// processStart is the cheap liveness/identity probe used for owners and parents.
func processStart(pid int) (int64, error) {
	h, err := openProcess(pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	return handleStart(h)
}

// processInfo reads start, parent and image from one handle so they describe the same process.
func processInfo(pid int) (winProcess, error) {
	h, err := openProcess(pid)
	if err != nil {
		return winProcess{}, err
	}
	defer windows.CloseHandle(h)
	start, err := handleStart(h)
	if err != nil {
		return winProcess{}, err
	}
	ppid, err := handleParent(h)
	if err != nil {
		return winProcess{}, err
	}
	image, err := handleImage(h)
	if err != nil {
		return winProcess{start: start, ppid: ppid}, err
	}
	return winProcess{start: start, ppid: ppid, path: normalizeProcessPath(image)}, nil
}

func normalizeProcessPath(p string) string {
	if strings.Contains(p, "~") {
		if long, ok := longPath(p); ok {
			p = long
		}
	}
	return normalizeWinImage(p)
}

func longPath(p string) (string, bool) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 1024)
	for {
		n, err := windows.GetLongPathName(name, &buf[0], uint32(len(buf)))
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

// systemProcesses takes a SystemProcessInformation snapshot, reusing buf between calls.
func systemProcesses(buf *[]byte) ([]spiProc, error) {
	size := len(*buf)
	if size == 0 {
		size = 512 << 10
	}
	for attempt := 0; attempt < 8; attempt++ {
		if len(*buf) < size {
			*buf = make([]byte, size)
		}
		var ret uint32
		err := windows.NtQuerySystemInformation(windows.SystemProcessInformation, unsafe.Pointer(&(*buf)[0]), uint32(len(*buf)), &ret)
		if err == nil {
			if ret == 0 || int(ret) > len(*buf) {
				ret = uint32(len(*buf))
			}
			return parseSPI((*buf)[:ret]), nil
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			return nil, err
		}
		size = int(ret) + 64<<10
		if size <= len(*buf) {
			size = len(*buf) * 2
		}
	}
	return nil, errors.New("process snapshot keeps growing")
}

func parseSPI(buf []byte) []spiProc {
	var out []spiProc
	size := int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	for off := 0; off >= 0 && off+size <= len(buf); {
		p := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buf[off]))
		out = append(out, spiProc{pid: int(p.UniqueProcessID), ppid: int(p.InheritedFromUniqueProcessID), start: p.CreateTime})
		if p.NextEntryOffset == 0 {
			break
		}
		off += int(p.NextEntryOffset)
	}
	return out
}

type systemProcessIDInformation struct {
	processID uintptr
	imageName windows.NTUnicodeString
}

// ntImagePath needs no process handle, so it works for processes that refuse OpenProcess.
func ntImagePath(pid int) (string, error) {
	buf := make([]uint16, 512)
	for attempt := 0; attempt < 3; attempt++ {
		info := systemProcessIDInformation{processID: uintptr(pid)}
		info.imageName.MaximumLength = uint16(len(buf) * 2)
		info.imageName.Buffer = &buf[0]
		err := windows.NtQuerySystemInformation(windows.SystemProcessIdInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)), nil)
		if err == nil {
			n := min(int(info.imageName.Length)/2, len(buf))
			return windows.UTF16ToString(buf[:n]), nil
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			return "", err
		}
		need := int(info.imageName.MaximumLength)/2 + 1
		if need <= len(buf) || need > 32767 {
			return "", err
		}
		buf = make([]uint16, need)
	}
	return "", windows.STATUS_INFO_LENGTH_MISMATCH
}

// dosDevices caches the \Device\HarddiskVolumeN → drive letter table.
type dosDevices struct {
	mu     sync.Mutex
	table  map[string]string
	loaded time.Time
}

func (d *dosDevices) toDos(nt string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.table != nil {
		if p, ok := ntToDos(d.table, nt); ok {
			return p, true
		}
	}
	if d.table != nil && time.Since(d.loaded) < 5*time.Second {
		return "", false
	}
	d.table, d.loaded = loadDosDevices(), time.Now()
	return ntToDos(d.table, nt)
}

func loadDosDevices() map[string]string {
	m := make(map[string]string)
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return m
	}
	buf := make([]uint16, 1024)
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		drive := string(rune('a'+i)) + ":"
		name, err := windows.UTF16PtrFromString(drive)
		if err != nil {
			continue
		}
		n, err := windows.QueryDosDevice(name, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			continue
		}
		if target := windows.UTF16ToString(buf[:n]); target != "" {
			m[strings.ToLower(target)] = drive
		}
	}
	return m
}
