---
title: "Failure Modes"
description: "Comprehensive catalog of failure modes the design considers: cause, impact, detection signals, mitigation, recovery, startup blocking, and data loss risk."
weight: 50
---

# Failure Modes

This page is the comprehensive failure-mode catalog the design considers. Each row pairs a failure scenario with its detection signal, mitigation, and recovery action. For the operator runbooks that act on these signals see [Operations: Troubleshooting](/operations/troubleshooting/) and [Operations: Disaster Recovery](/operations/disaster-recovery/).

## How To Use This Page

Each row in the catalog answers four questions:

- What goes wrong?
- How does an operator detect it?
- What design control mitigates it?
- What recovery action restores service?

Two columns flag operational severity:

- **Blocks API server startup?** indicates whether the API server cannot decrypt previously encrypted resources during startup if this failure is active.
- **Permanent data loss risk?** indicates whether the failure can leave Kubernetes resources unrecoverable.

## Bootstrap And Runtime

| Failure mode | Cause | Impact | Detection | Control | Recovery | Startup block? | Data loss risk |
|---|---|---|---|---|---|---|---|
| Plugin unavailable | Service not installed, crash, disabled | API server cannot reach KMS | systemd or kubelet status, socket missing, KMS unhealthy | Restart policy, health checks | Start plugin, fix configuration | Yes | No |
| Socket unavailable | Directory missing, listener failed | API server cannot call KMS | API server logs, `/live` failure | Pre-created runtime directory, safe socket setup | Fix path or permissions, restart plugin | Yes | No |
| Kubelet or container runtime unavailable for static pod | Host boot failure | Plugin static pod cannot start | kubelet or CRI logs | systemd mode, local image cache | Fix kubelet or CRI, or run plugin as host service | Yes | No |
| systemd ordering wrong | Plugin starts after API server | API server fails or retries | Boot logs | `Before=kubelet.service` where appropriate, tested units | Correct unit dependencies | Yes | No |
| Stale socket | Crash left socket path | Startup failure or wrong listener | Socket check | Safe stale cleanup | Remove verified-dead socket | Yes | No |
| Wrong socket permissions | API server cannot connect | KMS unavailable | API server permission errors | Mode and group validation | Fix group or mode | Yes | No |
| SELinux or AppArmor block | Host policy denies socket or file | KMS unavailable | Audit logs | Policy profiles, tests | Adjust policy | Yes | No |
| Configuration file permissions unsafe | World-readable or world-writable | Secret or topology exposure or tamper | Startup validation | Fail closed | Fix permissions | Yes | No |
| Plugin crash loop | Bug, bad configuration, OpenBao error path | KMS unavailable | Service logs | Supervisor, tests | Fix configuration or bug | Yes | No |
| Image unavailable for static pod | Pull failure, air gap | Plugin not started | kubelet events or logs | Preloaded image, `IfNotPresent` or `Never` | Load image | Yes | No |
| Package upgrade restarts systemd plugin | Maintenance event | Transient KMS outage | Service logs | Controlled rollout | Restart one node at a time | Possible | No |

## OpenBao And Transit

| Failure mode | Cause | Impact | Detection | Control | Recovery | Startup block? | Data loss risk |
|---|---|---|---|---|---|---|---|
| OpenBao unavailable | Network, DNS, load balancer, outage | Encrypt and decrypt fail | Readiness, metrics, OpenBao request errors | HA OpenBao, local routing, retries | Restore OpenBao reachability | Yes for encrypted data | No |
| OpenBao sealed | Manual seal, restart not unsealed | Transit unavailable | OpenBao health, plugin readiness | Auto-unseal, alerting | Unseal or restore OpenBao | Yes | No, unless key unavailable permanently |
| OpenBao inside same protected cluster | Circular dependency | KMS unavailable before API server | Bootstrap failure | External management plane | Start OpenBao independently or restore an external service | Yes | Possible if unrecoverable |
| Audit backend pressure | OpenBao audit device slow or failing | Transit latency or errors | OpenBao metrics, plugin latency | HA audit sinks, capacity planning | Repair audit backend | Possible | No |
| OpenBao leader failover | HA event | Transient errors or latency | OpenBao status, plugin retries | HA tuning, bounded retries | Wait or fix cluster | Possible | No |
| TLS certificate expired | Certificate not renewed | Plugin cannot connect | TLS errors | Certificate monitoring | Renew certificate, reload plugin | Yes | No |
| DNS or LB misrouting | Wrong backend or stale DNS | Auth or Transit errors | TLS or SNI errors, metadata mismatch | Pinned CA and SNI, instance ID checks | Fix DNS or load balancer | Yes | No |

## Transit Key Material

| Failure mode | Cause | Impact | Detection | Control | Recovery | Startup block? | Data loss risk |
|---|---|---|---|---|---|---|---|
| Transit key deleted | Destructive admin action | Old ciphertext undecryptable | Metadata read fails, decrypt failures | `deletion_allowed=false`, no delete permission | Restore OpenBao backup with key material | Yes | Yes if no valid backup |
| Transit key soft-deleted | Key archived or disabled | Encrypt and decrypt fail | Metadata state, decrypt errors | Change control | Restore key if possible | Yes | No if restored |
| Transit key recreated same name | Key lineage lost | Old data undecryptable; `key_id` collision risk | Lineage mismatch, decrypt failures | Key lineage ID, delete protection | Restore original key; do not accept new lineage | Yes | Yes if original key lost |
| `min_decryption_version` raised too early | Operator error | Old ciphertext undecryptable | Decrypt failures for old `key_id` values | Verify migration first | Lower setting if key versions still exist | Yes | Possible |
| Key backup missing | Disaster restore lacks Transit key versions | Data undecryptable | DR test failure | Coordinated OpenBao backups | Restore from valid backup | Yes | Yes |

## Authentication And Issuer State

| Failure mode | Cause | Impact | Detection | Control | Recovery | Startup block? | Data loss risk |
|---|---|---|---|---|---|---|---|
| JWT expired and API server down | Protected cluster issued JWT and cannot renew | OpenBao login fails | Auth metrics, JWT expiry check | External issuer, sufficient TTL, file refresh | Replace JWT from external issuer or restore enough API server function to issue a token | Yes | No |
| JWT file missing | Provisioning error | Login impossible | Startup validation | File checks, configuration management | Restore JWT file | Yes | No |
| JWT wrong audience | Issuer or configuration mismatch | Login denied | Auth error | Bound audience tests | Issue correct JWT or fix role | Yes | No |
| JWT wrong subject or claims | Role mismatch | Login denied | Auth error | Claim binding documentation | Issue correct JWT or fix role | Yes | No |
| Issuer changed | OIDC or JWT issuer rotation | Login denied | Auth logs | Planned overlap | Update OpenBao configuration and JWT source | Yes | No |
| JWKS rotated | New signing key unknown | Login denied | JWT auth errors | JWKS monitoring, overlapping keys | Refresh JWKS or OpenBao configuration | Yes | No |
| OpenBao cannot reach JWKS or OIDC discovery | Network failure | Login denied or cache expiry | Auth errors | Pinned public keys for recovery | Restore discovery or configure keys | Possible | No |
| Clock skew | Host, OpenBao, or issuer clocks differ | JWT invalid | Auth errors, NTP alerts | NTP or chrony, leeway | Fix clocks | Yes | No |
| Revoked JWT still cryptographically valid | JWT auth lacks TokenReview | Token may be accepted until expiry | Hard to detect | Short TTL, external issuer controls | Rotate JWT and signing keys if needed | No immediate | No |

## KMS Contract, Registry, And Decrypt

| Failure mode | Cause | Impact | Detection | Control | Recovery | Startup block? | Data loss risk |
|---|---|---|---|---|---|---|---|
| Status `key_id` differs from encrypt response | Race or bug | API server discards encrypt result, marks unhealthy | API server logs, plugin metrics | Snapshot consistency | Fix bug, restart with stable registry | Possible | No |
| `key_id` flip-flops | Unstable rotation observation | Stale marking oscillates | Metrics or logs hash changes | Stable observation count, activation delay | Pin active key, fix watcher | Possible | No |
| Unknown `key_id` on decrypt | Configuration or provider changed, old data | Decrypt rejected | Decrypt `key_id` errors | Preserve key history | Restore old configuration or registry | Yes for affected data | Possible |
| Registry state missing | State file removed or first startup after restore | Provider may need to rebuild snapshots before serving | `doctor`, `rotation-plan`, startup logs | Strict state file and checkpoint, rebuild only from complete available Transit metadata | Restore state file and checkpoint, or rebuild only when metadata completeness is proven | Possible | No |
| Registry state corrupt or rolled back | Disk corruption, unsafe restore, replayed file | Startup or probe fails closed | State load errors, hash mismatch, rollback error | State hash chain, checkpoint, permission checks, monotonic generation | Restore last valid state file and checkpoint or perform documented DR rebuild | Yes | No |
| Missing or malformed annotations | Old object, bug, corruption | Decrypt rejected when AAD required | AAD validation metrics | AAD required in the current release line; future compatibility must be explicit and bounded | Restore matching configuration or migrate with a release that supports the known legacy epoch | Yes for affected data | No if compatible recovery is possible |
| AAD mismatch | Wrong cluster, key, or provider metadata | Decrypt rejected | AAD error | Stable configuration, validation | Restore matching configuration | Yes for affected data | Possible |
| API server decrypt storm | Startup with many encrypted objects | Latency, timeouts | Duration metrics, API server logs | OpenBao capacity, local routing, soak evidence; batching remains future work | Scale OpenBao, tune timeout, reduce retries; implement batching only if release evidence requires it | Yes if severe | No |
| Provider name changed | `EncryptionConfiguration` drift | Old encrypted data may not match provider | API server errors | Immutable provider name | Restore old name or configure migration | Yes for affected data | Possible |

## Kubernetes Encryption Scope And Migration

| Failure mode | Cause | Impact | Detection | Control | Recovery | Startup block? | Data loss risk |
|---|---|---|---|---|---|---|---|
| `identity` fallback left enabled permanently | Migration incomplete | Plaintext writes possible if provider order changes or KMS is unavailable with `identity` first | Configuration audit | Remove after migration | Rewrite resources and remove fallback | No | Confidentiality loss |
| `identity` fallback removed too early | Old plaintext or misordered data | Reads may fail depending on provider set | API errors | Verify migration first | Restore fallback temporarily and migrate | Possible | No |
| Only some resources encrypted | Configuration scope incomplete | Unprotected resources in etcd | Encryption configuration review | Explicit resource list | Update configuration, rewrite resources | No | Confidentiality loss |
| Existing resources not rewritten | Encryption only applies on write | Old data remains under old provider or plaintext | Audit and migration checks | Storage migration | Rewrite resources | No | Confidentiality loss |
| Mixed plaintext and encrypted backups | Backups taken across migration | Inconsistent confidentiality | Backup audit | Backup labeling | Handle according to sensitivity | No | Confidentiality loss |
