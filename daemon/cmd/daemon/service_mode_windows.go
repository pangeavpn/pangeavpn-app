//go:build windows

package main

import (
	"context"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const daemonServiceName = "PangeaDaemon"

func shouldRunAsService() bool {
	hasServiceFlag := slices.ContainsFunc(os.Args[1:], func(arg string) bool {
		return strings.EqualFold(strings.TrimSpace(arg), "--service")
	})
	if hasServiceFlag {
		return true
	}

	isService, err := svc.IsWindowsService()
	return err == nil && isService
}

func runService() error {
	return svc.Run(daemonServiceName, &windowsServiceRunner{})
}

type windowsServiceRunner struct{}

const (
	serviceStartError uint32 = iota + 1
	serviceStopError
	serviceHTTPError
)

func (w *windowsServiceRunner) Execute(args []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	_ = args

	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending, WaitHint: 30_000}

	runtime, err := startDaemonRuntime()
	if err != nil {
		reportStartFailure(err)
		return true, serviceStartError
	}

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case <-runtime.serveErr:
			_ = stopDaemonRuntimeWithTimeout(runtime)
			return true, serviceHTTPError
		case request, ok := <-requests:
			if !ok {
				if stopDaemonRuntimeWithTimeout(runtime) != nil {
					return true, serviceStopError
				}
				return false, 0
			}

			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: uint32(shutdownTimeout / time.Millisecond)}
				if stopDaemonRuntimeWithTimeout(runtime) != nil {
					return true, serviceStopError
				}
				return false, 0
			default:
				changes <- request.CurrentStatus
			}
		}
	}
}

func stopDaemonRuntimeWithTimeout(runtime *daemonRuntime) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return stopDaemonRuntime(stopCtx, runtime)
}

// The daemon's own log lives in the state dir, so a failure to reach that dir
// is invisible anywhere but the Windows event log.
func reportStartFailure(err error) {
	_ = eventlog.InstallAsEventCreate(daemonServiceName, eventlog.Error)
	elog, openErr := eventlog.Open(daemonServiceName)
	if openErr != nil {
		return
	}
	defer elog.Close()
	_ = elog.Error(serviceStartError, "daemon failed to start: "+err.Error())
}
