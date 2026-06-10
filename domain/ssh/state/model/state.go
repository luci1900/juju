// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/domain"
	ssherrors "github.com/juju/juju/domain/ssh/errors"
	"github.com/juju/juju/internal/errors"
)

// State provides dqlite-backed storage for model-scoped virtual SSH host keys.
// It operates against the model database.
type State struct {
	*domain.StateBase
}

// NewState returns a new State for interacting with the model SSH host keys.
func NewState(factory database.TxnRunnerFactory) *State {
	return &State{
		StateBase: domain.NewStateBase(factory),
	}
}

// GetMachineVirtualSSHHostKey retrieves the PEM-encoded private key stored for
// the given machine. It returns [ssherrors.HostKeyNotFound] if no key has been
// set for this machine yet.
func (st *State) GetMachineVirtualSSHHostKey(ctx context.Context, machineUUID machine.UUID) (string, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return "", errors.Capture(err)
	}

	row := dbMachineVirtualSSHHostKey{MachineUUID: machineUUID.String()}
	stmt, err := st.Prepare(`
SELECT &dbMachineVirtualSSHHostKey.*
FROM   machine_virtual_ssh_host_key
WHERE  machine_uuid = $dbMachineVirtualSSHHostKey.machine_uuid`, row)
	if err != nil {
		return "", errors.Errorf("preparing get machine virtual SSH host key: %w", err)
	}

	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		err := tx.Query(ctx, stmt, row).Get(&row)
		if errors.Is(err, sqlair.ErrNoRows) {
			return ssherrors.HostKeyNotFound
		}
		return err
	})
	if err != nil {
		return "", errors.Capture(err)
	}
	return row.PrivateKey, nil
}

// SetMachineVirtualSSHHostKey stores (or replaces) the PEM-encoded private key
// for the given machine.
func (st *State) SetMachineVirtualSSHHostKey(ctx context.Context, machineUUID machine.UUID, privateKey string) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}

	row := dbMachineVirtualSSHHostKey{MachineUUID: machineUUID.String(), PrivateKey: privateKey}
	stmt, err := st.Prepare(`
INSERT INTO machine_virtual_ssh_host_key (machine_uuid, private_key)
VALUES      ($dbMachineVirtualSSHHostKey.machine_uuid, $dbMachineVirtualSSHHostKey.private_key)
ON CONFLICT (machine_uuid) DO UPDATE SET private_key = excluded.private_key`, row)
	if err != nil {
		return errors.Errorf("preparing set machine virtual SSH host key: %w", err)
	}

	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return tx.Query(ctx, stmt, row).Run()
	})
	return errors.Capture(err)
}

// GetUnitVirtualSSHHostKey retrieves the PEM-encoded private key stored for
// the given unit. It returns [ssherrors.HostKeyNotFound] if no key has been
// set for this unit yet.
func (st *State) GetUnitVirtualSSHHostKey(ctx context.Context, unitUUID unit.UUID) (string, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return "", errors.Capture(err)
	}

	row := dbUnitVirtualSSHHostKey{UnitUUID: unitUUID.String()}
	stmt, err := st.Prepare(`
SELECT &dbUnitVirtualSSHHostKey.*
FROM   unit_virtual_ssh_host_key
WHERE  unit_uuid = $dbUnitVirtualSSHHostKey.unit_uuid`, row)
	if err != nil {
		return "", errors.Errorf("preparing get unit virtual SSH host key: %w", err)
	}

	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		err := tx.Query(ctx, stmt, row).Get(&row)
		if errors.Is(err, sqlair.ErrNoRows) {
			return ssherrors.HostKeyNotFound
		}
		return err
	})
	if err != nil {
		return "", errors.Capture(err)
	}
	return row.PrivateKey, nil
}

// SetUnitVirtualSSHHostKey stores (or replaces) the PEM-encoded private key
// for the given unit.
func (st *State) SetUnitVirtualSSHHostKey(ctx context.Context, unitUUID unit.UUID, privateKey string) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}

	row := dbUnitVirtualSSHHostKey{UnitUUID: unitUUID.String(), PrivateKey: privateKey}
	stmt, err := st.Prepare(`
INSERT INTO unit_virtual_ssh_host_key (unit_uuid, private_key)
VALUES      ($dbUnitVirtualSSHHostKey.unit_uuid, $dbUnitVirtualSSHHostKey.private_key)
ON CONFLICT (unit_uuid) DO UPDATE SET private_key = excluded.private_key`, row)
	if err != nil {
		return errors.Errorf("preparing set unit virtual SSH host key: %w", err)
	}

	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return tx.Query(ctx, stmt, row).Run()
	})
	return errors.Capture(err)
}
