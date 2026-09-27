//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

const (
	kmsLoadSoakDurationEnv    = "KMS_LOAD_SOAK_DURATION"
	kmsLoadSoakWorkersEnv     = "KMS_LOAD_SOAK_WORKERS"
	kmsLoadSoakMaxP95Env      = "KMS_LOAD_SOAK_MAX_P95"
	kmsLoadSoakMinOpsEnv      = "KMS_LOAD_SOAK_MIN_OPS"
	kmsDecryptSoakDurationEnv = "KMS_DECRYPT_SOAK_DURATION"
	kmsDecryptSoakWorkersEnv  = "KMS_DECRYPT_SOAK_WORKERS"
	kmsDecryptSoakMaxP95Env   = "KMS_DECRYPT_SOAK_MAX_P95"
	kmsDecryptSoakMinOpsEnv   = "KMS_DECRYPT_SOAK_MIN_OPS"
	kmsDecryptSoakSamplesEnv  = "KMS_DECRYPT_SOAK_SAMPLES"

	loadSoakMemoryGrowthLimit = uint64(128 * 1024 * 1024)
	loadSoakPIDGrowthLimit    = 16
)

type providerResourceSnapshot struct {
	memoryBytes uint64
	pids        int
}

func TestProviderLoadSoakE2E(t *testing.T) {
	duration := providerSoakDuration(t)
	ctx, cancel := context.WithTimeout(context.Background(), duration+6*time.Minute)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-load-soak", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{JWTTTL: duration + time.Hour, JWTTokenTTL: "30s", JWTMaxTTL: "2m"},
		Config: providerContainerConfigOptions{
			ProbeInterval:          "1s",
			LoginBeforeTokenExpiry: "5s",
			DeepProbeInterval:      "5s",
			StatusMaxStaleness:     "15s",
		},
	})
	stack.runClient(ctx, "warmup-client", kmsClientModeWriteSample, sampleReadWrite)
	before := readProviderResourceSnapshot(t, ctx, stack.dockerPath, stack.providerName)

	runProviderSoakWindows(t, ctx, stack, duration, kmsClientModeLoadSoak, []string{
		kmsLoadSoakDurationEnv + "=20s",
		kmsLoadSoakWorkersEnv + "=4",
		kmsLoadSoakMaxP95Env + "=2s",
		kmsLoadSoakMinOpsEnv + "=60",
	})
	time.Sleep(2 * time.Second)
	after := readProviderResourceSnapshot(t, ctx, stack.dockerPath, stack.providerName)
	assertProviderResourceGrowth(t, before, after)
}

func TestProviderDecryptSoakE2E(t *testing.T) {
	duration := providerSoakDuration(t)
	ctx, cancel := context.WithTimeout(context.Background(), duration+7*time.Minute)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-decrypt-soak", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{JWTTTL: duration + time.Hour, JWTTokenTTL: "30s", JWTMaxTTL: "2m"},
		Config: providerContainerConfigOptions{
			ProbeInterval:          "1s",
			LoginBeforeTokenExpiry: "5s",
			DeepProbeInterval:      "5s",
			StatusMaxStaleness:     "15s",
		},
	})
	before := readProviderResourceSnapshot(t, ctx, stack.dockerPath, stack.providerName)

	runProviderSoakWindows(t, ctx, stack, duration, kmsClientModeDecryptSoak, []string{
		kmsDecryptSoakDurationEnv + "=30s",
		kmsDecryptSoakWorkersEnv + "=8",
		kmsDecryptSoakMaxP95Env + "=2s",
		kmsDecryptSoakMinOpsEnv + "=500",
		kmsDecryptSoakSamplesEnv + "=128",
	})
	time.Sleep(2 * time.Second)
	after := readProviderResourceSnapshot(t, ctx, stack.dockerPath, stack.providerName)
	assertProviderResourceGrowth(t, before, after)
}

func readProviderResourceSnapshot(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	containerName string,
) providerResourceSnapshot {
	t.Helper()

	output, err := runDockerOutput(
		ctx,
		dockerPath,
		"stats",
		"--no-stream",
		"--format",
		"{{.MemUsage}} {{.PIDs}}",
		containerName,
	)
	if err != nil {
		t.Fatalf("read provider docker stats: %v: %s", err, strings.TrimSpace(output))
	}
	return parseProviderResourceSnapshot(t, output)
}

func parseProviderResourceSnapshot(t *testing.T, output string) providerResourceSnapshot {
	t.Helper()

	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 4 || fields[1] != "/" {
		t.Fatalf("unexpected docker stats output: %q", strings.TrimSpace(output))
	}
	memoryBytes, err := parseDockerByteSize(fields[0])
	if err != nil {
		t.Fatalf("parse provider memory usage %q: %v", fields[0], err)
	}
	pids, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		t.Fatalf("parse provider pid count %q: %v", fields[len(fields)-1], err)
	}
	return providerResourceSnapshot{
		memoryBytes: memoryBytes,
		pids:        pids,
	}
}

func parseDockerByteSize(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	unitStart := len(raw)
	for index, value := range raw {
		if (value < '0' || value > '9') && value != '.' {
			unitStart = index
			break
		}
	}
	if unitStart == 0 {
		return 0, fmt.Errorf("missing numeric size")
	}
	number, err := strconv.ParseFloat(raw[:unitStart], 64)
	if err != nil {
		return 0, err
	}
	unit := strings.TrimSpace(raw[unitStart:])
	multiplier, ok := dockerByteSizeMultipliers()[unit]
	if !ok {
		return 0, fmt.Errorf("unknown size unit %q", unit)
	}
	return uint64(number * float64(multiplier)), nil
}

func dockerByteSizeMultipliers() map[string]uint64 {
	const kib = 1024
	return map[string]uint64{
		"B":   1,
		"kB":  1000,
		"KB":  1000,
		"KiB": kib,
		"MB":  1000 * 1000,
		"MiB": kib * kib,
		"GB":  1000 * 1000 * 1000,
		"GiB": kib * kib * kib,
	}
}

func assertProviderResourceGrowth(
	t *testing.T,
	before providerResourceSnapshot,
	after providerResourceSnapshot,
) {
	t.Helper()

	if after.memoryBytes > before.memoryBytes+loadSoakMemoryGrowthLimit {
		t.Fatalf(
			"provider memory grew too much during load soak: before=%d after=%d limit=%d",
			before.memoryBytes,
			after.memoryBytes,
			loadSoakMemoryGrowthLimit,
		)
	}
	if after.pids > before.pids+loadSoakPIDGrowthLimit {
		t.Fatalf(
			"provider pid count grew too much during load soak: before=%d after=%d limit=%d",
			before.pids,
			after.pids,
			loadSoakPIDGrowthLimit,
		)
	}
}

// Keep each client window bounded while the same provider stays up for the entire soak.
func runProviderSoakWindows(t *testing.T, ctx context.Context, stack *providerFailureStack,
	duration time.Duration, mode string, env []string,
) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for window := 1; ; window++ {
		stack.runClientWithEnv(ctx, "soak-client", mode, sampleNotMounted, env)
		resources := readProviderResourceSnapshot(t, ctx, stack.dockerPath, stack.providerName)
		t.Logf("soak window=%d memory_bytes=%d pids=%d", window, resources.memoryBytes, resources.pids)
		if !time.Now().Before(deadline) {
			return
		}
	}
}

func providerSoakDuration(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("E2E_SOAK_DURATION")
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration > 24*time.Hour {
		t.Fatalf("E2E_SOAK_DURATION must be positive and at most 24h: %q", value)
	}
	return duration
}
