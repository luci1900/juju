// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package controller provides dqlite-backed state for SSH host keys
// scoped to the controller database. The controller_ssh_host_key table
// holds exactly one row: the ED25519 private key used by the SSH jump
// server (sshserver worker) to identify itself to connecting agents.
package controller
