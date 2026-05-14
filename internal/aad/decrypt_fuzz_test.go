package aad_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

func FuzzPrepareDecrypt(f *testing.F) {
	fixture := loadGoldenFixture(f)
	active := fixture.Snapshot.keySnapshot()
	registry := newRegistry(f, active)

	validAnnotations, err := json.Marshal(fixture.ExpectedAnnotations)
	if err != nil {
		f.Fatalf("marshal valid annotations seed: %v", err)
	}
	unknown := active
	unknown.TransitVersion++
	unknown.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	unknown.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknown)
	if err != nil {
		f.Fatalf("derive unknown key ID seed: %v", err)
	}

	f.Add(fixture.ExpectedKeyID, string(validAnnotations))
	f.Add("not-a-key-id", "{}")
	f.Add(unknownKeyID, string(validAnnotations))
	f.Add(fixture.ExpectedKeyID, "{}")
	f.Add(fixture.ExpectedKeyID, "{")

	f.Fuzz(func(t *testing.T, keyID string, annotationsJSON string) {
		var annotations map[string]string
		if err := json.Unmarshal([]byte(annotationsJSON), &annotations); err != nil {
			return
		}

		prepared, err := aad.PrepareDecrypt(registry, keyID, annotations)
		if err != nil {
			return
		}
		if prepared.Snapshot.KubernetesKeyID != keyID {
			t.Fatalf("prepared decrypt returned wrong snapshot: %s != %s", prepared.Snapshot.KubernetesKeyID, keyID)
		}
		canonical, err := aad.BuildCanonical(prepared.Snapshot, annotations)
		if err != nil {
			t.Fatalf("build canonical from prepared decrypt input: %v", err)
		}
		if !bytes.Equal(prepared.Canonical, canonical) {
			t.Fatalf("canonical AAD mismatch:\nwant %s\ngot  %s", canonical, prepared.Canonical)
		}
		if prepared.TransitAssociatedData != aad.EncodeForTransit(canonical) {
			t.Fatalf("Transit AAD mismatch: %s", prepared.TransitAssociatedData)
		}
	})
}
