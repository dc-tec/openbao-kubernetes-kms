# WS12 Implementation Security Review

This review covers the implementation-backed WS12 review tasks:

- `WS12-T07`: security review of key ID/AAD.
- `WS12-T08`: security review of socket handling.
- `WS12-T09`: security review of JWT/token lifecycle.
- `WS12-T10`: threat model review with implementation evidence.

The review is scoped to the implemented provider surfaces and their release-gate evidence. It is not a full production-readiness sign-off; the remaining WS12 supply-chain gates, kubeadm VM coverage, OpenBao HA coverage, and release evidence pack still need their own gates.

## Commands Run

```sh
go test ./internal/aad ./internal/keyregistry ./internal/kmsv2 ./internal/socket ./internal/auth ./internal/openbao ./internal/runtime ./internal/config ./internal/logging ./internal/metrics
go test ./internal/auth ./internal/config ./cmd/bao-kms-provider ./test/deployment
go test ./...
go test -tags=e2e -list . ./test/e2e
make ci-core
```

The targeted package tests, full default package tests, and `make ci-core` passed. `make ci-core` skipped optional tools that were not installed in the local environment (`gofumpt`, `staticcheck`, and `govulncheck`) according to the repository Makefile. The e2e command listed available release-evidence tests only; Docker, OpenBao, and Kind lanes were not executed in this review pass.

## Findings

| ID | Task | Severity | Status | Summary |
|---|---|---|---|---|
| `WS12-SR-001` | `WS12-T08` | High | Remediated | Runtime socket directory was group-writable in deployment docs/package snippets, allowing the socket access group to replace the socket path instead of only connecting to the socket. |
| `WS12-SR-002` | `WS12-T09` | Medium | Remediated | Token renewal used `loginBeforeTokenExpiry` as the OpenBao renewal increment, capping renewed leases to the refresh-ahead threshold. |
| `WS12-SR-003` | `WS12-T09` | Medium | Remediated | Startup status probe failed fast with no bootstrap grace for JWT projection, network/DNS, OpenBao restart, or clock-sync races. |
| `WS12-SR-004` | `WS12-T09` | Medium | Remediated | Auth refresh retry backoff was fixed at one second with no jitter, creating synchronized login pressure during outages. |
| `WS12-SR-005` | `WS12-T09` | Low | Remediated | Local JWT validation parsed issuer, audience, and subject but could not optionally enforce expected values for early misconfiguration diagnostics. |
| `WS12-SR-006` | `WS12-T09` | Low | Remediated | JWT login used the same timeout as Transit operations, even though login can need a longer TLS/auth/audit path than steady-state encrypt/decrypt. |
| `WS12-SR-007` | `WS12-T09` | Low | Remediated | Cancellation while waiting for a coalesced refresh was correct but not covered by tests. |
| `WS12-SR-008` | `WS12-T09` | Low | Deferred | OpenBao HTTP calls still make one HTTP attempt per logical operation; opt-in HTTP-layer retry for transient 5xx/connection reset should be considered separately from auth-manager retry policy. |

Secondary validation rubric:

- The claimed code path had to match the implementation.
- The impact had to cross a security, fail-closed, or control-plane availability boundary.
- Existing controls had to be absent or weaker than the claim.
- The remediation had to preserve secret-redaction and typed-config constraints.
- The review artifact had to record either a patch with test evidence or an explicit deferred disposition.

### WS12-SR-001: Socket Directory Group Write Allowed Socket Path Replacement

The package and deployment model intentionally separates the provider private group from the socket access group so kube-apiserver can connect to `kms.sock` without gaining JWT access. Before this review, the documented and packaged runtime directory mode was `2770`, making `/run/openbao-kms` writable by `openbao-kms-socket`.

That is broader than the intended boundary. A process with the socket access group should be able to connect to the socket file, but it should not be able to unlink, replace, or race the socket directory entry. Directory write access could support denial of service or endpoint impersonation if a same-group local process replaces `kms.sock` after provider startup or during restart. The existing stale-socket checks protect startup takeover from live peers, but they do not protect a bound socket path after another same-group writer unlinks and replaces the directory entry.

Resolution:

- Runtime socket parents are now rejected when group-writable or world-writable.
- Deployment docs, package snippets, and lab scripts now use `2750` for `/run/openbao-kms`.
- Static pod guidance now requires the provider user to own the socket directory, with the kube-apiserver socket group limited to directory traversal and socket-file access.
- Tests cover group-writable socket parent rejection.

Affected evidence:

- `internal/socket/listener.go`
- `internal/config/validation.go`
- `deploy/package/linux/tmpfiles.d/openbao-kms.conf`
- `deploy/systemd/openbao-kms.tmpfiles.conf`
- `hack/kubeadm/install-static-pod-lab.sh`
- `docs/deployment/linux-identity-model.md`
- `docs/deployment/systemd.md`
- `docs/deployment/static-pod.md`
- `docs/security/hardening.md`

## WS12-T07: Key ID/AAD Review

Disposition: no reportable finding from the reviewed implementation.

Evidence reviewed:

- `internal/keyregistry.KeySnapshot` includes provider name, cluster ID, OpenBao instance ID, Transit mount ID, Transit key lineage ID, Transit version, Transit version creation time, and optional key epoch.
- `DeriveKeyID` derives opaque `obk2.<base64url-sha256>` IDs from stable non-secret identity fields.
- `Normalize` verifies stored key IDs against derived key IDs.
- `Registry.Lookup` parses key IDs and rejects unknown IDs before returning a snapshot.
- `internal/aad` builds non-secret annotations, enforces required AAD mode for v0.1, validates annotation hashes against the resolved snapshot, and reconstructs canonical AAD.
- `internal/kmsv2.Decrypt` calls `aad.PrepareDecrypt` before Transit decrypt.

Test evidence:

- Key ID golden fixtures, identity-field sensitivity tests, parser fuzzing, registry unknown-key rejection, AAD golden fixtures, malformed annotation fuzzing, annotation tamper rejection, and KMS decrypt-before-Transit rejection tests are present and passed in the targeted run.

Residual notes:

- Annotation and AAD hash values are non-secret identifiers, not a secrecy boundary. Documentation should continue to avoid treating hashed low-entropy identity fields as confidential.
- The top-level KMS server decodes proto annotation bytes before `aad.PrepareDecrypt`; malformed annotation bytes can therefore fail before key ID parsing at the RPC adapter edge. This does not allow Transit calls before key ID validation, but the strict documented validation order should be interpreted as the `PrepareDecrypt` security boundary.

## WS12-T08: Socket Handling Review

Disposition: one finding identified and remediated in this review: `WS12-SR-001`.

Evidence reviewed:

- Socket path must be absolute.
- Socket mode rejects world bits and execute bits.
- Parent directory must exist, must not be a symlink, must be a directory, and now must not be group-writable or world-writable.
- Existing target rejects symlinks, regular files, directories, and non-socket paths.
- Existing sockets are probed with bounded `net.DialTimeout`.
- Live sockets fail closed.
- Indeterminate probe failures fail closed.
- Only sockets proven dead by `ECONNREFUSED` are removed.
- Post-bind chmod/chown applies the configured socket mode and group.

Test evidence:

- Unit tests cover unsafe modes, missing parent, group/world-writable parent, symlink parent, symlink/regular/directory target, live collision, verified-dead stale reclaim, socket close unlink, and group ownership.
- Provider e2e inventory includes stale socket reclamation.

Residual notes:

- SELinux/AppArmor deployment notes and service restart behavior tests remain separate planned backlog items.
- Static pod deployments must prepare `/run/openbao-kms` as provider-owned. A group-writable directory should be treated as a deployment failure, not a convenience mode.

## WS12-T09: JWT/Token Lifecycle Review

Disposition: secondary review identified several lifecycle hardening issues. `WS12-SR-002` through `WS12-SR-007` are remediated; `WS12-SR-008` is deferred as a client-resilience follow-up rather than a current security gate.

Evidence reviewed:

- JWT file path must be absolute.
- JWT file must be regular, not a symlink, and mode must not include group write, execute bits, or world access.
- JWT file is checked before and after open with `os.SameFile` to reduce replacement races.
- JWT size is bounded and compact token whitespace is rejected.
- Local claim parsing requires `exp` and enforces `exp`, `nbf`, `iat`, minimum remaining TTL, and configured clock skew leeway.
- JWT signature verification is intentionally delegated to OpenBao JWT auth; local parsing is only for fail-fast expiry and diagnostics.
- Optional local issuer, audience, and subject checks can now fail fast on wrong JWT file/service-account wiring. These checks are diagnostics, not a replacement for OpenBao JWT role binding and signature verification.
- OpenBao client tokens are held in memory by `auth.Manager`.
- Re-login re-reads the JWT file.
- Renewal uses a separate `auth.tokenRenewalIncrement`, and falls back to JWT re-login when renewal fails.
- Auth refresh retry backoff is exponential with jitter and exposes consecutive failure count in redacted state.
- Concurrent refreshes are coalesced.
- A caller cancelled while waiting for a coalesced refresh returns `ctx.Err()` while the in-flight refresh can still update state for later callers.
- Auth state exposes TTLs and bounded status, not token/JWT material.
- OpenBao auth/client errors are classified and redacted.
- Startup retries the initial status probe within `bootstrap.graceTimeout` before exiting.
- JWT login can use `auth.loginTimeout`, defaulting to at least five seconds and independent from the Transit request timeout.

Test evidence:

- JWT fixture tests cover malformed, unsafe, expired, near-expiry, `nbf`, `iat`, leeway, and unreadable cases.
- Manager tests cover login, near-expiry rejection, JWT re-read before re-login, renewal increment, renewal-failure-to-relogin, short token lease rejection, exponential backoff behavior, concurrent initial login coalescing, cancellation while waiting for an in-flight refresh, and redacted unexpected errors.
- OpenBao auth tests cover JWT login, renew-self, and redacted login failures.
- Serve tests cover bootstrap grace retry/deadline behavior and derived auth login timeout.
- Provider e2e inventory includes expired JWT startup fail-closed behavior and JWT file rotation/re-login behavior.

Residual notes:

- Pure JWT validation cannot detect JWT revocation until expiry; this is documented in the design and mitigated through short JWT TTLs and issuer controls.
- Host-level compromise of the provider identity remains out of scope because the process necessarily holds the live OpenBao token in memory.
- HTTP-layer retry for transient OpenBao 5xx or connection reset remains deferred. The auth manager now reduces synchronized login pressure, but per-request Transit/auth retry policy should be designed as an explicit opt-in because retries can amplify load and must not retry deterministic 4xx responses.

## WS12-T10: Threat Model Review With Implementation Evidence

Disposition: threat model remains aligned after adding the socket path replacement control.

Evidence mapping:

| Threat model control | Implementation evidence | Test/evidence status |
|---|---|---|
| Deterministic, scoped, non-secret key IDs | `internal/keyregistry`, `internal/config.IdentityFingerprint` | Golden, sensitivity, parser, registry, and KMS tests passed. |
| Metadata binding through AAD | `internal/aad`, `internal/kmsv2`, `internal/openbao` | AAD golden, tamper, required-mode, Transit request tests passed. |
| Unknown key ID rejection before Transit | `Registry.Lookup`, `aad.PrepareDecrypt`, `kmsv2.Decrypt` | KMS tests prove malformed/unknown key IDs do not reach fake Transit. |
| Socket boundary | `internal/socket`, `internal/runtime`, deployment docs | Unit tests passed; `WS12-SR-001` remediated group-writable directory risk. |
| JWT theft controls | `internal/auth`, config filesystem validation, hardening docs | JWT and manager tests passed. |
| OpenBao token theft controls | `auth.Manager`, `openbao.TokenSource`, logging/redaction helpers | Token stays in memory; renewal and redaction tests passed in targeted packages. |
| OpenBao outage fail-closed behavior | `internal/status`, `internal/kmsv2`, e2e failure inventory | Targeted unit tests passed; e2e failure lanes listed but not run here. |
| Log/metric leakage controls | `internal/logging`, `internal/metrics`, KMS/OpenBao observers | Targeted logging/metrics tests passed. |

Remaining production-readiness gaps are already represented elsewhere in the backlog:

- OpenBao HA failover behavior tests.
- kubeadm VM e2e.
- SELinux/AppArmor deployment notes after testing.
- Full supply-chain gates: SBOM, vulnerability scan, license check, signatures, attestations, reproducibility, and release evidence pack.

## Review Outcome

`WS12-T07`, `WS12-T08`, `WS12-T09`, and `WS12-T10` are review-complete for the current implementation snapshot, with socket-handling and JWT/token lifecycle findings remediated during the review. `WS12-SR-008` remains a deferred OpenBao client-resilience follow-up and does not close the broader WS12 release-hardening workstream.
