package main

//go:generate goversioninfo -icon=pangeavpn.ico -o resource_windows.syso

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/api"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/auth"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/hysteria2"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reality"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/snowflake"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

const daemonAddr = "127.0.0.1:8787"

type daemonRuntime struct {
	service *api.Service
	server  *http.Server
	cancel  context.CancelFunc
}

func main() {
	if shouldRunAsService() {
		if err := runService(); err != nil {
			log.Fatalf("run service: %v", err)
		}
		return
	}

	if err := runInteractive(); err != nil {
		log.Fatalf("run daemon: %v", err)
	}
}

func runInteractive() error {
	runtime, err := startDaemonRuntime()
	if err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return runtime.Stop(shutdownCtx)
}

func startDaemonRuntime() (*daemonRuntime, error) {
	tokenPath, err := platform.TokenPath()
	if err != nil {
		return nil, fmt.Errorf("resolve token path: %w", err)
	}

	configPath, err := platform.ConfigPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	token, err := auth.LoadOrCreateToken(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	logs := state.NewLogStore(4000)
	attachLogFile(logs)
	logs.Add(state.LogInfo, state.SourceDaemon, "daemon booting")

	machine := state.NewMachine()
	configStore, err := state.NewConfigStore(configPath)
	if err != nil {
		return nil, fmt.Errorf("init config store: %w", err)
	}

	cloakManager := cloak.NewManager(logs)
	naiveManager := newNaiveManager(logs)
	// Without -tags with_utls, sing-box's TLS layer returns a clean
	// "rebuild with -tags with_utls" error on Start, so no stub is needed.
	realityManager := reality.NewManager(logs)
	hysteria2Manager := hysteria2.NewManager(logs)
	snowflakeManager := snowflake.NewManager(logs)
	wgManager := wg.NewManager(logs)
	killSwitch := platform.NewKillSwitch()
	// An unresolvable permit host is skipped rather than failing Enable; surface
	// it so a transport quietly losing its permit stays visible.
	platform.EndpointResolveWarn = func(host string, err error) {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("kill switch could not resolve permit host %s, skipping: %v", host, err))
	}
	// Degraded-but-closed: the lock holds but something is off. These become
	// leaks later, so they must not stay silent.
	platform.KillSwitchWarnf = func(format string, args ...any) {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(format, args...))
	}
	service := api.NewService(machine, logs, configStore, cloakManager, naiveManager, realityManager, hysteria2Manager, snowflakeManager, wgManager, killSwitch)

	// Per-network last-good-transport cache is a best-effort optimization; a
	// failure to open it just leaves auto-connect walking the full cascade.
	if memPath, pathErr := platform.TransportMemoryPath(); pathErr != nil {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("transport memory disabled: %v", pathErr))
	} else if memStore, storeErr := state.NewTransportMemoryStore(memPath); storeErr != nil {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("transport memory disabled: %v", storeErr))
	} else {
		service.SetTransportMemory(memStore)
	}

	handler := api.NewHandler(token, service)
	server := &http.Server{
		Addr:    daemonAddr,
		Handler: handler,
	}

	ctx, cancel := context.WithCancel(context.Background())
	service.StartBackground(ctx)

	go func() {
		log.Printf("daemon listening on %s", server.Addr)
		log.Printf("token file: %s", tokenPath)
		log.Printf("config file: %s", configPath)
		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Printf("http server error: %v", serveErr)
		}
	}()

	return &daemonRuntime{
		service: service,
		server:  server,
		cancel:  cancel,
	}, nil
}

func (r *daemonRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if r.cancel != nil {
		r.cancel()
	}

	// A graceful stop happens on every clean reboot, so a Lockdown lock must
	// survive it — clearing here would boot the device open.
	keepKillSwitch := false
	if persisted, err := platform.LoadKillSwitchStatePublic(); err == nil {
		keepKillSwitch = persisted.Active && persisted.Locked
	}
	_ = r.service.Disconnect(ctx, keepKillSwitch)
	if r.server == nil {
		return nil
	}
	if err := r.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	return nil
}
