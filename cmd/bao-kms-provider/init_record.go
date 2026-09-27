package main

import (
	"encoding/json"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
)

// The record contains selected non-secret installation inputs. Do not serialize
// the complete auth configuration or read credential files here.
type installationRecord struct {
	Generator           version.Info       `json:"generator"`
	Model               string             `json:"model"`
	Image               string             `json:"image,omitempty"`
	IdentityFingerprint string             `json:"identityFingerprint"`
	KeyLineageID        string             `json:"keyLineageId"`
	RuntimeUser         string             `json:"runtimeUser"`
	SocketGroup         string             `json:"socketGroup"`
	SocketPath          string             `json:"socketPath"`
	StatePath           string             `json:"statePath"`
	Files               []installationFile `json:"files"`
	RemainingActions    []string           `json:"remainingActions"`
}

type installationFile struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Purpose string `json:"purpose"`
}

func renderInstallationRecord(cfg config.Config, opts initOptions, files []initFile) ([]byte, error) {
	fingerprint, err := config.IdentityFingerprint(cfg)
	if err != nil {
		return nil, err
	}
	record := installationRecord{
		Generator: version.BuildInfo(), Model: opts.model, Image: opts.image,
		IdentityFingerprint: fingerprint, KeyLineageID: cfg.Transit.KeyIDScope.KeyLineageID,
		RuntimeUser: "openbao-kms", SocketGroup: cfg.Server.SocketGroup,
		SocketPath: cfg.Server.SocketPath, StatePath: cfg.State.Path,
		RemainingActions: []string{
			"Use a fresh disposable evaluation cluster. Stop if any API server already has an encryption configuration.",
			"Review all generated files. Generation has not installed or activated anything.",
			"Record config.yaml as the resolved values, including its lineage ID. Reuse it without --new-key for other nodes.",
			"Review openbao-setup.sh and have an OpenBao administrator run it once for the new Transit key.",
			"Provision independently renewable credentials and TLS trust on each node before starting the provider.",
			"Install the matching artifact on each node. Set file ownership for its runtime user and socket group.",
			"Run doctor with the runtime user and groups. " +
				"Run probe against each live socket and compare identity fingerprints and active key IDs across nodes.",
			"Install encryption-config-readers.yaml on every API server. " +
				"Verify readiness and reads through each endpoint directly.",
			"Only after all readers are ready, " +
				"install encryption-config.yaml one API server at a time and verify each endpoint.",
			"Verify probe ciphertext in etcd. Keep identity for existing plaintext; " +
				"this procedure does not migrate stored data.",
		},
	}
	if opts.model == initModelStaticPod {
		record.RuntimeUser = "65532:65532"
	}
	for _, file := range files {
		record.Files = append(record.Files, installationFile{
			Name: file.name, Mode: fmt.Sprintf("%04o", file.mode), Purpose: file.purpose,
		})
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render installation record: %w", err)
	}
	return append(content, '\n'), nil
}
