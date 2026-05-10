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

## Catalog

| Failure mode | Cause | Impact | Detection | Mitigation | Recovery action | Blocks API server startup? | Permanent data loss risk? |
|---|---|---|---|---|---|---|---|
| Plugin unavailable | Service not installed, crash, disabled | API server cannot reach KMS | systemd or kubelet status, socket missing, KMS unhealthy | restart policy, health checks | Start plugin, fix configuration | Yes | No |
| Socket unavailable | Directory missing, listener failed | API server cannot call KMS | API server logs, `/live` failure | pre-create runtime directory, safe socket setup | Fix path or permissions, restart plugin | Yes | No |
| OpenBao unavailable | Network, DNS, load balancer, outage | Encrypt and decrypt fail | readiness, metrics, OpenBao request errors | HA OpenBao, local routing, retries | Restore OpenBao reachability | Yes for encrypted data | No |
| OpenBao sealed | Manual seal, restart not unsealed | Transit unavailable | OpenBao health, plugin readiness | auto-unseal, alerting | Unseal or restore OpenBao | Yes | No, unless key unavailable permanently |
| OpenBao inside same protected cluster | Circular dependency | KMS unavailable before API server | bootstrap failure | external management plane | Start OpenBao independently or restore an external service | Yes | Possible if unrecoverable |
| JWT expired and API server down | Protected cluster issued JWT and cannot renew | OpenBao login fails | auth metrics, JWT expiry check | external issuer, sufficient TTL, file refresh | Replace JWT from external issuer or restore enough API server function to issue a token | Yes | No |
| Kubelet or container runtime unavailable for static pod | Host boot failure | Plugin static pod cannot start | kubelet or CRI logs | systemd mode, local image cache | Fix kubelet or CRI, or run plugin as host service | Yes | No |
| systemd ordering wrong | Plugin starts after API server | API server fails or retries | boot logs | `Before=kubelet.service` where appropriate, tested units | Correct unit dependencies | Yes | No |
| Transit key deleted | Destructive admin action | Old ciphertext undecryptable | metadata read fails, decrypt failures | `deletion_allowed=false`, no delete permission | Restore OpenBao backup with key material | Yes | Yes if no valid backup |
| Transit key soft-deleted | Key archived or disabled | Encrypt and decrypt fail | metadata state, decrypt errors | change control | Restore key if possible | Yes | No if restored |
| Transit key recreated same name | Key lineage lost | Old data undecryptable; `key_id` collision risk | lineage mismatch, decrypt failures | key lineage ID, delete protection | Restore original key; do not accept new lineage | Yes | Yes if original key lost |
| `min_decryption_version` raised too early | Operator error | Old ciphertext undecryptable | decrypt failures for old `key_id` values | verify migration first | Lower setting if key versions still exist | Yes | Possible |
| Key backup missing | Disaster restore lacks Transit key versions | Data undecryptable | DR test failure | coordinated OpenBao backups | Restore from valid backup | Yes | Yes |
| Audit backend pressure | OpenBao audit device slow or failing | Transit latency or errors | OpenBao metrics, plugin latency | HA audit sinks, capacity planning | Repair audit backend | Possible | No |
| OpenBao leader failover | HA event | transient errors or latency | OpenBao status, plugin retries | HA tuning, bounded retries | Wait or fix cluster | Possible | No |
| TLS certificate expired | Certificate not renewed | Plugin cannot connect | TLS errors | certificate monitoring | Renew certificate, reload plugin | Yes | No |
| DNS or LB misrouting | Wrong backend or stale DNS | Auth or transit errors | TLS or SNI errors, metadata mismatch | pinned CA and SNI, instance ID checks | Fix DNS or load balancer | Yes | No |
| JWT file missing | Provisioning error | Login impossible | startup validation | file checks, configuration management | Restore JWT file | Yes | No |
| JWT wrong audience | Issuer or configuration mismatch | Login denied | auth error | bound audience tests | Issue correct JWT or fix role | Yes | No |
| JWT wrong subject or claims | Role mismatch | Login denied | auth error | claim binding documentation | Issue correct JWT or fix role | Yes | No |
| Issuer changed | OIDC or JWT issuer rotation | Login denied | auth logs | planned overlap | Update OpenBao configuration and JWT source | Yes | No |
| JWKS rotated | New signing key unknown | Login denied | JWT auth errors | JWKS monitoring, overlapping keys | Refresh JWKS or OpenBao configuration | Yes | No |
| OpenBao cannot reach JWKS or OIDC discovery | Network failure | Login denied or cache expiry | auth errors | pinned public keys for recovery | Restore discovery or configure keys | Possible | No |
| Clock skew | Host, OpenBao, or issuer clocks differ | JWT invalid | auth errors, NTP alerts | NTP or chrony, leeway | Fix clocks | Yes | No |
| Revoked JWT still cryptographically valid | JWT auth lacks TokenReview | Token may be accepted until expiry | hard to detect | short TTL, external issuer controls | Rotate JWT and signing keys if needed | No immediate | No |
| Status `key_id` differs from encrypt response | Race or bug | API server discards encrypt result, marks unhealthy | API server logs, plugin metrics | snapshot consistency | Fix bug, restart with stable registry | Possible | No |
| `key_id` flip-flops | Unstable rotation observation | Stale marking oscillates | metrics or logs hash changes | stable observation count, activation delay | Pin active key, fix watcher | Possible | No |
| Unknown `key_id` on decrypt | Configuration or provider changed, old data | Decrypt rejected | decrypt `key_id` errors | preserve key history | Restore old configuration or registry | Yes for affected data | Possible |
| Missing or malformed annotations | Old object, bug, corruption | Decrypt rejected when AAD required | AAD validation metrics | compatibility mode for known epochs | Enable bounded compatibility or migrate | Yes for affected data | No if compatible recovery is possible |
| AAD mismatch | Wrong cluster, key, or provider metadata | Decrypt rejected | AAD error | stable configuration, validation | Restore matching configuration | Yes for affected data | Possible |
| API server decrypt storm | Startup with many encrypted objects | Latency, timeouts | duration metrics, API server logs | OpenBao capacity, local routing, optional batching | Scale OpenBao, tune timeout, reduce retries | Yes if severe | No |
| Stale socket | Crash left socket path | Startup failure or wrong listener | socket check | safe stale cleanup | Remove verified-dead socket | Yes | No |
| Wrong socket permissions | API server cannot connect | KMS unavailable | API server permission errors | mode and group validation | Fix group or mode | Yes | No |
| SELinux or AppArmor block | Host policy denies socket or file | KMS unavailable | audit logs | policy profiles, tests | Adjust policy | Yes | No |
| Configuration file permissions unsafe | World-readable or world-writable | Secret or topology exposure or tamper | startup validation | fail closed | Fix permissions | Yes | No |
| Plugin crash loop | Bug, bad configuration, OpenBao error path | KMS unavailable | service logs | supervisor, tests | Fix configuration or bug | Yes | No |
| Image unavailable for static pod | Pull failure, air gap | Plugin not started | kubelet events or logs | preloaded image, `IfNotPresent` or `Never` | Load image | Yes | No |
| Package upgrade restarts systemd plugin | Maintenance event | Transient KMS outage | service logs | controlled rollout | Restart one node at a time | Possible | No |
| `identity` fallback left enabled permanently | Migration incomplete | Plaintext writes possible if provider order changes or KMS is unavailable with `identity` first | configuration audit | remove after migration | Rewrite resources and remove fallback | No | Confidentiality loss |
| `identity` fallback removed too early | Old plaintext or misordered data | Reads may fail depending on provider set | API errors | verify migration first | Restore fallback temporarily and migrate | Possible | No |
| Only some resources encrypted | Configuration scope incomplete | Unprotected resources in etcd | encryption configuration review | explicit resource list | Update configuration, rewrite resources | No | Confidentiality loss |
| Existing resources not rewritten | Encryption only applies on write | Old data remains under old provider or plaintext | audit and migration checks | storage migration | Rewrite resources | No | Confidentiality loss |
| Mixed plaintext and encrypted backups | Backups taken across migration | Inconsistent confidentiality | backup audit | backup labeling | Handle according to sensitivity | No | Confidentiality loss |
| Provider name changed | `EncryptionConfiguration` drift | Old encrypted data may not match provider | API server errors | immutable provider name | Restore old name or configure migration | Yes for affected data | Possible |
