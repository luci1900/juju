// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package sshproxy serves the SSH upgrade endpoints on the API server.
// It serves the tunnel endpoint (GET /model/:modeluuid/ssh-tunnel/:tunnelID):
// machine agents push reverse SSH tunnels to the controller.
package sshproxy
