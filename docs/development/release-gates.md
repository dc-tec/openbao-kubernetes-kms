---
title: "Release Gates"
description: "v0.1 engineering preview gates, production readiness gates, CI tier breakdown, and manual pre-production validation steps."
weight: 60
---

# Release Gates

This page turns the [Testing](/development/testing/) strategy into release criteria.

## v0.1 Engineering Preview

Required before a v0.1 engineering-preview release:

1. KMS v2 fake conformance suite passes.
2. Hermetic OpenBao client integration suite passes.
3. Ephemeral OpenBao `2.5.3` CI e2e suite passes.
4. Pinned Kubernetes `1.34.3` Kind e2e proves Secret encryption and decryption work.
5. `kube-apiserver` restart with an encrypted Secret works.
6. The `Status.key_id == EncryptResponse.key_id` invariant is tested.
7. Unknown `key_id` decrypt is rejected before the Transit call.
8. AAD mismatch decrypt is rejected.
9. Transit encrypt uses explicit `key_version`.
10. Rotation from Transit version 1 to 2 works without `key_id` flip-flop.
11. Old ciphertext remains decryptable after rotation, new ciphertext uses the promoted `key_id`, and Transit version rollback is rejected.
12. JWT expiry, re-login, role claim binding, and signing-key rollover paths work.
13. OpenBao outage fails closed.
14. Provider backend replacement under a stable OpenBao endpoint fails closed during outage and decrypts existing ciphertext after recovery.
15. Containerized OpenBao integrated raft snapshot restore decrypts ciphertext created before restore.
16. Kind multi-control-plane convergence proves each API server can decrypt through its node-local provider.
17. Kind static-pod upgrade and rollback preserve decrypt compatibility, and provider binary upgrade and rollback with distinct images preserve old and new ciphertext readback.
18. Provider and OpenBao load-soak sustains Status, Encrypt, and Decrypt with bounded errors, latency, memory growth, and PID growth.
19. Kind DR runbook restores OpenBao raft data, rehydrates provider state and configuration, and proves Kubernetes Secret readback after replacement.
20. systemd and static-pod install scripts stage expected files and permissions.
21. `bao-kms-provider doctor` catches bad socket, bad JWT, bad policy, and bad Transit key configuration.
22. Logs and metrics redaction tests pass.
23. The static-pod manifest does not rely on Kubernetes API objects.
24. The systemd unit starts the plugin before kubelet in a kubeadm-style test.
25. Decrypt micro-batching is implemented behind configuration and benchmarked against the direct path.
26. The central CI version manifest pins OpenBao and Kubernetes test versions.
27. SBOM, vulnerability scan, license check, and vendored dependency policy pass.

v0.1 must be described as engineering preview, not production-ready. See [Reference: Support Policy](/reference/support-policy/).

## Production Readiness

Required before any production-ready claim:

- Kubernetes exact-pinned version matrix tested,
- OpenBao exact-pinned version matrix tested,
- multi-control-plane e2e tested,
- kubeadm VM e2e tested,
- systemd deployment tested,
- static-pod deployment tested,
- OpenBao HA failover tested,
- startup decrypt storm tested,
- disaster recovery restore tested,
- upgrade and rollback tested,
- security review completed,
- SBOM and vulnerability scan available,
- release artifacts signed,
- provenance attestations verified against the expected workflow identity,
- byte reproducibility check passes for published images and SBOMs,
- release evidence pack published or retained,
- documentation reviewed against implementation.

For the full supply-chain gate set see [CI And Supply Chain: Supply-Chain Gates](/development/ci-supply-chain/#supply-chain-gates).

## CI Tiers

### Every PR

- unit tests,
- KMS v2 fake conformance tests,
- configuration validation tests,
- redaction tests,
- key ID and AAD golden tests,
- `gofmt`, `go vet`, `staticcheck`,
- vendored dependency verification,
- dependency license check,
- static security scan,
- race tests for internal packages where feasible,
- fuzz smoke tests for parsers.

Target: under 10 minutes.

### Main Branch Or Nightly

- OpenBao `2.5.3` CI e2e tests,
- pinned Kubernetes `1.34.3` Kind e2e,
- pinned Kubernetes `1.34.3` Kind convergence e2e,
- pinned Kubernetes `1.34.3` static-pod upgrade and rollback e2e,
- rotation tests,
- failure injection tests,
- JWT role-claim rejection and signing-key rollover tests,
- OpenBao HA failover tests,
- OpenBao backend replacement and raft restore tests,
- provider and OpenBao load-soak tests,
- Kind DR restore runbook tests,
- performance smoke tests,
- container image scan,
- SBOM generation,
- provenance smoke checks where applicable.

### Release Candidate

- exact-pinned Kubernetes version matrix,
- exact-pinned OpenBao version matrix,
- local-only Harvester kubeadm production gate,
- local-only Harvester multi-control-plane kubeadm recovery gate,
- systemd deployment tests,
- static-pod deployment tests,
- paired OpenBao, provider state, and etcd restore in the Harvester lab,
- OpenBao HA failover tests,
- disaster recovery tests,
- startup decrypt storm test,
- upgrade and rollback test,
- release artifact signing and verification,
- byte reproducibility,
- provenance index generation.

## Manual Pre-Production Validation

For a real environment:

1. Run `bao-kms-provider doctor` on every control-plane node.
2. Verify the same Status `key_id` hash across all API servers.
3. Create, read, restart, and read a test Secret.
4. Confirm etcd does not contain plaintext test data.
5. Rotate the Transit key in a test environment.
6. Perform storage migration.
7. Restore an OpenBao and etcd backup pair in a lab.
8. Simulate an OpenBao outage.
9. Simulate JWT expiry and planned signing-key rollover.

The local Harvester gate is the closest pre-production harness in this
repository:

```sh
make harvester-lab-production-gate
```

Set `HARVESTER_ENABLE_MULTI_CONTROL_PLANE=true` before rendering the Harvester
values to include the multi-control-plane recovery topology in that gate. It
must stay outside public CI because it reboots VMs and intentionally stops
OpenBao inside the lab.

JWT role-claim rejection and pinned signing-key rollover remain portable
OpenBao/provider CI coverage, not Harvester lab coverage.
