# E2E Tests

This directory contains the Ginkgo/Gomega E2E suite.

The default OpenBao E2E target uses ephemeral OpenBao CI environments with Transit and JWT auth bootstrap. It runs the Ginkgo Transit/JWT assertions, including role claim binding and pinned signing-key rollover, and the provider full-stack OpenBao/KMS v2 socket test:

```sh
make test-e2e-openbao-ci
```

Run only the provider full-stack slice with:

```sh
make test-e2e-provider-openbao-ci
```

That target builds `E2E_PROVIDER_IMAGE` and runs the provider plus KMS v2 socket client in Docker containers. Set `E2E_PROVIDER_BUILD=false` to test a prebuilt image tag.

Run only the OpenBao certificate auth slice with:

```sh
make test-e2e-cert-auth-openbao-ci
```

That target runs real OpenBao with a TLS listener that requests client
certificates. It configures the OpenBao cert auth method with a URI SAN-bound
role, logs in through cert auth, and verifies Transit access with the issued
token. It does not replace provider E2E coverage for PKCS#11 source
availability.

Run the provider certificate-source lanes with:

```sh
make test-e2e-provider-certauth-sources-openbao-ci
```

That target runs the supported source-specific provider check. The PKCS#11 lane
builds an E2E image with SoftHSM, creates a real PKCS#11 key pair and client
certificate, configures OpenBao cert auth with the generated CA, and runs the
KMS v2 socket client through the provider.

Run the SPIFFE source implementation check explicitly with:

```sh
make test-e2e-provider-certauth-spiffe-openbao-ci
```

That lane starts real SPIRE server and agent containers, registers the provider
UID selector, fetches a real X.509 SVID from the Workload API, and validates it
through the provider SPIFFE certificate source code. It is not wired into CI or
the preview release gate, and it is not a support claim until the OpenBao
cert-auth identity alias behavior is compatible with URI-SAN-only SVIDs.

Run only the provider CLI slice with:

```sh
make test-e2e-provider-cli-openbao-ci
```

That target runs the real provider image CLI against mounted provider config, TLS/JWT files, local state, and real OpenBao. It covers `doctor`, `verify-key`, `rotation-plan`, `verify-rotation`, `config`, and `policy openbao`, plus JWT claim drift redaction, unsupported Transit key type diagnostics, and missing-state-after-rotation fail-closed behavior.

Run only the provider/OpenBao failure-mode slice with:

```sh
make test-e2e-provider-failure-openbao-ci
```

That target uses real OpenBao Transit/JWT auth and the real provider image. It covers OpenBao down, OpenBao sealed, reduced provider policy with `PermissionDenied` KMS errors, expired JWT and expected-claim drift startup fail-closed behavior, JWT file rotation and re-login, provider re-login after signing-key rollover, missing Transit key startup fail-closed behavior, Status staleness, and stale socket reclamation.

Run only the provider/OpenBao HA failover slice with:

```sh
make test-e2e-provider-ha-openbao-ci
```

That target starts three OpenBao integrated-raft nodes, points the provider at a standby node, writes encrypted data, removes the active OpenBao node, waits for a surviving voter to become active, and verifies old ciphertext readback plus new KMS operations through the same provider.

Run only the provider decrypt storm smoke slice with:

```sh
make test-e2e-provider-decrypt-storm-openbao-ci
```

That target performs concurrent KMS v2 Decrypt calls through the provider against real OpenBao. It is a smoke test, not a replacement for release-candidate load testing.

Run only the sustained direct decrypt soak slice with:

```sh
make test-e2e-provider-decrypt-soak-openbao-ci
```

That target prepares a fixed corpus of encrypted samples, sustains concurrent KMS v2 Decrypt calls through the real provider/OpenBao path, requires zero client-visible errors, enforces operation-count and p95-latency thresholds, and compares Docker memory/PID counts before and after the run.

Run only the provider/OpenBao load-soak slice with:

```sh
make test-e2e-provider-load-soak-openbao-ci
```

That target runs sustained Status, Encrypt, and Decrypt calls through the real provider/OpenBao path, requires zero client-visible errors, enforces operation-count and p95-latency thresholds, and compares Docker memory/PID counts before and after the run.

Run only the provider/OpenBao backend replacement and DR restore slice with:

```sh
make test-e2e-provider-restore-openbao-ci
```

That target runs OpenBao with integrated raft storage in Docker. It verifies provider fail-closed behavior while the backend is down, replacement of the backend under the same Docker network name, raft snapshot save/restore into a fresh storage volume, and decrypt of ciphertext created before the outage or restore.

Run only the provider/OpenBao Transit rotation slice with:

```sh
make test-e2e-provider-rotation-openbao-ci
```

That target runs OpenBao with integrated raft storage in Docker. It writes ciphertext on the initial Transit version, saves a pre-rotation raft snapshot, rotates the Transit key, waits for provider Status to promote a new `key_id`, verifies old and new ciphertext decrypt, rejects Transit `min_decryption_version` changes that strand retained historical versions, fails closed when local provider state disappears after rotation, restores the pre-rotation snapshot, and verifies the provider rejects the observed Transit version rollback.

Run only the provider binary upgrade/rollback slice with:

```sh
make test-e2e-provider-upgrade-rollback-openbao-ci
```

That target builds distinct old/new provider images, verifies their version metadata differs, encrypts through the old image, upgrades the same provider state volume to the new image, reads old ciphertext, encrypts new ciphertext, rolls back to the old image, and verifies both ciphertexts remain decryptable.

Run the pinned Kubernetes Kind smoke lane with:

```sh
make test-e2e-kind-smoke
```

That target builds and loads the provider image into a pinned Kind cluster, deploys the provider as a static pod, enables kube-apiserver KMS v2 encryption, creates and reads a Secret, verifies raw etcd storage uses the KMS v2 envelope, restarts kube-apiserver, and reads the Secret again.

Run the pinned Kubernetes Kind multi-control-plane convergence lane with:

```sh
make test-e2e-kind-convergence
```

That target creates a three-control-plane Kind cluster, stages the provider on each control-plane node, enables KMS v2 on each kube-apiserver, verifies the Secret is KMS-enveloped in each stacked etcd member, temporarily leaves each kube-apiserver as the only serving API endpoint to prove decrypt convergence, then restarts each kube-apiserver and reads the Secret again.

Run the pinned Kubernetes Kind static-pod upgrade/rollback lane with:

```sh
make test-e2e-kind-upgrade-rollback
```

That target mutates the provider static pod manifest, waits for kubelet to restart the provider, verifies old and new Secrets remain readable, restores the previous static pod manifest, and verifies readback after provider and kube-apiserver restart.

Run the pinned Kubernetes Kind DR restore runbook lane with:

```sh
make test-e2e-kind-dr-runbook
```

That target creates an encrypted Secret, saves an OpenBao raft snapshot and provider node config/TLS/JWT/state backup, removes the provider static pod and local files, restores OpenBao into a fresh raft volume, rehydrates the provider node files, restarts the provider and API server, and verifies Kubernetes readback before and after restore.

The suite manifest is `suites.yaml`. It defines active and planned lanes, while concrete OpenBao and Kubernetes versions remain centralized in `.ci/versions.yaml`.

Planned lanes:

- v0.1 release-gate coverage.
