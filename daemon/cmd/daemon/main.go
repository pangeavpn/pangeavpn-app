package main

//go:generate goversioninfo -platform-specific

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
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
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/snowflake"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

const (
	daemonAddr            = "127.0.0.1:8787"
	shutdownTimeout       = 12 * time.Second
	shutdownGraceTimeout  = 30 * time.Second
	readHeaderTimeout     = 5 * time.Second
	requestReadTimeout    = 15 * time.Second
	writeTimeout          = 30 * time.Second
	idleConnectionTimeout = 60 * time.Second
)

type daemonRuntime struct {
	service  *api.Service
	server   *http.Server
	listener net.Listener
	serveErr <-chan error
	cancel   context.CancelFunc
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
	defer signal.Stop(sigCh)

	var serveErr error
	select {
	case <-sigCh:
	case serveErr = <-runtime.serveErr:
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	stopErr := stopDaemonRuntime(shutdownCtx, runtime)
	if serveErr != nil {
		serveErr = fmt.Errorf("http server stopped: %w", serveErr)
	}
	return errors.Join(serveErr, stopErr)
}

func stopDaemonRuntime(ctx context.Context, runtime *daemonRuntime) error {
	done := make(chan error, 1)
	go func() {
		done <- runtime.Stop(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}

	// Teardown (kill switch rules, routes, tun adapter) may still be mid-flight
	// in the goroutine above; exiting now would abandon it, not just cancel it.
	log.Printf("daemon shutdown exceeded %s, waiting up to %s more for teardown to finish", shutdownTimeout, shutdownGraceTimeout)
	select {
	case err := <-done:
		return err
	case <-time.After(shutdownGraceTimeout):
		return fmt.Errorf("daemon shutdown abandoned after %s: kill switch/routes/tun adapter teardown may be incomplete", shutdownTimeout+shutdownGraceTimeout)
	}
}

func startDaemonRuntime() (*daemonRuntime, error) {
	// Attached before anything that can fail (token/config resolution), so the
	// most common startup errors land in daemon.log/crash log, not a
	// --service-mode stderr that doesn't exist.
	logs := state.NewLogStore(4000)
	attachLogFile(logs)
	log.SetOutput(os.Stderr)
	logs.Add(state.LogInfo, state.SourceDaemon, "daemon booting")

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

	// A previous daemon process may have died mid-session; restore whatever
	// network state it left behind before serving any new requests.
	wg.RestoreOrphanedState()

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
	shadowsocksManager := shadowsocks.NewManager(logs)
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
	service := api.NewService(machine, logs, configStore, cloakManager, naiveManager, realityManager, hysteria2Manager, shadowsocksManager, snowflakeManager, wgManager, killSwitch)

	service.SetShadowsocksProxy(shadowsocks.NewProxyManager(logs))

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
		Addr:              daemonAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       requestReadTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleConnectionTimeout,
		MaxHeaderBytes:    16 << 10,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", server.Addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	service.StartBackground(ctx)

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("daemon listening on %s", server.Addr)
		log.Printf("token file: %s", tokenPath)
		log.Printf("config file: %s", configPath)
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		} else if err != nil {
			log.Printf("http server error: %v", err)
		}
		serveErr <- err
		close(serveErr)
	}()

	return &daemonRuntime{
		service:  service,
		server:   server,
		listener: listener,
		serveErr: serveErr,
		cancel:   cancel,
	}, nil
}

func (r *daemonRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if r.cancel != nil {
		r.cancel()
	}

	var stopErrors []error
	// Stop admitting API work before the final disconnect. Close also cancels
	// active request contexts, while Disconnect cancels an in-flight Connect.
	if r.server != nil {
		if err := r.server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			stopErrors = append(stopErrors, fmt.Errorf("close http server: %w", err))
		}
	} else if r.listener != nil {
		if err := r.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			stopErrors = append(stopErrors, fmt.Errorf("close http listener: %w", err))
		}
	}

	if r.service != nil {
		if err := r.service.Shutdown(ctx); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("disconnect VPN: %w", err))
		}
	}
	return errors.Join(stopErrors...)
}
