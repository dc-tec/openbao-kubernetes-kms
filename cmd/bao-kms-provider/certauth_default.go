//go:build !certauth_pkcs11 && !certauth_spiffe

package main

import (
	"context"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

func newCertAuthManager(
	context.Context,
	config.Config,
	authRuntimeObserver,
) (*auth.Manager, error) {
	return nil, fmt.Errorf("%w: auth.method cert requires a certauth build variant", auth.ErrAuthConfig)
}

func checkLocalCertificateAuthForDoctor(
	_ context.Context,
	report *cli.Report,
	cfg config.Config,
) bool {
	sourceCheck := certificateSourceCheckID(cfg)
	report.Fail(sourceCheck, certificateSourceCheckTitle(cfg), "auth.method cert requires a certauth build variant")
	report.Skip(checkCertLocal, "Certificate identity", "certificate source is unavailable")
	return false
}
