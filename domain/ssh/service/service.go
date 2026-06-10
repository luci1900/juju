// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/trace"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/internal/errors"
)

// ControllerState describes the persistence methods required for controller
// SSH host key operations. Implementations store data in the controller DB.
type ControllerState interface {
	// GetControllerSSHHostKey returns the PEM-encoded private key for the
	// SSH jump server. The following errors can be expected:
	// - [ssherrors.HostKeyNotFound] when no key has been stored yet.
	GetControllerSSHHostKey(ctx context.Context) (string, error)

	// SetControllerSSHHostKey stores (or replaces) the PEM-encoded private
	// key for the SSH jump server.
	SetControllerSSHHostKey(ctx context.Context, privateKey string) error
}

// ModelState describes the persistence methods required for model-scoped
// virtual SSH host key operations. Implementations store data in the model DB.
type ModelState interface {
	// GetMachineVirtualSSHHostKey returns the PEM-encoded private key for the
	// given machine. The following errors can be expected:
	// - [ssherrors.HostKeyNotFound] when no key has been stored yet.
	GetMachineVirtualSSHHostKey(ctx context.Context, machineUUID machine.UUID) (string, error)

	// SetMachineVirtualSSHHostKey stores (or replaces) the PEM-encoded private
	// key for the given machine.
	SetMachineVirtualSSHHostKey(ctx context.Context, machineUUID machine.UUID, privateKey string) error

	// GetUnitVirtualSSHHostKey returns the PEM-encoded private key for the
	// given unit. The following errors can be expected:
	// - [ssherrors.HostKeyNotFound] when no key has been stored yet.
	GetUnitVirtualSSHHostKey(ctx context.Context, unitUUID unit.UUID) (string, error)

	// SetUnitVirtualSSHHostKey stores (or replaces) the PEM-encoded private
	// key for the given unit.
	SetUnitVirtualSSHHostKey(ctx context.Context, unitUUID unit.UUID, privateKey string) error
}

// ControllerService manages SSH host keys for the controller jump server.
// It wraps a [ControllerState] backed by the controller database.
type ControllerService struct {
	st ControllerState
}

// NewControllerService returns a new ControllerService backed by the given
// state implementation.
func NewControllerService(st ControllerState) *ControllerService {
	return &ControllerService{st: st}
}

// GetControllerSSHHostKey returns the PEM-encoded private key for the SSH
// jump server. The following errors can be expected:
// - [ssherrors.HostKeyNotFound] when no key has been stored yet.
func (s *ControllerService) GetControllerSSHHostKey(ctx context.Context) (string, error) {
	ctx, span := trace.Start(ctx, trace.NameFromFunc())
	defer span.End()

	key, err := s.st.GetControllerSSHHostKey(ctx)
	if err != nil {
		return "", errors.Capture(err)
	}
	return key, nil
}

// SetControllerSSHHostKey stores (or replaces) the PEM-encoded private key
// for the SSH jump server.
func (s *ControllerService) SetControllerSSHHostKey(ctx context.Context, privateKey string) error {
	ctx, span := trace.Start(ctx, trace.NameFromFunc())
	defer span.End()

	return errors.Capture(s.st.SetControllerSSHHostKey(ctx, privateKey))
}

// ModelService manages virtual SSH host keys for machine and unit routing
// targets. It wraps a [ModelState] backed by the model database.
type ModelService struct {
	st ModelState
}

// NewModelService returns a new ModelService backed by the given state
// implementation.
func NewModelService(st ModelState) *ModelService {
	return &ModelService{st: st}
}

// GetMachineVirtualSSHHostKey returns the PEM-encoded private key for the
// given machine. The following errors can be expected:
// - [ssherrors.HostKeyNotFound] when no key has been stored yet.
func (s *ModelService) GetMachineVirtualSSHHostKey(ctx context.Context, machineUUID machine.UUID) (string, error) {
	ctx, span := trace.Start(ctx, trace.NameFromFunc())
	defer span.End()

	key, err := s.st.GetMachineVirtualSSHHostKey(ctx, machineUUID)
	if err != nil {
		return "", errors.Capture(err)
	}
	return key, nil
}

// SetMachineVirtualSSHHostKey stores (or replaces) the PEM-encoded private
// key for the given machine.
func (s *ModelService) SetMachineVirtualSSHHostKey(ctx context.Context, machineUUID machine.UUID, privateKey string) error {
	ctx, span := trace.Start(ctx, trace.NameFromFunc())
	defer span.End()

	return errors.Capture(s.st.SetMachineVirtualSSHHostKey(ctx, machineUUID, privateKey))
}

// GetUnitVirtualSSHHostKey returns the PEM-encoded private key for the given
// unit. The following errors can be expected:
// - [ssherrors.HostKeyNotFound] when no key has been stored yet.
func (s *ModelService) GetUnitVirtualSSHHostKey(ctx context.Context, unitUUID unit.UUID) (string, error) {
	ctx, span := trace.Start(ctx, trace.NameFromFunc())
	defer span.End()

	key, err := s.st.GetUnitVirtualSSHHostKey(ctx, unitUUID)
	if err != nil {
		return "", errors.Capture(err)
	}
	return key, nil
}

// SetUnitVirtualSSHHostKey stores (or replaces) the PEM-encoded private key
// for the given unit.
func (s *ModelService) SetUnitVirtualSSHHostKey(ctx context.Context, unitUUID unit.UUID, privateKey string) error {
	ctx, span := trace.Start(ctx, trace.NameFromFunc())
	defer span.End()

	return errors.Capture(s.st.SetUnitVirtualSSHHostKey(ctx, unitUUID, privateKey))
}
