---
title: Failure modes
description: "Each failure the design considers, how it shows up, whether it blocks API server startup, and whether it can lose data."
eyebrow: Architecture
weight: 50
verifiedBy:
  - test/e2e/provider_failure_test.go
  - test/e2e/kind_dr_test.go
  - internal/kmsv2
---

Each row names a failure, its cause, its impact, and how you detect it. Two
columns flag severity: whether the failure stops the API server from
decrypting existing data at startup, and whether it can leave resources
permanently unrecoverable. For recovery steps, see
[Operate: Troubleshooting](/docs/operate/troubleshooting/) and
[Operate: Disaster recovery](/docs/operate/disaster-recovery/).

## Bootstrap and runtime

| Failure mode | Cause | Impact | Detection | Blocks startup | Data loss |
|---|---|---|---|---|---|
| Provider unavailable | Service not installed, crash, disabled | API server cannot reach KMS | systemd or kubelet status, socket missing, KMS unhealthy | Yes | No |
| Socket unavailable | Directory missing, listener failed | API server cannot call KMS | API server logs, `/live` failure | Yes | No |
| Kubelet or container runtime unavailable for static pod | Host boot failure | Provider static pod cannot start | kubelet or CRI logs | Yes | No |
| Provider bootstrap incomplete | API server starts before the KMS path is ready despite process-start ordering | API server fails or retries | Boot logs and `/ready` | Yes | No |
| Stale socket | Crash left socket path | Startup failure or wrong listener | Socket check | Yes | No |
| Wrong socket permissions | API server cannot connect | KMS unavailable | API server permission errors | Yes | No |
| SELinux or AppArmor block | Host policy denies socket or file | KMS unavailable | Audit logs | Yes | No |
| Configuration file permissions unsafe | World-readable or world-writable | Secret or topology exposure or tamper | Startup validation | Yes | No |
| Provider crash loop | Bug, bad configuration, OpenBao error path | KMS unavailable | Service logs | Yes | No |
| Image unavailable for static pod | Pull failure, air gap | Provider not started | kubelet events or logs | Yes | No |
| Package upgrade restarts systemd provider | Maintenance event | Transient KMS outage | Service logs | Possible | No |

## OpenBao and Transit

| Failure mode | Cause | Impact | Detection | Blocks startup | Data loss |
|---|---|---|---|---|---|
| OpenBao unavailable | Network, DNS, load balancer, outage | Encrypt and decrypt fail | Readiness, metrics, OpenBao request errors | Yes for encrypted data | No |
| OpenBao sealed | Manual seal, restart not unsealed | Transit unavailable | OpenBao health, provider readiness | Yes | No, unless key unavailable permanently |
| OpenBao inside same protected cluster | Circular dependency | KMS unavailable before API server | Bootstrap failure | Yes | Possible if unrecoverable |
| Audit backend pressure | OpenBao audit device slow or failing | Transit latency or errors | OpenBao metrics, provider latency | Possible | No |
| OpenBao leader failover | HA event | Transient errors or latency | OpenBao status, provider retries | Possible | No |
| TLS certificate expired | Certificate not renewed | Provider cannot connect | TLS errors | Yes | No |
| DNS or LB misrouting | Wrong backend or stale DNS | Auth or Transit errors | TLS or SNI errors, metadata mismatch | Yes | No |

## Transit key material

| Failure mode | Cause | Impact | Detection | Blocks startup | Data loss |
|---|---|---|---|---|---|
| Transit key deleted | Destructive admin action | Old ciphertext undecryptable | Metadata read fails, decrypt failures | Yes | Yes if no valid backup |
| Transit key soft-deleted | Key archived or disabled | Encrypt and decrypt fail | Metadata state, decrypt errors | Yes | No if restored |
| Transit key recreated same name | Key lineage lost | Old data undecryptable; `key_id` collision risk | Lineage mismatch, decrypt failures | Yes | Yes if original key lost |
| `disable_upsert` false or unreadable | Mount drift or missing read permission | Status and Encrypt fail closed | Metadata probe error, unhealthy Status | Yes | No |
| `min_decryption_version` raised too early | Operator error | Old ciphertext undecryptable | Decrypt failures for old `key_id` values | Yes | Possible |
| Key backup missing | Disaster restore lacks Transit key versions | Data undecryptable | DR test failure | Yes | Yes |

## Authentication and issuer state

| Failure mode | Cause | Impact | Detection | Blocks startup | Data loss |
|---|---|---|---|---|---|
| JWT expired and API server down | Protected cluster issued JWT and cannot renew | OpenBao login fails | Auth metrics, JWT expiry check | Yes | No |
| JWT file missing | Provisioning error | Login impossible | Startup validation | Yes | No |
| JWT wrong audience | Issuer or configuration mismatch | Login denied | Auth error | Yes | No |
| JWT wrong subject or claims | Role mismatch | Login denied | Auth error | Yes | No |
| Issuer changed | OIDC or JWT issuer rotation | Login denied | Auth logs | Yes | No |
| JWKS rotated | New signing key unknown | Login denied | JWT auth errors | Yes | No |
| OpenBao cannot reach JWKS or OIDC discovery | Network failure | Login denied or cache expiry | Auth errors | Possible | No |
| Clock skew | Host, OpenBao, or issuer clocks differ | JWT invalid | Auth errors, NTP alerts | Yes | No |
| Revoked JWT still cryptographically valid | JWT auth lacks TokenReview | Token may be accepted until expiry | Hard to detect | No immediate | No |
| Certificate expired | Certificate or SVID not renewed | OpenBao cert login fails | Auth metrics, certificate TTL metric | Yes | No |
| Certificate identity drift | Wrong certificate, SPIFFE ID, or trust domain | Local validation or OpenBao role rejects login | Auth error | Yes | No |
| PKCS#11 module or token unavailable | Module path, token label, key label, PIN, or hardware failure | Login impossible | Startup validation, auth error | Yes | No |
| SPIFFE Workload API unavailable | SPIFFE agent or socket unavailable | Login impossible | Auth error, certificate TTL metric approaches zero | Yes | No |

## KMS contract, registry, and decrypt

| Failure mode | Cause | Impact | Detection | Blocks startup | Data loss |
|---|---|---|---|---|---|
| Status `key_id` differs from encrypt response | Race or bug | API server discards encrypt result, marks unhealthy | API server logs, provider metrics | Possible | No |
| `key_id` flip-flops | Unstable rotation observation | Stale marking oscillates | Metrics or logs hash changes | Possible | No |
| Unknown `key_id` on decrypt | Configuration or provider changed, old data | Decrypt rejected | Decrypt `key_id` errors | Yes for affected data | Possible |
| Registry state missing | State file removed or first startup after restore | Provider may need to rebuild snapshots before serving | `doctor`, `verify-key`, `rotation-plan`, startup logs with auto-bootstrap eligibility reason | Possible | No |
| Registry state corrupt or rolled back | Disk corruption, unsafe restore, replayed file | Startup or probe fails closed | State load errors, hash mismatch, rollback error | Yes | No |
| Registry state and checkpoint both replaced | Privileged host compromise or unsafe full-directory restore | Local replay guard can be bypassed by a self-consistent pair | Node comparison, backup metadata, host integrity monitoring | Possible | Possible |
| Missing or malformed annotations | Bug, corruption, or mismatched provider | Decrypt rejected when AAD required | AAD validation metrics | Yes for affected data | No if recoverable backup exists |
| AAD mismatch | Wrong cluster, key, or provider metadata | Decrypt rejected | AAD error | Yes for affected data | Possible |
| API server decrypt storm | Startup with many encrypted objects | Latency, timeouts, or `ResourceExhausted` above the Decrypt limit | Duration and in-flight metrics, request status, API server logs | Yes if severe | No |
| OpenBao response exceeds its body limit | Backend fault, proxy fault, or unexpected response growth | Request fails as unavailable; metadata or deep-probe responses also make Status unhealthy | OpenBao request error metrics and provider logs | Possible | No |
| Provider name changed | `EncryptionConfiguration` drift | Old encrypted data may not match provider | API server errors | Yes for affected data | Possible |

## Kubernetes encryption scope and migration

| Failure mode | Cause | Impact | Detection | Blocks startup | Data loss |
|---|---|---|---|---|---|
| `identity` fallback left enabled permanently | Migration incomplete | Plaintext writes possible if provider order changes or KMS is unavailable with `identity` first | Configuration audit | No | Confidentiality loss |
| `identity` fallback removed too early | Old plaintext or misordered data | Reads may fail depending on provider set | API errors | Possible | No |
| Only some resources encrypted | Configuration scope incomplete | Unprotected resources in etcd | Encryption configuration review | No | Confidentiality loss |
| Existing resources not rewritten | Encryption only applies on write | Old data remains under old provider or plaintext | Audit and migration checks | No | Confidentiality loss |
| Mixed plaintext and encrypted backups | Backups taken across migration | Inconsistent confidentiality | Backup audit | No | Confidentiality loss |
