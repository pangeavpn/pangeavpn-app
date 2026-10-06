//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	bfeServiceName  = "BFE"
	bfeStartTimeout = 15 * time.Second
)

// bfeState reads the service state with the rights any caller has, so a
// running BFE is recognised even where the full-access manager is refused.
func bfeState() (svc.State, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, fmt.Errorf("open service manager: %w", err)
	}
	defer windows.CloseServiceHandle(scm)

	name, err := windows.UTF16PtrFromString(bfeServiceName)
	if err != nil {
		return 0, err
	}
	h, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return 0, fmt.Errorf("open %s service: %w", bfeServiceName, err)
	}
	s := &mgr.Service{Name: bfeServiceName, Handle: h}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return 0, fmt.Errorf("query %s service: %w", bfeServiceName, err)
	}
	return status.State, nil
}

// ensureBFERunning brings the Base Filtering Engine back after an "optimizer"
// stopped or disabled it; started is false when it was already running.
func ensureBFERunning() (started bool, err error) {
	state, err := bfeState()
	if err != nil {
		return false, err
	}
	if state == svc.Running {
		return false, nil
	}

	m, err := mgr.Connect()
	if err != nil {
		return false, fmt.Errorf("open service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(bfeServiceName)
	if err != nil {
		return false, fmt.Errorf("open %s service: %w", bfeServiceName, err)
	}
	defer s.Close()

	cfg, err := s.Config()
	if err != nil {
		return false, fmt.Errorf("read %s config: %w", bfeServiceName, err)
	}
	if cfg.StartType == mgr.StartDisabled {
		err := windows.ChangeServiceConfig(s.Handle, windows.SERVICE_NO_CHANGE, windows.SERVICE_AUTO_START, windows.SERVICE_NO_CHANGE, nil, nil, nil, nil, nil, nil, nil)
		if err != nil {
			return false, fmt.Errorf("re-enable %s service: %w", bfeServiceName, err)
		}
	}

	startRequested := false
	deadline := time.Now().Add(bfeStartTimeout)
	for {
		if state == svc.Running {
			return true, nil
		}
		if state == svc.Stopped && !startRequested {
			if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
				return false, fmt.Errorf("start %s service: %w", bfeServiceName, err)
			}
			startRequested = true
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("%s service did not start within %s (state %d)", bfeServiceName, bfeStartTimeout, state)
		}
		time.Sleep(250 * time.Millisecond)
		status, err := s.Query()
		if err != nil {
			return false, fmt.Errorf("query %s service: %w", bfeServiceName, err)
		}
		state = status.State
	}
}
