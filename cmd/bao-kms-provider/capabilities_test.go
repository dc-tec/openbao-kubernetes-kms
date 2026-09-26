package main

import (
	"context"
	"slices"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

type recordingCapabilityClient struct {
	fakeDiagnosticTransitClient
	requested []string
}

func (c *recordingCapabilityClient) Capabilities(
	_ context.Context, paths []string,
) (openbao.CapabilitiesResult, error) {
	c.requested = slices.Clone(paths)
	return c.capabilities, nil
}

func TestCapabilitiesRejectManagementPermissions(t *testing.T) {
	cfg := loadCommandConfig(t)
	for _, endpoint := range []string{
		"keys/k8s-workload-a-etcd/config", "keys/k8s-workload-a-etcd/trim", "config/keys",
		"keys/k8s-workload-a-etcd/rotate", "restore/k8s-workload-a-etcd", "restore", "rewrap/k8s-workload-a-etcd",
	} {
		for _, capability := range []string{capabilityUpdate, capabilityCreate, capabilityDelete, capabilitySudo, "root"} {
			t.Run(endpoint+"/"+capability, func(t *testing.T) {
				dangerousPath := cfg.Transit.MountPath + "/" + endpoint
				caps := hotPathCapabilities(cfg)
				caps.ByPath[dangerousPath] = []string{capability}
				client := &recordingCapabilityClient{fakeDiagnosticTransitClient: fakeDiagnosticTransitClient{capabilities: caps}}
				if err := checkCapabilities(t.Context(), cfg, client); err == nil {
					t.Fatal("management permission accepted")
				}
				if !slices.Contains(client.requested, dangerousPath) {
					t.Fatal("management path not queried")
				}
			})
		}
	}
}

func TestCapabilitiesRejectIncompleteResponse(t *testing.T) {
	cfg := loadCommandConfig(t)
	caps := hotPathCapabilities(cfg)
	delete(caps.ByPath, transitCapabilityPaths(cfg).restore)
	client := fakeDiagnosticTransitClient{capabilities: caps}
	if err := checkCapabilities(t.Context(), cfg, client); err == nil {
		t.Fatal("missing capability results accepted as deny")
	}
}

func TestCapabilitiesRejectEncryptKeyCreation(t *testing.T) {
	cfg := loadCommandConfig(t)
	caps := hotPathCapabilities(cfg)
	caps.ByPath[transitCapabilityPaths(cfg).encrypt] = []string{capabilityUpdate, capabilityCreate}
	if err := checkCapabilities(t.Context(), cfg, fakeDiagnosticTransitClient{capabilities: caps}); err == nil {
		t.Fatal("encrypt key creation permission accepted")
	}
}
