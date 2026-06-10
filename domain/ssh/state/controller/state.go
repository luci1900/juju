// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/domain"
	ssherrors "github.com/juju/juju/domain/ssh/errors"
	"github.com/juju/juju/internal/errors"
)

// State provides dqlite-backed storage for the controller SSH jump host key.
// It operates against the controller database.
type State struct {
	*domain.StateBase
}

// NewState returns a new State for interacting with the controller SSH host key.
func NewState(factory database.TxnRunnerFactory) *State {
	return &State{
		StateBase: domain.NewStateBase(factory),
	}
}

// GetControllerSSHHostKey retrieves the PEM-encoded private key stored for
// the SSH jump server. It returns [ssherrors.HostKeyNotFound] if the key
// has not been set yet.
func (st *State) GetControllerSSHHostKey(ctx context.Context) (string, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return "", errors.Capture(err)
	}

	row := dbControllerSSHHostKey{}
	stmt, err := st.Prepare(`
SELECT &dbControllerSSHHostKey.*
FROM   controller_ssh_host_key
WHERE  id = 0`, row)
	if err != nil {
		return "", errors.Errorf("preparing get controller SSH host key: %w", err)
	}

	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		err := tx.Query(ctx, stmt).Get(&row)
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

// SetControllerSSHHostKey stores (or replaces) the PEM-encoded private key
// for the SSH jump server. At most one row exists in the table; an upsert
// is used to keep the invariant.
func (st *State) SetControllerSSHHostKey(ctx context.Context, privateKey string) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}

	row := dbControllerSSHHostKey{ID: 0, PrivateKey: privateKey}
	stmt, err := st.Prepare(`
INSERT INTO controller_ssh_host_key (id, private_key)
VALUES      ($dbControllerSSHHostKey.id, $dbControllerSSHHostKey.private_key)
ON CONFLICT (id) DO UPDATE SET private_key = excluded.private_key`, row)
	if err != nil {
		return errors.Errorf("preparing set controller SSH host key: %w", err)
	}

	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return tx.Query(ctx, stmt, row).Run()
	})
	return errors.Capture(err)
}
