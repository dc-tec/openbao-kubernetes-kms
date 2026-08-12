# E2E Tests

The Ginkgo/Gomega end-to-end (E2E) suite lives in this directory.

The default OpenBao E2E target uses ephemeral OpenBao continuous integration
(CI) environments with Transit and JSON Web Token (JWT) authentication. It
runs the Transit and JWT assertions, including role claim binding and pinned
signing-key rollover. It also runs the provider full-stack OpenBao and
Kubernetes Key Management Service (KMS) v2 socket test:

```sh
make test-e2e-openbao-ci
```

Run only the provider full-stack slice with:

```sh
make test-e2e-provider-openbao-ci
```

The target builds `E2E_PROVIDER_IMAGE` and runs the provider and KMS v2 socket
client in Docker containers. To test a prebuilt image tag, set
`E2E_PROVIDER_BUILD=false`.

Run only the OpenBao certificate auth slice with:

```sh
make test-e2e-cert-auth-openbao-ci
```

The target runs real OpenBao with a Transport Layer Security (TLS) listener that
requests client certificates. It configures the OpenBao certificate
authentication method with a URI subject alternative name (SAN) role binding.
It then logs in through certificate authentication and verifies Transit access
with the issued token. It does not replace provider E2E coverage for PKCS#11
source availability.

Run the provider certificate-source lanes with:

```sh
make test-e2e-provider-certauth-sources-openbao-ci
```

The target runs the supported source-specific provider check. The PKCS#11 lane
builds an E2E image with SoftHSM and creates a real PKCS#11 key pair and client
certificate. It configures OpenBao certificate authentication with the generated
certificate authority (CA), then runs the KMS v2 socket client through the
provider.

Run the SPIFFE workload identity source implementation check with:

```sh
make test-e2e-provider-certauth-spiffe-openbao-ci
```

The lane starts real SPIFFE Runtime Environment (SPIRE) server and agent
containers. It registers the provider user identifier (UID) selector and fetches
a real X.509 SPIFFE Verifiable Identity Document (SVID) from the Workload API.
It validates the SVID through the provider SPIFFE certificate source code. The
lane is not wired into CI or the preview release gate. It does not establish a
support claim until OpenBao certificate authentication can derive identity
aliases from URI-SAN-only SVIDs.

Run only the provider CLI slice with:

```sh
make test-e2e-provider-cli-openbao-ci
```

The target runs the provider image command-line interface (CLI) against mounted
configuration, TLS and JWT files, local state, and real OpenBao. It covers:

- `doctor`, `verify-key`, `rotation-plan`, and `verify-rotation`;
- `config` and `policy openbao`;
- JWT claim-drift redaction;
- unsupported Transit key type diagnostics;
- fail-closed behavior when provider state is missing after rotation.

Run only the provider/OpenBao failure-mode slice with:

```sh
make test-e2e-provider-failure-openbao-ci
```

The target uses real OpenBao Transit and JWT authentication with the provider
image. It covers:

- unavailable and sealed OpenBao instances;
- reduced provider policy with `PermissionDenied` KMS errors;
- startup failure for expired JWTs and expected-claim drift;
- JWT file rotation and provider re-login;
- provider re-login after signing-key rollover;
- startup failure for a missing Transit key;
- stale Status data and stale socket reclamation.

Run only the provider and OpenBao high-availability (HA) failover slice with:

```sh
make test-e2e-provider-ha-openbao-ci
```

The target starts three OpenBao integrated-raft nodes and points the provider at
a standby node. It writes encrypted data, removes the active node, and waits for
a surviving voter to become active. It then verifies old ciphertext readback
and new KMS operations through the same provider.

Run only the provider decrypt storm smoke slice with:

```sh
make test-e2e-provider-decrypt-storm-openbao-ci
```

The target performs concurrent KMS v2 `Decrypt` calls through the provider
against real OpenBao. It is a smoke test, not a replacement for
release-candidate load testing.

Run only the sustained direct decrypt soak slice with:

```sh
make test-e2e-provider-decrypt-soak-openbao-ci
```

The target prepares a fixed corpus of encrypted samples and sustains concurrent
KMS v2 `Decrypt` calls through the provider and OpenBao. It requires zero
client-visible errors and enforces operation-count and p95-latency thresholds.
It also compares Docker memory and process identifier (PID) counts before and
after the run.

Run only the provider/OpenBao load-soak slice with:

```sh
make test-e2e-provider-load-soak-openbao-ci
```

The target runs sustained `Status`, `Encrypt`, and `Decrypt` calls through the
provider and OpenBao. It requires zero client-visible errors and enforces
operation-count and p95-latency thresholds. It also compares Docker memory and
PID counts before and after the run.

Run only the provider and OpenBao backend replacement and disaster-recovery
(DR) restore slice with:

```sh
make test-e2e-provider-restore-openbao-ci
```

The target runs OpenBao with integrated raft storage in Docker. It verifies:

- provider fail-closed behavior while the backend is unavailable;
- backend replacement under the same Docker network name;
- raft snapshot save and restore into a fresh storage volume;
- decryption of ciphertext created before the outage or restore.

Run only the provider/OpenBao Transit rotation slice with:

```sh
make test-e2e-provider-rotation-openbao-ci
```

The target runs OpenBao with integrated raft storage in Docker. It:

1. Writes ciphertext with the initial Transit key version.
2. Saves a pre-rotation raft snapshot.
3. Rotates the Transit key.
4. Waits for provider `Status` to promote a new `key_id`.
5. Verifies decryption of old and new ciphertext.
6. Rejects `min_decryption_version` changes that strand retained historical
   versions.
7. Verifies fail-closed behavior when local provider state disappears after
   rotation.
8. Restores the pre-rotation snapshot.
9. Verifies that the provider rejects the observed Transit version rollback.

Run only the provider binary upgrade/rollback slice with:

```sh
make test-e2e-provider-upgrade-rollback-openbao-ci
```

The target builds distinct old and new provider images and verifies that their
version metadata differs. It encrypts with the old image, upgrades the same
provider state volume, and reads the old ciphertext with the new image. It then
encrypts new ciphertext, rolls back to the old image, and verifies that both
ciphertexts remain decryptable.

Run the pinned Kubernetes Kind smoke lane with:

```sh
make test-e2e-kind-smoke
```

The target builds and loads the provider image into a pinned Kind cluster. It
deploys the provider as a static pod and enables kube-apiserver KMS v2
encryption. It then verifies Secret readback, the raw etcd KMS v2 envelope,
kube-apiserver restart, and readback after restart.

Run the pinned Kubernetes Kind multi-control-plane convergence lane with:

```sh
make test-e2e-kind-convergence
```

The target creates a three-control-plane Kind cluster. It stages the provider on
each node and enables KMS v2 on each kube-apiserver. The verification then:

- confirms the KMS envelope in each stacked etcd member;
- leaves each kube-apiserver as the only serving endpoint in turn;
- confirms decrypt convergence through each endpoint;
- restarts each kube-apiserver and confirms Secret readback.

Run the pinned Kubernetes Kind static-pod upgrade/rollback lane with:

```sh
make test-e2e-kind-upgrade-rollback
```

The target updates the provider static pod manifest and waits for kubelet to
restart the provider. It verifies that old and new Secrets remain readable,
restores the previous manifest, and verifies readback after provider and
kube-apiserver restart.

Run the pinned Kubernetes Kind DR restore runbook lane with:

```sh
make test-e2e-kind-dr-runbook
```

The target creates an encrypted Secret and saves the paired backup:

- an OpenBao raft snapshot;
- provider node configuration, TLS, JWT, and state files.

It removes the provider static pod and local files, then restores OpenBao into a
fresh raft volume. After it rehydrates the provider files, it restarts the
provider and API server. The test verifies Kubernetes readback before and after
the restore.

The `suites.yaml` manifest defines active and planned lanes. Concrete OpenBao
and Kubernetes versions remain centralized in `.ci/versions.yaml`.

Planned lanes:

- v0.1 release-gate coverage.
