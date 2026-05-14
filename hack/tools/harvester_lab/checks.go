package main

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type kubeadmCheck struct {
	host         string
	kubeconfig   string
	suffix       string
	providerMode string
}

const (
	providerModeSystemd   = "systemd"
	providerModeStaticPod = "static-pod"

	decryptWarmupNamespace      = "openbao-kms-decrypt-warmup"
	decryptWarmupCaseLabelKey   = "openbao-kms.dev/lab-case"
	decryptWarmupCaseLabelValue = "decrypt-warmup"
	decryptWarmupClusterLabel   = "openbao-kms.dev/lab-cluster"
)

func kubeadmChecks(cfg *labConfig) []kubeadmCheck {
	return []kubeadmCheck{
		{
			host:         cfg.systemdHost,
			kubeconfig:   filepath.Join(cfg.artifactDir, "kubeconfig-systemd.yaml"),
			suffix:       "systemd",
			providerMode: providerModeSystemd,
		},
		{
			host:         cfg.staticPodHost,
			kubeconfig:   filepath.Join(cfg.artifactDir, "kubeconfig-static.yaml"),
			suffix:       "static",
			providerMode: providerModeStaticPod,
		},
	}
}

func multiControlPlaneChecks(cfg *labConfig) []kubeadmCheck {
	checks := make([]kubeadmCheck, 0, len(cfg.multiControlPlaneHosts))
	for index, host := range cfg.multiControlPlaneHosts {
		suffix := fmt.Sprintf("mcp-%d", index+1)
		checks = append(checks, kubeadmCheck{
			host:         host,
			kubeconfig:   filepath.Join(cfg.artifactDir, "kubeconfig-"+suffix+".yaml"),
			suffix:       suffix,
			providerMode: providerModeStaticPod,
		})
	}
	return checks
}

func requireMultiControlPlaneChecks(cfg *labConfig) ([]kubeadmCheck, error) {
	if !cfg.multiControlPlaneEnabled {
		return nil, errors.New(
			"HARVESTER_ENABLE_MULTI_CONTROL_PLANE=true is required for multi-control-plane lab commands",
		)
	}
	checks := multiControlPlaneChecks(cfg)
	if len(checks) < 3 {
		return nil, errors.New("HARVESTER_MCP_HOSTS must contain at least three control-plane hosts")
	}
	return checks, nil
}

func labVerifyKMS(ctx context.Context, cfg *labConfig, _ []string) error {
	for _, check := range kubeadmChecks(cfg) {
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyKMSOne(ctx context.Context, cfg *labConfig, host string, kubeconfig string, suffix string) error {
	secretName := "openbao-kms-smoke-" + suffix
	secretValue, err := randomSecretHex()
	if err != nil {
		return err
	}
	tempFile, cleanup, err := writeTempSecretValue(secretValue)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := createKMSSecret(ctx, cfg, kubeconfig, secretName, tempFile); err != nil {
		return err
	}
	return verifyRemoteSecretEnvelope(ctx, cfg, host, secretName, tempFile)
}

func createKMSSecret(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	secretName string,
	valuePath string,
) error {
	env := []string{"KUBECONFIG=" + kubeconfig}
	if err := runCmdEnv(
		ctx,
		cfg,
		env,
		"kubectl",
		"delete",
		"secret",
		secretName,
		"-n",
		"default",
		"--ignore-not-found=true",
	); err != nil {
		return err
	}
	if err := runCmdEnv(
		ctx,
		cfg,
		env,
		"kubectl",
		"create",
		"secret",
		"generic",
		secretName,
		"-n",
		"default",
		"--from-file=value="+valuePath,
	); err != nil {
		return err
	}
	return runCmdEnv(ctx, cfg, env, "kubectl", "get", "secret", secretName, "-n", "default")
}

func verifyRemoteSecretEnvelope(
	ctx context.Context,
	cfg *labConfig,
	host string,
	secretName string,
	valuePath string,
) error {
	return verifyRemoteSecretEnvelopeInNamespace(ctx, cfg, host, "default", secretName, valuePath)
}

func verifyRemoteSecretEnvelopeInNamespace(
	ctx context.Context,
	cfg *labConfig,
	host string,
	namespace string,
	secretName string,
	valuePath string,
) error {
	remoteScript := filepath.Join(cfg.root, "hack", "harvester", "remote", "verify-kms-encryption.sh")
	if err := scpLab(ctx, cfg, remoteScript, host+":/tmp/verify-kms-encryption.sh"); err != nil {
		return err
	}
	envArgs := []string{
		"SECRET_NAME", secretName,
		"SECRET_NAMESPACE", namespace,
	}
	cleanup := ""
	if valuePath != "" {
		if err := scpLab(ctx, cfg, valuePath, host+":"+remoteValuePath); err != nil {
			return err
		}
		envArgs = append(envArgs, "SECRET_VALUE_FILE", remoteValuePath)
		cleanup = "; sudo rm -f " + remoteValuePath
	}
	command := joinEnvForSudo(envArgs...) +
		" sh /tmp/verify-kms-encryption.sh; status=$?" + cleanup + "; exit $status"
	return sshLab(ctx, cfg, host, command)
}

func randomSecretHex() (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(crand.Reader, data); err != nil {
		return "", fmt.Errorf("generate secret value: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func writeTempSecretValue(value string) (string, func(), error) {
	file, err := os.CreateTemp("", "openbao-kms-secret-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp secret file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	if _, err := file.WriteString(value); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write temp secret file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close temp secret file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("chmod temp secret file: %w", err)
	}
	return path, cleanup, nil
}

func labVerifyRecovery(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := labVerifyKMS(ctx, cfg, nil); err != nil {
		return fmt.Errorf("baseline KMS verification: %w", err)
	}
	if err := verifyProviderRestartRecovery(ctx, cfg); err != nil {
		return err
	}
	if err := verifyAPIServerRestartRecovery(ctx, cfg); err != nil {
		return err
	}
	if err := verifyOpenBaoRestartRecovery(ctx, cfg); err != nil {
		return err
	}
	if err := verifyKubeadmRebootRecovery(ctx, cfg); err != nil {
		return err
	}
	return verifyOpenBaoRebootRecovery(ctx, cfg)
}

func labVerifyMultiControlPlaneRecovery(ctx context.Context, cfg *labConfig, _ []string) error {
	checks, err := requireMultiControlPlaneChecks(cfg)
	if err != nil {
		return err
	}
	if err := verifyMultiControlPlaneKMS(ctx, cfg, checks, "baseline"); err != nil {
		return fmt.Errorf("multi-control-plane baseline KMS verification: %w", err)
	}
	if err := verifyMultiControlPlaneProviderRestart(ctx, cfg, checks); err != nil {
		return err
	}
	if err := verifyMultiControlPlaneAPIServerRestart(ctx, cfg, checks); err != nil {
		return err
	}
	return verifyMultiControlPlaneReboot(ctx, cfg, checks)
}

func verifyMultiControlPlaneKMS(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
	prefix string,
) error {
	if err := verifyMultiControlPlaneSharedSecret(ctx, cfg, checks, prefix+"-shared"); err != nil {
		return err
	}
	for _, check := range checks {
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, prefix+"-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyMultiControlPlaneSharedSecret(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
	suffix string,
) error {
	if len(checks) == 0 {
		return errors.New("multi-control-plane shared Secret check list is empty")
	}
	secret, cleanup, err := createTrackedSecret(ctx, cfg, checks[0], suffix)
	if err != nil {
		return err
	}
	defer cleanup()
	for _, check := range checks {
		env := []string{"KUBECONFIG=" + check.kubeconfig}
		if err := runCmdEnv(ctx, cfg, env, "kubectl", "get", "secret", secret.name, "-n", "default"); err != nil {
			return err
		}
		if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, secret.name, secret.path); err != nil {
			return err
		}
	}
	return nil
}

func verifyMultiControlPlaneProviderRestart(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
) error {
	for _, check := range checks {
		fmt.Printf("restarting multi-control-plane provider on %s\n", check.host)
		if err := restartProvider(ctx, cfg, check); err != nil {
			return fmt.Errorf("restart provider on %s: %w", check.host, err)
		}
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return err
		}
		if err := verifyMultiControlPlaneSharedSecret(ctx, cfg, checks, "provider-restart-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyMultiControlPlaneAPIServerRestart(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
) error {
	for _, check := range checks {
		fmt.Printf("restarting multi-control-plane kube-apiserver on %s\n", check.host)
		if err := sshLab(ctx, cfg, check.host, crictlStopContainerCommand("^kube-apiserver$")); err != nil {
			return err
		}
		remaining := remainingChecks(checks, check.host)
		if err := waitChecksAPIServers(ctx, cfg, remaining); err != nil {
			return err
		}
		if err := verifyMultiControlPlaneSharedSecret(
			ctx,
			cfg,
			remaining,
			"apiserver-outage-"+check.suffix,
		); err != nil {
			return fmt.Errorf("verify surviving API servers while %s restarts: %w", check.host, err)
		}
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return fmt.Errorf("wait for kube-apiserver on %s: %w", check.host, err)
		}
		if err := verifyMultiControlPlaneSharedSecret(ctx, cfg, checks, "apiserver-restart-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyMultiControlPlaneReboot(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
) error {
	for _, check := range checks {
		fmt.Printf("rebooting multi-control-plane VM %s\n", check.host)
		if err := startHostReboot(ctx, cfg, check.host); err != nil {
			return err
		}
		if err := waitForSSHDown(ctx, cfg, check.host, 2*time.Minute); err != nil {
			return err
		}
		remaining := remainingChecks(checks, check.host)
		if err := waitChecksAPIServers(ctx, cfg, remaining); err != nil {
			return err
		}
		if err := verifyMultiControlPlaneSharedSecret(
			ctx,
			cfg,
			remaining,
			"vm-outage-"+check.suffix,
		); err != nil {
			return fmt.Errorf("verify surviving API servers while %s reboots: %w", check.host, err)
		}
		if err := waitForSSH(ctx, cfg, check.host); err != nil {
			return err
		}
		if err := waitProviderReady(ctx, cfg, check.host); err != nil {
			return err
		}
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return fmt.Errorf("wait for kube-apiserver after reboot on %s: %w", check.host, err)
		}
		if err := verifyMultiControlPlaneSharedSecret(ctx, cfg, checks, "vm-reboot-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func waitChecksAPIServers(ctx context.Context, cfg *labConfig, checks []kubeadmCheck) error {
	if len(checks) == 0 {
		return errors.New("kube-apiserver check list is empty")
	}
	for _, check := range checks {
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return err
		}
	}
	return nil
}

func remainingChecks(checks []kubeadmCheck, excludedHost string) []kubeadmCheck {
	remaining := make([]kubeadmCheck, 0, len(checks)-1)
	for _, check := range checks {
		if check.host != excludedHost {
			remaining = append(remaining, check)
		}
	}
	return remaining
}

func verifyProviderRestartRecovery(ctx context.Context, cfg *labConfig) error {
	for _, check := range kubeadmChecks(cfg) {
		fmt.Printf("restarting provider on %s\n", check.host)
		if err := restartProvider(ctx, cfg, check); err != nil {
			return fmt.Errorf("restart provider on %s: %w", check.host, err)
		}
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return err
		}
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, "provider-restart-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyAPIServerRestartRecovery(ctx context.Context, cfg *labConfig) error {
	for _, check := range kubeadmChecks(cfg) {
		fmt.Printf("restarting kube-apiserver on %s\n", check.host)
		if err := restartAPIServer(ctx, cfg, check); err != nil {
			return fmt.Errorf("restart kube-apiserver on %s: %w", check.host, err)
		}
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, "apiserver-restart-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyOpenBaoRestartRecovery(ctx context.Context, cfg *labConfig) error {
	fmt.Println("restarting OpenBao")
	if err := restartOpenBaoAndWait(ctx, cfg); err != nil {
		return err
	}
	for _, check := range kubeadmChecks(cfg) {
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return err
		}
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, "openbao-restart-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyKubeadmRebootRecovery(ctx context.Context, cfg *labConfig) error {
	for _, check := range kubeadmChecks(cfg) {
		fmt.Printf("rebooting kubeadm VM %s\n", check.host)
		if err := rebootKubeadmHost(ctx, cfg, check); err != nil {
			return fmt.Errorf("reboot kubeadm VM %s: %w", check.host, err)
		}
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, "vm-reboot-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func verifyOpenBaoRebootRecovery(ctx context.Context, cfg *labConfig) error {
	fmt.Printf("rebooting OpenBao VM %s\n", cfg.openBaoHost)
	if err := rebootOpenBaoHost(ctx, cfg); err != nil {
		return err
	}
	for _, check := range kubeadmChecks(cfg) {
		if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
			return err
		}
		if err := verifyKMSOne(ctx, cfg, check.host, check.kubeconfig, "openbao-vm-reboot-"+check.suffix); err != nil {
			return err
		}
	}
	return nil
}

func restartProvider(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	switch check.providerMode {
	case providerModeSystemd:
		if err := sshLab(ctx, cfg, check.host, "sudo systemctl restart bao-kms-provider.service"); err != nil {
			return err
		}
	case providerModeStaticPod:
		if err := sshLab(ctx, cfg, check.host, crictlStopContainerCommand("^bao-kms-provider$")); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown provider mode: %s", check.providerMode)
	}
	return waitProviderReady(ctx, cfg, check.host)
}

func restartAPIServer(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	if err := sshLab(ctx, cfg, check.host, crictlStopContainerCommand("^kube-apiserver$")); err != nil {
		return err
	}
	return waitAPIServer(ctx, cfg, check.kubeconfig)
}

func crictlStopContainerCommand(namePattern string) string {
	crictl := strings.Join([]string{
		"crictl",
		"--config /dev/null",
		"--runtime-endpoint unix:///run/containerd/containerd.sock",
		"--image-endpoint unix:///run/containerd/containerd.sock",
	}, " ")
	script := "set -eu; cid=\"$(" + crictl + " ps --name " + shellQuote(namePattern) +
		" -q | head -n1)\"; test -n \"$cid\"; " + crictl + " stop \"$cid\" >/dev/null"
	return "sudo sh -c " + shellQuote(script)
}

func waitProviderReady(ctx context.Context, cfg *labConfig, host string) error {
	return waitRemoteCommand(
		ctx,
		cfg,
		host,
		"provider readiness",
		3*time.Minute,
		"curl -fsS http://127.0.0.1:8082/ready >/dev/null",
	)
}

func waitRemoteCommand(
	ctx context.Context,
	cfg *labConfig,
	host string,
	description string,
	timeout time.Duration,
	remoteCommand string,
) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := sshLab(ctx, cfg, host, remoteCommand, "-o", "ConnectTimeout=5"); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s on %s", description, host)
		}
		time.Sleep(5 * time.Second)
	}
}

func restartOpenBaoAndWait(ctx context.Context, cfg *labConfig) error {
	if err := sshLab(ctx, cfg, cfg.openBaoHost, "sudo systemctl restart openbao.service"); err != nil {
		return err
	}
	if err := unsealOpenBao(ctx, cfg); err != nil {
		return err
	}
	return waitOpenBaoAvailable(ctx, cfg)
}

func unsealOpenBao(ctx context.Context, cfg *labConfig) error {
	script := `set -eu
export BAO_ADDR="https://127.0.0.1:8200"
export BAO_CACERT="/etc/openbao.d/tls/ca.crt"
health_url="$BAO_ADDR/v1/sys/health?standbyok=true&sealedcode=200&uninitcode=200"
for _ in $(seq 1 60); do
	if curl -fsS --cacert "$BAO_CACERT" "$health_url" >/dev/null; then
		break
	fi
	sleep 2
done
if bao status -format=json | jq -e '.sealed == true' >/dev/null; then
	unseal_key="$(jq -r '.unseal_keys_b64[0]' /root/openbao-kms-lab/init.json)"
	bao operator unseal "$unseal_key" >/dev/null
fi`
	return sshLab(ctx, cfg, cfg.openBaoHost, "sudo sh -c "+shellQuote(script))
}

func waitOpenBaoAvailable(ctx context.Context, cfg *labConfig) error {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if err := verifyOpenBaoAvailable(ctx, cfg); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for OpenBao health and JWT login")
		}
		time.Sleep(5 * time.Second)
	}
}

func verifyOpenBaoAvailable(ctx context.Context, cfg *labConfig) error {
	openBaoIP, err := sshHostIP(cfg.sshConfig, cfg.openBaoHost)
	if err != nil {
		return err
	}
	if openBaoIP == "" {
		return errors.New("could not resolve OpenBao host IP from SSH config")
	}
	client, err := openBaoHTTPClient(cfg, openBaoIP)
	if err != nil {
		return err
	}
	if err := verifyOpenBaoHealth(ctx, client); err != nil {
		return err
	}
	return verifyOpenBaoJWTLogin(ctx, cfg, client)
}

func rebootKubeadmHost(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	if err := rebootHost(ctx, cfg, check.host); err != nil {
		return err
	}
	if err := waitProviderReady(ctx, cfg, check.host); err != nil {
		return err
	}
	return waitAPIServer(ctx, cfg, check.kubeconfig)
}

func rebootOpenBaoHost(ctx context.Context, cfg *labConfig) error {
	if err := rebootHost(ctx, cfg, cfg.openBaoHost); err != nil {
		return err
	}
	if err := unsealOpenBao(ctx, cfg); err != nil {
		return err
	}
	return waitOpenBaoAvailable(ctx, cfg)
}

func rebootHost(ctx context.Context, cfg *labConfig, host string) error {
	if err := startHostReboot(ctx, cfg, host); err != nil {
		return err
	}
	if err := waitForSSHDown(ctx, cfg, host, 2*time.Minute); err != nil {
		return err
	}
	return waitForSSH(ctx, cfg, host)
}

func startHostReboot(ctx context.Context, cfg *labConfig, host string) error {
	command := "sudo nohup sh -c 'sleep 1; systemctl reboot' >/dev/null 2>&1 &"
	return sshLab(ctx, cfg, host, command)
}

func waitForSSHDown(ctx context.Context, cfg *labConfig, host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := sshLab(ctx, cfg, host, "true", "-o", "ConnectTimeout=3")
		if err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ssh to go down: %s", host)
		}
		time.Sleep(3 * time.Second)
	}
}

func labVerifyOpenBaoOutage(ctx context.Context, cfg *labConfig, _ []string) (retErr error) {
	if err := labVerifyKMS(ctx, cfg, nil); err != nil {
		return fmt.Errorf("baseline KMS verification: %w", err)
	}
	if err := sshLab(ctx, cfg, cfg.openBaoHost, "sudo systemctl stop openbao.service"); err != nil {
		return err
	}
	defer func() {
		if err := sshLab(ctx, cfg, cfg.openBaoHost, "sudo systemctl start openbao.service"); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("restart OpenBao after outage: %w", err))
			return
		}
		if err := unsealOpenBao(ctx, cfg); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("unseal OpenBao after outage: %w", err))
			return
		}
		if err := waitOpenBaoAvailable(ctx, cfg); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("verify OpenBao after outage: %w", err))
			return
		}
		for _, check := range kubeadmChecks(cfg) {
			if err := waitAPIServer(ctx, cfg, check.kubeconfig); err != nil {
				retErr = errors.Join(retErr, err)
				return
			}
		}
	}()
	if err := waitRemoteCommand(
		ctx,
		cfg,
		cfg.openBaoHost,
		"OpenBao service stop",
		time.Minute,
		"! sudo systemctl is-active --quiet openbao.service",
	); err != nil {
		return err
	}
	for _, check := range kubeadmChecks(cfg) {
		if err := verifyOutageCachedWrite(ctx, cfg, check); err != nil {
			return err
		}
	}
	for _, check := range kubeadmChecks(cfg) {
		fmt.Printf("restarting kube-apiserver with OpenBao stopped on %s\n", check.host)
		if err := sshLab(ctx, cfg, check.host, crictlStopContainerCommand("^kube-apiserver$")); err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
		if err := expectKMSWriteFailure(ctx, cfg, check); err != nil {
			return err
		}
	}
	return nil
}

func verifyOutageCachedWrite(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	secret, cleanup, err := attemptOutageSecretCreate(ctx, cfg, check, "openbao-kms-outage-cache")
	if err != nil {
		fmt.Printf("KMS write failed while OpenBao was stopped for %s (expected)\n", check.suffix)
		return nil
	}
	defer cleanup()
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, secret.name, secret.path); err != nil {
		return fmt.Errorf("outage write succeeded for %s but did not verify as KMS envelope: %w", check.suffix, err)
	}
	fmt.Printf(
		"KMS write during OpenBao outage used cached API server state and remained envelope-encrypted for %s\n",
		check.suffix,
	)
	return nil
}

func expectKMSWriteFailure(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	secret, cleanup, err := attemptOutageSecretCreate(ctx, cfg, check, "openbao-kms-outage-cold")
	if err != nil {
		fmt.Printf("KMS write failed after kube-apiserver restart with OpenBao stopped for %s (expected)\n", check.suffix)
		return nil
	}
	defer cleanup()
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, secret.name, secret.path); err != nil {
		return fmt.Errorf("cold outage write succeeded for %s and envelope verification failed: %w", check.suffix, err)
	}
	return fmt.Errorf(
		"kubernetes Secret write unexpectedly succeeded after kube-apiserver cache clear while OpenBao was stopped for %s",
		check.suffix,
	)
}

func attemptOutageSecretCreate(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	prefix string,
) (trackedSecret, func(), error) {
	secretName := fmt.Sprintf("%s-%s-%d", prefix, check.suffix, time.Now().UnixNano())
	secretValue, err := randomSecretHex()
	if err != nil {
		return trackedSecret{}, func() {}, err
	}
	tempFile, cleanup, err := writeTempSecretValue(secretValue)
	if err != nil {
		return trackedSecret{}, func() {}, err
	}
	env := []string{"KUBECONFIG=" + check.kubeconfig}
	err = quietKubectl(
		ctx,
		cfg,
		env,
		"create",
		"secret",
		"generic",
		secretName,
		"-n",
		"default",
		"--from-file=value="+tempFile,
		"--request-timeout=10s",
	)
	if err != nil {
		cleanup()
		return trackedSecret{}, func() {}, err
	}
	cleanupSecret := func() {
		cleanup()
		_ = quietKubectl(
			ctx,
			cfg,
			env,
			"delete",
			"secret",
			secretName,
			"-n",
			"default",
			"--ignore-not-found=true",
		)
	}
	return trackedSecret{name: secretName, path: tempFile}, cleanupSecret, nil
}

func labVerifyLoad(ctx context.Context, cfg *labConfig, _ []string) error {
	for _, check := range kubeadmChecks(cfg) {
		if err := verifyLoadOne(ctx, cfg, check, cfg.loadSecretCount); err != nil {
			return err
		}
	}
	return nil
}

func verifyLoadOne(ctx context.Context, cfg *labConfig, check kubeadmCheck, count int) error {
	if count <= 0 {
		return errors.New("HARVESTER_LOAD_SECRET_COUNT must be positive")
	}
	env := []string{"KUBECONFIG=" + check.kubeconfig}
	if err := runCmdEnv(
		ctx,
		cfg,
		env,
		"kubectl",
		"delete",
		"secret",
		"-l",
		"openbao-kms.dev/lab-case=load",
		"-n",
		"default",
		"--ignore-not-found=true",
	); err != nil {
		return err
	}
	type sampleSecret struct {
		name    string
		path    string
		cleanup func()
	}
	samples := make([]sampleSecret, 0, 2)
	for index := 0; index < count; index++ {
		secretName := fmt.Sprintf("openbao-kms-load-%s-%03d", check.suffix, index)
		secretValue, err := randomSecretHex()
		if err != nil {
			return err
		}
		tempFile, cleanup, err := writeTempSecretValue(secretValue)
		if err != nil {
			return err
		}
		defer cleanup()
		if index == 0 || index == count-1 {
			samples = append(samples, sampleSecret{name: secretName, path: tempFile, cleanup: cleanup})
		}
		if err := createKMSSecret(ctx, cfg, check.kubeconfig, secretName, tempFile); err != nil {
			return err
		}
		if err := runCmdEnv(
			ctx,
			cfg,
			env,
			"kubectl",
			"label",
			"secret",
			secretName,
			"openbao-kms.dev/lab-case=load",
			"openbao-kms.dev/lab-mode="+check.suffix,
			"-n",
			"default",
			"--overwrite",
		); err != nil {
			return err
		}
	}
	for _, sample := range samples {
		if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, sample.name, sample.path); err != nil {
			return err
		}
		sample.cleanup()
	}
	fmt.Printf("created and read %d KMS-encrypted Secrets on %s\n", count, check.suffix)
	return nil
}

func labVerifyDecryptWarmup(ctx context.Context, cfg *labConfig, _ []string) error {
	if cfg.multiControlPlaneEnabled {
		checks, err := requireMultiControlPlaneChecks(cfg)
		if err != nil {
			return err
		}
		return verifyDecryptWarmupCluster(ctx, cfg, "mcp", checks)
	}
	for _, check := range kubeadmChecks(cfg) {
		if err := verifyDecryptWarmupCluster(ctx, cfg, check.suffix, []kubeadmCheck{check}); err != nil {
			return err
		}
	}
	return nil
}

func verifyDecryptWarmupCluster(
	ctx context.Context,
	cfg *labConfig,
	clusterName string,
	checks []kubeadmCheck,
) error {
	if cfg.decryptWarmupDuration <= 0 {
		return errors.New("HARVESTER_DECRYPT_WARMUP_DURATION must be positive")
	}
	corpus, cleanup, err := prepareDecryptWarmupCorpus(ctx, cfg, clusterName, checks)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := verifyDecryptWarmupSamples(ctx, cfg, checks, corpus.samples); err != nil {
		return err
	}
	if cfg.decryptWarmupRestartAPIServers {
		if err := restartDecryptWarmupAPIServers(ctx, cfg, checks); err != nil {
			return err
		}
	}
	return runDecryptWarmupSoak(ctx, cfg, clusterName, checks, corpus.selector, corpus.secretCount)
}

func labVerifyDecryptColdStart(ctx context.Context, cfg *labConfig, _ []string) error {
	if cfg.multiControlPlaneEnabled {
		checks, err := requireMultiControlPlaneChecks(cfg)
		if err != nil {
			return err
		}
		return verifyDecryptColdStartCluster(ctx, cfg, "mcp", checks)
	}
	for _, check := range kubeadmChecks(cfg) {
		if err := verifyDecryptColdStartCluster(ctx, cfg, check.suffix, []kubeadmCheck{check}); err != nil {
			return err
		}
	}
	return nil
}

func verifyDecryptColdStartCluster(
	ctx context.Context,
	cfg *labConfig,
	clusterName string,
	checks []kubeadmCheck,
) error {
	if cfg.decryptColdStartTimeout <= 0 {
		return errors.New("HARVESTER_DECRYPT_COLD_START_TIMEOUT must be positive")
	}
	corpus, cleanup, err := prepareDecryptWarmupCorpus(ctx, cfg, clusterName, checks)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := verifyDecryptWarmupSamples(ctx, cfg, checks, corpus.samples); err != nil {
		return err
	}
	before, err := readProviderMetricsForChecks(ctx, cfg, checks)
	if err != nil {
		return err
	}
	if err := restartDecryptWarmupAPIServers(ctx, cfg, checks); err != nil {
		return err
	}
	results := runDecryptColdStartLists(ctx, cfg, checks, corpus.selector)
	after, err := readProviderMetricsForChecks(ctx, cfg, checks)
	if err != nil {
		return err
	}
	stats := collectDecryptWarmupStatsFromSlice(results)
	p95 := percentileWarmupDuration(stats.durations, 95)
	maxDuration := maxWarmupDuration(stats.durations)
	successfulLists := stats.count - stats.errorCount
	if successfulLists < 0 {
		successfulLists = 0
	}
	secretObjectsRead := successfulLists * corpus.secretCount
	delta := after.subtract(before)
	fmt.Printf(
		"harvester_decrypt_cold_start cluster=%s secrets=%d endpoints=%d lists=%d "+
			"secret_objects_read=%d errors=%d list_p95=%s list_max=%s "+
			"provider_decrypt_delta=%d provider_decrypt_errors_delta=%d "+
			"transit_decrypt_delta=%d transit_decrypt_errors_delta=%d\n",
		clusterName,
		corpus.secretCount,
		len(checks),
		stats.count,
		secretObjectsRead,
		stats.errorCount,
		p95.Round(time.Millisecond),
		maxDuration.Round(time.Millisecond),
		delta.grpcDecryptOK,
		delta.grpcDecryptError,
		delta.transitDecryptOK,
		delta.transitDecryptError,
	)
	if stats.errorCount > 0 {
		return fmt.Errorf("decrypt cold start recorded %d errors; first=%s", stats.errorCount, stats.firstError)
	}
	if p95 > cfg.decryptColdStartMaxP95 {
		return fmt.Errorf("decrypt cold start list p95 = %s, max %s", p95, cfg.decryptColdStartMaxP95)
	}
	if delta.grpcDecryptError > 0 || delta.transitDecryptError > 0 {
		return fmt.Errorf(
			"decrypt cold start provider errors: grpc=%d transit=%d",
			delta.grpcDecryptError,
			delta.transitDecryptError,
		)
	}
	return nil
}

type decryptWarmupSample struct {
	name      string
	valuePath string
	cleanup   func()
}

type decryptWarmupCorpus struct {
	clusterName  string
	clusterLabel string
	selector     string
	secretCount  int
	samples      []decryptWarmupSample
	reused       bool
}

func prepareDecryptWarmupCorpus(
	ctx context.Context,
	cfg *labConfig,
	clusterName string,
	checks []kubeadmCheck,
) (decryptWarmupCorpus, func(), error) {
	if len(checks) == 0 {
		return decryptWarmupCorpus{}, func() {}, errors.New("decrypt warmup requires at least one kubeadm check")
	}
	if cfg.decryptWarmupSecretCount <= 0 {
		return decryptWarmupCorpus{}, func() {}, errors.New("HARVESTER_DECRYPT_WARMUP_SECRET_COUNT must be positive")
	}
	if cfg.decryptWarmupSeedBatchSize <= 0 {
		return decryptWarmupCorpus{}, func() {}, errors.New("HARVESTER_DECRYPT_WARMUP_SEED_BATCH_SIZE must be positive")
	}
	if cfg.decryptWarmupSeedWorkers <= 0 {
		return decryptWarmupCorpus{}, func() {}, errors.New("HARVESTER_DECRYPT_WARMUP_SEED_WORKERS must be positive")
	}
	clusterLabel := labelSafeValue(clusterName)
	selector := decryptWarmupSelector(clusterLabel)
	primary := checks[0]
	corpus := decryptWarmupCorpus{
		clusterName:  clusterName,
		clusterLabel: clusterLabel,
		selector:     selector,
		secretCount:  cfg.decryptWarmupSecretCount,
	}
	fmt.Printf(
		"preparing decrypt warmup corpus cluster=%s secrets=%d namespace=%s\n",
		clusterName,
		cfg.decryptWarmupSecretCount,
		decryptWarmupNamespace,
	)
	if err := ensureDecryptWarmupNamespace(ctx, cfg, primary.kubeconfig); err != nil {
		return decryptWarmupCorpus{}, func() {}, err
	}
	existing, err := countDecryptWarmupSecrets(ctx, cfg, primary.kubeconfig, selector)
	if err != nil {
		return decryptWarmupCorpus{}, func() {}, err
	}
	if cfg.decryptWarmupReuseCorpus && existing == cfg.decryptWarmupSecretCount {
		corpus.reused = true
		corpus.samples = decryptWarmupSamplesForExisting(clusterName, cfg.decryptWarmupSecretCount)
		fmt.Printf(
			"reusing decrypt warmup corpus cluster=%s secrets=%d\n",
			clusterName,
			cfg.decryptWarmupSecretCount,
		)
		return corpus, func() {}, nil
	}
	if existing > 0 {
		fmt.Printf(
			"deleting decrypt warmup corpus cluster=%s existing=%d target=%d\n",
			clusterName,
			existing,
			cfg.decryptWarmupSecretCount,
		)
	}
	if err := deleteDecryptWarmupSecrets(ctx, cfg, primary.kubeconfig, clusterName, selector); err != nil {
		return decryptWarmupCorpus{}, func() {}, err
	}
	samples, cleanup, err := seedDecryptWarmupCorpus(ctx, cfg, primary.kubeconfig, clusterName, clusterLabel)
	if err != nil {
		cleanup()
		return decryptWarmupCorpus{}, func() {}, err
	}
	created, err := countDecryptWarmupSecrets(ctx, cfg, primary.kubeconfig, selector)
	if err != nil {
		cleanup()
		return decryptWarmupCorpus{}, func() {}, err
	}
	if created != cfg.decryptWarmupSecretCount {
		cleanup()
		return decryptWarmupCorpus{}, func() {}, fmt.Errorf(
			"decrypt warmup corpus size = %d, want %d",
			created,
			cfg.decryptWarmupSecretCount,
		)
	}
	corpus.samples = samples
	return corpus, cleanup, nil
}

func ensureDecryptWarmupNamespace(ctx context.Context, cfg *labConfig, kubeconfig string) error {
	env := []string{"KUBECONFIG=" + kubeconfig}
	if err := quietKubectl(ctx, cfg, env, "get", "namespace", decryptWarmupNamespace); err == nil {
		return nil
	}
	return runCmdEnv(
		ctx,
		cfg,
		env,
		"kubectl",
		"create",
		"namespace",
		decryptWarmupNamespace,
	)
}

func deleteDecryptWarmupSecrets(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	clusterName string,
	selector string,
) error {
	env := []string{"KUBECONFIG=" + kubeconfig}
	if err := runCmdEnvDiscardOutput(
		ctx,
		cfg,
		env,
		"kubectl",
		"delete",
		"secret",
		"-n",
		decryptWarmupNamespace,
		"-l",
		selector,
		"--ignore-not-found=true",
		"--wait=false",
	); err != nil {
		return err
	}
	return waitDecryptWarmupSecretsDeleted(ctx, cfg, kubeconfig, clusterName, selector)
}

func waitDecryptWarmupSecretsDeleted(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	clusterName string,
	selector string,
) error {
	deadline := time.Now().Add(cfg.waitTimeout)
	lastRemaining := -1
	for {
		remaining, err := countDecryptWarmupSecrets(ctx, cfg, kubeconfig, selector)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return nil
		}
		if remaining != lastRemaining {
			fmt.Printf("deleting decrypt warmup corpus cluster=%s remaining=%d\n", clusterName, remaining)
			lastRemaining = remaining
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out deleting decrypt warmup corpus cluster=%s remaining=%d", clusterName, remaining)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

func verifyDecryptWarmupSamples(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
	samples []decryptWarmupSample,
) error {
	for _, check := range checks {
		for _, sample := range samples {
			if err := verifyRemoteSecretEnvelopeInNamespace(
				ctx,
				cfg,
				check.host,
				decryptWarmupNamespace,
				sample.name,
				sample.valuePath,
			); err != nil {
				return fmt.Errorf("verify decrypt warmup envelope on %s: %w", check.host, err)
			}
		}
	}
	return nil
}

func seedDecryptWarmupCorpus(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	clusterName string,
	clusterLabel string,
) ([]decryptWarmupSample, func(), error) {
	secretCount := cfg.decryptWarmupSecretCount
	chunks := decryptWarmupSeedChunks(secretCount, cfg.decryptWarmupSeedBatchSize)
	if cfg.decryptWarmupSeedWorkers <= 1 || len(chunks) == 1 {
		return seedDecryptWarmupCorpusSerial(ctx, cfg, kubeconfig, clusterName, clusterLabel, chunks, secretCount)
	}
	return seedDecryptWarmupCorpusParallel(ctx, cfg, kubeconfig, clusterName, clusterLabel, chunks, secretCount)
}

type decryptWarmupSeedChunk struct {
	start int
	end   int
}

type decryptWarmupSeedResult struct {
	chunk   decryptWarmupSeedChunk
	samples []decryptWarmupSample
	cleanup func()
	err     error
}

func decryptWarmupSeedChunks(secretCount int, batchSize int) []decryptWarmupSeedChunk {
	chunks := make([]decryptWarmupSeedChunk, 0, (secretCount+batchSize-1)/batchSize)
	for start := 0; start < secretCount; start += batchSize {
		end := start + batchSize
		if end > secretCount {
			end = secretCount
		}
		chunks = append(chunks, decryptWarmupSeedChunk{start: start, end: end})
	}
	return chunks
}

func seedDecryptWarmupCorpusSerial(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	clusterName string,
	clusterLabel string,
	chunks []decryptWarmupSeedChunk,
	secretCount int,
) ([]decryptWarmupSample, func(), error) {
	cleanups := make([]func(), 0, len(chunks))
	cleanupAll := func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}
	samples := make([]decryptWarmupSample, 0, 3)
	for _, chunk := range chunks {
		result := seedDecryptWarmupChunk(
			ctx,
			cfg,
			kubeconfig,
			clusterName,
			clusterLabel,
			secretCount,
			chunk,
		)
		if result.cleanup != nil {
			cleanups = append(cleanups, result.cleanup)
		}
		if result.err != nil {
			cleanupAll()
			return nil, func() {}, result.err
		}
		samples = append(samples, result.samples...)
		fmt.Printf(
			"seeded decrypt warmup corpus cluster=%s progress=%d/%d\n",
			clusterName,
			chunk.end,
			secretCount,
		)
	}
	return samples, cleanupAll, nil
}

func seedDecryptWarmupCorpusParallel(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	clusterName string,
	clusterLabel string,
	chunks []decryptWarmupSeedChunk,
	secretCount int,
) ([]decryptWarmupSample, func(), error) {
	seedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerCount := cfg.decryptWarmupSeedWorkers
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}
	jobs := make(chan decryptWarmupSeedChunk)
	results := make(chan decryptWarmupSeedResult, len(chunks))
	var wg sync.WaitGroup
	for workerID := 0; workerID < workerCount; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range jobs {
				result := seedDecryptWarmupChunk(
					seedCtx,
					cfg,
					kubeconfig,
					clusterName,
					clusterLabel,
					secretCount,
					chunk,
				)
				if result.err != nil {
					cancel()
				}
				results <- result
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, chunk := range chunks {
			select {
			case <-seedCtx.Done():
				return
			case jobs <- chunk:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	cleanups := make([]func(), 0, len(chunks))
	cleanupAll := func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}
	samples := make([]decryptWarmupSample, 0, 3)
	completed := 0
	var firstErr error
	for result := range results {
		if result.cleanup != nil {
			cleanups = append(cleanups, result.cleanup)
		}
		if result.err != nil {
			firstErr = errors.Join(firstErr, result.err)
			continue
		}
		samples = append(samples, result.samples...)
		completed += result.chunk.end - result.chunk.start
		fmt.Printf(
			"seeded decrypt warmup corpus cluster=%s progress=%d/%d workers=%d\n",
			clusterName,
			completed,
			secretCount,
			workerCount,
		)
	}
	if firstErr != nil {
		cleanupAll()
		return nil, func() {}, firstErr
	}
	return samples, cleanupAll, nil
}

func seedDecryptWarmupChunk(
	ctx context.Context,
	cfg *labConfig,
	kubeconfig string,
	clusterName string,
	clusterLabel string,
	secretCount int,
	chunk decryptWarmupSeedChunk,
) decryptWarmupSeedResult {
	manifestPath, samples, cleanup, err := writeDecryptWarmupManifestChunk(
		clusterName,
		clusterLabel,
		secretCount,
		chunk.start,
		chunk.end,
	)
	if err != nil {
		return decryptWarmupSeedResult{chunk: chunk, cleanup: cleanup, err: err}
	}
	if err := applyDecryptWarmupManifest(ctx, cfg, kubeconfig, manifestPath); err != nil {
		return decryptWarmupSeedResult{chunk: chunk, cleanup: cleanup, err: err}
	}
	return decryptWarmupSeedResult{chunk: chunk, samples: samples, cleanup: cleanup}
}

func writeDecryptWarmupManifestChunk(
	clusterName string,
	clusterLabel string,
	count int,
	start int,
	end int,
) (string, []decryptWarmupSample, func(), error) {
	file, err := os.CreateTemp("", "openbao-kms-decrypt-warmup-*.yaml")
	if err != nil {
		return "", nil, func() {}, fmt.Errorf("create decrypt warmup manifest: %w", err)
	}
	cleanupFile := func() {
		_ = os.Remove(file.Name())
	}
	cleanupAll := cleanupFile
	sampleIndexes := decryptWarmupSampleIndexes(count)
	samples := make([]decryptWarmupSample, 0, len(sampleIndexes))
	var manifest strings.Builder
	namePrefix := "openbao-kms-warmup-" + labelSafeValue(clusterName)
	for index := start; index < end; index++ {
		secretName := fmt.Sprintf("%s-%05d", namePrefix, index)
		secretValue, valueErr := randomSecretHex()
		if valueErr != nil {
			cleanupAll()
			return "", nil, func() {}, valueErr
		}
		if sampleIndexes[index] {
			valuePath, sampleCleanup, sampleErr := writeTempSecretValue(secretValue)
			if sampleErr != nil {
				cleanupAll()
				return "", nil, func() {}, sampleErr
			}
			previousCleanup := cleanupAll
			cleanupAll = func() {
				previousCleanup()
				sampleCleanup()
			}
			samples = append(samples, decryptWarmupSample{
				name:      secretName,
				valuePath: valuePath,
				cleanup:   sampleCleanup,
			})
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(secretValue))
		manifest.WriteString("---\n")
		manifest.WriteString("apiVersion: v1\n")
		manifest.WriteString("kind: Secret\n")
		manifest.WriteString("metadata:\n")
		manifest.WriteString("  name: " + secretName + "\n")
		manifest.WriteString("  namespace: " + decryptWarmupNamespace + "\n")
		manifest.WriteString("  labels:\n")
		manifest.WriteString("    " + decryptWarmupCaseLabelKey + ": " + decryptWarmupCaseLabelValue + "\n")
		manifest.WriteString("    " + decryptWarmupClusterLabel + ": " + clusterLabel + "\n")
		manifest.WriteString("type: Opaque\n")
		manifest.WriteString("data:\n")
		manifest.WriteString("  value: " + encoded + "\n")
	}
	if _, err := file.WriteString(manifest.String()); err != nil {
		_ = file.Close()
		cleanupAll()
		return "", nil, func() {}, fmt.Errorf("write decrypt warmup manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanupAll()
		return "", nil, func() {}, fmt.Errorf("close decrypt warmup manifest: %w", err)
	}
	return file.Name(), samples, cleanupAll, nil
}

func decryptWarmupSamplesForExisting(clusterName string, count int) []decryptWarmupSample {
	namePrefix := "openbao-kms-warmup-" + labelSafeValue(clusterName)
	indexes := decryptWarmupSampleIndexes(count)
	samples := make([]decryptWarmupSample, 0, len(indexes))
	keys := make([]int, 0, len(indexes))
	for index := range indexes {
		keys = append(keys, index)
	}
	sort.Ints(keys)
	for _, index := range keys {
		samples = append(samples, decryptWarmupSample{
			name: fmt.Sprintf("%s-%05d", namePrefix, index),
		})
	}
	return samples
}

func decryptWarmupSampleIndexes(count int) map[int]bool {
	indexes := map[int]bool{
		0:         true,
		count - 1: true,
	}
	if count > 2 {
		indexes[count/2] = true
	}
	return indexes
}

func applyDecryptWarmupManifest(ctx context.Context, cfg *labConfig, kubeconfig string, manifestPath string) error {
	return runCmdEnvDiscardOutput(
		ctx,
		cfg,
		[]string{"KUBECONFIG=" + kubeconfig},
		"kubectl",
		"create",
		"-f",
		manifestPath,
	)
}

func countDecryptWarmupSecrets(ctx context.Context, cfg *labConfig, kubeconfig string, selector string) (int, error) {
	output, err := outputCmdEnv(
		ctx,
		cfg,
		[]string{"KUBECONFIG=" + kubeconfig},
		"kubectl",
		"get",
		"secret",
		"-n",
		decryptWarmupNamespace,
		"-l",
		selector,
		"-o",
		"name",
	)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(string(output)) == "" {
		return 0, nil
	}
	return len(strings.Fields(string(output))), nil
}

func restartDecryptWarmupAPIServers(ctx context.Context, cfg *labConfig, checks []kubeadmCheck) error {
	if !cfg.decryptWarmupRestartParallel || len(checks) < 2 {
		for _, check := range checks {
			fmt.Printf("restarting kube-apiserver before decrypt warmup on %s\n", check.host)
			if err := restartAPIServer(ctx, cfg, check); err != nil {
				return fmt.Errorf("restart kube-apiserver on %s: %w", check.host, err)
			}
		}
		return nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(checks))
	for _, check := range checks {
		wg.Add(1)
		go func(check kubeadmCheck) {
			defer wg.Done()
			fmt.Printf("restarting kube-apiserver before decrypt warmup on %s\n", check.host)
			if err := restartAPIServer(ctx, cfg, check); err != nil {
				errs <- fmt.Errorf("restart kube-apiserver on %s: %w", check.host, err)
			}
		}(check)
	}
	wg.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	return joined
}

type decryptWarmupResult struct {
	check    string
	duration time.Duration
	err      error
}

type decryptWarmupStats struct {
	count      int
	errorCount int
	firstError string
	durations  []time.Duration
}

func runDecryptWarmupSoak(
	ctx context.Context,
	cfg *labConfig,
	clusterName string,
	checks []kubeadmCheck,
	selector string,
	secretCount int,
) error {
	workers := cfg.decryptWarmupWorkers
	if workers <= 0 {
		workers = len(checks)
	}
	results := make(chan decryptWarmupResult, workers)
	soakCtx, cancel := context.WithTimeout(ctx, cfg.decryptWarmupDuration)
	defer cancel()
	var wg sync.WaitGroup
	startedAt := time.Now()
	for workerID := 0; workerID < workers; workerID++ {
		check := checks[workerID%len(checks)]
		wg.Add(1)
		go func(check kubeadmCheck) {
			defer wg.Done()
			runDecryptWarmupWorker(soakCtx, cfg, check, selector, results)
		}(check)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	stats := collectDecryptWarmupStats(results)
	elapsed := time.Since(startedAt)
	p95 := percentileWarmupDuration(stats.durations, 95)
	maxDuration := maxWarmupDuration(stats.durations)
	successfulLists := stats.count - stats.errorCount
	if successfulLists < 0 {
		successfulLists = 0
	}
	secretObjectsRead := successfulLists * secretCount
	fmt.Printf(
		"harvester_decrypt_warmup cluster=%s secrets=%d workers=%d duration=%s lists=%d "+
			"secret_objects_read=%d errors=%d list_p95=%s list_max=%s\n",
		clusterName,
		secretCount,
		workers,
		elapsed.Round(time.Second),
		stats.count,
		secretObjectsRead,
		stats.errorCount,
		p95.Round(time.Millisecond),
		maxDuration.Round(time.Millisecond),
	)
	if stats.errorCount > 0 {
		return fmt.Errorf("decrypt warmup recorded %d errors; first=%s", stats.errorCount, stats.firstError)
	}
	if stats.count < cfg.decryptWarmupMinLists {
		return fmt.Errorf("decrypt warmup lists = %d, want at least %d", stats.count, cfg.decryptWarmupMinLists)
	}
	if p95 > cfg.decryptWarmupMaxP95 {
		return fmt.Errorf("decrypt warmup list p95 = %s, max %s", p95, cfg.decryptWarmupMaxP95)
	}
	return nil
}

func runDecryptColdStartLists(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
	selector string,
) []decryptWarmupResult {
	listCtx, cancel := context.WithTimeout(ctx, cfg.decryptColdStartTimeout)
	defer cancel()
	results := make([]decryptWarmupResult, len(checks))
	var wg sync.WaitGroup
	for index, check := range checks {
		wg.Add(1)
		go func(index int, check kubeadmCheck) {
			defer wg.Done()
			fmt.Printf("listing decrypt cold-start corpus through %s\n", check.host)
			results[index] = runDecryptWarmupList(listCtx, cfg, check, selector)
		}(index, check)
	}
	wg.Wait()
	return results
}

func runDecryptWarmupWorker(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	selector string,
	results chan<- decryptWarmupResult,
) {
	for {
		if ctx.Err() != nil {
			return
		}
		result := runDecryptWarmupList(ctx, cfg, check, selector)
		if ctx.Err() != nil {
			return
		}
		results <- result
		if result.err != nil {
			time.Sleep(2 * time.Second)
		}
	}
}

func runDecryptWarmupList(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	selector string,
) decryptWarmupResult {
	env := []string{"KUBECONFIG=" + check.kubeconfig}
	startedAt := time.Now()
	err := runCmdEnvDiscardOutput(
		ctx,
		cfg,
		env,
		"kubectl",
		"get",
		"secret",
		"-n",
		decryptWarmupNamespace,
		"-l",
		selector,
		"-o",
		"json",
	)
	return decryptWarmupResult{
		check:    check.suffix,
		duration: time.Since(startedAt),
		err:      err,
	}
}

func collectDecryptWarmupStats(results <-chan decryptWarmupResult) decryptWarmupStats {
	var stats decryptWarmupStats
	for result := range results {
		if result.duration > 0 {
			stats.count++
			stats.durations = append(stats.durations, result.duration)
		}
		if result.err != nil {
			stats.errorCount++
			if stats.firstError == "" {
				stats.firstError = result.check + ": " + result.err.Error()
			}
		}
	}
	return stats
}

func collectDecryptWarmupStatsFromSlice(results []decryptWarmupResult) decryptWarmupStats {
	var stats decryptWarmupStats
	for _, result := range results {
		if result.duration > 0 {
			stats.count++
			stats.durations = append(stats.durations, result.duration)
		}
		if result.err != nil {
			stats.errorCount++
			if stats.firstError == "" {
				stats.firstError = result.check + ": " + result.err.Error()
			}
		}
	}
	return stats
}

type providerMetrics struct {
	grpcDecryptOK       int
	grpcDecryptError    int
	transitDecryptOK    int
	transitDecryptError int
}

func (m providerMetrics) subtract(previous providerMetrics) providerMetrics {
	return providerMetrics{
		grpcDecryptOK:       m.grpcDecryptOK - previous.grpcDecryptOK,
		grpcDecryptError:    m.grpcDecryptError - previous.grpcDecryptError,
		transitDecryptOK:    m.transitDecryptOK - previous.transitDecryptOK,
		transitDecryptError: m.transitDecryptError - previous.transitDecryptError,
	}
}

func (m providerMetrics) add(other providerMetrics) providerMetrics {
	return providerMetrics{
		grpcDecryptOK:       m.grpcDecryptOK + other.grpcDecryptOK,
		grpcDecryptError:    m.grpcDecryptError + other.grpcDecryptError,
		transitDecryptOK:    m.transitDecryptOK + other.transitDecryptOK,
		transitDecryptError: m.transitDecryptError + other.transitDecryptError,
	}
}

func readProviderMetricsForChecks(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
) (providerMetrics, error) {
	var total providerMetrics
	for _, check := range checks {
		metrics, err := readProviderMetrics(ctx, cfg, check.host)
		if err != nil {
			return providerMetrics{}, err
		}
		total = total.add(metrics)
	}
	return total, nil
}

func readProviderMetrics(ctx context.Context, cfg *labConfig, host string) (providerMetrics, error) {
	output, err := sshLabOutput(ctx, cfg, host, "curl -fsS http://127.0.0.1:8081/metrics")
	if err != nil {
		return providerMetrics{}, err
	}
	metrics := string(output)
	return providerMetrics{
		grpcDecryptOK: prometheusCounterValue(
			metrics,
			"openbao_kms_grpc_requests_total",
			[]string{`method="decrypt"`, `status="ok"`},
		),
		grpcDecryptError: prometheusCounterValue(
			metrics,
			"openbao_kms_grpc_requests_total",
			[]string{`method="decrypt"`, `status="error"`},
		),
		transitDecryptOK: prometheusCounterValue(
			metrics,
			"openbao_kms_openbao_requests_total",
			[]string{`operation="transit_decrypt"`, `status="ok"`},
		),
		transitDecryptError: prometheusCounterValue(
			metrics,
			"openbao_kms_openbao_requests_total",
			[]string{`operation="transit_decrypt"`, `status="error"`},
		),
	}, nil
}

func prometheusCounterValue(metrics string, name string, requiredLabels []string) int {
	for _, line := range strings.Split(metrics, "\n") {
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		matches := true
		for _, label := range requiredLabels {
			if !strings.Contains(line, label) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return 0
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0
		}
		return int(value)
	}
	return 0
}

func percentileWarmupDuration(durations []time.Duration, percentile int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i int, j int) bool {
		return sorted[i] < sorted[j]
	})
	index := ((percentile * len(sorted)) + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}

func maxWarmupDuration(durations []time.Duration) time.Duration {
	var maxDuration time.Duration
	for _, duration := range durations {
		if duration > maxDuration {
			maxDuration = duration
		}
	}
	return maxDuration
}

func decryptWarmupSelector(clusterLabel string) string {
	return decryptWarmupCaseLabelKey + "=" + decryptWarmupCaseLabelValue + "," +
		decryptWarmupClusterLabel + "=" + clusterLabel
}

func labelSafeValue(value string) string {
	lower := strings.ToLower(value)
	var builder strings.Builder
	previousDash := false
	for _, char := range lower {
		valid := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if valid {
			builder.WriteRune(char)
			previousDash = false
			continue
		}
		if !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	cleaned := strings.Trim(builder.String(), "-")
	if cleaned == "" {
		return "cluster"
	}
	if len(cleaned) > 24 {
		cleaned = strings.Trim(cleaned[:24], "-")
	}
	if cleaned == "" {
		return "cluster"
	}
	return cleaned
}

func labVerifyUpgradeRollback(ctx context.Context, cfg *labConfig, _ []string) error {
	if _, err := requireProviderInputs(cfg); err != nil {
		return err
	}
	if err := verifySystemdUpgradeRollback(ctx, cfg); err != nil {
		return err
	}
	return verifyStaticPodUpgradeRollback(ctx, cfg)
}

func labVerifyPairedRestore(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := labVerifyKMS(ctx, cfg, nil); err != nil {
		return fmt.Errorf("baseline KMS verification: %w", err)
	}
	restoreID := fmt.Sprintf("paired-restore-%d", time.Now().Unix())
	checks := kubeadmChecks(cfg)
	preRestore, cleanupPre, err := createPairedRestoreSecrets(ctx, cfg, checks, restoreID, "pre")
	if err != nil {
		return err
	}
	defer cleanupPre()
	if err := backupPairedRestoreState(ctx, cfg, checks, restoreID); err != nil {
		return err
	}
	if err := runOpenBaoBackupRestoreScript(ctx, cfg, "rotate-transit", restoreID); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	postRestore, cleanupPost, err := createPairedRestoreSecrets(ctx, cfg, checks, restoreID, "post")
	if err != nil {
		return err
	}
	defer cleanupPost()
	if err := runOpenBaoBackupRestoreScript(ctx, cfg, "restore", restoreID); err != nil {
		return err
	}
	return restorePairedKubeadmChecks(ctx, cfg, checks, restoreID, preRestore, postRestore)
}

func createPairedRestoreSecrets(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
	restoreID string,
	phase string,
) (map[string]trackedSecret, func(), error) {
	cleanups := make([]func(), 0, len(checks))
	cleanupAll := func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}
	secrets := make(map[string]trackedSecret, len(checks))
	for _, check := range checks {
		secret, cleanup, err := createTrackedSecret(ctx, cfg, check, restoreID+"-"+phase+"-"+check.suffix)
		if err != nil {
			cleanupAll()
			return nil, func() {}, err
		}
		cleanups = append(cleanups, cleanup)
		secrets[check.suffix] = secret
		if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, secret.name, secret.path); err != nil {
			cleanupAll()
			return nil, func() {}, err
		}
	}
	return secrets, cleanupAll, nil
}

func backupPairedRestoreState(ctx context.Context, cfg *labConfig, checks []kubeadmCheck, restoreID string) error {
	for _, check := range checks {
		if err := runProviderStateScript(ctx, cfg, check, "backup", restoreID); err != nil {
			return err
		}
	}
	if err := runOpenBaoBackupRestoreScript(ctx, cfg, "backup", restoreID); err != nil {
		return err
	}
	for _, check := range checks {
		if err := runEtcdSnapshotRestoreScript(ctx, cfg, check, "snapshot", restoreID); err != nil {
			return err
		}
	}
	return nil
}

func restorePairedKubeadmChecks(
	ctx context.Context,
	cfg *labConfig,
	checks []kubeadmCheck,
	restoreID string,
	preRestore map[string]trackedSecret,
	postRestore map[string]trackedSecret,
) error {
	for _, check := range checks {
		if err := restoreKubeadmPair(ctx, cfg, check, restoreID); err != nil {
			return err
		}
		if err := verifyRemoteSecretEnvelope(
			ctx,
			cfg,
			check.host,
			preRestore[check.suffix].name,
			preRestore[check.suffix].path,
		); err != nil {
			return err
		}
		if err := verifySecretAbsent(ctx, cfg, check.kubeconfig, postRestore[check.suffix].name); err != nil {
			return err
		}
	}
	return nil
}

func restoreKubeadmPair(ctx context.Context, cfg *labConfig, check kubeadmCheck, restoreID string) error {
	if err := stopProviderForRestore(ctx, cfg, check); err != nil {
		return err
	}
	if err := runProviderStateScript(ctx, cfg, check, "restore", restoreID); err != nil {
		return err
	}
	if err := startProviderAfterRestore(ctx, cfg, check); err != nil {
		return err
	}
	if err := runEtcdSnapshotRestoreScript(ctx, cfg, check, "restore", restoreID); err != nil {
		return err
	}
	return waitAPIServer(ctx, cfg, check.kubeconfig)
}

func runOpenBaoBackupRestoreScript(ctx context.Context, cfg *labConfig, action string, restoreID string) error {
	return runRemoteRootScript(
		ctx,
		cfg,
		cfg.openBaoHost,
		"openbao-backup-restore.sh",
		"ACTION",
		action,
		"RESTORE_ID",
		restoreID,
	)
}

func runProviderStateScript(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	action string,
	restoreID string,
) error {
	return runRemoteRootScript(
		ctx,
		cfg,
		check.host,
		"provider-state-backup-restore.sh",
		"ACTION",
		action,
		"RESTORE_ID",
		restoreID+"-"+check.suffix,
	)
}

func runEtcdSnapshotRestoreScript(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	action string,
	restoreID string,
) error {
	return runRemoteRootScript(
		ctx,
		cfg,
		check.host,
		"etcd-snapshot-restore.sh",
		"ACTION",
		action,
		"RESTORE_ID",
		restoreID+"-"+check.suffix,
	)
}

func runRemoteRootScript(
	ctx context.Context,
	cfg *labConfig,
	host string,
	scriptName string,
	envPairs ...string,
) error {
	source := filepath.Join(cfg.root, "hack", "harvester", "remote", scriptName)
	remotePath := "/tmp/" + scriptName
	if err := scpLab(ctx, cfg, source, host+":"+remotePath); err != nil {
		return err
	}
	return sshLab(ctx, cfg, host, joinEnvForSudo(envPairs...)+" sh "+remotePath)
}

func stopProviderForRestore(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	switch check.providerMode {
	case providerModeSystemd:
		return sshLab(ctx, cfg, check.host, "sudo systemctl stop bao-kms-provider.service")
	case providerModeStaticPod:
		command := strings.Join([]string{
			"sudo sh -c",
			shellQuote(strings.Join([]string{
				"set -eu",
				"install -d -m 0700 /root/openbao-kms-lab/provider-manifest-off",
				"if [ -f /etc/kubernetes/manifests/bao-kms-provider.yaml ]; then",
				"mv /etc/kubernetes/manifests/bao-kms-provider.yaml " +
					"/root/openbao-kms-lab/provider-manifest-off/bao-kms-provider.yaml",
				"fi",
			}, "\n")),
		}, " ")
		if err := sshLab(ctx, cfg, check.host, command); err != nil {
			return err
		}
		return waitRemoteCommand(
			ctx,
			cfg,
			check.host,
			"static-pod provider stop",
			2*time.Minute,
			noContainerCommand("^bao-kms-provider$"),
		)
	default:
		return fmt.Errorf("unknown provider mode: %s", check.providerMode)
	}
}

func startProviderAfterRestore(ctx context.Context, cfg *labConfig, check kubeadmCheck) error {
	switch check.providerMode {
	case providerModeSystemd:
		if err := sshLab(ctx, cfg, check.host, "sudo systemctl start bao-kms-provider.service"); err != nil {
			return err
		}
	case providerModeStaticPod:
		command := strings.Join([]string{
			"sudo sh -c",
			shellQuote(strings.Join([]string{
				"set -eu",
				"if [ -f /root/openbao-kms-lab/provider-manifest-off/bao-kms-provider.yaml ]; then",
				"mv /root/openbao-kms-lab/provider-manifest-off/bao-kms-provider.yaml " +
					"/etc/kubernetes/manifests/bao-kms-provider.yaml",
				"fi",
			}, "\n")),
		}, " ")
		if err := sshLab(ctx, cfg, check.host, command); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown provider mode: %s", check.providerMode)
	}
	return waitProviderReady(ctx, cfg, check.host)
}

func noContainerCommand(namePattern string) string {
	crictl := strings.Join([]string{
		"sudo crictl",
		"--config /dev/null",
		"--runtime-endpoint unix:///run/containerd/containerd.sock",
		"--image-endpoint unix:///run/containerd/containerd.sock",
	}, " ")
	script := "test -z \"$(" + crictl + " ps -a --name " + shellQuote(namePattern) + " -q | head -n1)\""
	return "sh -c " + shellQuote(script)
}

func verifySecretAbsent(ctx context.Context, cfg *labConfig, kubeconfig string, secretName string) error {
	err := quietKubectl(
		ctx,
		cfg,
		[]string{"KUBECONFIG=" + kubeconfig},
		"get",
		"secret",
		secretName,
		"-n",
		"default",
	)
	if err != nil {
		fmt.Printf("post-restore Secret %s is absent as expected\n", secretName)
		return nil
	}
	return fmt.Errorf("post-restore Secret still exists after paired restore: %s", secretName)
}

func verifySystemdUpgradeRollback(ctx context.Context, cfg *labConfig) error {
	check := kubeadmChecks(cfg)[0]
	oldBinary := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-upgrade-old")
	newBinary := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-upgrade-new")
	if err := buildProviderBinaryAt(ctx, cfg, "harvester-lab-upgrade-old", oldBinary); err != nil {
		return err
	}
	if err := buildProviderBinaryAt(ctx, cfg, "harvester-lab-upgrade-new", newBinary); err != nil {
		return err
	}
	if err := applySystemdBinary(ctx, cfg, check, oldBinary, "old"); err != nil {
		return err
	}
	oldSecret, oldCleanup, err := createTrackedSecret(ctx, cfg, check, "upgrade-systemd-old")
	if err != nil {
		return err
	}
	defer oldCleanup()
	if err := applySystemdBinary(ctx, cfg, check, newBinary, "new"); err != nil {
		return err
	}
	newSecret, newCleanup, err := createTrackedSecret(ctx, cfg, check, "upgrade-systemd-new")
	if err != nil {
		return err
	}
	defer newCleanup()
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, oldSecret.name, oldSecret.path); err != nil {
		return err
	}
	if err := applySystemdBinary(ctx, cfg, check, oldBinary, "rollback"); err != nil {
		return err
	}
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, oldSecret.name, oldSecret.path); err != nil {
		return err
	}
	return verifyRemoteSecretEnvelope(ctx, cfg, check.host, newSecret.name, newSecret.path)
}

func applySystemdBinary(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	binaryPath string,
	label string,
) error {
	if err := ensureRemoteProviderAssetDir(ctx, cfg, check.host); err != nil {
		return err
	}
	remoteBinary := remoteProviderAssetDir + "/bao-kms-provider-" + label
	if err := scpLab(ctx, cfg, binaryPath, check.host+":"+remoteBinary); err != nil {
		return err
	}
	command := "sudo install -m 0755 -o root -g root " + remoteBinary +
		" /usr/bin/bao-kms-provider && sudo systemctl restart bao-kms-provider.service"
	if err := sshLab(ctx, cfg, check.host, command); err != nil {
		return err
	}
	if err := waitProviderReady(ctx, cfg, check.host); err != nil {
		return err
	}
	return waitAPIServer(ctx, cfg, check.kubeconfig)
}

type trackedSecret struct {
	name string
	path string
}

func createTrackedSecret(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	suffix string,
) (trackedSecret, func(), error) {
	secretName := "openbao-kms-" + suffix
	secretValue, err := randomSecretHex()
	if err != nil {
		return trackedSecret{}, func() {}, err
	}
	tempFile, cleanup, err := writeTempSecretValue(secretValue)
	if err != nil {
		return trackedSecret{}, func() {}, err
	}
	if err := createKMSSecret(ctx, cfg, check.kubeconfig, secretName, tempFile); err != nil {
		cleanup()
		return trackedSecret{}, func() {}, err
	}
	return trackedSecret{name: secretName, path: tempFile}, cleanup, nil
}

func verifyStaticPodUpgradeRollback(ctx context.Context, cfg *labConfig) error {
	check := kubeadmChecks(cfg)[1]
	oldImage := imageWithTag(cfg.providerImage, "harvester-lab-upgrade-old")
	newImage := imageWithTag(cfg.providerImage, "harvester-lab-upgrade-new")
	oldTar := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-image-upgrade-old.tar")
	newTar := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-image-upgrade-new.tar")
	oldManifest := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-upgrade-old.yaml")
	newManifest := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-upgrade-new.yaml")
	if err := buildProviderImage(ctx, cfg, oldImage, "harvester-lab-upgrade-old"); err != nil {
		return err
	}
	if err := runCmd(ctx, cfg, "docker", "save", oldImage, "-o", oldTar); err != nil {
		return err
	}
	if err := buildProviderImage(ctx, cfg, newImage, "harvester-lab-upgrade-new"); err != nil {
		return err
	}
	if err := runCmd(ctx, cfg, "docker", "save", newImage, "-o", newTar); err != nil {
		return err
	}
	if err := writeStaticPodManifestForImage(cfg, oldManifest, oldImage); err != nil {
		return err
	}
	if err := writeStaticPodManifestForImage(cfg, newManifest, newImage); err != nil {
		return err
	}
	if err := applyStaticPodRelease(ctx, cfg, check, oldTar, oldManifest, "old"); err != nil {
		return err
	}
	oldSecret, oldCleanup, err := createTrackedSecret(ctx, cfg, check, "upgrade-static-old")
	if err != nil {
		return err
	}
	defer oldCleanup()
	if err := applyStaticPodRelease(ctx, cfg, check, newTar, newManifest, "new"); err != nil {
		return err
	}
	newSecret, newCleanup, err := createTrackedSecret(ctx, cfg, check, "upgrade-static-new")
	if err != nil {
		return err
	}
	defer newCleanup()
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, oldSecret.name, oldSecret.path); err != nil {
		return err
	}
	if err := applyStaticPodRelease(ctx, cfg, check, oldTar, oldManifest, "rollback"); err != nil {
		return err
	}
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, oldSecret.name, oldSecret.path); err != nil {
		return err
	}
	return verifyRemoteSecretEnvelope(ctx, cfg, check.host, newSecret.name, newSecret.path)
}

func imageWithTag(image string, tag string) string {
	if strings.Contains(image, "@") {
		return strings.Split(image, "@")[0] + ":" + tag
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon+1] + tag
	}
	return image + ":" + tag
}

func applyStaticPodRelease(
	ctx context.Context,
	cfg *labConfig,
	check kubeadmCheck,
	imageTar string,
	manifest string,
	label string,
) error {
	if err := ensureRemoteProviderAssetDir(ctx, cfg, check.host); err != nil {
		return err
	}
	remoteTar := remoteProviderAssetDir + "/bao-kms-provider-image-" + label + ".tar"
	remoteManifest := remoteProviderAssetDir + "/bao-kms-provider-" + label + ".yaml"
	if err := scpLab(ctx, cfg, imageTar, check.host+":"+remoteTar); err != nil {
		return err
	}
	if err := scpLab(ctx, cfg, manifest, check.host+":"+remoteManifest); err != nil {
		return err
	}
	command := "sudo ctr -n k8s.io images import " + remoteTar + " >/dev/null" +
		" && sudo install -m 0644 -o root -g root " + remoteManifest +
		" /etc/kubernetes/manifests/bao-kms-provider.yaml"
	if err := sshLab(ctx, cfg, check.host, command); err != nil {
		return err
	}
	if err := waitProviderReady(ctx, cfg, check.host); err != nil {
		return err
	}
	return waitAPIServer(ctx, cfg, check.kubeconfig)
}

func ensureRemoteProviderAssetDir(ctx context.Context, cfg *labConfig, host string) error {
	command := "sudo install -d -m 0700 " + remoteProviderAssetDir +
		" && sudo chown " + shellQuote(cfg.sshUser) + ":" + shellQuote(cfg.sshUser) + " " + remoteProviderAssetDir
	return sshLab(ctx, cfg, host, command)
}

func labProductionGate(ctx context.Context, cfg *labConfig, _ []string) error {
	steps := []struct {
		name string
		run  func(context.Context, *labConfig, []string) error
	}{
		{"verify-guests", labVerifyGuests},
		{"verify-kms", labVerifyKMS},
		{"verify-recovery", labVerifyRecovery},
		{"verify-openbao-outage", labVerifyOpenBaoOutage},
		{"verify-upgrade-rollback", labVerifyUpgradeRollback},
		{"verify-paired-restore", labVerifyPairedRestore},
		{"verify-load", labVerifyLoad},
	}
	if cfg.multiControlPlaneEnabled {
		steps = append(steps, struct {
			name string
			run  func(context.Context, *labConfig, []string) error
		}{"verify-mcp-recovery", labVerifyMultiControlPlaneRecovery})
	}
	for _, step := range steps {
		fmt.Printf("==> harvester lab production gate: %s\n", step.name)
		if err := step.run(ctx, cfg, nil); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}
