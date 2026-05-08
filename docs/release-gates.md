# Release Gates

This document turns the testing strategy into release criteria.

## v0.1 Engineering Preview

Required before a v0.1 engineering-preview release:

1. KMS v2 fake conformance suite passes.
2. Real OpenBao `2.5.3` integration suite passes.
3. Pinned Kubernetes `1.34.x` kind e2e proves Secret encryption/decryption works.
4. kube-apiserver restart with encrypted Secret works.
5. `Status.key_id == EncryptResponse.key_id` invariant is tested.
6. Unknown `key_id` decrypt is rejected before Transit call.
7. AAD mismatch decrypt is rejected.
8. Transit encrypt uses explicit `key_version`.
9. Rotation from Transit version 1 to 2 works without key ID flip-flop.
10. Old ciphertext remains decryptable after rotation.
11. JWT expiry and re-login path works.
12. OpenBao outage fails closed.
13. `doctor` catches bad socket, bad JWT, bad policy, and bad Transit key config.
14. Logs and metrics redaction tests pass.
15. Static pod manifest does not rely on API objects.
16. systemd unit starts plugin before kubelet in kubeadm-style test.
17. Decrypt micro-batching is implemented behind config and benchmarked against the direct path.
18. Central CI version manifest pins OpenBao and Kubernetes test versions.
19. SBOM, vulnerability scan, license check, and vendored dependency policy pass.

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

- OpenBao `2.5.3` integration tests,
- pinned Kubernetes `1.34.x` kind e2e,
- rotation tests,
- failure injection tests,
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
