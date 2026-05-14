package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseHostsSortsAndUsesRoleLabel(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"metadata": {
					"name": "obk-kubeadm-systemd-1",
					"labels": {"openbao-kms.dev/lab-role": "kubeadm-systemd"}
				},
				"status": {"interfaces": [{"ipAddress": "10.0.0.12"}]}
			},
			{
				"metadata": {
					"name": "obk-openbao-1",
					"labels": {"openbao-kms.dev/lab-role": "openbao"}
				},
				"status": {"interfaces": [{"ipAddress": "10.0.0.10"}]}
			},
			{
				"metadata": {"name": "obk-no-ip-1", "labels": {}},
				"status": {"interfaces": []}
			}
		]
	}`)

	got, err := parseHosts(input)
	if err != nil {
		t.Fatalf("parseHosts() error = %v", err)
	}

	want := []sshHost{
		{Name: "obk-kubeadm-systemd-1", Role: "kubeadm-systemd", IP: "10.0.0.12"},
		{Name: "obk-openbao-1", Role: "openbao", IP: "10.0.0.10"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseHosts() = %#v, want %#v", got, want)
	}
}

func TestParseHostsFallsBackToNameWhenRoleLabelIsMissing(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"metadata": {"name": "obk-openbao-1", "labels": {}},
				"status": {"interfaces": [{"ipAddress": "10.0.0.10"}]}
			}
		]
	}`)

	got, err := parseHosts(input)
	if err != nil {
		t.Fatalf("parseHosts() error = %v", err)
	}

	want := []sshHost{{Name: "obk-openbao-1", Role: "obk-openbao-1", IP: "10.0.0.10"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseHosts() = %#v, want %#v", got, want)
	}
}

func TestParseHostsFromVMNetworkConfigsUsesVMRoleMap(t *testing.T) {
	vmNetCfgInput := []byte(`{
		"items": [
			{
				"metadata": {"name": "obk-openbao-1"},
				"spec": {"vmName": "obk-openbao-1"},
				"status": {"networkConfigs": [{"allocatedIPAddress": "172.16.10.24"}]}
			},
			{
				"metadata": {"name": "unrelated-vm"},
				"spec": {"vmName": "unrelated-vm"},
				"status": {"networkConfigs": [{"allocatedIPAddress": "172.16.10.99"}]}
			}
		]
	}`)
	vmRoles := map[string]string{"obk-openbao-1": "openbao"}

	got, err := parseHostsFromVMNetworkConfigs(vmNetCfgInput, vmRoles)
	if err != nil {
		t.Fatalf("parseHostsFromVMNetworkConfigs() error = %v", err)
	}

	want := []sshHost{{Name: "obk-openbao-1", Role: "openbao", IP: "172.16.10.24"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseHostsFromVMNetworkConfigs() = %#v, want %#v", got, want)
	}
}

func TestCollectHostsFallsBackToVMNetworkConfigs(t *testing.T) {
	dir := t.TempDir()
	vmiPath := filepath.Join(dir, "vmi.json")
	vmPath := filepath.Join(dir, "vm.json")
	vmNetCfgPath := filepath.Join(dir, "vmnetcfg.json")

	mustWriteFile(t, vmiPath, `{
		"items": [{
			"metadata": {
				"name": "obk-openbao-1",
				"labels": {"openbao-kms.dev/lab-role": "openbao"}
			},
			"status": {"interfaces": [{"name": "default"}]}
		}]
	}`)
	mustWriteFile(t, vmPath, `{
		"items": [{
			"metadata": {
				"name": "obk-openbao-1",
				"labels": {"openbao-kms.dev/lab-role": "openbao"}
			}
		}]
	}`)
	mustWriteFile(t, vmNetCfgPath, `{
		"items": [{
			"metadata": {"name": "obk-openbao-1"},
			"spec": {"vmName": "obk-openbao-1"},
			"status": {"networkConfigs": [{"allocatedIPAddress": "172.16.10.24"}]}
		}]
	}`)

	got, err := collectHosts("", vmiPath, vmPath, vmNetCfgPath)
	if err != nil {
		t.Fatalf("collectHosts() error = %v", err)
	}

	want := []sshHost{{Name: "obk-openbao-1", Role: "openbao", IP: "172.16.10.24"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectHosts() = %#v, want %#v", got, want)
	}
}

func TestWriteSSHConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ssh-config")
	hosts := []sshHost{
		{Name: "obk-openbao-1", Role: "openbao", IP: "10.0.0.10"},
	}

	if err := writeSSHConfig(path, "ubuntu", "/tmp/id_ed25519", hosts); err != nil {
		t.Fatalf("writeSSHConfig() error = %v", err)
	}

	// #nosec G304 -- test reads a file path created under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	got := string(data)

	for _, want := range []string{
		"  UserKnownHostsFile " + filepath.Join(filepath.Dir(path), "known_hosts"),
		"Host obk-openbao obk-openbao-1",
		"  HostName 10.0.0.10",
		"  User ubuntu",
		"  IdentityFile /tmp/id_ed25519",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ssh config missing %q:\n%s", want, got)
		}
	}
}

func TestWriteIdentityFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	if err := writeIdentityFiles(dir, "issuer", "audience", "subject", now, time.Hour); err != nil {
		t.Fatalf("writeIdentityFiles() error = %v", err)
	}

	for _, name := range []string{"jwt_private_key.pem", "jwt_public_key.pem", "identity.jwt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}

	// #nosec G304 -- test reads a file path created under t.TempDir.
	jwtData, err := os.ReadFile(filepath.Join(dir, "identity.jwt"))
	if err != nil {
		t.Fatalf("read identity.jwt: %v", err)
	}
	parts := strings.Split(string(jwtData), ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}
	claimsData, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT claims: %v", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsData, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	if claims.Issuer != "issuer" ||
		claims.Subject != "subject" ||
		!reflect.DeepEqual(claims.Audience, []string{"audience"}) {
		t.Fatalf("claims = %#v", claims)
	}
	if claims.ExpiresAt != now.Add(time.Hour).Unix() {
		t.Fatalf("expires = %d, want %d", claims.ExpiresAt, now.Add(time.Hour).Unix())
	}
}

func TestPatchAPIServerManifest(t *testing.T) {
	input := []byte(`apiVersion: v1
kind: Pod
spec:
  containers:
    - name: kube-apiserver
      command:
        - kube-apiserver
      volumeMounts:
        - name: k8s-certs
          mountPath: /etc/kubernetes/pki
  volumes:
    - name: k8s-certs
      hostPath:
        path: /etc/kubernetes/pki
        type: DirectoryOrCreate
`)

	output, err := patchAPIServerManifest(input)
	if err != nil {
		t.Fatalf("patchAPIServerManifest() error = %v", err)
	}
	patched := string(output)

	for _, want := range []string{
		"--encryption-provider-config=/etc/kubernetes/openbao-kms/encryption-config.yaml",
		"name: openbao-kms-run",
		"mountPath: /run/openbao-kms",
		"name: openbao-kms-encryption-config",
		"path: /etc/kubernetes/openbao-kms/encryption-config.yaml",
		"type: File",
	} {
		if !strings.Contains(patched, want) {
			t.Fatalf("patched manifest missing %q:\n%s", want, patched)
		}
	}

	republished, err := patchAPIServerManifest(output)
	if err != nil {
		t.Fatalf("second patchAPIServerManifest() error = %v", err)
	}
	if strings.Count(string(republished), "openbao-kms-run") != strings.Count(patched, "openbao-kms-run") {
		t.Fatalf("patch is not idempotent:\n%s", string(republished))
	}
}

func TestParseSSHHostIP(t *testing.T) {
	config := `Host obk-*
  StrictHostKeyChecking accept-new

Host obk-openbao obk-openbao-1
  HostName 172.16.10.19

Host obk-kubeadm-static obk-kubeadm-static-1
  HostName 172.16.10.45
`

	if got := parseSSHHostIP(config, "obk-openbao"); got != "172.16.10.19" {
		t.Fatalf("parseSSHHostIP() = %q, want 172.16.10.19", got)
	}
	if got := parseSSHHostIP(config, "obk-kubeadm-static-1"); got != "172.16.10.45" {
		t.Fatalf("parseSSHHostIP() = %q, want 172.16.10.45", got)
	}
	if got := parseSSHHostIP(config, "missing"); got != "" {
		t.Fatalf("parseSSHHostIP() = %q, want empty", got)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b c")
	want := `'a'"'"'b c'`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func TestDefaultLabValues(t *testing.T) {
	cfg := &labConfig{
		namespace:      "default",
		networkName:    "default/vm4000",
		imageNamespace: "default",
		imageName:      "image-wjmvv",
		sshUser:        "ubuntu",
	}

	values := defaultLabValues(cfg, "ssh-ed25519 test")
	if values.Namespace != "default" {
		t.Fatalf("namespace = %q", values.Namespace)
	}
	if values.Network.Name != "default/vm4000" {
		t.Fatalf("network = %q", values.Network.Name)
	}
	if len(values.VMs) != 3 {
		t.Fatalf("len(VMs) = %d, want 3", len(values.VMs))
	}
	if values.VMs[0].Name != openBaoHostName || values.VMs[0].Role != "openbao" {
		t.Fatalf("first VM = %#v", values.VMs[0])
	}
}

func TestDefaultLabValuesWithMultiControlPlane(t *testing.T) {
	cfg := &labConfig{
		namespace:                "default",
		networkName:              "default/vm4000",
		imageNamespace:           "default",
		imageName:                "image-wjmvv",
		sshUser:                  "ubuntu",
		multiControlPlaneEnabled: true,
		multiControlPlaneHosts: []string{
			"obk-kubeadm-mcp-1",
			"obk-kubeadm-mcp-2",
			"obk-kubeadm-mcp-3",
		},
	}

	values := defaultLabValues(cfg, "ssh-ed25519 test")
	if len(values.VMs) != 6 {
		t.Fatalf("len(VMs) = %d, want 6", len(values.VMs))
	}
	last := values.VMs[len(values.VMs)-1]
	if last.Name != "obk-kubeadm-mcp-3" || last.Role != "kubeadm-mcp-3" {
		t.Fatalf("last VM = %#v", last)
	}
}

func TestDecryptWarmupSampleIndexes(t *testing.T) {
	got := decryptWarmupSampleIndexes(5)
	for _, index := range []int{0, 2, 4} {
		if !got[index] {
			t.Fatalf("sample index %d missing from %#v", index, got)
		}
	}
	if got[1] || got[3] {
		t.Fatalf("unexpected sample indexes: %#v", got)
	}
}

func TestDecryptWarmupSamplesForExisting(t *testing.T) {
	got := decryptWarmupSamplesForExisting("MCP.Cluster", 5)
	want := []decryptWarmupSample{
		{name: "openbao-kms-warmup-mcp-cluster-00000"},
		{name: "openbao-kms-warmup-mcp-cluster-00002"},
		{name: "openbao-kms-warmup-mcp-cluster-00004"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decryptWarmupSamplesForExisting() = %#v, want %#v", got, want)
	}
}

func TestDecryptWarmupSeedChunks(t *testing.T) {
	got := decryptWarmupSeedChunks(2500, 1000)
	want := []decryptWarmupSeedChunk{
		{start: 0, end: 1000},
		{start: 1000, end: 2000},
		{start: 2000, end: 2500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decryptWarmupSeedChunks() = %#v, want %#v", got, want)
	}
}

func TestLabelSafeValue(t *testing.T) {
	tests := map[string]string{
		"MCP":                            "mcp",
		"static_pod.example":             "static-pod-example",
		"---":                            "cluster",
		"012345678901234567890123456789": "012345678901234567890123",
	}
	for input, want := range tests {
		if got := labelSafeValue(input); got != want {
			t.Fatalf("labelSafeValue(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPrometheusCounterValue(t *testing.T) {
	metrics := `# HELP openbao_kms_grpc_requests_total Total gRPC requests.
# TYPE openbao_kms_grpc_requests_total counter
openbao_kms_grpc_requests_total{method="decrypt",status="ok"} 42
openbao_kms_grpc_requests_total{method="decrypt",status="error"} 3
openbao_kms_grpc_requests_total{method="encrypt",status="ok"} 7
openbao_kms_openbao_requests_total{operation="transit_decrypt",status="ok"} 11
`

	if got := prometheusCounterValue(
		metrics,
		"openbao_kms_grpc_requests_total",
		[]string{`method="decrypt"`, `status="ok"`},
	); got != 42 {
		t.Fatalf("decrypt ok counter = %d, want 42", got)
	}
	if got := prometheusCounterValue(
		metrics,
		"openbao_kms_openbao_requests_total",
		[]string{`operation="transit_decrypt"`, `status="ok"`},
	); got != 11 {
		t.Fatalf("transit decrypt ok counter = %d, want 11", got)
	}
	if got := prometheusCounterValue(
		metrics,
		"openbao_kms_grpc_requests_total",
		[]string{`method="status"`, `status="ok"`},
	); got != 0 {
		t.Fatalf("missing counter = %d, want 0", got)
	}
}

func TestEnvDurationParsesUnitsBeforeSecondsFallback(t *testing.T) {
	t.Setenv("LAB_TEST_DURATION", "1m")
	if got := envDuration("LAB_TEST_DURATION", time.Second); got != time.Minute {
		t.Fatalf("duration = %s, want 1m", got)
	}
	t.Setenv("LAB_TEST_DURATION", "30")
	if got := envDuration("LAB_TEST_DURATION", time.Second); got != 30*time.Second {
		t.Fatalf("duration = %s, want 30s", got)
	}
}

func TestRewriteKubeconfigServer(t *testing.T) {
	input := []byte(`apiVersion: v1
kind: Config
clusters:
  - name: kubernetes
    cluster:
      certificate-authority-data: test
      server: https://10.0.0.1:6443
contexts: []
current-context: ""
users: []
`)

	output, err := rewriteKubeconfigServer(input, "https://10.0.0.2:6443")
	if err != nil {
		t.Fatalf("rewriteKubeconfigServer() error = %v", err)
	}
	if !strings.Contains(string(output), "server: https://10.0.0.2:6443") {
		t.Fatalf("rewritten kubeconfig did not include new server:\n%s", string(output))
	}
	if !strings.Contains(string(output), "certificate-authority-data: test") {
		t.Fatalf("rewritten kubeconfig did not preserve CA data:\n%s", string(output))
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
