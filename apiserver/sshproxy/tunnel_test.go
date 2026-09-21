// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package sshproxy

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/juju/tc"

	loggertesting "github.com/juju/juju/internal/logger/testing"
)

type TunnelHandlerSuite struct{}

func TestTunnelHandlerSuite(t *testing.T) {
	tc.Run(t, &TunnelHandlerSuite{})
}

// fakeConnRequestService is a stub SSHConnRequestService. If err is set,
// GetSSHConnRequest always fails. Otherwise it succeeds only for the
// configured machineName, mirroring the real service's per-machine scoping.
type fakeConnRequestService struct {
	machineName string
	err         error
}

func (f *fakeConnRequestService) GetSSHConnRequest(_ context.Context, machineName, _ string) (SSHConnRequest, error) {
	if f.err != nil {
		return SSHConnRequest{}, f.err
	}
	if machineName != f.machineName {
		return SSHConnRequest{}, errNotFound
	}
	return SSHConnRequest{MachineName: machineName}, nil
}

var errNotFound = errorString("not found")

type errorString string

func (e errorString) Error() string { return string(e) }

// fakeTracker is a stub TunnelTracker. PushTunnel reports the pushed
// connection over the pushed channel and returns (done, err).
type fakeTracker struct {
	err    error
	done   chan struct{}
	pushed chan net.Conn
}

func newFakeTracker() *fakeTracker {
	return &fakeTracker{
		done:   make(chan struct{}),
		pushed: make(chan net.Conn, 1),
	}
}

func (f *fakeTracker) PushTunnel(_ context.Context, _ string, conn net.Conn) (<-chan struct{}, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.pushed <- conn
	return f.done, nil
}

// ctxConfig configures the request context values the apiserver's real
// wrapper would normally inject.
type ctxConfig struct {
	machineName    string
	hasMachineName bool
	dying          <-chan struct{}
}

func wrapWithContext(h http.Handler, cfg ctxConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if cfg.hasMachineName {
			ctx = context.WithValue(ctx, AuthenticatedMachineNameKey{}, cfg.machineName)
		}
		if cfg.dying != nil {
			ctx = context.WithValue(ctx, DyingKey{}, cfg.dying)
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withFinishedSignal wraps h so the returned channel is closed once
// ServeHTTP returns, letting tests detect completion of the handler.
func withFinishedSignal(h http.Handler) (http.Handler, <-chan struct{}) {
	finished := make(chan struct{})
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		h.ServeHTTP(w, r)
	})
	return wrapped, finished
}

// newTunnelTestServer builds an httptest server around handler with the
// given context values injected, and returns a channel that is closed once
// the handler returns from a request.
func newTunnelTestServer(c *tc.C, handler *TunnelHandler, cfg ctxConfig) (*httptest.Server, <-chan struct{}) {
	tracked, finished := withFinishedSignal(handler)
	srv := httptest.NewServer(wrapWithContext(tracked, cfg))
	c.Cleanup(srv.Close)
	return srv, finished
}

func newTunnelHandler(c *tc.C, tracker TunnelTracker, connReqService SSHConnRequestService) *TunnelHandler {
	h, err := NewTunnelHandler(TunnelHandlerConfig{
		Logger:                loggertesting.WrapCheckLog(c),
		Tracker:               tracker,
		SSHConnRequestService: connReqService,
	})
	c.Assert(err, tc.ErrorIsNil)
	return h
}

func (s *TunnelHandlerSuite) TestValidateRejectsMissingDependencies(c *tc.C) {
	_, err := NewTunnelHandler(TunnelHandlerConfig{})
	c.Assert(err, tc.ErrorMatches, ".*nil Logger.*")

	_, err = NewTunnelHandler(TunnelHandlerConfig{
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorMatches, ".*nil Tracker.*")

	_, err = NewTunnelHandler(TunnelHandlerConfig{
		Logger:  loggertesting.WrapCheckLog(c),
		Tracker: newFakeTracker(),
	})
	c.Assert(err, tc.ErrorMatches, ".*nil SSHConnRequestService.*")
}

func (s *TunnelHandlerSuite) TestServeHTTPMissingAuthenticatedMachine(c *tc.C) {
	handler := newTunnelHandler(c, newFakeTracker(), &fakeConnRequestService{machineName: "0"})
	srv, _ := newTunnelTestServer(c, handler, ctxConfig{})

	resp, err := srv.Client().Get(srv.URL + "/?:tunnelID=tunnel-0")
	c.Assert(err, tc.ErrorIsNil)
	defer resp.Body.Close()
	c.Check(resp.StatusCode, tc.Equals, http.StatusUnauthorized)
}

func (s *TunnelHandlerSuite) TestServeHTTPMissingTunnelID(c *tc.C) {
	handler := newTunnelHandler(c, newFakeTracker(), &fakeConnRequestService{machineName: "0"})
	srv, _ := newTunnelTestServer(c, handler, ctxConfig{machineName: "0", hasMachineName: true})

	resp, err := srv.Client().Get(srv.URL + "/")
	c.Assert(err, tc.ErrorIsNil)
	defer resp.Body.Close()
	c.Check(resp.StatusCode, tc.Equals, http.StatusBadRequest)
}

func (s *TunnelHandlerSuite) TestServeHTTPTunnelNotFound(c *tc.C) {
	// The connection request service rejects a machine name that does not
	// match the one bound to the tunnel ID.
	handler := newTunnelHandler(c, newFakeTracker(), &fakeConnRequestService{machineName: "1"})
	srv, _ := newTunnelTestServer(c, handler, ctxConfig{machineName: "0", hasMachineName: true})

	resp, err := srv.Client().Get(srv.URL + "/?:tunnelID=tunnel-0")
	c.Assert(err, tc.ErrorIsNil)
	defer resp.Body.Close()
	c.Check(resp.StatusCode, tc.Equals, http.StatusNotFound)
}

// dialTunnel opens a raw TCP connection to addr and writes an upgrade
// request for tunnelID, returning the connection and the parsed response
// head. The caller owns the connection afterwards.
func dialTunnel(c *tc.C, addr, tunnelID string) (net.Conn, *http.Response) {
	conn, err := net.Dial("tcp", addr)
	c.Assert(err, tc.ErrorIsNil)
	c.Cleanup(func() { _ = conn.Close() })

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	c.Assert(err, tc.ErrorIsNil)
	req.URL.RawQuery = ":tunnelID=" + tunnelID
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", TunnelUpgradeToken)
	c.Assert(req.Write(conn), tc.ErrorIsNil)

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	c.Assert(err, tc.ErrorIsNil)
	return conn, resp
}

func (s *TunnelHandlerSuite) TestServeHTTPPushTunnelErrorClosesConn(c *tc.C) {
	tracker := newFakeTracker()
	tracker.err = errorString("push failed")
	handler := newTunnelHandler(c, tracker, &fakeConnRequestService{machineName: "0"})
	srv, finished := newTunnelTestServer(c, handler, ctxConfig{machineName: "0", hasMachineName: true})

	conn, resp := dialTunnel(c, srv.Listener.Addr().String(), "tunnel-0")
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, tc.Equals, http.StatusSwitchingProtocols)

	waitFinished(c, finished)

	// The handler closes conn after a failed push: a read now observes EOF
	// rather than blocking.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	c.Check(err, tc.NotNil)
}

func (s *TunnelHandlerSuite) TestServeHTTPBlocksUntilTunnelDone(c *tc.C) {
	tracker := newFakeTracker()
	handler := newTunnelHandler(c, tracker, &fakeConnRequestService{machineName: "0"})
	srv, finished := newTunnelTestServer(c, handler, ctxConfig{machineName: "0", hasMachineName: true})

	_, resp := dialTunnel(c, srv.Listener.Addr().String(), "tunnel-0")
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, tc.Equals, http.StatusSwitchingProtocols)

	// The connection was pushed to the tracker; the handler must still be
	// blocked in the done/dying select.
	select {
	case <-tracker.pushed:
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for tunnel to be pushed")
	}
	select {
	case <-finished:
		c.Fatalf("handler returned before tunnel done fired")
	case <-time.After(50 * time.Millisecond):
	}

	close(tracker.done)
	waitFinished(c, finished)
}

func (s *TunnelHandlerSuite) TestServeHTTPClosesConnOnDying(c *tc.C) {
	tracker := newFakeTracker()
	dying := make(chan struct{})
	handler := newTunnelHandler(c, tracker, &fakeConnRequestService{machineName: "0"})
	srv, finished := newTunnelTestServer(c, handler, ctxConfig{
		machineName:    "0",
		hasMachineName: true,
		dying:          dying,
	})

	conn, resp := dialTunnel(c, srv.Listener.Addr().String(), "tunnel-0")
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, tc.Equals, http.StatusSwitchingProtocols)

	select {
	case <-tracker.pushed:
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for tunnel to be pushed")
	}

	close(dying)
	waitFinished(c, finished)

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	c.Check(err, tc.NotNil)
}

func waitFinished(c *tc.C, finished <-chan struct{}) {
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for handler to return")
	}
}
