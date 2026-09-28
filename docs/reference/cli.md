---
title: CLI
description: "Every bao-kms-provider command, what doctor and verify-key check, the rotation reports, file generation with init, common flags, and exit codes."
eyebrow: Reference
weight: 10
verifiedBy:
  - cmd/bao-kms-provider
  - internal/cli/exit.go
---

Commands never print plaintext, JSON Web Tokens (JWTs), OpenBao tokens, or full
ciphertext. Report-style commands (`doctor`, `verify-key`, `rotation-plan`,
`verify-rotation`, `retire-versions`) take `--output text|json`; `text` is the
default and the JSON shape is stable.

## serve

```sh
bao-kms-provider serve --config /etc/openbao-kms/config.yaml
```

Validates the configuration, logs in to OpenBao, loads the active key snapshot,
creates the Unix socket, serves KMS v2 gRPC, and runs the background probes.
Metrics are served on `server.metricsAddress` and health endpoints on
`server.healthAddress`.

## doctor

Run before promoting a binary or changing the API server
`EncryptionConfiguration`.

```sh
bao-kms-provider doctor \
  --config /etc/openbao-kms/config.yaml \
  --encryption-config /etc/kubernetes/openbao-kms/encryption-config.yaml
```

| Check ID | What it checks |
|---|---|
| `config.load` | Typed configuration loads. |
| `config.validate` | Configuration and local filesystem checks pass, including socket parent permissions and target type. |
| `socket.group` | The configured socket group resolves locally. |
| `jwt.local` | For JWT files: file safety, claims, and remaining lifetime. Signature verification occurs in OpenBao. |
| `oauth2.local` | For OAuth: the client-secret file is readable and safely permissioned. |
| `oauth2.acquire` | For OAuth: token acquisition; a pass also means OpenBao accepted the token. |
| `auth.cert.pkcs11` | For a certificate build: the configured PKCS#11 source is reachable. |
| `auth.cert.spiffe` | For a SPIFFE test build: the configured source is reachable. This does not establish release support. |
| `auth.cert.local` | The certificate identity is locally valid for authentication. |
| `auth.cert.signer` | The signer matches the certificate and signs a probe. |
| `kubernetes.encryption_config` | With `--encryption-config`: the configured provider uses KMS v2 and the matching socket. Warns if an `identity` fallback remains. |
| `openbao.tls` | The CA bundle and TLS client configuration can be loaded. The subsequent login verifies the remote endpoint. |
| `openbao.auth` | Login with the configured method and role succeeds. |
| `transit.capabilities` | Required permissions exist, and the checked paths grant no prohibited management capabilities. |
| `transit.metadata` | The configured Transit key metadata is readable. |
| `transit.profile` | Key type, derivation, convergence, export, backup, and deletion settings match the supported profile. |
| `transit.disable_upsert` | The mount has `disable_upsert=true`. |
| `transit.probe` | A non-secret Transit encrypt/decrypt probe round-trips. |
| `key_id.deterministic` | Repeated derivation from the same metadata gives the same key ID. |
| `kms.status_encrypt` | A local diagnostic KMS server returns matching Status and Encrypt key IDs. It does not connect to the running provider's socket. |

`doctor` exits non-zero when any check fails. Auth-specific checks appear only
for the configured method and available build. A failed prerequisite stops
later checks or marks dependent checks as skipped; absent or skipped checks
are not passes. Run `doctor` under the intended provider identity to inspect
its access to local files. `serve` also checks socket-directory ownership
before binding.

The `transit.capabilities` check queries the token's effective capabilities on
the configured key's configuration, trim, rotate, export, backup, rewrap, and
restore paths, and on the mount configuration. It rejects incomplete answers.
It does not audit the whole policy or permissions on other keys; see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/#policy).

The `kubernetes.encryption_config` check accepts migration files that also list
`aescbc`, `aesgcm`, `secretbox`, `identity`, or other KMS providers, and finds
the configured provider wherever it appears. Passing does not prove the
provider encrypts new writes or that migration is complete; see
[EncryptionConfiguration: Migration files](/docs/reference/encryption-config/#migration-files).
Transit profile findings carry the `cryptographic_safety` or
`api_server_availability` impact class described in
[Compatibility](/docs/reference/compatibility/#required-openbao-features).

## probe

Connect to the running provider's Unix socket and perform a KMS v2 Status,
Encrypt, and Decrypt round trip with fresh random probe bytes:

```sh
bao-kms-provider probe --socket /run/openbao-kms/kms.sock --output json
```

This command does not load provider configuration, credentials, or persisted
state. It does not log in directly to OpenBao. Unix permissions govern access;
run it with the intended socket client's UID and groups. A root invocation
reports a warning because it cannot prove non-root access.

| Flag | Purpose |
|---|---|
| `--socket` | Absolute Unix socket path. Default: `/run/openbao-kms/kms.sock`. The final path component must be a socket, not a symlink. |
| `--timeout` | Deadline for the whole probe. Default: `10s`. |
| `--expected-key-id` | Require the active key ID reported by a verified peer after rotation converges. |
| `--output text\|json` | Report format. Default: `text`. |

The report separates caller identity, socket path, healthy Status, Encrypt,
Decrypt, and active key agreement. The `kms.key_id` check contains the validated
non-secret key ID. A failed check exits non-zero and identifies a corrective
action. Raw server errors, plaintext, ciphertext, and annotations are omitted.
If a rotation changes the key between Status and Encrypt, wait for convergence
and retry. The command writes no files and retains no sample ciphertext.

A successful probe establishes a live provider round trip under the caller's
identity. It does not establish the provider's credential file access,
Kubernetes integration, old-data decryptability, or migration completion.
Use `doctor` under the provider identity for local/authentication checks, then
perform the per-API-server activation and cold-read checks.

## verify-key

```sh
bao-kms-provider verify-key --config /etc/openbao-kms/config.yaml
```

Checks the Transit key alone: it exists with an allowed type, derived and
convergent settings, deletion, export, and plaintext backup match the profile,
the latest version is usable, and neither `min_encryption_version` nor
`min_decryption_version` blocks a version the provider needs.

The report shares configuration, auth, capabilities, metadata, profile, upsert,
and deterministic-key checks with `doctor`. It also reports `registry.state`
for local state and checkpoint validation and `transit.version_restrictions`
for retained versions. Missing state or an absent or lagging checkpoint emits
a warning; without state, version checks cover the latest Transit version.
`verify-key` omits the encrypt/decrypt and Status/Encrypt probes.

## rotation-plan

```sh
bao-kms-provider rotation-plan --config /etc/openbao-kms/config.yaml
```

Reports rotation state without changing it: the live metadata check
(`transitMetadataStatus`, with a redacted `transitMetadataError` on failure),
whether local registry state loaded, its generation and state hash, the checkpoint status, the active and
latest observed Transit versions, the active `key_id` hash, and any pending
promotion with its expected time.

| Checkpoint status | Meaning |
|---|---|
| `current` | The checkpoint matches the loaded state. |
| `behind` | The checkpoint accepts a newer state generation. |
| `missing` | State exists but the checkpoint does not. |
| `absent` | Neither exists. |

Without local state, the command reports initial state only for an unrotated
key, and otherwise fails with the reason bootstrap is denied, such as an
advanced `latest_version`, `min_available_version`, or `min_decryption_version`.
A checkpoint without a matching state file makes it fail closed.

If OpenBao authentication or the metadata read fails, the command exits with
`4` and reports `transitMetadataStatus: fail` next to whatever local state it
could read. That partial report does not describe the current OpenBao state.

## verify-rotation

```sh
bao-kms-provider verify-rotation --config /etc/openbao-kms/config.yaml
```

Reports the same view as `rotation-plan`, with `confidence: limited` and a
`limitations` field. It does not scan Kubernetes resources, etcd, or backups,
and it never recommends raising `min_decryption_version`. Like
`rotation-plan`, it exits with `4` when the metadata check fails.

## retire-versions

Plans or applies the removal of old versions from local decryption. Follow the
procedure in [Operate: Rotation](/docs/operate/rotation/#retire-old-versions).

```sh
bao-kms-provider retire-versions \
  --config /etc/openbao-kms/config.yaml \
  --before-version 2 --output json
```

| Flag | Meaning |
|---|---|
| `--before-version N` | Remove versions below `N`. `N` is greater than `1` and at most the local active version. |
| `--apply` | Save the transition. Without it, the command only reports a plan. |
| `--expected-state-hash HASH` | Required with `--apply`; the `stateHash` from the reviewed plan. |

The report lists `applied`, `beforeVersion`, `stateHash`, `nextStateHash`,
`nextGeneration`, `activeKeyIdHash`, `removedVersions`, and `limitations`. The
command needs existing local state and valid live metadata, and rejects a
pending rotation or an active version that differs from Transit
`latest_version`. `--apply` also needs the provider stopped and the state
directory owned by the invoking user. It exits with `4` when a check or the
save fails. Removed identities stay in the state as hashed `removed` records;
OpenBao is not changed.

## benchmark

```sh
bao-kms-provider benchmark --config /etc/openbao-kms/config.yaml --iterations 5
```

Measures Transit encrypt and decrypt latency with non-secret data.

## config

```sh
bao-kms-provider config --config /etc/openbao-kms/config.yaml
bao-kms-provider config schema
```

`config` prints the resolved configuration after defaults, the file,
environment overrides, and flags, including the identity fingerprint when all
identity-bearing values are set. `config schema` prints the JSON Schema, which
rejects unknown fields and requires `configVersion: v1alpha1`.

`config` can inspect incomplete defaults. It does not validate whether the
configuration can start the provider. Use `doctor` for validation and local
and remote checks. An unreadable or malformed configuration file exits with `3`.

## init

Generates every file that shares the provider's identity values from one
values file, so they cannot disagree.

```sh
bao-kms-provider init --values values.yaml --out ./generated --new-key
```

| Flag | Meaning |
|---|---|
| `--values <path>` | Required. A file in the provider configuration format with the values you choose; see [Plan identity values](/docs/get-started/plan-values/#generate-the-files-with-init). |
| `--out <dir>` | Required. Output directory; it must be absent or empty. |
| `--model systemd\|static-pod` | Deployment model. Default: `systemd`. |
| `--new-key` | Generate `transit.keyIdScope.keyLineageId` for a Transit key you are about to create. Rejected if the values file already sets one. |
| `--policy-name <name>` | OpenBao policy name. Default: `openbao-kms-<clusterId>`. |
| `--image <ref>` | Static pod only, required. The provider image pinned by `@sha256` digest. |
| `--socket-gid <gid>` | Static pod only, required. This node's numeric socket GID; overrides `server.socketGroup` in the values file. |

`init` fills the documented host paths for an omitted `openbao.caCertFile`,
`auth.jwt.jwtFile` for source `file`, and `server.socketGroup`. For static pods,
`--socket-gid` selects the local group even when the values contain another
node's group. `init` then validates the result as `serve` would.
With JWT auth it also needs `auth.jwt.expectedIssuer`,
`expectedAudience`, and `expectedSubject`, because the OpenBao role binds them.
Generation ignores environment overrides and rejects runtime configuration flags;
set them in `--values` instead. The generated JWT role TTL is 30 minutes;
`auth.loginBeforeTokenExpiry` must be shorter. Policy names must be dedicated
ASCII names without path or list separators; `root` and `default` are reserved.
See [Generate installation files](/docs/get-started/plan-values/) for mount
constraints and [Prepare OpenBao](/docs/get-started/openbao/) for setup phases.

| File | Contents |
|---|---|
| `config.yaml` | The complete provider configuration for every control-plane node. |
| `encryption-config-readers.yaml` | Phase 1: `identity` first and KMS second. Stage the reader on every API server while writes remain plaintext. |
| `encryption-config.yaml` | Phase 2: KMS first and `identity` second. Enable writes only after all readers are ready. Both files are cross-checked against `config.yaml`. |
| `openbao-policy.hcl` | The least-privilege policy, including token renewal. |
| `openbao-setup.sh` | The `bao` commands for the Transit mount, key, policy, and JWT role, for an administrator to review and run. |
| `bao-kms-provider.yaml` | Static pod only: the manifest with the image digest and socket GID. |
| `installation.json` | Generator build metadata, shared fingerprint and lineage ID, model, image when supplied, runtime identity, host paths, generated files, and remaining actions. No credentials or activation success claim. |

`init` prints the identity fingerprint, and the generated lineage ID with
`--new-key`. It never contacts OpenBao, never writes outside `--out`, and never
replaces an existing file. It exits with `2` for invalid flags, `3` for invalid
values, and `1` when writing fails.

## policy openbao

```sh
bao-kms-provider policy openbao --config /etc/openbao-kms/config.yaml
```

Prints the least-privilege policy for the configured mount and key: metadata
read, encrypt, decrypt, `disable_upsert` inspection, and
`sys/capabilities-self`. It includes `auth/token/renew-self` by default, matching
`init` and the runtime's renewal behavior. Use `--include-token-renewal=false`
only when tokens are non-renewable or another attached policy grants renewal.
This flag changes generated policy, not runtime behavior. See
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/).

## version and completion

`bao-kms-provider version` prints `version`, `commit`, `buildDate`, and `dirty`;
use it to compare nodes during upgrades and incidents.
`bao-kms-provider completion bash|fish|powershell|zsh` prints a shell
completion script.

## Common flags

| Flag | Effect |
|---|---|
| `--config <path>` | Configuration file. |
| `--log-level debug\|info\|warn\|error` | Overrides `logging.level`. |
| `--metrics-address <host:port>` | Overrides `server.metricsAddress`. |
| `--health-address <host:port>` | Overrides `server.healthAddress`. |

## Exit codes

| Code | Meaning |
|---:|---|
| 0 | Success |
| 1 | Unclassified error |
| 2 | Invalid command usage |
| 3 | Configuration load or validation error |
| 4 | A diagnostic check failed |
| 5 | Provider runtime failure |

Unknown commands or flags, unexpected positional arguments, missing flag
values, and invalid typed flag values exit with `2`. Configuration settings
that fail validation exit with `3`, including settings supplied through flags.
Runtime failures after configuration validation exit with `5`.
