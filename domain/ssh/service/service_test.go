// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"testing"

	"github.com/juju/tc"
	"go.uber.org/mock/gomock"

	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/unit"
	ssherrors "github.com/juju/juju/domain/ssh/errors"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/testhelpers"
)

// ---- ControllerService tests ----

type controllerServiceSuite struct {
	testhelpers.IsolationSuite

	state *MockControllerState
}

func TestControllerServiceSuite(t *testing.T) {
	tc.Run(t, &controllerServiceSuite{})
}

func (s *controllerServiceSuite) setupMocks(c *tc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.state = NewMockControllerState(ctrl)
	return ctrl
}

func (s *controllerServiceSuite) TestGetControllerSSHHostKey(c *tc.C) {
	defer s.setupMocks(c).Finish()

	const key = "test-private-key"
	s.state.EXPECT().GetControllerSSHHostKey(gomock.Any()).Return(key, nil)

	svc := NewControllerService(s.state)
	got, err := svc.GetControllerSSHHostKey(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, key)
}

func (s *controllerServiceSuite) TestGetControllerSSHHostKeyNotFound(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().GetControllerSSHHostKey(gomock.Any()).Return("", ssherrors.HostKeyNotFound)

	svc := NewControllerService(s.state)
	_, err := svc.GetControllerSSHHostKey(c.Context())
	c.Assert(err, tc.ErrorIs, ssherrors.HostKeyNotFound)
}

func (s *controllerServiceSuite) TestSetControllerSSHHostKey(c *tc.C) {
	defer s.setupMocks(c).Finish()

	const key = "test-private-key"
	s.state.EXPECT().SetControllerSSHHostKey(gomock.Any(), key).Return(nil)

	svc := NewControllerService(s.state)
	err := svc.SetControllerSSHHostKey(c.Context(), key)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *controllerServiceSuite) TestSetControllerSSHHostKeyStateError(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().SetControllerSSHHostKey(gomock.Any(), gomock.Any()).Return(errors.New("db error"))

	svc := NewControllerService(s.state)
	err := svc.SetControllerSSHHostKey(c.Context(), "key")
	c.Assert(err, tc.ErrorMatches, "db error")
}

// ---- ModelService tests ----

type modelServiceSuite struct {
	testhelpers.IsolationSuite

	state *MockModelState
}

func TestModelServiceSuite(t *testing.T) {
	tc.Run(t, &modelServiceSuite{})
}

func (s *modelServiceSuite) setupMocks(c *tc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.state = NewMockModelState(ctrl)
	return ctrl
}

func (s *modelServiceSuite) TestGetMachineVirtualSSHHostKey(c *tc.C) {
	defer s.setupMocks(c).Finish()

	machineUUID := machine.UUID("machine-uuid-1")
	const key = "machine-private-key"
	s.state.EXPECT().GetMachineVirtualSSHHostKey(gomock.Any(), machineUUID).Return(key, nil)

	svc := NewModelService(s.state)
	got, err := svc.GetMachineVirtualSSHHostKey(c.Context(), machineUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, key)
}

func (s *modelServiceSuite) TestGetMachineVirtualSSHHostKeyNotFound(c *tc.C) {
	defer s.setupMocks(c).Finish()

	machineUUID := machine.UUID("machine-uuid-1")
	s.state.EXPECT().GetMachineVirtualSSHHostKey(gomock.Any(), machineUUID).Return("", ssherrors.HostKeyNotFound)

	svc := NewModelService(s.state)
	_, err := svc.GetMachineVirtualSSHHostKey(c.Context(), machineUUID)
	c.Assert(err, tc.ErrorIs, ssherrors.HostKeyNotFound)
}

func (s *modelServiceSuite) TestSetMachineVirtualSSHHostKey(c *tc.C) {
	defer s.setupMocks(c).Finish()

	machineUUID := machine.UUID("machine-uuid-1")
	const key = "machine-private-key"
	s.state.EXPECT().SetMachineVirtualSSHHostKey(gomock.Any(), machineUUID, key).Return(nil)

	svc := NewModelService(s.state)
	err := svc.SetMachineVirtualSSHHostKey(c.Context(), machineUUID, key)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *modelServiceSuite) TestGetUnitVirtualSSHHostKey(c *tc.C) {
	defer s.setupMocks(c).Finish()

	unitUUID := unit.UUID("unit-uuid-1")
	const key = "unit-private-key"
	s.state.EXPECT().GetUnitVirtualSSHHostKey(gomock.Any(), unitUUID).Return(key, nil)

	svc := NewModelService(s.state)
	got, err := svc.GetUnitVirtualSSHHostKey(c.Context(), unitUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, key)
}

func (s *modelServiceSuite) TestGetUnitVirtualSSHHostKeyNotFound(c *tc.C) {
	defer s.setupMocks(c).Finish()

	unitUUID := unit.UUID("unit-uuid-1")
	s.state.EXPECT().GetUnitVirtualSSHHostKey(gomock.Any(), unitUUID).Return("", ssherrors.HostKeyNotFound)

	svc := NewModelService(s.state)
	_, err := svc.GetUnitVirtualSSHHostKey(c.Context(), unitUUID)
	c.Assert(err, tc.ErrorIs, ssherrors.HostKeyNotFound)
}

func (s *modelServiceSuite) TestSetUnitVirtualSSHHostKey(c *tc.C) {
	defer s.setupMocks(c).Finish()

	unitUUID := unit.UUID("unit-uuid-1")
	const key = "unit-private-key"
	s.state.EXPECT().SetUnitVirtualSSHHostKey(gomock.Any(), unitUUID, key).Return(nil)

	svc := NewModelService(s.state)
	err := svc.SetUnitVirtualSSHHostKey(c.Context(), unitUUID, key)
	c.Assert(err, tc.ErrorIsNil)
}
