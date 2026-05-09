package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/spf13/cobra"
)

const (
	reportNameRotation        = "rotation"
	rotationConfidenceLimited = "limited"
	rotationLimitationsLocal  = "local registry and Transit metadata cannot prove every Kubernetes object " +
		"or retained backup has been rewritten"
)

type rotationReport struct {
	Name                 string
	StateLoaded          bool
	StateGeneration      uint64
	StateHash            string
	ActiveKeyIDHash      string
	ActiveTransitVersion int
	LatestTransitVersion int
	RotationState        status.RotationState
	PendingKeyIDHash     string
	PendingVersion       int
	PendingStableCount   int
	PendingPromotesAfter time.Time
	Confidence           string
	Limitations          string
}

func newRotationPlanCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rotation-plan",
		Short: "Report rotation state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			report, err := buildRotationReport(commandContext(cmd), cfg, "rotation-plan")
			if err != nil {
				return cli.WithExitCode(cli.ExitCheckFailed, err)
			}
			printRotationReport(cmd.OutOrStdout(), report)
			return nil
		},
	}
}

func newVerifyRotationCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "verify-rotation",
		Short: "Verify rotation migration state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			report, err := buildRotationReport(commandContext(cmd), cfg, "verify-rotation")
			if err != nil {
				return cli.WithExitCode(cli.ExitCheckFailed, err)
			}
			report.Confidence = rotationConfidenceLimited
			report.Limitations = rotationLimitationsLocal
			printRotationReport(cmd.OutOrStdout(), report)
			return nil
		},
	}
}

func buildRotationReport(ctx context.Context, cfg config.Config, name string) (rotationReport, error) {
	report := rotationReport{Name: name, RotationState: status.RotationStateUnknown}
	state, _, err := keyregistry.LoadStateFile(cfg.State.Path, keyregistry.StateLoadOptions{})
	if err != nil && !errors.Is(err, keyregistry.ErrStateNotFound) {
		return rotationReport{}, err
	}
	if err == nil {
		report.StateLoaded = true
		report.StateGeneration = state.Generation
		report.StateHash = state.CurrentHash
		active, activeErr := state.ActiveSnapshot()
		if activeErr != nil {
			return rotationReport{}, activeErr
		}
		report.ActiveTransitVersion = active.TransitVersion
		report.ActiveKeyIDHash = aad.HashValue(active.KubernetesKeyID)
		report.RotationState = status.RotationStateActive
		if pending, ok := pendingSnapshot(state); ok {
			report.RotationState = status.RotationStatePending
			report.PendingVersion = pending.TransitVersion
			report.PendingKeyIDHash = aad.HashValue(pending.KubernetesKeyID)
			report.PendingStableCount = pending.StableObservationCount
			if pending.StableAtUnix != 0 {
				stableAt := time.Unix(pending.StableAtUnix, 0).UTC()
				report.PendingPromotesAfter = stableAt.Add(cfg.Rotation.ActivationDelay)
			}
		}
	}

	clients, err := rotationClients(ctx, cfg)
	if err != nil {
		if report.StateLoaded {
			return report, nil
		}
		return rotationReport{}, err
	}
	profile, err := clients.transitClient.ReadKeyProfile(ctx, cfg.Transit.MountPath, cfg.Transit.KeyName)
	if err != nil {
		if report.StateLoaded {
			return report, nil
		}
		return rotationReport{}, err
	}
	report.LatestTransitVersion = profile.LatestVersion
	if !report.StateLoaded {
		observer, observerErr := status.NewObserver(snapshotScope(cfg), rotationPolicy(cfg))
		if observerErr != nil {
			return rotationReport{}, observerErr
		}
		rebuilt, rebuildErr := observer.RebuildState(profile, time.Now().UTC())
		if rebuildErr != nil {
			return rotationReport{}, rebuildErr
		}
		active, activeErr := rebuilt.ActiveSnapshot()
		if activeErr != nil {
			return rotationReport{}, activeErr
		}
		report.ActiveTransitVersion = active.TransitVersion
		report.ActiveKeyIDHash = aad.HashValue(active.KubernetesKeyID)
		report.RotationState = status.RotationStateActive
	}
	return report, nil
}

func rotationClients(ctx context.Context, cfg config.Config) (diagnosticClients, error) {
	report := cli.Report{Name: reportNameRotation}
	clients, ok := authenticateForDiagnostics(ctx, &report, cfg)
	if !ok {
		return diagnosticClients{}, fmt.Errorf("OpenBao diagnostics unavailable")
	}
	return clients, nil
}

func pendingSnapshot(state keyregistry.StateFile) (keyregistry.SnapshotStateRecord, bool) {
	for _, record := range state.Snapshots {
		if keyregistry.SnapshotState(record.State) == keyregistry.StatePending {
			return record, true
		}
	}
	return keyregistry.SnapshotStateRecord{}, false
}

func printRotationReport(out io.Writer, report rotationReport) {
	_, _ = fmt.Fprintln(out, report.Name)
	_, _ = fmt.Fprintf(out, "stateLoaded: %t\n", report.StateLoaded)
	if report.StateLoaded {
		_, _ = fmt.Fprintf(out, "stateGeneration: %d\n", report.StateGeneration)
		_, _ = fmt.Fprintf(out, "stateHash: %s\n", report.StateHash)
	}
	_, _ = fmt.Fprintf(out, "rotationState: %s\n", report.RotationState)
	_, _ = fmt.Fprintf(out, "activeKeyIdHash: %s\n", report.ActiveKeyIDHash)
	_, _ = fmt.Fprintf(out, "activeTransitVersion: %d\n", report.ActiveTransitVersion)
	_, _ = fmt.Fprintf(out, "latestTransitVersion: %d\n", report.LatestTransitVersion)
	if report.RotationState == status.RotationStatePending {
		_, _ = fmt.Fprintf(out, "pendingKeyIdHash: %s\n", report.PendingKeyIDHash)
		_, _ = fmt.Fprintf(out, "pendingTransitVersion: %d\n", report.PendingVersion)
		_, _ = fmt.Fprintf(out, "pendingStableObservationCount: %d\n", report.PendingStableCount)
		if !report.PendingPromotesAfter.IsZero() {
			_, _ = fmt.Fprintf(out, "pendingPromotesAfter: %s\n", report.PendingPromotesAfter.Format(time.RFC3339))
		}
	}
	if report.Confidence != "" {
		_, _ = fmt.Fprintf(out, "confidence: %s\n", report.Confidence)
	}
	if report.Limitations != "" {
		_, _ = fmt.Fprintf(out, "limitations: %s\n", report.Limitations)
	}
}
