package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak/ck/common"

	mux "github.com/pangeavpn/pangeavpn-desktop/daemon/internal/cloak/ck/multiplex"
	log "github.com/sirupsen/logrus"
)

// ErrSessionCancelled is returned by MakeSession when the supplied context is
// cancelled before all underlying connections are established. Callers should
// treat this as a clean shutdown signal rather than an error to log loudly.
var ErrSessionCancelled = errors.New("cloak session cancelled")

// MakeSession dials the remote cloak server and establishes a multiplex
// session. The retry loop inside each underlying connection attempt honors
// ctx so that a Stop() on the caller side can unblock this call in bounded
// time rather than spinning forever when the server is unreachable.
//
// On different invocations to MakeSession, authInfo.SessionId MUST be different.
func MakeSession(ctx context.Context, connConfig RemoteConnConfig, authInfo AuthInfo, dialer common.Dialer) (*mux.Session, error) {
	log.Info("Attempting to start a new session")

	connsCh := make(chan net.Conn, connConfig.NumConn)
	var _sessionKey atomic.Value
	var wg sync.WaitGroup
	cancelled := make(chan struct{})
	var once sync.Once
	signalCancelled := func() {
		once.Do(func() { close(cancelled) })
	}

	for i := 0; i < connConfig.NumConn; i++ {
		wg.Add(1)
		transportConfig := connConfig.Transport
		go func() {
			defer wg.Done()

			for {
				if ctx.Err() != nil {
					signalCancelled()
					return
				}

				transportConn := transportConfig.CreateTransport()
				remoteConn, err := dialer.Dial("tcp", connConfig.RemoteAddr)
				if err != nil {
					log.Errorf("Failed to establish new connections to remote: %v", err)
					if !sleepWithContext(ctx, 3*time.Second) {
						signalCancelled()
						return
					}
					continue
				}

				sk, err := transportConn.Handshake(remoteConn, authInfo)
				if err != nil {
					log.Errorf("Failed to prepare connection to remote: %v", err)
					transportConn.Close()

					// In Cloak v2.11.0, we've updated uTLS version and subsequently increased the first packet size for chrome above 1500
					// https://github.com/cbeuw/Cloak/pull/306#issuecomment-2862728738. As a backwards compatibility feature, if we fail
					// to connect using chrome signature, retry with firefox which has a smaller packet size.
					if transportConfig.mode == "direct" && transportConfig.browser == chrome {
						transportConfig.browser = firefox
						log.Warnf("failed to connect with chrome signature, falling back to retry with firefox")
					}
					if !sleepWithContext(ctx, 3*time.Second) {
						signalCancelled()
						return
					}
					continue
				}

				// sessionKey given by each connection should be identical
				_sessionKey.Store(sk)

				// If the caller cancelled while we were handshaking, drop the
				// new conn instead of parking it in connsCh where nothing will
				// consume it.
				select {
				case <-ctx.Done():
					transportConn.Close()
					signalCancelled()
					return
				case connsCh <- transportConn:
				}
				return
			}
		}()
	}

	// Wait for either all dialers to finish OR the cancel signal to fire.
	// Using a goroutine so we don't block on wg.Wait() if cancellation happens.
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-cancelled:
		// Drain any in-flight connections and return.
		drainAndCloseConns(connsCh, waitDone)
		return nil, ErrSessionCancelled
	case <-ctx.Done():
		drainAndCloseConns(connsCh, waitDone)
		return nil, ErrSessionCancelled
	}

	log.Debug("All underlying connections established")

	rawKey := _sessionKey.Load()
	if rawKey == nil {
		// Every dialer returned via cancellation path; nothing to build.
		drainAndCloseConns(connsCh, waitDone)
		return nil, ErrSessionCancelled
	}
	sessionKey := rawKey.([32]byte)
	obfuscator, err := mux.MakeObfuscator(authInfo.EncryptionMethod, sessionKey)
	if err != nil {
		log.Fatal(err)
	}

	seshConfig := mux.SessionConfig{
		Singleplex:         connConfig.Singleplex,
		Obfuscator:         obfuscator,
		Valve:              nil,
		Unordered:          authInfo.Unordered,
		MsgOnWireSizeLimit: appDataMaxLength,
	}
	sesh := mux.MakeSession(authInfo.SessionId, seshConfig)

	for i := 0; i < connConfig.NumConn; i++ {
		conn := <-connsCh
		sesh.AddConnection(conn)
	}

	log.Infof("Session %v established", authInfo.SessionId)
	return sesh, nil
}

// sleepWithContext waits d or until ctx is cancelled. Returns true if the
// full duration elapsed, false if ctx was cancelled first.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// drainAndCloseConns closes any connections already parked in connsCh and
// waits for outstanding dial goroutines to finish before returning.
func drainAndCloseConns(connsCh chan net.Conn, waitDone <-chan struct{}) {
	go func() {
		<-waitDone
		close(connsCh)
	}()
	for c := range connsCh {
		_ = c.Close()
	}
}
