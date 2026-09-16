// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model_test

import (
	"context"
	"testing"

	"github.com/juju/names/v6"
	"github.com/juju/tc"

	"github.com/juju/juju/apiserver/common/model"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/permission"
	coreuser "github.com/juju/juju/core/user"
	coreusertesting "github.com/juju/juju/core/user/testing"
	modelerrors "github.com/juju/juju/domain/model/errors"
	"github.com/juju/juju/internal/testhelpers"
	"github.com/juju/juju/rpc/params"
)

type modelUserService struct {
	users map[coreuser.Name]coremodel.ModelUserInfo
}

func (s modelUserService) GetModelUsers(ctx context.Context, modelUUID coremodel.UUID) ([]coremodel.ModelUserInfo, error) {
	var users []coremodel.ModelUserInfo
	for _, u := range s.users {
		users = append(users, u)
	}
	return users, nil
}

func (s modelUserService) GetModelUser(ctx context.Context, modelUUID coremodel.UUID, name coreuser.Name) (coremodel.ModelUserInfo, error) {
	u, ok := s.users[name]
	if !ok {
		return coremodel.ModelUserInfo{}, modelerrors.UserNotFoundOnModel
	}
	return u, nil
}

type modelUserSuite struct {
	testhelpers.IsolationSuite

	modelTag names.ModelTag
	apiUser  coreuser.Name
	userInfo coremodel.ModelUserInfo
}

func TestModelUserSuite(t *testing.T) {
	tc.Run(t, &modelUserSuite{})
}

func (s *modelUserSuite) SetUpTest(c *tc.C) {
	s.modelTag = names.NewModelTag("00000000-0000-0000-0000-000000000001")
	s.apiUser = coreusertesting.GenNewName(c, "bob@external")
	s.userInfo = coremodel.ModelUserInfo{
		Name:   s.apiUser,
		Access: permission.ReadAccess,
	}
}

func (s *modelUserSuite) TestModelUserInfoAdmin(c *tc.C) {
	service := modelUserService{users: map[coreuser.Name]coremodel.ModelUserInfo{
		s.apiUser: s.userInfo,
	}}
	info, err := model.ModelUserInfo(c.Context(), service, s.modelTag, s.apiUser, true, permission.AdminAccess)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info, tc.HasLen, 1)
	c.Check(info[0].UserName, tc.Equals, s.apiUser.Name())
	c.Check(info[0].Access, tc.Equals, params.ModelReadAccess)
}

func (s *modelUserSuite) TestModelUserInfoNonAdminWithRecord(c *tc.C) {
	service := modelUserService{users: map[coreuser.Name]coremodel.ModelUserInfo{
		s.apiUser: s.userInfo,
	}}
	info, err := model.ModelUserInfo(c.Context(), service, s.modelTag, s.apiUser, false, permission.ReadAccess)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info, tc.HasLen, 1)
	c.Check(info[0].UserName, tc.Equals, s.apiUser.Name())
	c.Check(info[0].Access, tc.Equals, params.ModelReadAccess)
}

func (s *modelUserSuite) TestModelUserInfoNonAdminNoRecord(c *tc.C) {
	// A caller authorized by a delegator has no local model-user record.
	service := modelUserService{}
	info, err := model.ModelUserInfo(c.Context(), service, s.modelTag, s.apiUser, false, permission.ReadAccess)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info, tc.HasLen, 1)
	c.Check(info[0].UserName, tc.Equals, s.apiUser.Name())
	c.Check(info[0].DisplayName, tc.Equals, "")
	c.Check(info[0].Access, tc.Equals, params.ModelReadAccess)
	c.Check(info[0].LastConnection, tc.IsNil)
}
