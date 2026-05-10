# Troubleshooting

This guide maps common symptoms to likely causes and recovery steps.

## API Server Cannot Connect To KMS

Symptoms:

- kube-apiserver logs mention KMS endpoint connection failure,
- `/run/openbao-kms/kms.sock` missing,
- plugin service or static pod not running.

Check:

```sh
systemctl status bao-kms-provider.service
ls -l /run/openbao-kms
bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Recovery:

1. Start or restart plugin.
2. Fix socket directory ownership and mode.
3. Confirm kube-apiserver endpoint path matches plugin config.
4. Restart kube-apiserver if needed.

## Socket Permission Denied

Symptoms:

- socket exists,
- kube-apiserver cannot connect,
- permission denied in API server or plugin logs.

Check:

```sh
ls -ld /run/openbao-kms
ls -l /run/openbao-kms/kms.sock
id kube-apiserver
```

Recovery:

1. Set runtime directory group to API server group.
2. Set socket mode to `0660`.
3. Restart plugin.
4. Restart kube-apiserver if it does not reconnect.

## OpenBao Unavailable Or Sealed

Symptoms:

- plugin `/ready` false,
- KMS Status unhealthy,
- OpenBao request errors,
- decrypt/encrypt timeouts.

Check:

```sh
bao status
bao-kms-provider doctor --config /etc/openbao-kms/config.yaml
```

Recovery:

1. Restore OpenBao reachability.
2. Unseal or repair OpenBao.
3. Verify TLS and DNS.
4. Run `verify-key`.
5. Restart plugin only if it does not recover.

## JWT Login Fails

Symptoms:

- OpenBao auth errors,
- plugin ready false,
- token refresh failures.

Check:

- JWT file exists and is readable by plugin,
- JWT `exp` is not near expiry,
- issuer/audience/subject match OpenBao role,
- OpenBao JWT auth config has current signing keys,
- clocks are synchronized.

Recovery:

1. Replace JWT with a valid token.
2. Fix OpenBao JWT role constraints if they are wrong.
3. Fix issuer/JWKS/OIDC discovery reachability.
4. Restart or signal plugin to re-login.

## Transit Key Missing

Symptoms:

- `verify-key` fails,
- OpenBao metadata read returns not found,
- decrypt/encrypt fail.

Recovery:

1. Confirm mount path and key name.
2. Confirm OpenBao namespace.
3. Confirm token policy.
4. If key was deleted, restore OpenBao backup containing the original key.

Do not recreate the key with the same name and expect old data to decrypt.

## Unknown key_id

Symptoms:

- decrypt rejected before Transit call,
- metric `openbao_kms_decrypt_key_id_errors_total` increases,
- old Kubernetes objects fail to read after config change.

Likely causes:

- provider name changed,
- cluster ID changed,
- OpenBao instance ID changed,
- Transit mount ID changed,
- key lineage ID changed,
- local key registry/history missing,
- object encrypted by another provider.

Recovery:

1. Restore original identity-bearing config.
2. Restore key registry state if used.
3. Verify active/historical key snapshots.
4. Restart plugin.
5. Retry Kubernetes read.

## AAD Mismatch

Symptoms:

- decrypt rejects object with AAD error,
- Transit decrypt may fail authentication if validation reached Transit.

Likely causes:

- annotations modified or corrupted,
- provider/cluster/key scope changed,
- compatibility mode missing for old object,
- bug in canonical AAD serialization.

Recovery:

1. Do not disable AAD globally as a first response.
2. Compare object annotations with expected key snapshot hashes.
3. Restore correct config.
4. Enable bounded compatibility mode only for known pre-AAD epochs if appropriate.
5. File a bug if canonical serialization changed.

## Status key_id Differs From Encrypt key_id

Symptoms:

- API server marks plugin unhealthy,
- encrypt responses are discarded,
- conformance test fails.

Likely causes:

- race in active key snapshot handling,
- rotation promotion bug,
- multiple plugin instances with inconsistent config,
- Transit metadata observed inconsistently.

Recovery:

1. Stop rotation.
2. Compare config on all control-plane nodes.
3. Compare plugin versions.
4. Restart affected plugin instance.
5. Roll back only if the older version supports current key ID/AAD formats.

## min_decryption_version Raised Too Early

Symptoms:

- old objects fail to decrypt,
- old key IDs fail after rotation,
- OpenBao decrypt returns version restriction errors.

Recovery:

1. Lower `min_decryption_version` if the old key version still exists.
2. Rerun storage migration.
3. Verify old backups are either expired or still decryptable.

If old key material no longer exists, restore OpenBao backup.

## Static Pod Image Missing

Symptoms:

- kubelet cannot start plugin static pod,
- image pull errors,
- socket missing.

Recovery:

1. Load the image on the node.
2. Use immutable digest already present locally.
3. Set image pull policy appropriately for air-gapped environments.
4. Restart kubelet if needed.

## Identity Fallback Issues

If `identity` fallback remains enabled too long:

- plaintext writes become easier after future misconfiguration,
- audits may miss resources that were never migrated.

If `identity` fallback is removed too early:

- old plaintext objects may become unreadable depending on provider set and migration state.

Recovery:

1. Restore the last known-good encryption config.
2. Restart or reload kube-apiserver.
3. Complete resource migration.
4. Remove fallback after verification.

## Do Not Do This During Incidents

- Do not delete encrypted etcd data.
- Do not recreate Transit keys with the same name.
- Do not change provider name to bypass errors.
- Do not raise `min_decryption_version`.
- Do not log plaintext or full ciphertext.
- Do not disable AAD globally without identifying affected epochs.

