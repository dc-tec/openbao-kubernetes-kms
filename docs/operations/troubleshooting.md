---
title: "Troubleshooting"
description: "Symptom-driven recovery for common bao-kms-provider failures: socket connectivity, OpenBao auth, Transit key issues, key_id and AAD validation, identity fallback, static pod problems."
weight: 40
---

# Troubleshooting

This page maps common symptoms to likely causes and recovery steps. For the comprehensive failure-mode catalog with detection signals, mitigations, and impact analysis, see [Architecture: Failure Modes](/architecture/failure-modes/).

Start with the least destructive checks:

```sh
curl -sf http://127.0.0.1:8082/live
curl -sf http://127.0.0.1:8082/ready
curl -sf http://127.0.0.1:8081/metrics | grep -E 'openbao_kms_status_key_id_hash|openbao_kms_status_cache_age_seconds'
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/encryption-config.yaml
```

Do not change identity-bearing fields, recreate Transit keys, or change Kubernetes encryption configuration until the failing layer is known.

## API Server Cannot Connect To KMS

Symptoms:

- `kube-apiserver` logs mention KMS endpoint connection failure,
- `/run/openbao-kms/kms.sock` is missing,
- the plugin service or static pod is not running.

Check:

```sh
systemctl status bao-kms-provider.service
journalctl -u kubelet --since -10m
crictl ps --name bao-kms-provider
ls -l /run/openbao-kms
bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Use the systemd command for host-service deployments. Use kubelet and container-runtime tooling for static-pod deployments.

Recovery:

1. Start or restart the plugin.
2. Fix socket directory ownership and mode (see [Deployment: Linux Identity Model](/deployment/linux-identity-model/)).
3. Confirm the API server endpoint path matches `server.socketPath` in the provider configuration.
4. Restart `kube-apiserver` if it does not reconnect.

## Socket Permission Denied

Symptoms:

- the socket exists,
- `kube-apiserver` cannot connect,
- permission denied errors appear in API server or plugin logs.

Check:

```sh
ls -ld /run/openbao-kms
ls -l /run/openbao-kms/kms.sock
getent group openbao-kms-socket
```

Recovery:

1. Ensure the API server runtime identity is a member of `openbao-kms-socket`.
2. Set the runtime directory group to `openbao-kms-socket` and mode `2750`.
3. In static-pod mode, ensure the numeric socket group GID matches `spec.securityContext.supplementalGroups` and `server.socketGroup`.
4. Set the socket mode to `0660`.
5. Restart the plugin.
6. Restart `kube-apiserver` if it does not reconnect.

## OpenBao Unavailable Or Sealed

Symptoms:

- plugin `/ready` returns non-200,
- KMS Status is unhealthy,
- OpenBao request errors appear in metrics,
- decrypt or encrypt operations time out.

Check:

```sh
bao status
curl -sf http://127.0.0.1:8082/ready
bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Recovery:

1. Restore OpenBao reachability.
2. Unseal or repair OpenBao.
3. Verify TLS and DNS.
4. Run `bao-kms-provider verify-key --config /etc/openbao-kms/config.yaml`.
5. Restart the plugin only if it does not recover on its own after OpenBao is healthy.

## Auth Login Fails

Symptoms:

- OpenBao auth errors appear in plugin logs,
- plugin `/ready` returns non-200,
- token refresh failures.

Check:

- for JWT auth, the JWT file exists and is readable by the plugin process,
- for JWT auth, the JWT `exp` claim is not near expiry,
- for JWT auth, `iss`, `aud`, and `sub` claims match the OpenBao role configuration,
- for JWT auth, OpenBao has the current signing keys through JWKS, OIDC discovery, or pinned public keys,
- for certificate auth, the OpenBao listener requests client certificates,
- for certificate auth, the certificate is not expired, has client-auth usage, and matches the configured role constraints,
- for SPIFFE auth, the Workload API socket is reachable and returns the configured SPIFFE ID,
- for PKCS#11 auth, the module path, token label, key label, and PIN file are correct,
- host, OpenBao, issuer, CA, and SPIFFE clocks are synchronized.

Recovery:

1. Replace or restore the configured auth material.
2. Fix OpenBao auth role constraints if they are wrong.
3. Fix issuer, JWKS, OIDC discovery, certificate authority, PKCS#11, or SPIFFE Workload API reachability.
4. Restart the plugin if the current in-memory token does not recover. The provider re-reads auth material before re-login.

## Transit Key Missing

Symptoms:

- `verify-key` fails,
- OpenBao metadata read returns not found,
- decrypt or encrypt operations fail.

Recovery:

1. Confirm the Transit mount path and key name match the provider configuration.
2. Confirm the OpenBao namespace if applicable.
3. Confirm the token policy grants metadata read on the configured key path.
4. If the key was deleted, restore the OpenBao backup containing the original key. See [Disaster Recovery: Transit Key Loss](/operations/disaster-recovery/#transit-key-loss).

Do not recreate the key with the same name and expect old data to decrypt. Recreated keys produce a new lineage; old ciphertext is bound to the previous lineage.

## Unknown Key ID

Symptoms:

- decrypt is rejected before the Transit call,
- the metric `openbao_kms_decrypt_key_id_errors_total` increases,
- old Kubernetes objects fail to read after a configuration change.

Likely causes:

- provider name changed,
- cluster ID changed,
- OpenBao instance ID changed,
- Transit mount ID changed,
- key lineage ID changed,
- the local key registry state or checkpoint is missing, corrupted, or rolled back,
- the object was encrypted by a different provider.

Recovery:

1. Restore the original identity-bearing configuration; see [Configuration: Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).
2. Restore the key registry state file and checkpoint if they were lost.
3. Verify active and historical key snapshots are present.
4. Restart the plugin.
5. Retry the Kubernetes read.

## AAD Mismatch

Symptoms:

- decrypt rejects the object with an AAD error,
- the Transit decrypt call returns an authentication failure if validation reaches Transit.

Likely causes:

- annotations were modified or corrupted,
- provider, cluster, or key scope changed,
- a bug in canonical AAD serialization.

Recovery:

1. Do not disable AAD globally.
2. Compare object annotations with the expected key snapshot hashes.
3. Restore the correct configuration.
4. Do not modify code or local state to bypass AAD; that is unsafe as an incident response.
5. File a bug if canonical serialization changed unexpectedly.

## Status Key ID Differs From Encrypt Key ID

Symptoms:

- the API server marks the plugin unhealthy,
- encrypt responses are discarded by the API server,
- KMS v2 conformance tests fail.

Likely causes:

- a race in active key snapshot handling,
- a rotation promotion bug,
- multiple plugin instances running with inconsistent configuration,
- Transit metadata observed inconsistently between probes.

Recovery:

1. Stop any in-progress rotation; see [Operations: Rotation](/operations/rotation/).
2. Compare configuration on every control-plane node.
3. Compare plugin versions across nodes.
4. Restart the affected plugin instance.
5. Roll back the plugin only if the older version supports the current `key_id` and AAD formats.

## min_decryption_version Raised Too Early

Symptoms:

- old objects fail to decrypt,
- old `key_id` references fail after rotation,
- OpenBao decrypt returns version restriction errors.

Recovery:

1. Lower `min_decryption_version` if the old key version still exists.
2. Rerun storage migration; see [Operations: Rotation](/operations/rotation/#migrate-kubernetes-data).
3. Verify old backups are either expired or still decryptable.

If old key material no longer exists, restore the OpenBao backup. See [Disaster Recovery: Transit Key Loss](/operations/disaster-recovery/#transit-key-loss).

## Static Pod Image Missing

Symptoms:

- kubelet cannot start the plugin static pod,
- image pull errors appear in kubelet logs,
- the socket is missing.

Recovery:

1. Load the image on the node.
2. Use the immutable digest already present locally.
3. Set image pull policy appropriately for air-gapped environments.
4. Restart kubelet if needed.

See [Deployment: Static Pod Deployment](/deployment/static-pod/) for the image preload and digest-pinning rules.

## Identity Fallback Issues

If `identity` fallback remains enabled too long:

- plaintext writes become more likely after future misconfiguration,
- audits may miss resources that were never migrated.

If `identity` fallback is removed too early:

- old plaintext objects may become unreadable depending on the provider set and migration state.

Recovery:

1. Restore the last known-good `EncryptionConfiguration`.
2. Restart or reload `kube-apiserver`.
3. Complete resource migration; see [Kubernetes Encryption Config: Migrate Existing Resources](/getting-started/kubernetes-encryption-config/#migrate-existing-resources).
4. Remove the fallback after migration verification.

## Do Not Do This During Incidents

- Do not delete encrypted etcd data.
- Do not recreate Transit keys with the same name.
- Do not change the provider name to clear errors.
- Do not raise `min_decryption_version`.
- Do not log plaintext or full ciphertext.
- Do not disable AAD globally.
