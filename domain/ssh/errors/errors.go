// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package errors

import "github.com/juju/juju/internal/errors"

const (
	// HostKeyNotFound describes an error that occurs when an SSH host key
	// cannot be found.
	HostKeyNotFound = errors.ConstError("SSH host key not found")
)
