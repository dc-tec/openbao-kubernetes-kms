package aad_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

const contractNamespace = "admin/workload-a"

func TestAADHashMutationCatalogFailsPreflight(t *testing.T) {
	fixture := loadGoldenFixture(t)
	baseSnapshot := fixture.Snapshot.keySnapshot()

	tests := []struct {
		name     string
		snapshot func() keySnapshotWithAnnotations
		key      string
	}{
		{
			name: "key_id_hash",
			snapshot: func() keySnapshotWithAnnotations {
				return buildContractSnapshot(t, baseSnapshot, fixture.PluginVersion)
			},
			key: aad.KeyKeyIDHash,
		},
		{
			name: "transit_mount_hash",
			snapshot: func() keySnapshotWithAnnotations {
				return buildContractSnapshot(t, baseSnapshot, fixture.PluginVersion)
			},
			key: aad.KeyTransitMountHash,
		},
		{
			name: "transit_key_hash",
			snapshot: func() keySnapshotWithAnnotations {
				return buildContractSnapshot(t, baseSnapshot, fixture.PluginVersion)
			},
			key: aad.KeyTransitKeyHash,
		},
		{
			name: "openbao_namespace_hash",
			snapshot: func() keySnapshotWithAnnotations {
				namespaced := baseSnapshot
				namespaced.OpenBaoNamespace = contractNamespace
				return buildContractSnapshot(t, namespaced, fixture.PluginVersion)
			},
			key: aad.KeyOpenBaoNamespaceHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := tt.snapshot()
			mutated := copyAnnotations(contract.annotations)
			mutated[tt.key] = aad.HashValue("different-" + tt.name)

			_, err := aad.PrepareDecrypt(contract.registry, contract.snapshot.KubernetesKeyID, mutated)
			if !errors.Is(err, aad.ErrAnnotationMismatch) {
				t.Fatalf("expected annotation mismatch for %s mutation before Transit decrypt, got %v", tt.key, err)
			}
		})
	}
}

func TestAADNamespaceHashRequiredOnlyWhenConfigured(t *testing.T) {
	fixture := loadGoldenFixture(t)
	withoutNamespace := fixture.Snapshot.keySnapshot()
	withoutNamespace.OpenBaoNamespace = ""
	withoutNamespaceContract := buildContractSnapshot(t, withoutNamespace, fixture.PluginVersion)

	if _, ok := withoutNamespaceContract.annotations[aad.KeyOpenBaoNamespaceHash]; ok {
		t.Fatal("unexpected namespace hash annotation when OpenBao namespace is not configured")
	}
	if _, err := aad.PrepareDecrypt(
		withoutNamespaceContract.registry,
		withoutNamespaceContract.snapshot.KubernetesKeyID,
		withoutNamespaceContract.annotations,
	); err != nil {
		t.Fatalf("prepare decrypt without namespace hash for non-namespaced snapshot: %v", err)
	}

	unexpectedNamespace := copyAnnotations(withoutNamespaceContract.annotations)
	unexpectedNamespace[aad.KeyOpenBaoNamespaceHash] = aad.HashValue(contractNamespace)
	if _, err := aad.PrepareDecrypt(
		withoutNamespaceContract.registry,
		withoutNamespaceContract.snapshot.KubernetesKeyID,
		unexpectedNamespace,
	); !errors.Is(err, aad.ErrAnnotationMismatch) {
		t.Fatalf("expected unexpected namespace hash to fail closed, got %v", err)
	}

	withNamespace := fixture.Snapshot.keySnapshot()
	withNamespace.OpenBaoNamespace = contractNamespace
	withNamespaceContract := buildContractSnapshot(t, withNamespace, fixture.PluginVersion)
	delete(withNamespaceContract.annotations, aad.KeyOpenBaoNamespaceHash)
	if _, err := aad.PrepareDecrypt(
		withNamespaceContract.registry,
		withNamespaceContract.snapshot.KubernetesKeyID,
		withNamespaceContract.annotations,
	); !errors.Is(err, aad.ErrAnnotationMismatch) {
		t.Fatalf("expected missing namespace hash for namespaced snapshot to fail closed, got %v", err)
	}
}

func TestAADContractDoesNotExposeRawTopology(t *testing.T) {
	fixture := loadGoldenFixture(t)
	snapshot := fixture.Snapshot.keySnapshot()
	snapshot.OpenBaoNamespace = contractNamespace
	contract := buildContractSnapshot(t, snapshot, fixture.PluginVersion)

	canonical, err := aad.BuildCanonical(contract.snapshot, contract.annotations)
	if err != nil {
		t.Fatalf("build canonical AAD: %v", err)
	}
	annotationValues := strings.Join(values(contract.annotations), "\n")
	for _, raw := range []string{
		snapshot.OpenBaoInstanceID,
		snapshot.OpenBaoNamespace,
		snapshot.TransitMountID,
		snapshot.TransitKeyLineageID,
	} {
		if strings.Contains(annotationValues, raw) {
			t.Fatalf("annotations exposed raw topology value %q", raw)
		}
		if strings.Contains(string(canonical), raw) {
			t.Fatalf("canonical AAD exposed raw topology value %q", raw)
		}
	}
}

type keySnapshotWithAnnotations struct {
	snapshot    keyregistry.KeySnapshot
	annotations map[string]string
	registry    keyregistry.Registry
}

func buildContractSnapshot(
	t testing.TB,
	snapshot keyregistry.KeySnapshot,
	pluginVersion string,
) keySnapshotWithAnnotations {
	t.Helper()

	normalized, err := snapshot.Normalize()
	if err != nil {
		t.Fatalf("normalize snapshot: %v", err)
	}
	annotations, err := aad.BuildAnnotations(normalized, pluginVersion)
	if err != nil {
		t.Fatalf("build annotations: %v", err)
	}
	return keySnapshotWithAnnotations{
		snapshot:    normalized,
		annotations: annotations,
		registry:    newRegistry(t, normalized),
	}
}
