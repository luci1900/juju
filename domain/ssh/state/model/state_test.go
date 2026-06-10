// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model

import (
	"context"
	"database/sql"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/domain/life"
	schematesting "github.com/juju/juju/domain/schema/testing"
	ssherrors "github.com/juju/juju/domain/ssh/errors"
	"github.com/juju/juju/internal/uuid"
)

type modelStateSuite struct {
	schematesting.ModelSuite
}

func TestModelStateSuite(t *testing.T) {
	tc.Run(t, &modelStateSuite{})
}

// createMachine inserts a minimal machine row and returns the machine UUID.
func (s *modelStateSuite) createMachine(c *tc.C) machine.UUID {
	netNodeUUID := uuid.MustNewUUID().String()
	machineUUID := machine.UUID(uuid.MustNewUUID().String())

	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO net_node (uuid) VALUES (?)`, netNodeUUID,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO machine (uuid, name, net_node_uuid, life_id) VALUES (?, ?, ?, ?)`,
			machineUUID.String(), "0", netNodeUUID, life.Alive,
		)
		return err
	})
	c.Assert(err, tc.ErrorIsNil)
	return machineUUID
}

// createUnit inserts a minimal charm/application/unit chain and returns the unit UUID.
func (s *modelStateSuite) createUnit(c *tc.C) unit.UUID {
	unitUUID := unit.UUID(uuid.MustNewUUID().String())
	netNodeUUID := uuid.MustNewUUID().String()

	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO charm (uuid, reference_name, source_id) VALUES (?, 'test', 0)`,
			"charm-uuid-test",
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO charm_metadata (charm_uuid, name, subordinate) VALUES (?, 'test', 0)`,
			"charm-uuid-test",
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO application (uuid, charm_uuid, name, life_id, space_uuid) VALUES (?, ?, 'test', ?, ?)`,
			"app-uuid-test", "charm-uuid-test", life.Alive, network.AlphaSpaceId,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO net_node (uuid) VALUES (?)`, netNodeUUID,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO unit (uuid, name, life_id, net_node_uuid, application_uuid, charm_uuid)
             SELECT ?, 'test/0', ?, ?, uuid, charm_uuid FROM application WHERE uuid = ?`,
			unitUUID.String(), life.Alive, netNodeUUID, "app-uuid-test",
		)
		return err
	})
	c.Assert(err, tc.ErrorIsNil)
	return unitUUID
}

// --- Machine virtual SSH host key tests ---

func (s *modelStateSuite) TestGetMachineVirtualSSHHostKeyNotFound(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	machineUUID := s.createMachine(c)

	_, err := st.GetMachineVirtualSSHHostKey(c.Context(), machineUUID)
	c.Assert(err, tc.ErrorIs, ssherrors.HostKeyNotFound)
}

func (s *modelStateSuite) TestSetAndGetMachineVirtualSSHHostKey(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	machineUUID := s.createMachine(c)

	const privateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nmachine-key\n-----END OPENSSH PRIVATE KEY-----\n"

	err := st.SetMachineVirtualSSHHostKey(c.Context(), machineUUID, privateKey)
	c.Assert(err, tc.ErrorIsNil)

	got, err := st.GetMachineVirtualSSHHostKey(c.Context(), machineUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, privateKey)
}

func (s *modelStateSuite) TestSetMachineVirtualSSHHostKeyUpserts(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	machineUUID := s.createMachine(c)

	const key1 = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-one\n-----END OPENSSH PRIVATE KEY-----\n"
	const key2 = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-two\n-----END OPENSSH PRIVATE KEY-----\n"

	err := st.SetMachineVirtualSSHHostKey(c.Context(), machineUUID, key1)
	c.Assert(err, tc.ErrorIsNil)

	err = st.SetMachineVirtualSSHHostKey(c.Context(), machineUUID, key2)
	c.Assert(err, tc.ErrorIsNil)

	got, err := st.GetMachineVirtualSSHHostKey(c.Context(), machineUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, key2)
}

func (s *modelStateSuite) TestMachineVirtualSSHHostKeyIsolated(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	machineUUID1 := s.createMachine(c)
	machineUUID2 := machine.UUID(uuid.MustNewUUID().String())

	// Create a second machine (reusing same net_node trick is not possible,
	// so we insert directly without net_node uniqueness constraint issues).
	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		netNodeUUID := uuid.MustNewUUID().String()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO net_node (uuid) VALUES (?)`, netNodeUUID,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO machine (uuid, name, net_node_uuid, life_id) VALUES (?, ?, ?, ?)`,
			machineUUID2.String(), "1", netNodeUUID, life.Alive,
		)
		return err
	})
	c.Assert(err, tc.ErrorIsNil)

	const key1 = "machine-key-one"
	const key2 = "machine-key-two"

	err = st.SetMachineVirtualSSHHostKey(c.Context(), machineUUID1, key1)
	c.Assert(err, tc.ErrorIsNil)
	err = st.SetMachineVirtualSSHHostKey(c.Context(), machineUUID2, key2)
	c.Assert(err, tc.ErrorIsNil)

	got1, err := st.GetMachineVirtualSSHHostKey(c.Context(), machineUUID1)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got1, tc.Equals, key1)

	got2, err := st.GetMachineVirtualSSHHostKey(c.Context(), machineUUID2)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got2, tc.Equals, key2)
}

// --- Unit virtual SSH host key tests ---

func (s *modelStateSuite) TestGetUnitVirtualSSHHostKeyNotFound(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	unitUUID := s.createUnit(c)

	_, err := st.GetUnitVirtualSSHHostKey(c.Context(), unitUUID)
	c.Assert(err, tc.ErrorIs, ssherrors.HostKeyNotFound)
}

func (s *modelStateSuite) TestSetAndGetUnitVirtualSSHHostKey(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	unitUUID := s.createUnit(c)

	const privateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nunit-key\n-----END OPENSSH PRIVATE KEY-----\n"

	err := st.SetUnitVirtualSSHHostKey(c.Context(), unitUUID, privateKey)
	c.Assert(err, tc.ErrorIsNil)

	got, err := st.GetUnitVirtualSSHHostKey(c.Context(), unitUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, privateKey)
}

func (s *modelStateSuite) TestSetUnitVirtualSSHHostKeyUpserts(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())
	unitUUID := s.createUnit(c)

	const key1 = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-one\n-----END OPENSSH PRIVATE KEY-----\n"
	const key2 = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-two\n-----END OPENSSH PRIVATE KEY-----\n"

	err := st.SetUnitVirtualSSHHostKey(c.Context(), unitUUID, key1)
	c.Assert(err, tc.ErrorIsNil)

	err = st.SetUnitVirtualSSHHostKey(c.Context(), unitUUID, key2)
	c.Assert(err, tc.ErrorIsNil)

	got, err := st.GetUnitVirtualSSHHostKey(c.Context(), unitUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, key2)
}
