// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/core/database"
	internaldatabase "github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/errors"
)

// InsertInitialSSHHostKey inserts the controller SSH jump host key into the
// controller database at bootstrap time. The key must be a PEM-encoded
// ED25519 private key.
func InsertInitialSSHHostKey(privateKey string) internaldatabase.BootstrapOpt {
	return func(ctx context.Context, controller, _ database.TxnRunner) error {
		row := dbControllerSSHHostKey{ID: 0, PrivateKey: privateKey}
		stmt, err := sqlair.Prepare(`
INSERT INTO controller_ssh_host_key (id, private_key)
VALUES      ($dbControllerSSHHostKey.id, $dbControllerSSHHostKey.private_key)
ON CONFLICT (id) DO UPDATE SET private_key = excluded.private_key`, row)
		if err != nil {
			return errors.Errorf("preparing insert controller SSH host key: %w", err)
		}

		return errors.Capture(controller.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
			return errors.Capture(tx.Query(ctx, stmt, row).Run())
		}))
	}
}

type dbControllerSSHHostKey struct {
	ID         int    `db:"id"`
	PrivateKey string `db:"private_key"`
}
