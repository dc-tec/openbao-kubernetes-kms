//go:build !certauth_pkcs11 && !certauth_spiffe

package main

import (
	"context"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

func newCertAuthManager(
	context.Context,
	config.Config,
	authRuntimeObserver,
) (*auth.Manager, error) {
	return nil, fmt.Errorf("%w: auth.method cert requires a certauth build variant", auth.ErrAuthConfig)
}
