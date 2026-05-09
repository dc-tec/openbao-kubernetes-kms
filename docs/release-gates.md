# Release Gates

This document turns the testing strategy into release criteria.

## v0.1 Engineering Preview

Required before a v0.1 engineering-preview release:

1. KMS v2 fake conformance suite passes.
2. Hermetic OpenBao client integration suite passes.
3. Ephemeral OpenBao `2.5.3` CI e2e suite passes.
4. Pinned Kubernetes `1.34.3` kind e2e proves Secret encryption/decryption works.
5. kube-apiserver restart with encrypted Secret works.
6. `Status.key_id == EncryptResponse.key_id` invariant is tested.
7. Unknown `key_id` decrypt is rejected before Transit call.
8. AAD mismatch decrypt is rejected.
9. Transit encrypt uses explicit `key_version`.
10. Rotation from Transit version 1 to 2 works without key ID flip-flop.
11. Old ciphertext remains decryptable after rotation, new ciphertext uses the promoted key ID, and Transit version rollback is rejected.
12. JWT expiry and re-login path works.
13. OpenBao outage fails closed.
14. Provider backend replacement under a stable OpenBao endpoint fails closed during outage and decrypts existing ciphertext after recovery.
15. Containerized OpenBao integrated raft snapshot restore decrypts ciphertext created before restore.
16. Kind multi-control-plane convergence proves each API server can decrypt through its node-local provider.
17. Kind static-pod upgrade and rollback preserve decrypt compatibility, and provider binary upgrade/rollback with distinct images preserves old/new ciphertext readback.
18. systemd and static-pod install scripts stage expected files and permissions.
19. `doctor` catches bad socket, bad JWT, bad policy, and bad Transit key config.
20. Logs and metrics redaction tests pass.
21. Static pod manifest does not rely on API objects.
22. systemd unit starts plugin before kubelet in kubeadm-style test.
23. Decrypt micro-batching is implemented behind config and benchmarked against the direct path.
24. Central CI version manifest pins OpenBao and Kubernetes test versions.
25. SBOM, vulnerability scan, license check, and vendored dependency policy pass.

v0.1 must be described as engineering preview, not production-ready.

## Production Readiness

Required before production-ready claims:

- Kubernetes exact-pinned version matrix tested.
- OpenBao exact-pinned version matrix tested.
- Multi-control-plane e2e tested.
- kubeadm VM e2e tested.
- systemd deployment tested.
- static pod deployment tested.
- OpenBao HA failover tested.
- startup decrypt storm tested.
- disaster recovery restore tested.
- upgrade and rollback tested.
- security review completed.
- SBOM and vulnerability scan available.
- release artifacts signed.
- provenance attestations verified against expected workflow identity.
- byte reproducibility check passes for published images and SBOMs.
- release evidence pack is published or retained.
- documentation reviewed against implementation.

## CI Tiers

### Every PR

- unit tests,
- KMS v2 fake conformance tests,
- config validation tests,
- redaction tests,
- key ID/AAD golden tests,
- gofmt/go vet/staticcheck,
- vendored dependency verification,
- dependency license check,
- static security scan,
- race tests for internal packages where feasible,
- fuzz smoke tests for parsers.

Target: under 10 minutes.

### Main Branch Or Nightly

- OpenBao `2.5.3` CI e2e tests,
- pinned Kubernetes `1.34.3` kind e2e,
- pinned Kubernetes `1.34.3` kind convergence e2e,
- pinned Kubernetes `1.34.3` static-pod upgrade/rollback e2e,
- rotation tests,
- failure injection tests,
- OpenBao backend replacement and raft restore tests,
- performance smoke tests,
- container image scan,
- SBOM generation,
- provenance smoke checks where applicable.

### Release Candidate

- exact-pinned Kubernetes version matrix,
- exact-pinned OpenBao version matrix,
- kubeadm VM tests,
- systemd deployment tests,
- static pod deployment tests,
- OpenBao HA failover tests,
- disaster recovery tests,
- startup decrypt storm test,
- upgrade and rollback test.
- release artifact signing and verification,
- byte reproducibility,
- provenance index generation.

## Manual Pre-Production Validation

For a real environment:

1. Run `doctor` on every control-plane node.
2. Verify the same Status key ID hash across all API servers.
3. Create/read/restart/read a test Secret.
4. Confirm etcd does not contain plaintext test data.
5. Rotate Transit key in a test environment.
6. Perform storage migration.
7. Restore OpenBao and etcd backup pair in a lab.
8. Simulate OpenBao outage.
9. Simulate JWT expiry.
