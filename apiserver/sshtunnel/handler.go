// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package sshtunnel provides HTTP handlers for the SSH reverse tunnel and
// relay upgrade endpoints on the controller API server.
//
// Both endpoints replace the previous SSH-based transport with an HTTP
// upgrade: the caller opens an HTTPS request, authentication happens at
// the HTTP layer over TLS, and after a 101 Switching Protocols response
// the connection carries raw bytes over TLS with no SSH layer between
// the caller and the controller.
package sshtunnel

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"time"

	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/internal/errors"
)

const (
	// TunnelUpgradeToken is the HTTP upgrade token used by machine agents
	// pushing a reverse tunnel to the controller.
	TunnelUpgradeToken = "juju-ssh-tunnel"

	// RelayUpgradeToken is the HTTP upgrade token used by JIMM to relay a
	// user's SSH session to the controller.
	RelayUpgradeToken = "juju-ssh-relay"

	// pushTunnelTimeout bounds the wait for a RequestTunnel consumer to
	// take ownership of a pushed tunnel connection, matching the previous
	// sshserver behaviour.
	pushTunnelTimeout = 10 * time.Second
)

// hijack upgrades the HTTP request to a raw connection. It validates the
// upgrade headers, writes the 101 Switching Protocols response, hijacks
// the connection, drains any bytes buffered by the server's bufio reader,
// and clears any deadlines so the protocol taking over the connection
// owns its lifecycle.
//
// The returned connection is the caller's responsibility; the HTTP server
// no longer tracks it.
func hijack(w http.ResponseWriter, r *http.Request, token string) (net.Conn, error) {
	if r.Header.Get("Connection") != "Upgrade" || r.Header.Get("Upgrade") != token {
		http.Error(w, "invalid upgrade request", http.StatusBadRequest)
		return nil, errors.Errorf("expected Upgrade: %s", token)
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "connection cannot be upgraded", http.StatusInternalServerError)
		return nil, errors.Errorf("response writer does not support hijacking")
	}

	// Write the 101 response before hijacking, so the headers go out
	// through the server's machinery.
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Upgrade", token)
	w.WriteHeader(http.StatusSwitchingProtocols)

	conn, buf, err := hijacker.Hijack()
	if err != nil {
		return nil, errors.Errorf("hijacking connection: %w", err)
	}

	// Drain any bytes the server buffered beyond the request head before
	// handing the connection to the next protocol.
	reader := buf.Reader
	if buffered := reader.Buffered(); buffered > 0 {
		if _, err := reader.Discard(buffered); err != nil {
			_ = conn.Close()
			return nil, errors.Errorf("draining buffered bytes: %w", err)
		}
	}

	// Clear any read/write deadlines the HTTP server set; the protocol
	// taking over the connection manages its own lifecycle.
	_ = conn.SetDeadline(time.Time{})

	return conn, nil
}

// watchDying closes the hijacked connection when the apiserver is shutting
// down. Hijacked connections are invisible to http.Server.Shutdown, so the
// handler must select on the dying signal itself; this helper arranges for
// the connection to be closed when that happens. It returns a stop function
// that should be called when the connection closes for any other reason.
func watchDying(conn net.Conn, dying <-chan struct{}, logger logger.Logger) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-dying:
			logger.Debugf(context.TODO(), "apiserver dying, closing upgraded connection")
			_ = conn.Close()
		case <-done:
		}
	}()
	var once bool
	return func() {
		if !once {
			once = true
			close(done)
		}
	}
}

// bufioReadWriter is a minimal interface over the value returned by
// http.Hijacker.Hijack, to keep the hijack helper testable.
type bufioReadWriter interface {
	Reader() *bufio.Reader
}
