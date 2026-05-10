package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const roleLabel = "openbao-kms.dev/lab-role"

const (
	defaultJWTIssuer   = "https://issuer.example.internal"
	defaultJWTAudience = "bao-kms-provider"
	defaultJWTSubject  = "system:openbao-kms:workload-a"

	encryptionConfigPath       = "/etc/kubernetes/openbao-kms/encryption-config.yaml"
	encryptionConfigVolumeName = "openbao-kms-encryption-config"
	kmsSocketDir               = "/run/openbao-kms"
	kmsSocketVolumeName        = "openbao-kms-run"
)

type vmiList struct {
	Items []vmi `json:"items"`
}

type vmList struct {
	Items []vm `json:"items"`
}

type vm struct {
	Metadata metadata `json:"metadata"`
}

type vmi struct {
	Metadata metadata  `json:"metadata"`
	Status   vmiStatus `json:"status"`
}

type metadata struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

type vmiStatus struct {
	Interfaces []vmiInterface `json:"interfaces"`
}

type vmiInterface struct {
	IPAddress string `json:"ipAddress"`
}

type vmNetworkConfigList struct {
	Items []vmNetworkConfig `json:"items"`
}

type vmNetworkConfig struct {
	Metadata metadata              `json:"metadata"`
	Spec     vmNetworkConfigSpec   `json:"spec"`
	Status   vmNetworkConfigStatus `json:"status"`
}

type vmNetworkConfigSpec struct {
	VMName string `json:"vmName"`
}

type vmNetworkConfigStatus struct {
	NetworkConfigs []vmNetworkConfigEntry `json:"networkConfigs"`
}

type vmNetworkConfigEntry struct {
	AllocatedIPAddress string `json:"allocatedIPAddress"`
}

type sshHost struct {
	Name string
	Role string
	IP   string
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type jwtClaims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  []string `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	NotBefore int64    `json:"nbf"`
	IssuedAt  int64    `json:"iat"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: harvester_lab <identity|lab|patch-apiserver|ssh-config>")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "identity":
		if err := runIdentity(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "lab":
		if err := runLab(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "patch-apiserver":
		if err := runPatchAPIServer(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "ssh-config":
		if err := runSSHConfig(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}

func runIdentity(argv []string) error {
	flags := flag.NewFlagSet("identity", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	outputDir := flags.String("output-dir", "", "directory for generated lab identity files")
	issuer := flags.String("issuer", defaultJWTIssuer, "JWT issuer")
	audience := flags.String("audience", defaultJWTAudience, "JWT audience")
	subject := flags.String("subject", defaultJWTSubject, "JWT subject")
	ttl := flags.Duration("ttl", 12*time.Hour, "JWT validity duration")
	if err := flags.Parse(argv); err != nil {
		return err
	}

	if *outputDir == "" {
		return errors.New("-output-dir is required")
	}
	if *ttl <= 0 {
		return errors.New("-ttl must be positive")
	}

	return writeIdentityFiles(*outputDir, *issuer, *audience, *subject, time.Now().UTC(), *ttl)
}

func runSSHConfig(argv []string) error {
	flags := flag.NewFlagSet("ssh-config", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	inputPath := flags.String("input", "", "VMI list JSON path, or - for stdin")
	vmiInputPath := flags.String("vmi-input", "", "VMI list JSON path, or - for stdin")
	vmInputPath := flags.String("vm-input", "", "VirtualMachine list JSON path")
	vmNetCfgInputPath := flags.String("vmnetcfg-input", "", "Harvester VirtualMachineNetworkConfig list JSON path")
	outputPath := flags.String("output", "", "SSH config output path")
	user := flags.String("user", "ubuntu", "SSH user")
	identityFile := flags.String("identity-file", "", "SSH private key path")
	if err := flags.Parse(argv); err != nil {
		return err
	}

	if *outputPath == "" {
		return errors.New("-output is required")
	}
	if *identityFile == "" {
		return errors.New("-identity-file is required")
	}

	hosts, err := collectHosts(*inputPath, *vmiInputPath, *vmInputPath, *vmNetCfgInputPath)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no VM IP addresses found")
	}

	return writeSSHConfig(*outputPath, *user, *identityFile, hosts)
}

func writeIdentityFiles(
	outputDir string,
	issuer string,
	audience string,
	subject string,
	now time.Time,
	ttl time.Duration,
) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate JWT signing key: %w", err)
	}

	publicKeyPEM, err := publicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		return err
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	jwt, err := signJWT(privateKey, issuer, audience, subject, now, ttl)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", outputDir, err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "jwt_private_key.pem"), privateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write JWT private key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "jwt_public_key.pem"), []byte(publicKeyPEM), 0o600); err != nil {
		return fmt.Errorf("write JWT public key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "identity.jwt"), []byte(jwt), 0o600); err != nil {
		return fmt.Errorf("write identity JWT: %w", err)
	}
	fmt.Printf("wrote lab identity files under %s\n", outputDir)
	return nil
}

func publicKeyPEM(publicKey *rsa.PublicKey) (string, error) {
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal JWT public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), nil
}

func signJWT(
	privateKey *rsa.PrivateKey,
	issuer string,
	audience string,
	subject string,
	now time.Time,
	ttl time.Duration,
) (string, error) {
	header, err := encodeJWTHeader(jwtHeader{Algorithm: "RS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := encodeJWTClaims(jwtClaims{
		Issuer:    issuer,
		Subject:   subject,
		Audience:  []string{audience},
		ExpiresAt: now.Add(ttl).Unix(),
		NotBefore: now.Add(-30 * time.Second).Unix(),
		IssuedAt:  now.Unix(),
	})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + claims
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func encodeJWTHeader(value jwtHeader) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JWT header: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func encodeJWTClaims(value jwtClaims) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JWT claims: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func runPatchAPIServer(argv []string) error {
	flags := flag.NewFlagSet("patch-apiserver", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	inputPath := flags.String("input", "", "kube-apiserver manifest input path")
	outputPath := flags.String("output", "", "patched manifest output path")
	if err := flags.Parse(argv); err != nil {
		return err
	}
	if *inputPath == "" {
		return errors.New("-input is required")
	}
	if *outputPath == "" {
		return errors.New("-output is required")
	}

	input, err := readInput(*inputPath)
	if err != nil {
		return err
	}
	output, err := patchAPIServerManifest(input)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outputPath, output, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", *outputPath, err)
	}
	return nil
}

func patchAPIServerManifest(input []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(input, &document); err != nil {
		return nil, fmt.Errorf("decode kube-apiserver manifest: %w", err)
	}
	if len(document.Content) != 1 {
		return nil, errors.New("kube-apiserver manifest must contain one YAML document")
	}

	root := document.Content[0]
	spec := mappingValue(root, "spec")
	containers := mappingValue(spec, "containers")
	if containers == nil || containers.Kind != yaml.SequenceNode || len(containers.Content) == 0 {
		return nil, errors.New("kube-apiserver manifest must contain spec.containers[0]")
	}
	container := containers.Content[0]
	command := ensureSequence(container, "command")
	appendScalarIfMissing(command, "--encryption-provider-config="+encryptionConfigPath)

	volumeMounts := ensureSequence(container, "volumeMounts")
	appendMappingIfNameMissing(
		volumeMounts,
		kmsSocketVolumeName,
		volumeMountNode(kmsSocketVolumeName, kmsSocketDir, false),
	)
	appendMappingIfNameMissing(
		volumeMounts,
		encryptionConfigVolumeName,
		volumeMountNode(encryptionConfigVolumeName, encryptionConfigPath, true),
	)

	volumes := ensureSequence(spec, "volumes")
	appendMappingIfNameMissing(
		volumes,
		kmsSocketVolumeName,
		hostPathVolumeNode(kmsSocketVolumeName, kmsSocketDir, "Directory"),
	)
	appendMappingIfNameMissing(
		volumes,
		encryptionConfigVolumeName,
		hostPathVolumeNode(encryptionConfigVolumeName, encryptionConfigPath, "File"),
	)

	output, err := yaml.Marshal(&document)
	if err != nil {
		return nil, fmt.Errorf("encode kube-apiserver manifest: %w", err)
	}
	return output, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func ensureSequence(mapping *yaml.Node, key string) *yaml.Node {
	existing := mappingValue(mapping, key)
	if existing != nil {
		return existing
	}
	sequence := &yaml.Node{Kind: yaml.SequenceNode}
	mapping.Content = append(mapping.Content, scalarNode(key), sequence)
	return sequence
}

func appendScalarIfMissing(sequence *yaml.Node, value string) {
	for _, item := range sequence.Content {
		if item.Value == value {
			return
		}
	}
	sequence.Content = append(sequence.Content, scalarNode(value))
}

func appendMappingIfNameMissing(sequence *yaml.Node, name string, item *yaml.Node) {
	for _, existing := range sequence.Content {
		nameNode := mappingValue(existing, "name")
		if nameNode != nil && nameNode.Value == name {
			return
		}
	}
	sequence.Content = append(sequence.Content, item)
}

func volumeMountNode(name string, mountPath string, readOnly bool) *yaml.Node {
	return mappingNode(
		scalarNode("name"), scalarNode(name),
		scalarNode("mountPath"), scalarNode(mountPath),
		scalarNode("readOnly"), boolNode(readOnly),
	)
}

func hostPathVolumeNode(name string, path string, hostPathType string) *yaml.Node {
	return mappingNode(
		scalarNode("name"), scalarNode(name),
		scalarNode("hostPath"), mappingNode(
			scalarNode("path"), scalarNode(path),
			scalarNode("type"), scalarNode(hostPathType),
		),
	)
}

func mappingNode(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: pairs}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func boolNode(value bool) *yaml.Node {
	if value {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func collectHosts(
	inputPath string,
	vmiInputPath string,
	vmInputPath string,
	vmNetCfgInputPath string,
) ([]sshHost, error) {
	normalizedVMIInputPath, err := normalizeVMIInputPath(inputPath, vmiInputPath, vmNetCfgInputPath)
	if err != nil {
		return nil, err
	}

	hostsByName := make(map[string]sshHost)
	if normalizedVMIInputPath != "" {
		if err := collectVMIHosts(hostsByName, normalizedVMIInputPath); err != nil {
			return nil, err
		}
	}
	if vmNetCfgInputPath != "" {
		if err := collectVMNetworkConfigHosts(hostsByName, vmInputPath, vmNetCfgInputPath); err != nil {
			return nil, err
		}
	}

	return sortedHostsByName(hostsByName), nil
}

func normalizeVMIInputPath(inputPath string, vmiInputPath string, vmNetCfgInputPath string) (string, error) {
	if inputPath != "" && vmiInputPath != "" {
		return "", errors.New("use only one of -input or -vmi-input")
	}
	if inputPath != "" {
		vmiInputPath = inputPath
	}
	if vmiInputPath == "" && vmNetCfgInputPath == "" {
		return "", errors.New("one of -input, -vmi-input, or -vmnetcfg-input is required")
	}
	return vmiInputPath, nil
}

func collectVMIHosts(hostsByName map[string]sshHost, vmiInputPath string) error {
	input, err := readInput(vmiInputPath)
	if err != nil {
		return err
	}
	hosts, err := parseHosts(input)
	if err != nil {
		return err
	}
	addHostsByName(hostsByName, hosts, true)
	return nil
}

func collectVMNetworkConfigHosts(
	hostsByName map[string]sshHost,
	vmInputPath string,
	vmNetCfgInputPath string,
) error {
	vmNetCfgInput, err := readInput(vmNetCfgInputPath)
	if err != nil {
		return err
	}
	vmRoles, err := loadVMRoles(vmInputPath)
	if err != nil {
		return err
	}
	hosts, err := parseHostsFromVMNetworkConfigs(vmNetCfgInput, vmRoles)
	if err != nil {
		return err
	}
	addHostsByName(hostsByName, hosts, false)
	return nil
}

func loadVMRoles(vmInputPath string) (map[string]string, error) {
	if vmInputPath == "" {
		return map[string]string{}, nil
	}
	vmInput, err := readInput(vmInputPath)
	if err != nil {
		return nil, err
	}
	return parseVMRoles(vmInput)
}

func addHostsByName(hostsByName map[string]sshHost, hosts []sshHost, overwrite bool) {
	for _, host := range hosts {
		if _, exists := hostsByName[host.Name]; !exists || overwrite {
			hostsByName[host.Name] = host
		}
	}
}

func sortedHostsByName(hostsByName map[string]sshHost) []sshHost {
	hosts := make([]sshHost, 0, len(hostsByName))
	for _, host := range hostsByName {
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})
	return hosts
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}

	// #nosec G304 -- local lab tooling reads the caller-provided kubectl JSON path.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func parseHosts(input []byte) ([]sshHost, error) {
	var list vmiList
	if err := json.Unmarshal(input, &list); err != nil {
		return nil, fmt.Errorf("decode VMI list: %w", err)
	}

	hosts := make([]sshHost, 0, len(list.Items))
	for _, item := range list.Items {
		ip := firstIP(item.Status.Interfaces)
		if item.Metadata.Name == "" || ip == "" {
			continue
		}
		role := item.Metadata.Labels[roleLabel]
		if role == "" {
			role = item.Metadata.Name
		}
		hosts = append(hosts, sshHost{
			Name: item.Metadata.Name,
			Role: role,
			IP:   ip,
		})
	}

	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})
	return hosts, nil
}

func parseVMRoles(input []byte) (map[string]string, error) {
	var list vmList
	if err := json.Unmarshal(input, &list); err != nil {
		return nil, fmt.Errorf("decode VirtualMachine list: %w", err)
	}

	roles := make(map[string]string, len(list.Items))
	for _, item := range list.Items {
		if item.Metadata.Name == "" {
			continue
		}
		role := item.Metadata.Labels[roleLabel]
		if role == "" {
			role = item.Metadata.Name
		}
		roles[item.Metadata.Name] = role
	}
	return roles, nil
}

func parseHostsFromVMNetworkConfigs(input []byte, vmRoles map[string]string) ([]sshHost, error) {
	var list vmNetworkConfigList
	if err := json.Unmarshal(input, &list); err != nil {
		return nil, fmt.Errorf("decode VirtualMachineNetworkConfig list: %w", err)
	}

	hosts := make([]sshHost, 0, len(list.Items))
	for _, item := range list.Items {
		vmName := item.Spec.VMName
		if vmName == "" {
			vmName = item.Metadata.Name
		}
		ip := firstAllocatedIP(item.Status.NetworkConfigs)
		if vmName == "" || ip == "" {
			continue
		}
		role := vmRoles[vmName]
		if len(vmRoles) > 0 && role == "" {
			continue
		}
		if role == "" {
			role = vmName
		}
		hosts = append(hosts, sshHost{
			Name: vmName,
			Role: role,
			IP:   ip,
		})
	}

	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})
	return hosts, nil
}

func firstIP(interfaces []vmiInterface) string {
	for _, iface := range interfaces {
		if iface.IPAddress != "" {
			return iface.IPAddress
		}
	}
	return ""
}

func firstAllocatedIP(networkConfigs []vmNetworkConfigEntry) string {
	for _, networkConfig := range networkConfigs {
		if networkConfig.AllocatedIPAddress != "" {
			return networkConfig.AllocatedIPAddress
		}
	}
	return ""
}

func writeSSHConfig(path string, user string, identityFile string, hosts []sshHost) error {
	var builder strings.Builder
	knownHostsPath := filepath.Join(filepath.Dir(path), "known_hosts")
	builder.WriteString("# Generated by hack/tools/harvester_lab\n")
	builder.WriteString("Host obk-*\n")
	builder.WriteString("  StrictHostKeyChecking accept-new\n")
	builder.WriteString("  UserKnownHostsFile " + knownHostsPath + "\n\n")

	for _, host := range hosts {
		alias := "obk-" + host.Role
		builder.WriteString("Host " + alias + " " + host.Name + "\n")
		builder.WriteString("  HostName " + host.IP + "\n")
		builder.WriteString("  User " + user + "\n")
		builder.WriteString("  IdentityFile " + identityFile + "\n\n")
	}

	// #nosec G306 -- generated SSH config contains host metadata, not secrets.
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}
