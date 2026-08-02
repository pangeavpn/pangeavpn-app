//go:build windows

package main

import (
	"context"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
)

const daemonServiceName = "PangeaDaemon"

func shouldRunAsService() bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(strings.TrimSpace(arg), "--service") {
			return true
		}
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
		return true, serviceStartError
	}

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case <-runtime.serveErr:
			stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			_ = stopDaemonRuntime(stopCtx, runtime)
			cancel()
			return true, serviceHTTPError
		case request, ok := <-requests:
			if !ok {
				stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
				stopErr := stopDaemonRuntime(stopCtx, runtime)
				cancel()
				if stopErr != nil {
					return true, serviceStopError
				}
				return false, 0
			}

			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: uint32(shutdownTimeout / time.Millisecond)}
				stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
				stopErr := stopDaemonRuntime(stopCtx, runtime)
				cancel()
				if stopErr != nil {
					return true, serviceStopError
				}
				return false, 0
			default:
				changes <- request.CurrentStatus
			}
		}
	}
}
