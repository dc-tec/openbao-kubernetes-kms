//go:build !certauth_pkcs11 && !certauth_spiffe

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

func TestBuildAuthManagerRejectsCertAuthInDefaultBuild(t *testing.T) {
	cfg := config.Config{}
	cfg.Auth.Method = "cert"

	_, err := buildAuthManager(context.Background(), cfg, nil)
	if !errors.Is(err, auth.ErrAuthConfig) {
		t.Fatalf("expected auth config error, got %v", err)
	}
	if !strings.Contains(err.Error(), "certauth build variant") {
		t.Fatalf("expected certauth build variant error, got %v", err)
	}
}
