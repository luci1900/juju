// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"context"

	"github.com/juju/names/v6"

	"github.com/juju/juju/core/permission"
	coreuser "github.com/juju/juju/core/user"
	accesserrors "github.com/juju/juju/domain/access/errors"
	"github.com/juju/juju/internal/errors"
)

// UserAccessFunc represents a func that can answer the question about what
// level of access a user has for a given target.
type UserAccessFunc func(ctx context.Context, userName coreuser.Name, target permission.ID) (permission.Access, error)

// tagKindPermission describes how a tag kind maps onto a permission
// object type, along with the validator for access levels on that
// object type.
type tagKindPermission struct {
	objectType permission.ObjectType
	validate   func(permission.Access) error
}

// permissionsByTagKind is the single source of truth for which tag
// kinds carry permissions, and how. Both HasPermission and
// UserAccessLevel look up here rather than maintaining their own
// copies of this mapping, so the two can't drift out of sync.
var permissionsByTagKind = map[string]tagKindPermission{
	names.ControllerTagKind:       {permission.Controller, permission.ValidateControllerAccess},
	names.ModelTagKind:            {permission.Model, permission.ValidateModelAccess},
	names.ApplicationOfferTagKind: {permission.Offer, permission.ValidateOfferAccess},
	names.CloudTagKind:            {permission.Cloud, permission.ValidateCloudAccess},
}

// HasPermission returns true if the specified user has the specified
// permission on target.
func HasPermission(
	ctx context.Context,
	accessGetter UserAccessFunc,
	utag names.Tag,
	requestedPermission permission.Access,
	target names.Tag,
) (bool, error) {
	tkp, ok := permissionsByTagKind[target.Kind()]
	if !ok {
		return false, nil
	}
	objectType := tkp.objectType
	if err := tkp.validate(requestedPermission); err != nil {
		return false, nil
	}

	userTag, ok := utag.(names.UserTag)
	if !ok {
		// Reveal no more than is strictly necessary.
		return false, nil
	}

	userAccess, err := accessGetter(ctx, coreuser.NameFromTag(userTag), permission.ID{
		ObjectType: objectType,
		Key:        target.Id(),
	})
	if err != nil && !errors.IsOneOf(err,
		accesserrors.AccessNotFound,
		accesserrors.UserNotFound,
		accesserrors.PermissionNotFound,
	) {
		return false, errors.Errorf("while obtaining %s user: %w", target.Kind(), err)
	}
	if userAccess == permission.NoAccess {
		return false, nil
	}

	modelPermission := userAccess.EqualOrGreaterModelAccessThan(requestedPermission) && target.Kind() == names.ModelTagKind
	controllerPermission := userAccess.EqualOrGreaterControllerAccessThan(requestedPermission) && target.Kind() == names.ControllerTagKind
	offerPermission := userAccess.EqualOrGreaterOfferAccessThan(requestedPermission) && target.Kind() == names.ApplicationOfferTagKind
	cloudPermission := userAccess.EqualOrGreaterCloudAccessThan(requestedPermission) && target.Kind() == names.CloudTagKind
	if !controllerPermission && !modelPermission && !offerPermission && !cloudPermission {
		return false, nil
	}
	return true, nil
}

// UserAccessLevel resolves the caller's access level on target in a
// single call, using accessGetter directly rather than probing candidate
// levels one at a time. It returns [permission.NoAccess] if the caller's
// tag is not a user, the tag kind has no associated permission object
// type, or the user has no access recorded for the target.
func UserAccessLevel(
	ctx context.Context,
	accessGetter UserAccessFunc,
	utag names.Tag,
	target names.Tag,
) (permission.Access, error) {
	tkp, ok := permissionsByTagKind[target.Kind()]
	if !ok {
		return permission.NoAccess, nil
	}
	objectType := tkp.objectType

	userTag, ok := utag.(names.UserTag)
	if !ok {
		// Reveal no more than is strictly necessary.
		return permission.NoAccess, nil
	}

	userAccess, err := accessGetter(ctx, coreuser.NameFromTag(userTag), permission.ID{
		ObjectType: objectType,
		Key:        target.Id(),
	})
	if err != nil && !errors.IsOneOf(err,
		accesserrors.AccessNotFound,
		accesserrors.UserNotFound,
		accesserrors.PermissionNotFound,
	) {
		return permission.NoAccess, errors.Errorf("while obtaining %s user: %w", target.Kind(), err)
	}
	return userAccess, nil
}
