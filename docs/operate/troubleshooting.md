---
title: Troubleshooting
description: "Find the failing layer first, then apply the recovery for that symptom: socket, OpenBao, auth, Transit key, key_id and AAD validation, fallback, or static pod."
eyebrow: Operate · Diagnosis
weight: 40
verifiedBy:
  - cmd/bao-kms-provider/diagnostics.go
  - internal/health/handler.go
  - internal/metrics/collectors.go
---

Find the failing layer before you change configuration or recovery state.
Start with the checks that change nothing:

```sh
curl -fsS http://127.0.0.1:8082/live
curl -sS -i http://127.0.0.1:8082/ready
curl -fsS http://127.0.0.1:8081/metrics | grep -E 'openbao_kms_status_key_id_hash|openbao_kms_status_cache_age_seconds'
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/openbao-kms/encryption-config.yaml
```

On a healthy node both endpoints return HTTP 200, the metrics show the active
`key_id` hash and cache age, and `doctor` reports no `[fail]` check. The rules
in [Disaster recovery: During an incident](/docs/operate/disaster-recovery/#during-an-incident)
apply to every fix below. For the full catalog of failure modes, see
[Architecture: Failure modes](/docs/architecture/failure-modes/).

When `/ready` returns HTTP 503, read `reasons` and the optional
`metadata_error_class` and `deep_error_class` fields. These report cached
conditions; the endpoint does not run a new probe. Use the matching
`status.probe` warning for the probe kind and failure class. See the
[reason codes](/docs/reference/observability/#endpoints).

At the default log level, `serve.start`, `serve.ready`, and `serve.shutdown`
show startup and exit. A `serve.shutdown` with `startup_failed` means setup
failed before readiness. A `key.promoted` event identifies the previous and new
key hashes and Transit versions; wait for the new key's deep probe to pass.

## API server cannot connect to KMS

**Signs:** the API server log reports a KMS connection failure, or
`/run/openbao-kms/kms.sock` is missing.

**Check** the service (`systemctl status bao-kms-provider.service`) or the
static pod (`crictl ps --name bao-kms-provider`, `journalctl -u kubelet`), then
`ls -l /run/openbao-kms`.

**Fix:**

1. Start or restart the provider.
2. Correct the socket directory owner and mode; see
   [Security: Linux identity model](/docs/security/linux-identity-model/).
3. Confirm the `EncryptionConfiguration` endpoint matches `server.socketPath`.
4. Restart `kube-apiserver` if it does not reconnect.

## Socket permission denied

**Signs:** the socket exists, but the API server or provider log reports
permission denied.

**Check:** `ls -ld /run/openbao-kms`, `ls -l /run/openbao-kms/kms.sock`, and
`getent group openbao-kms-socket`.

**Fix:** the API server identity must be in `openbao-kms-socket`, the directory
must be group `openbao-kms-socket` with mode `2750`, and the socket mode
`0660`. For static pods, the socket GID must equal both `supplementalGroups`
and `server.socketGroup`. Restart the provider, then the API server if needed.

## OpenBao unavailable or sealed

**Signs:** `/ready` fails, KMS Status is unhealthy, and OpenBao request errors
or timeouts appear in metrics.

**Check:** `bao status`, `/ready`, and `doctor`. Use the KMS log `error_class`
to locate transport failures: `openbao_tls_failed` points to certificate or TLS
validation, `openbao_dns_failed` to name resolution, and
`openbao_connection_failed` to the listener or network connection.

**Fix:** restore OpenBao reachability, unseal or repair it, and check TLS and
DNS. Run `verify-key`. The provider recovers on its own once OpenBao is
healthy; restart it only if it does not.

## Transit profile fails closed

**Signs:** `/ready` fails and `doctor` or `verify-key` reports a
`transit.profile` failure. Writes fail while cached reads might still work.

**Fix:** read each finding's impact prefix. For `cryptographic_safety`, restore
the validated key profile before routing writes through the provider. For
`api_server_availability`, repair the setting that blocks encryption or
decryption, such as key deletion or version restrictions. Re-run `verify-key`,
and wait for `/ready` before restarting the API server.

## Auth login fails

**Signs:** OpenBao auth errors in the provider log, `/ready` fails, or token
refresh fails.

**Check:**

- JWT: the file is readable by the provider, `exp` is not close, `iss`, `aud`,
  and `sub` match the OpenBao role, and OpenBao has the issuer's current
  signing keys.
- Certificate: the OpenBao listener requests client certificates, and the
  certificate is valid, has client-auth usage, and matches the role.
- PKCS#11: the module path, token label, key label, and PIN file are correct.
  If signer probes fail after a delay, check HSM latency and session usage.
  Pool waits use `auth.loginTimeout`; native HSM calls require finite timeouts
  in the vendor client. See [Configuration](/docs/reference/configuration/#auth).
- Host, OpenBao, issuer, and CA clocks agree.

**Fix:** replace the auth material or correct the role, the issuer
reachability, or the CA, then confirm the next request or background probe
logs in. The provider re-reads auth material before each login and backs off
after failures. A pool timeout recovers when a session becomes available. A
native HSM call that never returns can require a provider restart after HSM
connectivity recovers.

After a token revocation or an OpenBao restore, the provider logs in again on a
`401` or `403` and retries the rejected request once, without waiting for the
token TTL. Recovery starts at most once every five seconds, with exponential
backoff for failed logins. OpenBao returns `403` for both invalid tokens and
policy denials, so a `403` that persists after a successful login points at the
role's policies or the Transit capabilities. Local credential failures and
rejected logins surface as `auth_failed`; see
[Observability: Error classes](/docs/reference/observability/#error-classes).

## Transit key missing

**Signs:** `verify-key` fails with not found, and encryption and decryption
fail.

**Fix:** confirm the Transit mount path, key name, and OpenBao namespace match
the configuration, and that the policy grants read on the key path. If the key
was deleted, restore it from an OpenBao backup; see
[Disaster recovery: Transit key loss](/docs/operate/disaster-recovery/#transit-key-loss).
A recreated key with the same name never decrypts old data.

## Registry state save failed

When `/ready` reports `state_save_failed` or `persistence_degraded: true`, check
free space, filesystem errors, mount writability, and the state directory's
owner and permissions. Repair the storage problem while preserving the registry
file and its checkpoint. Do not
delete either file to bypass the error.

The provider retries the exact attempted transition and validates it against
fresh Transit metadata before restoring readiness. A newer state file with an
older checkpoint can result from a partial save. Conflicting files remain a
failure and require investigation; do not replace them with older copies while
the provider is running. See [Disaster recovery](/docs/operate/disaster-recovery/)
for recovery with matching state, checkpoint, and Transit evidence.

An observation-only save that leaves both files unchanged can keep Encrypt and
Decrypt available with the published keys. In this case, `/ready` can return
HTTP 200 with `persistence_degraded: true`. Rotation progress stays unpublished
until storage recovers. Other save failures stop Encrypt; Decrypt can still use
keys in the last published registry.

Before resuming a rotation, confirm `/ready` returns HTTP 200 without
`persistence_degraded`, check that `openbao_kms_rotation_persistence_degraded`
is `0`, and validate reads and writes.

## Unknown key ID

**Signs:** decryption is rejected before any Transit call,
`openbao_kms_decrypt_key_id_errors_total` increases, and old objects fail to
read after a configuration change.

**Causes:** a changed identity-bearing value, a missing, corrupted, or
rolled-back registry state or checkpoint, or data from a different provider.

**Fix:** restore the original identity-bearing values and the registry state
file and checkpoint, confirm the active and historical key snapshots are
present, then restart the provider and retry the read. After a rotation, state
must come from backup or a healthy peer; see
[Disaster recovery: Local registry state](/docs/operate/disaster-recovery/#local-registry-state).

## Transit allows implicit key creation

**Signs:** KMS Status is unhealthy without a `key_id`, and probes report
`disable_upsert` as false or unreadable.

**Fix:** set `disable_upsert=true` on the Transit mount, confirm the provider
token can read `<mount>/config/keys`, run `doctor`, and wait for the next
metadata probe to report healthy.

## Transit version creation time changed

**Signs:** `/ready`, `doctor`, or `verify-key` reports a changed creation time
and KMS Status has no `key_id`, typically after an OpenBao restore or import.

The provider records each version's first observed creation time, to the
second, and treats a different second as identity drift.

**Fix:** restore an OpenBao backup with the original key and metadata, and the
matching provider state and checkpoint. Confirm the lineage ID still matches,
then run `verify-key`. Keep the provider stopped if the original metadata
cannot be restored. Never edit timestamps, `key_id` values, or state by hand.

## Intermediate Transit version metadata missing

**Signs:** `rotation-plan`, `verify-rotation`, `/ready`, or startup reports
invalid metadata after `latest_version` skipped versions, and Status has no
`key_id`. Causes are back-to-back rotations before convergence, or a restore
that dropped a version's creation time.

**Fix:**

1. Stop rotations and keep every Transit version decryptable.
2. Restore OpenBao metadata that includes each intermediate version's creation
   time, and restore provider state from backup or a healthy peer if it was
   lost.
3. Run `rotation-plan` on every node, and resume only after all nodes report
   the same healthy `key_id` hash.

## AAD mismatch

**Signs:** decryption rejects an object with an additional authenticated data
(AAD) error, or Transit returns an authentication failure.

**Causes:** modified or corrupted annotations, a changed provider, cluster, or
key scope, or a serialization bug.

**Fix:** compare the object's annotations with the expected key snapshot
hashes and restore the correct configuration. Never disable or bypass AAD. File
a bug if canonical serialization changed.

## Status key ID differs from encrypt key ID

**Signs:** the API server marks the provider unhealthy and discards encrypt
responses.

**Causes:** nodes with inconsistent configuration or provider versions, an
inconsistent Transit metadata read, or a promotion bug.

**Fix:** stop any rotation, compare configuration and provider versions on
every node, and restart the affected provider. Roll back only as allowed in
[Upgrade: Roll back](/docs/operate/upgrade/#roll-back).

## min_decryption_version raised too early

**Signs:** old objects fail to decrypt after a rotation, and OpenBao returns
version restriction errors.

**Fix:** lower `min_decryption_version` if the old version still exists and
policy allows it; otherwise restore an OpenBao backup that holds it. Rewrite
the affected resources once reads through the provider work again, and confirm
retained backups are expired or still decryptable.

## Static pod image missing

**Signs:** kubelet cannot start the provider, reports image pull errors, and
the socket is missing.

**Fix:** import the verified image digest on the node, and restart kubelet if
needed. See [Run as a static pod: Preload the image](/docs/get-started/static-pod/#step-3-preload-the-image).

## Identity fallback issues

An `identity` fallback left in place makes plaintext writes possible after a
future misconfiguration. Removed too early, it leaves unmigrated plaintext
objects unreadable.

**Fix:** restore the last known-good `EncryptionConfiguration` and restart or
reload the API server. Rewrite the remaining resources as in
[Enable encryption](/docs/get-started/enable-encryption/#step-5-rewrite-existing-secrets),
verify, then remove the fallback.
