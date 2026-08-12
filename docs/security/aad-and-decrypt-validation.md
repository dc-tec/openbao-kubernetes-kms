---
title: "AAD And Decrypt Validation"
description: "What Transit associated_data binds, how decrypt validation rejects unknown or tampered ciphertext, and which compatibility-mode actions are unsafe."
weight: 40
---

# AAD And Decrypt Validation

The provider uses additional authenticated data (AAD) and local decrypt validation to reject ciphertext outside its expected scope. OpenBao exposes AAD through the `associated_data` field. For the exact `key_id` format, AAD envelope, annotation rules, and decrypt validation order, see [Reference: Key ID And AAD](/reference/key-id-and-aad/).

## What AAD Protects Against

OpenBao Transit `associated_data` binds ciphertext to non-secret metadata for
AEAD ciphers. Decrypt succeeds only when the caller supplies the same data. The
provider uses AAD to bind every ciphertext to the provider, cluster, OpenBao
instance, Transit mount, key lineage, and active key version that produced it.

This addresses the following threats:

- **Replay across clusters.** A Transit ciphertext encrypted for one cluster cannot be successfully decrypted in another cluster, even if the OpenBao key is shared, because the cluster identity is bound into the AAD.
- **Replay across key lineages.** If a Transit key is deleted and recreated with the same name, the new key has a different lineage ID. Old ciphertext, even if presented to the new key, fails AAD reconstruction.
- **Replay across providers.** A Transit ciphertext produced by a different application using the same Transit key cannot be decrypted by the Kubernetes KMS provider because the provider name is bound into the AAD.
- **Annotation tampering.** Annotations that disagree with the active key snapshot are rejected before Transit is called, preventing maliciously edited ciphertext from reaching the cipher.

## Decrypt Validation Order

The provider rejects ciphertext as early as possible to keep failure observable and to avoid spending Transit decrypt calls on doomed payloads. The validation pipeline is:

1. Parse the `key_id`.
2. Look up the matching historical key snapshot in the local registry.
3. Validate annotation keys and versions.
4. Validate annotation hashes against the snapshot.
5. Reconstruct the canonical AAD bytes.
6. Call OpenBao Transit decrypt.

Unknown `key_id` values, mismatched snapshots, missing required annotations, and AAD reconstruction failures all fail before step 6. Operators see these as `key_id_unknown`, `key_id_malformed`, `aad_missing`, `aad_mismatch`, or `annotation_invalid` in the [error class catalog](/reference/observability/#error-classes).

For the exact field-by-field validation steps see [Reference: Key ID And AAD](/reference/key-id-and-aad/).

## Compatibility Modes

| Mode | Behavior | Acceptable use |
|---|---|---|
| `aad.required` | Encrypt and decrypt require valid AAD metadata. | The required mode for new deployments. |

`aad.required` is the only supported mode. The provider does not expose a config
switch to disable AAD, and state validation rejects non-required AAD modes.

## Compatibility-Mode Misuse

Disabling AAD globally as an incident response is unsafe. Specifically:

- bypassing AAD would expose the decrypt path to ciphertext that was not bound to the current cluster, key lineage, or provider name,
- accepting an AAD-disabled state re-enables the cross-cluster replay class of threats that AAD prevents,
- future compatibility read modes must require explicit retained historical state before they are introduced.

If decrypt is failing during an incident, follow [Operations: Troubleshooting: AAD Mismatch](/operations/troubleshooting/#aad-mismatch) instead of disabling AAD.

## Threats Not Addressed Here

AAD and decrypt validation do not protect against:

- A compromised provider binary. The provider sees plaintext on the way through, before AAD is reconstructed and after it is verified.
- An attacker with valid Transit decrypt permission. Transit will decrypt any ciphertext encrypted under the key, AAD or not, and the attacker can supply matching AAD if they have read access to the configuration.
- Loss of Transit key material. AAD validates ciphertext authenticity; it does not recover lost keys. See [Operations: Disaster Recovery](/operations/disaster-recovery/).

For the full asset and threat catalog see [Threat Model](/security/threat-model/).
