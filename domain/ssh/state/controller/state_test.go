// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"testing"

	"github.com/juju/tc"

	schematesting "github.com/juju/juju/domain/schema/testing"
	ssherrors "github.com/juju/juju/domain/ssh/errors"
)

type controllerStateSuite struct {
	schematesting.ControllerSuite
}

func TestControllerStateSuite(t *testing.T) {
	tc.Run(t, &controllerStateSuite{})
}

func (s *controllerStateSuite) TestGetControllerSSHHostKeyNotFound(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())

	_, err := st.GetControllerSSHHostKey(c.Context())
	c.Assert(err, tc.ErrorIs, ssherrors.HostKeyNotFound)
}

func (s *controllerStateSuite) TestSetAndGetControllerSSHHostKey(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())

	const privateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\ntest-key\n-----END OPENSSH PRIVATE KEY-----\n"

	err := st.SetControllerSSHHostKey(c.Context(), privateKey)
	c.Assert(err, tc.ErrorIsNil)

	got, err := st.GetControllerSSHHostKey(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, privateKey)
}

func (s *controllerStateSuite) TestSetControllerSSHHostKeyUpserts(c *tc.C) {
	st := NewState(s.TxnRunnerFactory())

	const key1 = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-one\n-----END OPENSSH PRIVATE KEY-----\n"
	const key2 = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-two\n-----END OPENSSH PRIVATE KEY-----\n"

	// Set key1 first.
	err := st.SetControllerSSHHostKey(c.Context(), key1)
	c.Assert(err, tc.ErrorIsNil)

	// Overwrite with key2.
	err = st.SetControllerSSHHostKey(c.Context(), key2)
	c.Assert(err, tc.ErrorIsNil)

	// Verify only the latest key is stored.
	got, err := st.GetControllerSSHHostKey(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, key2)
}
