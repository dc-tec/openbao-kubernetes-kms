package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
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
	Name                      string
	StateLoaded               bool
	StateGeneration           uint64
	StateHash                 string
	StateCheckpointLoaded     bool
	StateCheckpointStatus     string
	StateCheckpointGeneration uint64
	StateCheckpointHash       string
	StateBootstrapEligible    bool
	StateBootstrapReason      string
	ActiveKeyIDHash           string
	ActiveTransitVersion      int
	LatestTransitVersion      int
	RotationState             status.RotationState
	PendingKeyIDHash          string
	PendingVersion            int
	PendingStableCount        int
	PendingPromotesAfter      time.Time
	Confidence                string
	Limitations               string
}

func newRotationPlanCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	var output string
	cmd := &cobra.Command{
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
			return printRotationReport(cmd.OutOrStdout(), report, output)
		},
	}
	addOutputFlag(cmd, &output)
	return cmd
}

func newVerifyRotationCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "verify-rotation",
		Short: "Report local rotation preflight state",
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
			return printRotationReport(cmd.OutOrStdout(), report, output)
		},
	}
	addOutputFlag(cmd, &output)
	return cmd
}

func buildRotationReport(ctx context.Context, cfg config.Config, name string) (rotationReport, error) {
	report := rotationReport{Name: name, RotationState: status.RotationStateUnknown}
	loaded, err := loadRegistryStateWithCheckpoint(cfg.State.Path)
	if err != nil && !errors.Is(err, keyregistry.ErrStateNotFound) {
		return rotationReport{}, err
	}
	report.StateCheckpointLoaded = loaded.CheckpointLoaded
	report.StateCheckpointStatus = loaded.CheckpointStatus
	report.StateCheckpointGeneration = loaded.CheckpointGeneration
	report.StateCheckpointHash = loaded.CheckpointHash
	if loaded.StateLoaded {
		state := loaded.State
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
	return applyTransitProfileToRotationReport(cfg, report, profile, time.Now().UTC())
}

func applyTransitProfileToRotationReport(
	cfg config.Config,
	report rotationReport,
	profile openbao.KeyProfile,
	now time.Time,
) (rotationReport, error) {
	report.LatestTransitVersion = profile.LatestVersion
	if !report.StateLoaded {
		assessment := status.AssessAutoBootstrapState(profile)
		report.StateBootstrapEligible = assessment.Allowed
		report.StateBootstrapReason = assessment.Reason
		if !assessment.Allowed {
			return rotationReport{}, fmt.Errorf(
				"%w: local registry state is absent and cannot be auto-bootstrapped: %s",
				status.ErrStateUnavailable,
				assessment.Reason,
			)
		}
		observer, observerErr := status.NewObserver(snapshotScope(cfg), rotationPolicy(cfg))
		if observerErr != nil {
			return rotationReport{}, observerErr
		}
		rebuilt, rebuildErr := observer.RebuildState(profile, now)
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

func printRotationReport(out io.Writer, report rotationReport, output string) error {
	switch normalizeOutputFormat(output) {
	case outputFormatText:
		printRotationReportText(out, report)
		return nil
	case outputFormatJSON:
		return printRotationReportJSON(out, report)
	default:
		return unsupportedOutputFormat(output)
	}
}

func printRotationReportText(out io.Writer, report rotationReport) {
	_, _ = fmt.Fprintln(out, report.Name)
	_, _ = fmt.Fprintf(out, "stateLoaded: %t\n", report.StateLoaded)
	if report.StateLoaded {
		_, _ = fmt.Fprintf(out, "stateGeneration: %d\n", report.StateGeneration)
		_, _ = fmt.Fprintf(out, "stateHash: %s\n", report.StateHash)
	}
	_, _ = fmt.Fprintf(out, "stateCheckpointLoaded: %t\n", report.StateCheckpointLoaded)
	if report.StateCheckpointStatus != "" {
		_, _ = fmt.Fprintf(out, "stateCheckpointStatus: %s\n", report.StateCheckpointStatus)
	}
	if report.StateCheckpointLoaded {
		_, _ = fmt.Fprintf(out, "stateCheckpointGeneration: %d\n", report.StateCheckpointGeneration)
		_, _ = fmt.Fprintf(out, "stateCheckpointHash: %s\n", report.StateCheckpointHash)
	}
	if report.StateBootstrapReason != "" {
		_, _ = fmt.Fprintf(out, "stateBootstrapEligible: %t\n", report.StateBootstrapEligible)
		_, _ = fmt.Fprintf(out, "stateBootstrapReason: %s\n", report.StateBootstrapReason)
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

type rotationReportJSON struct {
	Name                          string               `json:"name"`
	StateLoaded                   bool                 `json:"stateLoaded"`
	StateGeneration               uint64               `json:"stateGeneration,omitempty"`
	StateHash                     string               `json:"stateHash,omitempty"`
	StateCheckpointLoaded         bool                 `json:"stateCheckpointLoaded"`
	StateCheckpointStatus         string               `json:"stateCheckpointStatus,omitempty"`
	StateCheckpointGeneration     uint64               `json:"stateCheckpointGeneration,omitempty"`
	StateCheckpointHash           string               `json:"stateCheckpointHash,omitempty"`
	StateBootstrapEligible        bool                 `json:"stateBootstrapEligible,omitempty"`
	StateBootstrapReason          string               `json:"stateBootstrapReason,omitempty"`
	ActiveKeyIDHash               string               `json:"activeKeyIdHash,omitempty"`
	ActiveTransitVersion          int                  `json:"activeTransitVersion,omitempty"`
	LatestTransitVersion          int                  `json:"latestTransitVersion,omitempty"`
	RotationState                 status.RotationState `json:"rotationState"`
	PendingKeyIDHash              string               `json:"pendingKeyIdHash,omitempty"`
	PendingTransitVersion         int                  `json:"pendingTransitVersion,omitempty"`
	PendingStableObservationCount int                  `json:"pendingStableObservationCount,omitempty"`
	PendingPromotesAfter          string               `json:"pendingPromotesAfter,omitempty"`
	Confidence                    string               `json:"confidence,omitempty"`
	Limitations                   string               `json:"limitations,omitempty"`
}

func printRotationReportJSON(out io.Writer, report rotationReport) error {
	jsonReport := rotationReportJSON{
		Name:                          report.Name,
		StateLoaded:                   report.StateLoaded,
		StateGeneration:               report.StateGeneration,
		StateHash:                     report.StateHash,
		StateCheckpointLoaded:         report.StateCheckpointLoaded,
		StateCheckpointStatus:         report.StateCheckpointStatus,
		StateCheckpointGeneration:     report.StateCheckpointGeneration,
		StateCheckpointHash:           report.StateCheckpointHash,
		StateBootstrapEligible:        report.StateBootstrapEligible,
		StateBootstrapReason:          report.StateBootstrapReason,
		ActiveKeyIDHash:               report.ActiveKeyIDHash,
		ActiveTransitVersion:          report.ActiveTransitVersion,
		LatestTransitVersion:          report.LatestTransitVersion,
		RotationState:                 report.RotationState,
		PendingKeyIDHash:              report.PendingKeyIDHash,
		PendingTransitVersion:         report.PendingVersion,
		PendingStableObservationCount: report.PendingStableCount,
		Confidence:                    report.Confidence,
		Limitations:                   report.Limitations,
	}
	if !report.PendingPromotesAfter.IsZero() {
		jsonReport.PendingPromotesAfter = report.PendingPromotesAfter.Format(time.RFC3339)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonReport)
}
