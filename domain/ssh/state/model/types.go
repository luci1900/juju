// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model

// dbMachineVirtualSSHHostKey is the dqlite representation of a
// machine_virtual_ssh_host_key row.
type dbMachineVirtualSSHHostKey struct {
	// MachineUUID is the UUID of the machine.
	MachineUUID string `db:"machine_uuid"`

	// PrivateKey is the PEM-encoded ED25519 private key.
	PrivateKey string `db:"private_key"`
}

// dbUnitVirtualSSHHostKey is the dqlite representation of a
// unit_virtual_ssh_host_key row.
type dbUnitVirtualSSHHostKey struct {
	// UnitUUID is the UUID of the unit.
	UnitUUID string `db:"unit_uuid"`

	// PrivateKey is the PEM-encoded ED25519 private key.
	PrivateKey string `db:"private_key"`
}
