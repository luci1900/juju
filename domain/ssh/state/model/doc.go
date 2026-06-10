// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package model provides dqlite-backed state for virtual SSH host keys
// scoped to the model database. Two tables are owned here:
//
//   - machine_virtual_ssh_host_key: one row per IAAS machine, with a strict FK
//     to machine(uuid).
//   - unit_virtual_ssh_host_key: one row per unit, with a strict FK to
//     unit(uuid). Kept separate to accommodate K8s units that are not backed by
//     IAAS machines.
//
// Both tables store ED25519 private keys in PEM format.
package model
