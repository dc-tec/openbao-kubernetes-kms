---
title: AAD and decrypt validation
description: "What the additional authenticated data bound to every ciphertext protects against, how decryption rejects unknown or tampered ciphertext early, and why AAD is never disabled."
eyebrow: Security · Data integrity
weight: 40
verifiedBy:
  - internal/aad
  - internal/kmsv2
  - internal/keyregistry
---

Every ciphertext the provider creates is bound to additional authenticated data
(AAD) through Transit's `associated_data`. Transit decrypts only when the caller
supplies the same AAD. For the exact `key_id` format, AAD envelope, and
annotation rules, see [Reference: Key ID and AAD](/docs/reference/key-id-and-aad/).

## What AAD protects against

The AAD names the provider, cluster, OpenBao instance, Transit mount, key
lineage, and key version that produced the ciphertext. That stops:

- **Replay across clusters**: ciphertext from one cluster fails in another,
  even with a shared Transit key.
- **Replay across key lineages**: a key recreated with the same name has a new
  lineage ID, so old ciphertext fails.
- **Replay across providers**: ciphertext another application made with the
  same Transit key fails, because the provider name differs.
- **Annotation tampering**: annotations that disagree with the key snapshot
  are rejected before Transit is called.

## Validation before Transit

The provider rejects bad ciphertext as early as possible, which keeps failures
visible and saves Transit calls. It parses the `key_id`, finds the matching
active, pending, or retired snapshot, checks the annotations and their hashes,
and rebuilds the AAD before it calls Transit. An unknown `key_id` triggers one
rate-limited metadata lookup; new identities are validated and saved before
they are accepted.

Failures before the Transit call surface as `key_id_unknown`,
`key_id_malformed`, `aad_mismatch`, or `annotation_invalid`; see
[Reference: Observability](/docs/reference/observability/#error-classes).

Missing or empty annotations produce `annotation_invalid`. The retained
`aad_missing` class describes an unsupported AAD mode in an internal snapshot.
Normal configuration and persisted-state validation reject that mode before
requests are served; it is not the class for a request without annotations.

## AAD is always required

AAD is always required. The provider has no setting to disable it, and state
validation rejects any other mode. Bypassing AAD during an incident would
reopen every replay above; follow
[Troubleshooting: AAD mismatch](/docs/operate/troubleshooting/#aad-mismatch)
instead.

AAD does not protect against a compromised provider, which sees plaintext in
flight, or against anyone with Transit decrypt permission and read access to
the configuration, who can supply matching AAD. It also cannot recover lost
key material.
