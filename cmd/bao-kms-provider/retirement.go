package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/spf13/cobra"
)

type retirementOptions struct {
	BeforeVersion     int
	Apply             bool
	ExpectedStateHash string
}

type retiredVersionReport struct {
	TransitVersion int    `json:"transitVersion"`
	KeyIDHash      string `json:"keyIdHash"`
}

type retirementReport struct {
	Applied         bool                   `json:"applied"`
	BeforeVersion   int                    `json:"beforeVersion"`
	StateHash       string                 `json:"stateHash"`
	NextStateHash   string                 `json:"nextStateHash"`
	NextGeneration  uint64                 `json:"nextGeneration"`
	ActiveKeyIDHash string                 `json:"activeKeyIdHash"`
	RemovedVersions []retiredVersionReport `json:"removedVersions"`
	Limitations     string                 `json:"limitations"`
}

func newRetireVersionsCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	var opts retirementOptions
	var output string
	cmd := &cobra.Command{
		Use:   "retire-versions",
		Short: "Plan or apply operator-authorized retirement of historical decrypt keys",
		Long: "Plan removal of historical versions from local decrypt lookup. Before applying, " +
			"verify migration and backup evidence, stop this node's provider, and run as its OS user.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.BeforeVersion <= 1 || (opts.Apply && opts.ExpectedStateHash == "") {
				return cli.WithExitCode(cli.ExitUsage, fmt.Errorf(
					"--before-version must exceed 1; --apply also requires --expected-state-hash from a reviewed plan"))
			}
			if normalizeOutputFormat(output) != outputFormatText && normalizeOutputFormat(output) != outputFormatJSON {
				return unsupportedOutputFormat(output)
			}
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			report, err := runRetirement(commandContext(cmd), cfg, opts, readRetirementProfile)
			if err != nil {
				return cli.WithExitCode(cli.ExitCheckFailed, err)
			}
			return printRetirementReport(cmd.OutOrStdout(), report, output)
		},
	}
	cmd.Flags().IntVar(&opts.BeforeVersion, "before-version", 0, "Retire historical Transit versions below this version")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false,
		"Persist retirement after operator verification of migration and backups")
	cmd.Flags().StringVar(&opts.ExpectedStateHash, "expected-state-hash", "", "Exact stateHash from the reviewed plan")
	addOutputFlag(cmd, &output)
	return cmd
}

func readRetirementProfile(ctx context.Context, cfg config.Config) (openbao.KeyProfile, error) {
	clients, err := rotationClients(ctx, cfg)
	if err != nil {
		return openbao.KeyProfile{}, err
	}
	return clients.transitClient.ReadKeyProfile(ctx, cfg.Transit.MountPath, cfg.Transit.KeyName)
}

func runRetirement(
	ctx context.Context,
	cfg config.Config,
	opts retirementOptions,
	readProfile func(context.Context, config.Config) (openbao.KeyProfile, error),
) (retirementReport, error) {
	if opts.Apply {
		lock, err := keyregistry.LockState(cfg.State.Path)
		if err != nil {
			return retirementReport{}, err
		}
		defer func() { _ = lock.Close() }()
	}
	loaded, err := loadRegistryStateWithCheckpoint(cfg.State.Path)
	if err != nil {
		return retirementReport{}, err
	}
	if opts.Apply && (opts.ExpectedStateHash == "" || opts.ExpectedStateHash != loaded.State.CurrentHash) {
		return retirementReport{}, fmt.Errorf("state hash differs from reviewed plan; generate and review a new plan")
	}
	next, err := keyregistry.RetireVersions(loaded.State, opts.BeforeVersion)
	if err != nil {
		return retirementReport{}, err
	}
	profile, err := readProfile(ctx, cfg)
	if err != nil {
		return retirementReport{}, fmt.Errorf("read Transit metadata for retirement: %w", err)
	}
	if err := validateRetirementProfile(cfg, next, profile); err != nil {
		return retirementReport{}, err
	}
	report := newRetirementReport(loaded.State, next, opts.BeforeVersion)
	if opts.Apply {
		if err := ctx.Err(); err != nil {
			return retirementReport{}, err
		}
		if err := (status.FileStateStore{Path: cfg.State.Path}).Save(next); err != nil {
			return retirementReport{}, fmt.Errorf("save retirement state/checkpoint: %w; inspect state before retrying", err)
		}
		report.Applied = true
	}
	return report, nil
}

func newRetirementReport(previous, next keyregistry.StateFile, beforeVersion int) retirementReport {
	report := retirementReport{
		BeforeVersion: beforeVersion, StateHash: previous.CurrentHash,
		NextStateHash: next.CurrentHash, NextGeneration: next.Generation,
		ActiveKeyIDHash: aad.HashValue(next.ActiveKeyID),
		RemovedVersions: make([]retiredVersionReport, 0), Limitations: rotationLimitationsLocal,
	}
	for _, record := range previous.Snapshots {
		if keyregistry.SnapshotState(record.State) == keyregistry.StateRetired && record.TransitVersion < beforeVersion {
			report.RemovedVersions = append(report.RemovedVersions, retiredVersionReport{
				TransitVersion: record.TransitVersion, KeyIDHash: aad.HashValue(record.KubernetesKeyID),
			})
		}
	}
	return report
}

func validateRetirementProfile(cfg config.Config, state keyregistry.StateFile, profile openbao.KeyProfile) error {
	active, err := state.ActiveSnapshot()
	if err != nil {
		return err
	}
	if profile.LatestVersion != active.TransitVersion {
		return fmt.Errorf("retirement requires local active version to equal Transit latest_version")
	}
	observer, err := status.NewObserver(snapshotScope(cfg), rotationPolicy(cfg))
	if err != nil {
		return err
	}
	return observer.ValidateStateProfile(state, profile)
}

func printRetirementReport(out io.Writer, report retirementReport, output string) error {
	if normalizeOutputFormat(output) == outputFormatJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, err := fmt.Fprintf(out,
		"retire-versions\napplied: %t\nbeforeVersion: %d\nstateHash: %s\nnextStateHash: %s\n"+
			"nextGeneration: %d\nactiveKeyIdHash: %s\nlimitations: %s\n",
		report.Applied, report.BeforeVersion, report.StateHash, report.NextStateHash,
		report.NextGeneration, report.ActiveKeyIDHash, report.Limitations)
	if err != nil {
		return err
	}
	for _, version := range report.RemovedVersions {
		if _, err := fmt.Fprintf(out, "remove: transitVersion=%d keyIdHash=%s\n",
			version.TransitVersion, version.KeyIDHash); err != nil {
			return err
		}
	}
	return nil
}
