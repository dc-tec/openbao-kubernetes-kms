package main

import (
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRotationReportRejectsMissingStateAfterTransitRotation(t *testing.T) {
	cfg := loadCommandConfig(t)
	profile := commandTestProfile(func(profile *openbao.KeyProfile) {
		profile.LatestVersion = 2
		profile.VersionCreationTimes = append(profile.VersionCreationTimes, openbao.KeyVersion{
			Version:   2,
			CreatedAt: time.Unix(1_778_277_660, 0).UTC(),
		})
	})

	_, err := applyTransitProfileToRotationReport(
		cfg,
		rotationReport{Name: reportNameRotation, RotationState: status.RotationStateUnknown},
		profile,
		time.Now().UTC(),
	)
	if !errors.Is(err, status.ErrStateUnavailable) {
		t.Fatalf("expected missing rotated state to fail closed, got %v", err)
	}
}

func TestRotationReportAllowsMissingStateForInitialBootstrap(t *testing.T) {
	cfg := loadCommandConfig(t)
	profile := commandTestProfile(nil)

	report, err := applyTransitProfileToRotationReport(
		cfg,
		rotationReport{Name: reportNameRotation, RotationState: status.RotationStateUnknown},
		profile,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("expected initial missing state report to rebuild: %v", err)
	}
	if report.RotationState != status.RotationStateActive {
		t.Fatalf("unexpected rotation state: %s", report.RotationState)
	}
	if report.ActiveTransitVersion != 1 || report.ActiveKeyIDHash == "" {
		t.Fatalf("expected synthesized initial active version in report: %#v", report)
	}
}
