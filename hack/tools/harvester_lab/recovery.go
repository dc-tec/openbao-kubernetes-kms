package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

func verifyColdHistoricalSecrets(
	ctx context.Context, cfg *labConfig, checks []kubeadmCheck, corpus map[string]trackedSecret,
) error {
	for _, check := range checks {
		fmt.Printf("restarting API server for cold historical reads on %s\n", check.host)
		if err := restartAPIServer(ctx, cfg, check); err != nil {
			return err
		}
		secret, ok := corpus[check.suffix]
		if !ok {
			return fmt.Errorf("missing pre-fault Secret for %s", check.suffix)
		}
		if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, secret.name, secret.path); err != nil {
			return err
		}
	}
	return nil
}

func rotateAndConfirmProviders(ctx context.Context, cfg *labConfig, checks []kubeadmCheck, runID string) error {
	before := make(map[string]int, len(checks))
	for _, check := range checks {
		version, err := remoteActiveVersion(ctx, cfg, check.host)
		if err != nil {
			return err
		}
		before[check.host] = version
	}
	if err := runOpenBaoBackupRestoreScript(ctx, cfg, "rotate-transit", runID); err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for _, check := range checks {
		for {
			version, err := remoteActiveVersion(deadline, cfg, check.host)
			if err == nil && version == before[check.host]+1 {
				fmt.Printf("confirmed provider promotion on %s to version %d\n", check.host, version)
				break
			}
			select {
			case <-deadline.Done():
				return fmt.Errorf("provider promotion on %s was not confirmed: %w", check.host, deadline.Err())
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

func remoteActiveVersion(ctx context.Context, cfg *labConfig, host string) (int, error) {
	output, err := sshLabOutput(ctx, cfg, host, "sudo cat /var/lib/openbao-kms/state/key-registry.json")
	if err != nil {
		return 0, err
	}
	return activeVersionFromState(output)
}

func activeVersionFromState(data []byte) (int, error) {
	var state keyregistry.StateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, fmt.Errorf("decode provider registry: %w", err)
	}
	if err := state.Validate(); err != nil {
		return 0, err
	}
	active, err := state.ActiveSnapshot()
	if err != nil {
		return 0, err
	}
	return active.TransitVersion, nil
}

func remoteFailedEncryptions(ctx context.Context, cfg *labConfig, host string) (float64, error) {
	output, err := sshLabOutput(ctx, cfg, host, "curl --fail --silent --max-time 5 http://127.0.0.1:8081/metrics")
	if err != nil {
		return 0, err
	}
	return failedEncryptionCount(string(output))
}

func failedEncryptionCount(metrics string) (float64, error) {
	var total float64
	found := false
	for line := range strings.SplitSeq(metrics, "\n") {
		if !strings.HasPrefix(line, "openbao_kms_grpc_requests_total{") {
			continue
		}
		found = true
		if !strings.Contains(line, `method="encrypt"`) || strings.Contains(line, `status="ok"`) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return 0, fmt.Errorf("invalid KMS request counter")
		}
		count, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || count < 0 || math.IsNaN(count) || math.IsInf(count, 0) {
			return 0, fmt.Errorf("invalid KMS request counter value")
		}
		total += count
	}
	if !found {
		return 0, fmt.Errorf("KMS request counters missing from provider metrics")
	}
	return total, nil
}

// A cold API server can reject an unhealthy Status with an empty key ID before
// calling Encrypt. Require KMS-specific evidence, not just a failed write.
func waitForKMSOutage(ctx context.Context, cfg *labConfig, check kubeadmCheck, before float64) error {
	deadline, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		count, err := remoteFailedEncryptions(deadline, cfg, check.host)
		if err == nil && count > before {
			fmt.Printf("confirmed new failed KMS Encrypt request on %s\n", check.host)
			return nil
		}
		_, healthErr := outputCmdEnv(deadline, cfg, []string{"KUBECONFIG=" + check.kubeconfig},
			"kubectl", "get", "--raw=/readyz/kms-providers", "--request-timeout=5s")
		if isUnhealthyKMSStatus(healthErr) {
			fmt.Printf("confirmed API server KMS health check rejected unhealthy provider Status on %s\n", check.host)
			return nil
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("cold write failed without KMS outage evidence on %s: %w", check.host, deadline.Err())
		case <-time.After(time.Second):
		}
	}
}

func isUnhealthyKMSStatus(err error) bool {
	return err != nil && strings.Contains(err.Error(), "kmsv2 Provider openbao-kms-workload-a is not healthy") &&
		strings.Contains(err.Error(), "got unexpected healthz status: unhealthy")
}

// Readiness cannot succeed while KMS is unavailable. The version endpoint proves
// that the restarted API server can receive a request without requiring KMS.
func waitAPIEndpoint(ctx context.Context, cfg *labConfig, kubeconfig string) error {
	return waitAPIPath(ctx, cfg, kubeconfig, "/version")
}

func waitAPIPath(ctx context.Context, cfg *labConfig, kubeconfig, path string) error {
	// Repeated fault scenarios can reach kubelet's five-minute restart backoff.
	deadline, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	for {
		if err := quietKubectl(deadline, cfg, []string{"KUBECONFIG=" + kubeconfig},
			"get", "--raw="+path, "--request-timeout=5s"); err == nil {
			return nil
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("API server %s unavailable (%s): %w", path, kubeconfig, deadline.Err())
		case <-time.After(time.Second):
		}
	}
}

func backupQuiescedProvider(ctx context.Context, cfg *labConfig, check kubeadmCheck, restoreID string) (retErr error) {
	defer func() {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		if err := startProviderAfterRestore(recoveryCtx, cfg, check); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("restart provider after backup: %w", err))
		}
	}()
	if err := stopProviderForRestore(ctx, cfg, check); err != nil {
		return err
	}
	return runProviderStateScript(ctx, cfg, check, "backup", restoreID)
}

func recoverOpenBaoOutage(
	ctx context.Context, cfg *labConfig, checks []kubeadmCheck, corpus map[string]trackedSecret,
) error {
	if err := sshLab(ctx, cfg, cfg.openBaoHost, "sudo systemctl start openbao.service"); err != nil {
		return fmt.Errorf("restart OpenBao after outage: %w", err)
	}
	if err := unsealOpenBao(ctx, cfg); err != nil {
		return fmt.Errorf("unseal OpenBao after outage: %w", err)
	}
	if err := waitOpenBaoAvailable(ctx, cfg); err != nil {
		return fmt.Errorf("verify OpenBao after outage: %w", err)
	}
	for _, check := range checks {
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return err
		}
	}
	if err := verifyColdHistoricalSecrets(ctx, cfg, checks, corpus); err != nil {
		return fmt.Errorf("cold read after OpenBao outage: %w", err)
	}
	return nil
}

func requireCandidateProviders(ctx context.Context, cfg *labConfig) error {
	expected := gitShortCommit(ctx, cfg)
	checks := kubeadmChecks(cfg)
	if cfg.multiControlPlaneEnabled {
		checks = append(checks, multiControlPlaneChecks(cfg)...)
	}
	for _, check := range checks {
		command := "/usr/bin/bao-kms-provider version"
		if check.providerMode == providerModeStaticPod {
			crictl := "sudo crictl --config /dev/null --runtime-endpoint unix:///run/containerd/containerd.sock"
			command = "sh -c " + shellQuote("id=$("+crictl+" ps --name '^bao-kms-provider$' -q); "+
				`test -n "$id" && `+crictl+` exec "$id" /bao-kms-provider version`)
		}
		output, err := sshLabOutput(ctx, cfg, check.host, command)
		if err != nil {
			return err
		}
		if err := requireCandidateVersion(string(output), expected); err != nil {
			return fmt.Errorf("provider on %s: %w", check.host, err)
		}
	}
	fmt.Printf("all provider deployments report candidate commit %s\n", expected)
	return nil
}

func requireCandidateVersion(output, expected string) error {
	if expected != "" && expected != "unknown" {
		for line := range strings.SplitSeq(output, "\n") {
			if strings.TrimSpace(line) == "commit: "+expected {
				return nil
			}
		}
	}
	return errors.New("provider does not report the checkout commit; deploy the intended candidate before qualification")
}
