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

func (w *windowsServiceRunner) Execute(args []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	_ = args

	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	runtime, err := startDaemonRuntime()
	if err != nil {
		return false, 1
	}

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		request, ok := <-requests
		if !ok {
			stopCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			_ = runtime.Stop(stopCtx)
			return false, 0
		}

		switch request.Cmd {
		case svc.Interrogate:
			changes <- request.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			stopCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if stopErr := runtime.Stop(stopCtx); stopErr != nil {
				return false, 2
			}
			return false, 0
		default:
			changes <- request.CurrentStatus
		}
	}
}
