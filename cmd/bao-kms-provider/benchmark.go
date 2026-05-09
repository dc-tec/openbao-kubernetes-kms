package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/spf13/cobra"
)

const (
	defaultBenchmarkIterations = 5
	benchmarkPlaintextBytes    = 32
	benchmarkSmokeAAD          = "openbao-kubernetes-kms/benchmark-smoke/v1"
	reportNameBenchmark        = "benchmark"
)

type benchmarkResult struct {
	Iterations      int
	EncryptDuration []time.Duration
	DecryptDuration []time.Duration
}

func newBenchmarkCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	var iterations int

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Measure provider and Transit latency",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if iterations <= 0 {
				return cli.WithExitCode(cli.ExitUsage, fmt.Errorf("iterations must be positive"))
			}
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			clients, err := benchmarkClients(commandContext(cmd), cfg)
			if err != nil {
				return cli.WithExitCode(cli.ExitCheckFailed, err)
			}
			result, err := runBenchmark(commandContext(cmd), cfg, clients.transitClient, iterations)
			if err != nil {
				return cli.WithExitCode(cli.ExitCheckFailed, err)
			}
			printBenchmark(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", defaultBenchmarkIterations, "Number of probe encrypt/decrypt iterations")
	return cmd
}

func benchmarkClients(ctx context.Context, cfg config.Config) (diagnosticClients, error) {
	report := cli.Report{Name: reportNameBenchmark}
	clients, ok := authenticateForDiagnostics(ctx, &report, cfg)
	if !ok {
		return diagnosticClients{}, fmt.Errorf("benchmark prerequisites failed")
	}
	return clients, nil
}

func runBenchmark(
	ctx context.Context,
	cfg config.Config,
	client openbao.TransitClient,
	iterations int,
) (benchmarkResult, error) {
	profile, err := client.ReadKeyProfile(ctx, cfg.Transit.MountPath, cfg.Transit.KeyName)
	if err != nil {
		return benchmarkResult{}, err
	}

	result := benchmarkResult{
		Iterations:      iterations,
		EncryptDuration: make([]time.Duration, 0, iterations),
		DecryptDuration: make([]time.Duration, 0, iterations),
	}
	for i := 0; i < iterations; i++ {
		plaintext, err := randomBytes(benchmarkPlaintextBytes)
		if err != nil {
			return benchmarkResult{}, err
		}
		aadValue := []byte(benchmarkSmokeAAD)

		start := time.Now()
		encrypted, err := client.Encrypt(ctx, openbao.EncryptRequest{
			MountPath:      cfg.Transit.MountPath,
			KeyName:        cfg.Transit.KeyName,
			Plaintext:      plaintext,
			AssociatedData: aadValue,
			KeyVersion:     profile.LatestVersion,
		})
		if err != nil {
			return benchmarkResult{}, err
		}
		result.EncryptDuration = append(result.EncryptDuration, time.Since(start))

		start = time.Now()
		decrypted, err := client.Decrypt(ctx, openbao.DecryptRequest{
			MountPath:      cfg.Transit.MountPath,
			KeyName:        cfg.Transit.KeyName,
			Ciphertext:     encrypted.Ciphertext,
			AssociatedData: aadValue,
		})
		if err != nil {
			return benchmarkResult{}, err
		}
		if !bytes.Equal(decrypted.Plaintext, plaintext) {
			return benchmarkResult{}, fmt.Errorf("benchmark decrypt did not return original plaintext")
		}
		result.DecryptDuration = append(result.DecryptDuration, time.Since(start))
	}
	return result, nil
}

func printBenchmark(out io.Writer, result benchmarkResult) {
	_, _ = fmt.Fprintln(out, reportNameBenchmark)
	_, _ = fmt.Fprintf(out, "iterations: %d\n", result.Iterations)
	printDurationSummary(out, "transit_encrypt", result.EncryptDuration)
	printDurationSummary(out, "transit_decrypt", result.DecryptDuration)
}

func printDurationSummary(out io.Writer, name string, values []time.Duration) {
	sorted := make([]time.Duration, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i int, j int) bool {
		return sorted[i] < sorted[j]
	})
	if len(sorted) == 0 {
		_, _ = fmt.Fprintf(out, "%s_ms_min: 0\n", name)
		_, _ = fmt.Fprintf(out, "%s_ms_p50: 0\n", name)
		_, _ = fmt.Fprintf(out, "%s_ms_max: 0\n", name)
		return
	}
	_, _ = fmt.Fprintf(out, "%s_ms_min: %.3f\n", name, durationMilliseconds(sorted[0]))
	_, _ = fmt.Fprintf(out, "%s_ms_p50: %.3f\n", name, durationMilliseconds(sorted[len(sorted)/2]))
	_, _ = fmt.Fprintf(out, "%s_ms_max: %.3f\n", name, durationMilliseconds(sorted[len(sorted)-1]))
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}
