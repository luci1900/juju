// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package sshserver

import (
	"bytes"
	"context"

	"github.com/juju/errors"
	"github.com/lestrrat-go/jwx/v3/jwt"
	ssh "github.com/tailscale/gliderssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/juju/juju/core/logger"
)

type authenticatedViaPublicKey struct{}

type userJWT struct{}

const externalAuthUser = "external-auth"

// JWTParser parses a JWT in the password authentication payload.
type JWTParser interface {
	// Parse parses the provided JWT string and returns a jwt.Token if valid.
	Parse(context.Context, string) (jwt.Token, error)
}

// UserPublicKeyService retrieves the public keys registered for a user.
type UserPublicKeyService interface {
	PublicKeys(context.Context, string) ([]gossh.PublicKey, error)
}

// authenticator implements the Authenticator interface for the SSH server.
// It handles public key authentication by users.
type authenticator struct {
	logger     logger.Logger
	jwtParser  JWTParser
	publicKeys UserPublicKeyService
}

// PublicKeyAuthentication implements a public key authentication handler.
func (a authenticator) PublicKeyAuthentication(ctx ssh.Context, key ssh.PublicKey) (bool, error) {
	keys, err := a.publicKeys.PublicKeys(ctx, ctx.User())
	if err != nil {
		return false, errors.Annotatef(err, "getting SSH public keys for user %q", ctx.User())
	}

	for _, authorizedKey := range keys {
		if bytes.Equal(key.Marshal(), authorizedKey.Marshal()) {
			ctx.SetValue(authenticatedViaPublicKey{}, true)
			return true, nil
		}
	}

	return false, nil
}

// PasswordAuthentication implements a password authentication handler.
// It supports two types of password authentication:
// 1. Decoding a JWT as the password for external-auth.
// 2. Reverse-tunnel authentication for machine agents.
func (a authenticator) PasswordAuthentication(ctx ssh.Context, password string) (bool, error) {
	ctx.SetValue(authenticatedViaPublicKey{}, false)

	switch ctx.User() {
	case externalAuthUser:
		token, err := a.jwtParser.Parse(ctx, password)
		if err != nil {
			return false, errors.Annotate(err, "parsing SSH JWT")
		}
		ctx.SetValue(userJWT{}, token)
		return true, nil
	}
	return false, nil
}
