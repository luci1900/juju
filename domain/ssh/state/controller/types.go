// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

// dbControllerSSHHostKey is the dqlite representation of the
// controller_ssh_host_key row.
type dbControllerSSHHostKey struct {
	// ID is the sentinel row identifier (always 0).
	ID int `db:"id"`

	// PrivateKey is the PEM-encoded ED25519 private key.
	PrivateKey string `db:"private_key"`
}
