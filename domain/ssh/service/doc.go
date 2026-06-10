// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package service provides domain service contracts for SSH host key
// management. Ownership is split explicitly:
//
//   - [ControllerService] manages the jump server's host key, stored in the
//     controller database via [ControllerState].
//   - [ModelService] manages per-machine and per-unit virtual host keys, stored
//     in the model database via [ModelState].
package service
