# Testing Strategy

The testing strategy should be built around one principle:

A KMS plugin failure is not just a failed service; it can prevent the API server from starting or make encrypted Kubernetes resources unreadable.

So I would not treat this like a normal Go service with unit tests plus a few integration tests. The project needs a protocol conformance suite, failure-mode tests, rotation tests, and recovery tests from the beginning.

Kubernetes KMS v2 is the right baseline to test against: upstream documentation says KMS v2 is stable since Kubernetes v1.29, while KMS v1 is deprecated since v1.28 and disabled by default since v1.29. The same documentation also makes Status, key_id, annotations, decrypt validation, and latency requirements central to the plugin contract.

---

## 1. Test priorities

I would rank the testing priorities like this:

| Priority | Why it matters |
| --- | --- |
| KMS v2 protocol correctness | If this is wrong, Kubernetes will reject encrypt responses, mark the plugin unhealthy, or fail decrypts. |
| key_id invariants | Rotation and decrypt safety depend on stable, non-reused, understood key IDs. |
| Decrypt compatibility | Old encrypted data must remain readable after restart, upgrade, and rotation. |
| OpenBao Transit behavior | We rely on explicit key_version, AAD, metadata, min_decryption_version, and disable_upsert. |
| API server startup behavior | Kubernetes may perform thousands of decrypt calls on startup and recommends decrypt latency under 10 ms. |
| Runtime deployment | systemd and static pod behavior differ materially. |
| Disaster recovery | Loss of Transit key state or config mismatch can make data unrecoverable. |
| Observability and redaction | The component handles sensitive data and must not leak plaintext, tokens, or full ciphertext. |

The negative tests are more important than the happy path. Encrypt/decrypt working once is easy. The hard part is proving the plugin fails safely when OpenBao is sealed, the JWT expires, annotations are malformed, the key version changes mid-flight, the socket is stale, or an old key_id appears after rotation.

---

## 2. Test layers

### Layer 1: pure unit tests

These should be fast and run on every PR.

Focus areas:

internal/keyregistry
internal/aad
internal/kmsv2
internal/auth/jwt
internal/socket
internal/config
internal/log

Important unit test groups:

keyregistry

This is one of the most security-critical packages.

Tests:

same inputs produce same Kubernetes key_id
different Transit version produces different key_id
different cluster_id produces different key_id
different provider_name produces different key_id
different key lineage produces different key_id
previous key_id is never reused after rollback
active snapshot does not flip-flop
pending snapshot requires stable observation count
version rollback is rejected unless explicit DR mode is enabled
old snapshots remain lookupable for decrypt

I would add property-style tests here. For example:

For any two key snapshots:
  if any identity-bearing field differs,
  key_id must differ.
For any active rotation sequence:
  Status.key_id must either remain old or move once to new,
  never old -> new -> old.

aad

AAD must be deterministic. Any nondeterminism here breaks decrypt.

Tests:

canonical AAD serialization is stable
annotation order does not affect reconstructed AAD
missing required annotation fails
malformed key version fails
wrong provider fails
wrong cluster hash fails
wrong transit key hash fails
wrong AAD version fails
legacy compatibility mode accepts only configured legacy epochs
AAD never includes raw key name, raw mount path, JWT, token, or plaintext

OpenBao Transit supports associated_data for AEAD ciphers on encrypt and decrypt, so we should test that our AAD builder produces exactly the same bytes on both paths.

kmsv2

This package should have contract-style unit tests using fake Transit and fake registry.

Tests:

Status returns cached active key_id
Status does not call Transit directly
Encrypt uses active snapshot
Encrypt returns Status.key_id
Encrypt returns annotations when AAD enabled
Encrypt fails if no active key snapshot exists
Decrypt rejects unknown key_id
Decrypt rejects malformed key_id
Decrypt rejects missing annotations when AAD required
Decrypt rejects AAD mismatch
Decrypt passes reconstructed AAD to Transit
Decrypt never attempts fallback across all keys

The Kubernetes KMS v2 contract requires Status.key_id, EncryptResponse.key_id, annotations, and decrypt-side key_id verification; it also says the API server throws away an encrypt response when its key_id differs from Status.key_id.

auth/jwt

Tests:

JWT file missing fails closed
JWT file unreadable fails closed
expired JWT fails closed
JWT below min remaining TTL fails closed
JWT file is re-read before re-login
OpenBao token is stored only in memory
token refresh schedules before expiry
wrong issuer/audience/subject is detected where locally checkable
clock skew handling works

socket

Tests:

creates socket with configured mode
sets configured group
rejects unsafe parent directory
rejects symlink socket path
rejects regular file at socket path
removes only verified dead Unix socket
does not remove live socket
does not use abstract socket by default

Kubernetes allows Unix domain sockets and notes that abstract Linux sockets do not have normal filesystem ACL semantics, so tests should enforce file-based sockets by default.

config

Tests:

safe config passes
world-writable config fails
world-readable JWT file warns or fails depending policy
missing providerName fails
missing clusterId fails
missing keyLineageId fails
AAD enabled without required scope fails
dangerous timeout values fail
invalid socket path fails

log

Tests:

plaintext never appears in logs
JWT never appears in logs
OpenBao token never appears in logs
full ciphertext never appears in info/error logs
raw Transit key path is redacted by default
debug mode requires explicit opt-in

---

### Layer 2: KMS v2 protocol conformance tests

This should be a dedicated test suite, not just ordinary unit tests.

The suite should start the plugin with fake OpenBao/Transit and connect to the Unix socket using the real Kubernetes KMS v2 protobuf client.

Core conformance tests:

Status returns:
  version = v2
  healthz = ok
  non-empty key_id
Encrypt returns:
  non-empty ciphertext
  key_id equal to Status.key_id
  valid annotations
Decrypt accepts:
  ciphertext + key_id + annotations returned by Encrypt
Decrypt rejects:
  ciphertext with unknown key_id
  ciphertext with malformed key_id
  ciphertext with missing annotations
  ciphertext with modified annotations
  ciphertext from different providerName
  ciphertext from different clusterId
Status behavior:
  cheap under repeated polling
  no OpenBao call per Status
  unhealthy after status cache staleness
  healthy after background probe recovers

Specific invariant:

for i in 1..N:
  statusKeyID := Status().key_id
  enc := Encrypt(randomPlaintext)
  assert enc.key_id == statusKeyID

This is one of the most important tests in the whole project.

---

### Layer 3: OpenBao client integration tests

These should be hermetic. They should run against in-process HTTPS fakes that model OpenBao Transit response shapes, TLS behavior, error bodies, and policy capability responses. They must not require external OpenBao credentials.

Exact-version OpenBao validation belongs in the e2e lane. PR and release-gate validation should own an ephemeral OpenBao CI environment.

Important OpenBao tests:

JWT login succeeds with configured role
JWT login fails with wrong audience
JWT login fails with wrong subject
token can read Transit key metadata
token can encrypt
token can decrypt
token cannot rotate key
token cannot export key
token cannot backup key
token cannot delete key
Transit key metadata is parsed correctly
explicit key_version is sent on encrypt
empty associated_data is rejected by the client
associated_data decrypt succeeds with matching AAD
associated_data decrypt fails with changed AAD
batch decrypt preserves per-item associated_data and reference values
min_encryption_version blocks old encrypt version
min_decryption_version blocks old decrypt version
disable_upsert prevents accidental key creation
capabilities-self results are parsed for policy diagnostics

OpenBao Transit supports explicit key_version on encrypt, and if it is omitted Transit uses the latest version. Our design intentionally does not want implicit latest-version behavior in the hot path.

OpenBao also exposes min_encryption_version, min_decryption_version, exportability, plaintext backup, deletion settings, and key version metadata through Transit key configuration and metadata, so the integration tests should assert these are read and interpreted correctly.

For disable_upsert, OpenBao documents a Transit keys configuration that disables automatic creation of unknown keys through encrypt operations. That must be tested because typo-driven key creation would be a serious operational footgun.

---

### Layer 4: Kubernetes API server e2e tests

This is where we prove the plugin works with a real API server.

E2E suites should use Ginkgo v2 for spec structure, labels, timeouts, reports, and suite-level setup, with Gomega for assertions.

The suite should live under one root `test/e2e` package with shared helpers in `test/e2e/framework`. Suite lanes are declared in `test/e2e/suites.yaml`; concrete OpenBao and Kubernetes versions stay in `.ci/versions.yaml` and are referenced by the manifest instead of duplicated. See [E2E framework](e2e-framework.md).

I would use two e2e tracks:

| Track | Purpose |
| --- | --- |
| kind-based | Fast-ish CI feedback, API server integration, encryption config behavior. |
| kubeadm VM-based | More realistic static pod, host filesystem, systemd, socket, and boot behavior. |

The kind suite is useful, but it is not enough for production confidence.

kind e2e tests

Test flow:

start OpenBao outside the Kubernetes API dependency path
start plugin with Unix socket mounted into control-plane node
configure kube-apiserver EncryptionConfiguration with KMS v2
restart API server
create Secret
verify Secret is readable through API
verify etcd does not contain plaintext Secret value
restart API server
verify Secret is still readable
rotate Transit key
wait for plugin Status.key_id change
rewrite Secret
verify old and new data readable

Kubernetes encryption-at-rest applies to Kubernetes API resource data, not container filesystems or volumes, so the e2e test should verify API resources such as Secrets and optionally ConfigMaps/CRDs.

kubeadm VM e2e tests

This is where we test the real operational model.

Test variants:

systemd plugin + kubeadm static pod API server
static pod plugin + kubeadm static pod API server
single-node control plane
three-node control plane

For static pod mode, the test must verify that the manifest does not rely on ServiceAccount, ConfigMap, or Secret references, because Kubernetes static pods cannot refer to API objects.

Important kubeadm tests:

host boot with plugin available before API server
host boot with OpenBao temporarily unavailable
host boot with stale socket
host boot with wrong socket permissions
control-plane node replacement
plugin package upgrade
plugin static pod image unavailable
API server restart decrypt storm

---

### Layer 5: rotation tests

Rotation deserves its own suite.

Rotation test cases:

initial active Transit version = 1
Status.key_id = KID1
Encrypt uses Transit key_version 1
rotate Transit key to version 2
plugin observes version 2 but does not activate immediately
Status still returns KID1 during pending observation
after stable observation + activation delay:
  Status.key_id = KID2
  Encrypt uses Transit key_version 2
  EncryptResponse.key_id = KID2
Decrypt old ciphertext:
  request key_id = KID1
  annotations version = 1
  Transit decrypt succeeds
Decrypt new ciphertext:
  request key_id = KID2
  annotations version = 2
  Transit decrypt succeeds

Negative rotation tests:

Transit latest version briefly appears as 2 then reverts to 1
  plugin must not flip-flop
plugin restarts during pending rotation
  active key decision remains stable
OpenBao metadata read fails during rotation
  plugin must not promote
Transit key is recreated with same name
  lineage mismatch fails closed
min_decryption_version is raised too early
  old decrypt fails, doctor reports destructive condition
Status.key_id changes but Encrypt still returns old key_id
  conformance test must fail

Kubernetes treats Status.key_id as authoritative and requires the plugin to keep Status and EncryptResponse consistent; this is why rotation tests should focus less on “did Transit rotate?” and more on “did the plugin expose the rotation safely to Kubernetes?”

---

### Layer 6: failure injection tests

We should maintain an explicit failure-mode test matrix.

Minimum failure injection suite:

| Failure | Expected behavior |
| --- | --- |
| OpenBao down | Status eventually unhealthy; encrypt/decrypt fail closed. |
| OpenBao sealed | Ready false; no plaintext fallback. |
| TLS cert expired | Auth/transit calls fail; clear error class. |
| DNS failure | bounded retries; no request pileup. |
| JWT expired | re-login fails; ready false; logs do not leak JWT. |
| JWT file rotated | plugin re-reads file before re-login. |
| Transit key missing | ready false; doctor fails. |
| Transit key deleted/recreated | lineage mismatch or decrypt failure; fail closed. |
| AAD mismatch | decrypt rejected. |
| Unknown key ID | decrypt rejected without Transit brute-force attempts. |
| API server startup storm | latency and error rate measured. |
| OpenBao audit backend slow | plugin latency increases but does not deadlock. |
| Plugin crash loop | systemd/kubelet restart behavior works. |
| Stale socket | safe cleanup only if verified dead. |
| Wrong socket group | API server cannot connect; doctor catches it. |
| Static pod image missing | documented failure and recovery path. |

These tests should be automated where possible, but some systemd/static-pod cases may live in nightly VM tests rather than every PR.

---

### Layer 7: performance and load tests

The performance target should be based on Kubernetes behavior:

* EncryptRequest: aim under 100 ms.
* DecryptRequest: aim under 10 ms.
* Startup may involve thousands of decrypt requests.
* Status is polled continuously and must be optimized.

Performance tests:

single encrypt latency p50/p95/p99
single decrypt latency p50/p95/p99
concurrent decrypt latency
API server startup decrypt storm simulation
Status polling for 1 hour
OpenBao HA failover during load
token renewal during load
JWT re-login during load
Transit metadata probe during load

I would define early SLOs like this:

```yaml
performanceTargets:
  status:
    p99: 5ms
    externalOpenBaoCalls: 0
  encrypt:
    p95: 100ms
    p99: 250ms
  decrypt:
    p95: 10ms
    p99: 50ms
  startupStorm:
    noDeadlock: true
    boundedMemory: true
    boundedGoroutines: true
```

The p99 values may need adjustment based on OpenBao/network reality, but the test should make the tradeoff visible.

Decrypt micro-batching is included in v0.1 behind disabled-by-default configuration. It must remain disabled by default unless tests prove benefit under realistic API server startup conditions.

---

### Layer 8: security tests

Security tests should include both automated checks and review gates.

Automated:

gosec
govulncheck
staticcheck
race detector
fuzz tests for annotations and key_id parsing
dependency license scan
container image vulnerability scan
SBOM generation
signature/provenance verification

Security-specific functional tests:

logs do not contain plaintext
logs do not contain JWT
logs do not contain OpenBao token
metrics do not expose raw key_id
metrics do not expose raw OpenBao mount/key path
doctor does not print secrets
panic output redacts sensitive config
debug endpoints disabled by default
admin endpoint bound only to localhost when enabled

Fuzz targets:

DecryptRequest annotations
key_id parser
AAD envelope parser
OpenBao error parser
EncryptionConfiguration parser
JWT parser wrapper
config loader

A good fuzz target would be:

Given arbitrary annotations and key_id:
  Decrypt must either reject safely
  or produce exactly the expected AAD for a known valid object.
It must never panic.
It must never call Transit for unknown key_id.

---

### Layer 9: disaster recovery tests

This is where many KMS projects are weak.

DR tests:

restore OpenBao backup and read existing Kubernetes Secret
restore etcd backup with matching OpenBao backup
restore etcd backup with too-new OpenBao state
restore etcd backup with too-old OpenBao state
plugin config lost and restored
plugin config changed incorrectly
JWT issuer unavailable
control-plane node replaced
single-node control plane boot recovery
multi-node control plane one-node-at-a-time recovery

Critical destructive tests in isolated environments:

delete Transit key and prove data is unrecoverable without backup
raise min_decryption_version too early and prove old data fails
recreate Transit key with same name and prove old data fails
change providerName and prove decrypt/AAD behavior
change clusterId and prove AAD behavior

These tests should not just validate the plugin. They validate the documentation and runbooks.

---

## 3. Suggested CI structure

Every PR

Fast tests only:

go test ./...
go test -race ./internal/...
staticcheck
gofmt/go vet
unit tests
KMS v2 fake conformance tests
fuzz smoke tests
redaction tests
config validation tests

Target: under 10 minutes.

Main branch / nightly

Heavier tests:

OpenBao client integration tests
kind e2e tests
rotation tests
failure injection tests
performance smoke tests
container image scan
SBOM generation

Release candidate

Full test suite:

Kubernetes version matrix
OpenBao version matrix
kubeadm VM tests
systemd deployment tests
static pod deployment tests
OpenBao HA failover tests
DR restore tests
startup decrypt storm test
upgrade/downgrade test

Manual pre-production validation

For customer or serious lab environments:

run doctor on every control-plane node
verify same Status.key_id across all API servers
create/read/restart/read test
rotate Transit key in test environment
perform storage migration
restore OpenBao + etcd backup pair
simulate OpenBao outage
simulate JWT expiry

---

## 4. Version matrix

The initial matrix should be exact-pinned and should not use floating `latest` inputs:

```yaml
kubernetes:
  required:
    - "1.34.3"
  futureCandidates:
    - "1.34.7"
    - "1.35.x"
    - "1.36.x"
openbao:
  required:
    - "2.5.3"
deployment:
  - kind
  - kubeadm-systemd
  - kubeadm-static-pod
auth:
  - jwt-local-public-key
  - jwt-jwks
  - jwt-oidc-discovery
transit:
  - aes256-gcm96
  - xchacha20-poly1305 optional
  - aad-enabled
  - decrypt-microbatching-disabled-default
  - decrypt-microbatching-enabled-benchmark
```

The exact Kubernetes patch version and Kind node image digest live in the central CI version policy. Upstream `1.34.7` is tracked as the latest `1.34` patch, but the initial Kind lane pins `1.34.3` because newer official Kind node images are unavailable. Additional Kubernetes or OpenBao versions should be future candidates until exact-pinned release-gate lanes exist.

---

## 5. Concrete “must pass before MVP” list

For v0.1, I would make these blocking:

1. KMS v2 fake conformance suite passes.
2. Hermetic OpenBao client integration suite passes.
3. Pinned OpenBao `2.5.3` plus Kubernetes `1.34.3` kind e2e proves Secret encryption/decryption works.
4. Kube-apiserver restart with encrypted Secret works.
5. Status.key_id == EncryptResponse.key_id invariant is tested.
6. Unknown key_id decrypt is rejected before Transit call.
7. AAD mismatch decrypt is rejected.
8. Transit encrypt uses explicit key_version.
9. Rotation from Transit v1 -> v2 works without key_id flip-flop.
10. Old ciphertext remains decryptable after rotation.
11. JWT expiry/re-login path works.
12. OpenBao outage fails closed.
13. Doctor catches bad socket, bad JWT, bad policy, bad Transit key config.
14. Logs and metrics redaction tests pass.
15. Static pod manifest does not rely on API objects.
16. Systemd unit starts plugin before kubelet in kubeadm-style test.
17. AAD is required for v0.1 objects.
18. Decrypt micro-batching is implemented behind disabled-by-default config and benchmarked.
19. CI uses exact-pinned OpenBao, Kubernetes, and image versions.
20. SBOM, vulnerability scan, license check, signatures, attestations, and release evidence gates pass.

That is the minimal bar where I would be comfortable saying: this is an engineering-preview KMS v2 provider with meaningful correctness coverage.

Not production-ready yet, but no longer just a prototype.

---

## 6. Test data and golden fixtures

We should keep golden fixtures for:

KeySnapshot -> key_id
KeySnapshot + annotations -> AAD
Transit metadata -> parsed key profile
EncryptionConfiguration -> parsed provider config
JWT claims -> validation result
OpenBao errors -> error class

Example:

```text
testdata/
├── keyid/
│   ├── snapshot-v1.yaml
│   └── expected-keyid.txt
├── aad/
│   ├── annotations-v1.yaml
│   └── expected-aad-base64.txt
├── transit/
│   ├── key-metadata-v1.json
│   ├── key-metadata-rotated.json
│   └── key-metadata-dangerous-min-decrypt.json
├── encryptionconfig/
│   ├── valid-kms-v2.yaml
│   ├── identity-fallback.yaml
│   └── wrong-provider-name.yaml
└── jwt/
    ├── valid.claims.json
    ├── expired.claims.json
    └── wrong-audience.claims.json
```

Golden tests are useful because they catch accidental changes to key_id or AAD derivation. Those are effectively wire-format compatibility guarantees.

---

## 7. Key registry test implication

The key registry decision is resolved as a hybrid model: key IDs are derived from stable config plus Transit metadata, while a non-secret local registry records observed and promoted snapshots for restart, rollback, and rotation decisions.

Derived inputs include:

providerName
clusterId
openbaoInstanceId
transitMountId
keyLineageId
Transit key version
Transit key version creation timestamp

Tests must prove that after plugin restart:

old key_id lookup still works
old ciphertext still decrypts
rotation state does not accidentally re-promote an old version

The local registry state file should live under:

```text
/var/lib/openbao-kms/state/key-registry.json
```

That file needs tests for permissions, corruption handling, rebuild-from-metadata behavior, backup/restore, and rollback detection.

---

## 8. My recommended next step

Before writing production code, create the test harness first:

1. Fake Transit implementation.
2. KMS v2 gRPC client test helper.
3. Key ID/AAD golden tests.
4. Hermetic OpenBao client integration harness.
5. Minimal kind e2e.

Then implement enough plugin code to satisfy the tests.
