---
title: Compatibility
description: "The tested Kubernetes, OpenBao, auth, and deployment matrix, the Transit features the provider needs, and the compatibility promises between releases."
eyebrow: Reference · Lifecycle
weight: 70
verifiedBy:
  - .ci/versions.yaml
  - internal/config/validation.go
  - internal/keyregistry
---

A release covers only what its release notes and this page list as tested.
Everything else might work but is outside the tested matrix.

## Tested matrix

| Component | Tested | Not covered |
|---|---|---|
| Kubernetes | `1.34.3` and `1.35.0`, pinned by Kind node-image digest in `.ci/versions.yaml` | Other `1.34.x` and `1.35.x` patches unless a release lists them; `1.29` to `1.33` might work with KMS v2; below `1.29` is not targeted |
| Kubernetes KMS API | v2 | KMS v1 is not implemented |
| OpenBao | `2.6.0` | Other `2.6.x` until tested and pinned; `2.5.x` |
| Transit key type | `aes256-gcm96` | Other AEAD types, derived or convergent keys |
| Host | Linux control-plane nodes with filesystem Unix sockets | Windows, abstract Unix sockets |
| Deployment | systemd and kubelet static pod | DaemonSet in the protected cluster, sidecar to `kube-apiserver` |

## Auth methods

| Method | Build | Status |
|---|---|---|
| JWT | Default release artifacts | Supported. |
| Certificate, PKCS#11 source | `bao-kms-provider-certauth-pkcs11` host artifacts | Supported only when the selected release publishes the artifact and marks the path as tested. |
| Certificate, SPIFFE source | Local verification builds | Not supported; configuration validation rejects it. |
| OpenBao Kubernetes auth | Any | Not supported: TokenReview depends on the protected API server. |

Certificate auth also needs an OpenBao listener that requests client
certificates, a cert auth role bound to the provider identity, and a PKCS#11
module on the host.

## Required OpenBao features

The provider relies on Transit encrypt and decrypt with an explicit
`key_version`, `associated_data`, key metadata with version creation times,
`min_encryption_version`, `min_decryption_version`, `disable_upsert`, and JWT
or TLS certificate auth.

It fails closed when Transit metadata falls outside the validated profile. Each
finding has an impact class:

- `cryptographic_safety`: the key type, exportability, plaintext backup, or
  derived or convergent mode would change the validated encryption contract.
- `api_server_availability`: deletion, unsupported operations, or version
  restrictions would strand reads or writes of active or historical versions.

Failing closed protects the cryptographic contract but stops API server writes
until OpenBao is fixed. Check `verify-key`, `doctor`, `/ready`, and KMS Status
before changing API server encryption.

## Compatibility promises

After the first stable release, these stay backward compatible within a major
version:

- `key_id` derivation for existing epochs, including the Unix-second
  normalization of Transit version creation times,
- the annotation schema and AAD canonicalization,
- the meaning of identity-bearing configuration values,
- decryption of historical `key_id` values,
- the JSON report shape of report-style CLI commands.

Until then, minor releases can break these surfaces, and each break comes with
a migration guide; see
[Release and support lifecycle](/docs/reference/release-lifecycle/#versioning).

## Operator retirement

`retire-versions --apply` ends local decryption for the versions you select and
changes nothing else: `key_id` derivation, annotations, AAD, the active key,
and other historical identities stay the same.

Retirement adds `removed` records to the state file, which older binaries
reject. Upgrade every provider before retiring versions, and afterwards never
downgrade to such a binary or erase the records. The state directory must be
owned by the provider's OS user, and retirement must run as that user. See
[Operate: Rotation](/docs/operate/rotation/#retire-old-versions).

## Breaking changes

### Unreleased CLI exit-code corrections

Invalid command usage now returns the documented exit code `2`, and an
unreadable or malformed file passed to `config` returns `3`. These cases
previously returned the unclassified error code `1`. Unknown help topics
and completion subcommands also return `2`. Update scripts that matched the
previous codes. Diagnostic failures remain `4` and runtime failures remain `5`.

### Unreleased configuration cross-checks

Configuration validation now rejects a probe interval greater than or equal to
the status staleness window, a renewal increment less than or equal to the
token refresh lead time, and duplicate fixed health and metrics endpoints.
These checks apply to the effective configuration after file, environment,
and flag overrides. Defaults satisfy all three checks.

Empty listener addresses still disable their listeners, and port `0` still
requests an available port. Endpoint comparisons do not resolve DNS names or
detect every wildcard overlap; other bind conflicts remain runtime errors.
No key identity, encryption format, or persisted state changes are required.

### Unreleased Transit key-name validation

Configuration and its JSON Schema now enforce the name pattern used by the
OpenBao 2.6 Transit key-metadata endpoint. Names start and end with an ASCII
letter, digit, or underscore; interior characters can also be dots or hyphens.
Previously accepted names outside this pattern now fail configuration
validation, including `.` and `..`, whitespace, and URL delimiters.

Valid names, key IDs, AAD, annotations, and state formats are unchanged. The
provider does not rewrite names. Treat a name change as an identity change;
do not rename an existing configuration to bypass validation without checking
the backend key and existing encrypted data.

### Unreleased static-pod JWT mount correction

New static-pod scaffolds place file JWTs in
`/var/lib/openbao-kms/credentials/identity.jwt` and mount the directory read-only
so atomic replacement is visible. Existing file mounts need a one-time
[manifest and credential-path migration](/docs/get-started/static-pod/#migrate-an-existing-jwt-file-mount).
The generator rejects JWT directories that overlap provider state or socket
directories. Existing deployments are not changed automatically. Systemd
paths and the encryption format are unchanged.

### Unreleased rotation corrections

Pending versions now decrypt as soon as their metadata is validated and saved,
though they still cannot encrypt before promotion. An unknown `key_id` can
trigger a metadata read, bounded by the request timeout and
`status.probeInterval`, which never counts toward promotion. State transitions
keep pending identities alongside active and retired ones.

`key_id` derivation, AAD bytes, annotations, and the state schema are
unchanged, and no Kubernetes data rewrite is needed. Pause rotation, upgrade
every provider, and confirm all nodes report the same active `key_id` before
the next rotation. Older binaries ignore pending versions for decryption, so
never roll a node back while another node might have encrypted with its pending
version.

### What counts as breaking

Changing `key_id` derivation, AAD canonicalization, the default AAD mode, or
provider-name handling, or dropping an annotation version or decrypt support
for retained historical `key_id` values, is breaking. A breaking change needs a
written design decision, a migration guide, updated test fixtures, and a
compatibility section in the release notes.
