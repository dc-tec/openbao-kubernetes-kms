//go:build e2e

package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	defaultOpenBaoHANodeCount          = 3
	defaultOpenBaoHAProviderNodeIndex  = 1
	defaultOpenBaoHAClusterWaitTimeout = 75 * time.Second
)

type OpenBaoHAEnvironmentConfig struct {
	Image             string
	TransitMount      string
	TransitKey        string
	StartupWait       time.Duration
	DockerBinary      string
	NetworkName       string
	JWTTTL            time.Duration
	JWTTokenTTL       string
	JWTMaxTTL         string
	JWTIssuer         string
	JWTAudience       string
	JWTSubject        string
	NodeCount         int
	ProviderNodeIndex int
}

type OpenBaoHAEnvironment struct {
	OpenBaoEnvironment
	nodes             []openBaoHANode
	providerNodeIndex int
}

type openBaoHANode struct {
	name          string
	storageVolume string
}

type raftPeersResponse struct {
	Data struct {
		Config struct {
			Servers []raftPeer `json:"servers"`
		} `json:"config"`
	} `json:"data"`
}

type raftPeer struct {
	NodeID string `json:"node_id"`
	Voter  bool   `json:"voter"`
}

func StartOpenBaoHAEnvironment(ctx context.Context, cfg OpenBaoHAEnvironmentConfig) (*OpenBaoHAEnvironment, error) {
	baseCfg := defaultOpenBaoEnvironmentConfig(OpenBaoEnvironmentConfig{
		Image:        cfg.Image,
		TransitMount: cfg.TransitMount,
		TransitKey:   cfg.TransitKey,
		StartupWait:  cfg.StartupWait,
		DockerBinary: cfg.DockerBinary,
		NetworkName:  cfg.NetworkName,
		JWTTTL:       cfg.JWTTTL,
		JWTTokenTTL:  cfg.JWTTokenTTL,
		JWTMaxTTL:    cfg.JWTMaxTTL,
		JWTIssuer:    cfg.JWTIssuer,
		JWTAudience:  cfg.JWTAudience,
		JWTSubject:   cfg.JWTSubject,
	})
	nodeCount := cfg.NodeCount
	if nodeCount == 0 {
		nodeCount = defaultOpenBaoHANodeCount
	}
	if nodeCount < 3 {
		return nil, fmt.Errorf("OpenBao HA environment requires at least three nodes")
	}
	providerNodeIndex := cfg.ProviderNodeIndex
	if providerNodeIndex == 0 {
		providerNodeIndex = defaultOpenBaoHAProviderNodeIndex
	}
	if providerNodeIndex < 0 || providerNodeIndex >= nodeCount {
		return nil, fmt.Errorf("OpenBao HA provider node index out of range: %d", providerNodeIndex)
	}

	dockerPath, err := resolveDockerBinary(baseCfg.DockerBinary)
	if err != nil {
		return nil, err
	}
	if err := checkDocker(ctx, dockerPath); err != nil {
		return nil, err
	}
	if baseCfg.NetworkName == "" {
		return nil, fmt.Errorf("OpenBao HA environment requires a Docker network")
	}
	artifactDir, err := EnsureArtifactDir()
	if err != nil {
		return nil, fmt.Errorf("prepare e2e artifact directory: %w", err)
	}
	artifactDir, err = filepath.Abs(artifactDir)
	if err != nil {
		return nil, fmt.Errorf("resolve e2e artifact directory: %w", err)
	}
	certDir, err := os.MkdirTemp(artifactDir, "openbao-ha-tls-")
	if err != nil {
		return nil, fmt.Errorf("create OpenBao HA TLS directory: %w", err)
	}
	suffix, err := randomHex(6)
	if err != nil {
		_ = os.RemoveAll(certDir)
		return nil, fmt.Errorf("generate OpenBao HA name: %w", err)
	}

	nodes := make([]openBaoHANode, 0, nodeCount)
	nodeNames := make([]string, 0, nodeCount)
	for index := 0; index < nodeCount; index++ {
		name := fmt.Sprintf("bao-kms-e2e-ha-%s-%d", suffix, index+1)
		nodes = append(nodes, openBaoHANode{
			name:          name,
			storageVolume: name + "-data",
		})
		nodeNames = append(nodeNames, name)
	}
	environment := &OpenBaoHAEnvironment{
		OpenBaoEnvironment: OpenBaoEnvironment{
			TLSServerName:  openBaoTLSServerName,
			TransitMount:   baseCfg.TransitMount,
			TransitKey:     baseCfg.TransitKey,
			AuthMount:      openBaoJWTAuthMount,
			AuthRole:       openBaoJWTAuthRole,
			transitKeyType: baseCfg.TransitKeyType,
			image:          baseCfg.Image,
			certDir:        certDir,
			dockerBinary:   dockerPath,
			networkName:    baseCfg.NetworkName,
			jwtTTL:         baseCfg.JWTTTL,
			jwtTokenTTL:    baseCfg.JWTTokenTTL,
			jwtMaxTTL:      baseCfg.JWTMaxTTL,
			jwtIssuer:      baseCfg.JWTIssuer,
			jwtAudience:    baseCfg.JWTAudience,
			jwtSubject:     baseCfg.JWTSubject,
		},
		nodes:             nodes,
		providerNodeIndex: providerNodeIndex,
	}
	if err := environment.start(ctx, baseCfg.StartupWait, nodeNames); err != nil {
		_ = environment.Close(context.Background())
		return nil, err
	}
	return environment, nil
}

func (h *OpenBaoHAEnvironment) ProviderAddress() string {
	return "https://" + h.nodes[h.providerNodeIndex].name + ":8200"
}

func (h *OpenBaoHAEnvironment) StopActiveNode(ctx context.Context) error {
	if len(h.nodes) == 0 {
		return fmt.Errorf("OpenBao HA environment has no nodes")
	}
	if err := h.removeContainer(ctx, h.nodes[0].name); err != nil {
		return err
	}
	return h.waitAnySurvivingActiveNode(ctx, defaultOpenBaoHAClusterWaitTimeout)
}

func (h *OpenBaoHAEnvironment) Close(ctx context.Context) error {
	var closeErr error
	for _, node := range h.nodes {
		if err := h.removeContainer(ctx, node.name); err != nil && closeErr == nil {
			closeErr = err
		}
		if err := h.removeVolume(ctx, node.storageVolume); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if !strings.EqualFold(os.Getenv(EnvSkipCleanup), "true") && h.certDir != "" {
		if err := os.RemoveAll(h.certDir); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("remove OpenBao HA TLS directory: %w", err)
		}
	}
	return closeErr
}

func (h *OpenBaoHAEnvironment) start(ctx context.Context, startupWait time.Duration, nodeNames []string) error {
	if err := writeOpenBaoServerTLSFilesForHosts(h.certDir, nodeNames); err != nil {
		return err
	}
	for _, node := range h.nodes {
		if err := prepareOpenBaoStorageVolume(ctx, h.dockerBinary, h.image, node.storageVolume); err != nil {
			return err
		}
	}
	if err := h.writeHAConfigs(nodeNames); err != nil {
		return err
	}
	if err := h.startNode(ctx, 0); err != nil {
		return err
	}
	if err := h.withNode(0, func() error {
		if err := h.waitUntilEndpoint(ctx, startupWait); err != nil {
			return err
		}
		if _, err := h.initializeRaftStorage(ctx); err != nil {
			return err
		}
		if err := h.waitUntilReady(ctx, startupWait); err != nil {
			return err
		}
		if err := h.bootstrapTransit(ctx); err != nil {
			return err
		}
		if err := h.bootstrapJWTAuth(ctx); err != nil {
			return err
		}
		return h.configureAutopilot(ctx)
	}); err != nil {
		return err
	}
	for index := 1; index < len(h.nodes); index++ {
		if err := h.startNode(ctx, index); err != nil {
			return err
		}
		if err := h.withNode(index, func() error {
			if err := h.waitUntilEndpoint(ctx, startupWait); err != nil {
				return err
			}
			httpClient, err := openbao.NewHTTPClient(h.CACertFile, openBaoTLSServerName, 30*time.Second)
			if err != nil {
				return err
			}
			return h.unseal(ctx, httpClient)
		}); err != nil {
			return err
		}
	}
	for index := 1; index < len(h.nodes); index++ {
		if err := h.promoteNode(ctx, index); err != nil {
			return err
		}
	}
	if err := h.waitPeerCount(ctx, len(h.nodes), defaultOpenBaoHAClusterWaitTimeout); err != nil {
		return err
	}
	return h.waitJWTLoginThroughNode(ctx, h.providerNodeIndex, defaultOpenBaoHAClusterWaitTimeout)
}

func (h *OpenBaoHAEnvironment) writeHAConfigs(nodeNames []string) error {
	for index, node := range h.nodes {
		leaderNodes := nodeNames[:1]
		if index == 0 {
			leaderNodes = nil
		}
		if err := writeOpenBaoHARaftStorageConfig(h.certDir, node.name, leaderNodes); err != nil {
			return err
		}
	}
	return nil
}

func (h *OpenBaoHAEnvironment) promoteNode(ctx context.Context, index int) error {
	if index <= 0 || index >= len(h.nodes) {
		return fmt.Errorf("OpenBao HA promote node index out of range: %d", index)
	}
	args := []string{
		"exec",
		"--env", "BAO_ADDR=https://127.0.0.1:8200",
		"--env", "BAO_CACERT=/bao/tls/ca.pem",
		"--env", "BAO_TOKEN=" + h.Token,
		h.nodes[0].name,
		"bao", "operator", "raft", "promote", h.nodes[index].name,
	}
	cmd := exec.CommandContext(ctx, h.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(output))
		if !strings.Contains(trimmed, "server is not a non-voter") {
			return fmt.Errorf("promote OpenBao HA node %s: %w: %s", h.nodes[index].name, err, trimmed)
		}
	}
	return nil
}

func (h *OpenBaoHAEnvironment) startNode(ctx context.Context, index int) error {
	node := h.nodes[index]
	configPath := "/bao/tls/" + node.name + ".hcl"
	args := []string{
		"run",
		"--rm",
		"--detach",
		"--name", node.name,
		"--network", h.networkName,
		"--publish", "127.0.0.1::8200",
		"--volume", h.certDir + ":/bao/tls:ro",
		"--volume", node.storageVolume + ":/bao/data",
		h.image,
		"server",
		"-config=" + configPath,
	}
	cmd := exec.CommandContext(ctx, h.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start OpenBao HA node %s: %w: %s", node.name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *OpenBaoHAEnvironment) withNode(index int, fn func() error) error {
	if index < 0 || index >= len(h.nodes) {
		return fmt.Errorf("OpenBao HA node index out of range: %d", index)
	}
	node := h.nodes[index]
	h.containerName = node.name
	h.storageVolume = node.storageVolume
	return fn()
}

func (h *OpenBaoHAEnvironment) waitPeerCount(ctx context.Context, count int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		peers, err := h.raftPeers(ctx)
		if err == nil {
			missing := false
			for _, node := range h.nodes[:count] {
				peer, ok := peers[node.name]
				if !ok || !peer.Voter {
					missing = true
					break
				}
			}
			if !missing {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for OpenBao HA raft peers")
		}
		time.Sleep(time.Second)
	}
}

func (h *OpenBaoHAEnvironment) raftPeers(ctx context.Context) (map[string]raftPeer, error) {
	args := []string{
		"exec",
		"--env", "BAO_ADDR=https://127.0.0.1:8200",
		"--env", "BAO_CACERT=/bao/tls/ca.pem",
		"--env", "BAO_TOKEN=" + h.Token,
		h.nodes[0].name,
		"bao", "operator", "raft", "list-peers", "-format=json",
	}
	cmd := exec.CommandContext(ctx, h.dockerBinary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list OpenBao HA raft peers: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var response raftPeersResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("decode OpenBao HA raft peers: %w", err)
	}
	peers := make(map[string]raftPeer, len(response.Data.Config.Servers))
	for _, peer := range response.Data.Config.Servers {
		peers[peer.NodeID] = peer
	}
	return peers, nil
}

func (h *OpenBaoHAEnvironment) configureAutopilot(ctx context.Context) error {
	args := []string{
		"exec",
		"--env", "BAO_ADDR=https://127.0.0.1:8200",
		"--env", "BAO_CACERT=/bao/tls/ca.pem",
		"--env", "BAO_TOKEN=" + h.Token,
		h.nodes[0].name,
		"bao", "operator", "raft", "autopilot", "set-config",
		"-server-stabilization-time=1s",
		"-last-contact-threshold=2s",
		"-min-quorum=3",
	}
	cmd := exec.CommandContext(ctx, h.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("configure OpenBao HA autopilot: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *OpenBaoHAEnvironment) waitJWTLoginThroughNode(
	ctx context.Context,
	index int,
	timeout time.Duration,
) error {
	return h.withNode(index, func() error {
		deadline := time.Now().Add(timeout)
		for {
			if err := h.refreshEndpoint(ctx); err == nil {
				if err := h.probeHealthStandbyOK(ctx); err == nil {
					authClient, clientErr := h.NewAuthClient()
					if clientErr == nil {
						jwt, jwtErr := h.IssueJWT(time.Now().UTC(), h.jwtTTL, JWTClaimsOptions{})
						if jwtErr == nil {
							_, loginErr := authClient.LoginJWT(ctx, openbao.JWTLoginRequest{
								MountPath: h.AuthMount,
								Role:      h.AuthRole,
								JWT:       jwt,
							})
							if loginErr == nil {
								return nil
							}
						}
					}
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for OpenBao HA JWT login through %s", h.nodes[index].name)
			}
			time.Sleep(time.Second)
		}
	})
}

func (h *OpenBaoHAEnvironment) waitAnySurvivingActiveNode(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		for index := 1; index < len(h.nodes); index++ {
			err := h.withNode(index, func() error {
				if err := h.refreshEndpoint(ctx); err != nil {
					return err
				}
				return h.probeHealth(ctx)
			})
			if err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timed out waiting for an OpenBao HA standby to become active\n%s",
				h.survivorDiagnostics(ctx),
			)
		}
		time.Sleep(time.Second)
	}
}

func (h *OpenBaoHAEnvironment) survivorDiagnostics(ctx context.Context) string {
	var out strings.Builder
	for index := 1; index < len(h.nodes); index++ {
		node := h.nodes[index]
		_, _ = fmt.Fprintf(&out, "== %s status ==\n", node.name)
		status := exec.CommandContext(
			ctx,
			h.dockerBinary,
			"exec",
			"--env", "BAO_ADDR=https://127.0.0.1:8200",
			"--env", "BAO_CACERT=/bao/tls/ca.pem",
			node.name,
			"bao", "status", "-format=json",
		)
		if output, err := status.CombinedOutput(); err == nil {
			_, _ = out.Write(bytes.TrimSpace(output))
			_, _ = out.WriteString("\n")
		} else {
			_, _ = fmt.Fprintf(&out, "status failed: %v: %s\n", err, strings.TrimSpace(string(output)))
		}
		_, _ = fmt.Fprintf(&out, "== %s logs ==\n", node.name)
		logs := exec.CommandContext(ctx, h.dockerBinary, "logs", "--tail", "40", node.name)
		if output, err := logs.CombinedOutput(); err == nil {
			_, _ = out.Write(bytes.TrimSpace(output))
			_, _ = out.WriteString("\n")
		} else {
			_, _ = fmt.Fprintf(&out, "logs failed: %v: %s\n", err, strings.TrimSpace(string(output)))
		}
	}
	return out.String()
}

func (h *OpenBaoHAEnvironment) probeHealthStandbyOK(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(h.CACertFile, openBaoTLSServerName, 2*time.Second)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		h.Address+"/v1/sys/health?standbyok=true",
		nil,
	)
	if err != nil {
		return err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenBao HA health status %d", response.StatusCode)
	}
	return nil
}

func (h *OpenBaoHAEnvironment) removeContainer(ctx context.Context, name string) error {
	if name == "" || h.dockerBinary == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, h.dockerBinary, "rm", "-f", name)
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "No such container") {
		return fmt.Errorf("remove OpenBao HA container %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *OpenBaoHAEnvironment) removeVolume(ctx context.Context, name string) error {
	if name == "" || h.dockerBinary == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, h.dockerBinary, "volume", "rm", "-f", name)
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "No such volume") {
		return fmt.Errorf("remove OpenBao HA volume %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeOpenBaoHARaftStorageConfig(dir string, nodeName string, retryJoinNodes []string) error {
	var retryJoin strings.Builder
	for _, retryJoinNode := range retryJoinNodes {
		_, _ = fmt.Fprintf(&retryJoin, `
  retry_join {
    leader_api_addr = "https://%s:8200"
    leader_ca_cert_file = "/bao/tls/ca.pem"
    leader_tls_servername = "%s"
  }
`, retryJoinNode, openBaoTLSServerName)
	}
	raw := fmt.Sprintf(`api_addr = "https://%s:8200"
cluster_addr = "https://%s:8201"

storage "raft" {
  path = "/bao/data"
  node_id = "%s"
%s}

listener "tcp" {
  address = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_cert_file = "/bao/tls/server.crt"
  tls_key_file = "/bao/tls/server.key"
}
`, nodeName, nodeName, nodeName, retryJoin.String())
	if err := os.WriteFile(filepath.Join(dir, nodeName+".hcl"), []byte(raw), 0o644); err != nil {
		return fmt.Errorf("write OpenBao HA raft storage config: %w", err)
	}
	return nil
}
