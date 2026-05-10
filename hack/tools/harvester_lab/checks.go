package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	remoteScript := filepath.Join(cfg.root, "hack", "harvester", "remote", "verify-kms-encryption.sh")
	if err := scpLab(ctx, cfg, remoteScript, host+":/tmp/verify-kms-encryption.sh"); err != nil {
		return err
	}
	if err := scpLab(ctx, cfg, valuePath, host+":"+remoteValuePath); err != nil {
		return err
	}
	command := joinEnvForSudo(
		"SECRET_NAME", secretName,
		"SECRET_VALUE_FILE", remoteValuePath,
	) + " sh /tmp/verify-kms-encryption.sh; sudo rm -f " + remoteValuePath
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
