# E2E Tests

This directory contains the Ginkgo/Gomega E2E suite.

The default OpenBao E2E target uses ephemeral OpenBao CI environments with Transit and JWT auth bootstrap. It runs the Ginkgo Transit/JWT assertions and the provider full-stack OpenBao/KMS v2 socket test:

```sh
make test-e2e-openbao-ci
```

Run only the provider full-stack slice with:

```sh
make test-e2e-provider-openbao-ci
```

That target builds `E2E_PROVIDER_IMAGE` and runs the provider plus KMS v2 socket client in Docker containers. Set `E2E_PROVIDER_BUILD=false` to test a prebuilt image tag.

Run only the provider/OpenBao failure-mode slice with:

```sh
make test-e2e-provider-failure-openbao-ci
```

That target uses real OpenBao Transit/JWT auth and the real provider image. It covers OpenBao down, OpenBao sealed, reduced provider policy, expired JWT startup fail-closed behavior, JWT file rotation and re-login, missing Transit key startup fail-closed behavior, Status staleness, and stale socket reclamation.

Run only the provider decrypt storm smoke slice with:

```sh
make test-e2e-provider-decrypt-storm-openbao-ci
```

That target performs concurrent KMS v2 Decrypt calls through the provider against real OpenBao. It is a smoke test, not a replacement for release-candidate load testing.

Run only the provider/OpenBao backend replacement and DR restore slice with:

```sh
make test-e2e-provider-restore-openbao-ci
```

That target runs OpenBao with integrated raft storage in Docker. It verifies provider fail-closed behavior while the backend is down, replacement of the backend under the same Docker network name, raft snapshot save/restore into a fresh storage volume, and decrypt of ciphertext created before the outage or restore.

Run only the provider/OpenBao Transit rotation slice with:

```sh
make test-e2e-provider-rotation-openbao-ci
```

That target runs OpenBao with integrated raft storage in Docker. It writes ciphertext on the initial Transit version, saves a pre-rotation raft snapshot, rotates the Transit key, waits for provider Status to promote a new `key_id`, verifies old and new ciphertext decrypt, restores the pre-rotation snapshot, and verifies the provider rejects the observed Transit version rollback.

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

The suite manifest is `suites.yaml`. It defines active and planned lanes, while concrete OpenBao and Kubernetes versions remain centralized in `.ci/versions.yaml`.

Planned lanes:

- v0.1 release-gate coverage.
