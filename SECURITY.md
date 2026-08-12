# Security Policy

`bao-kms-provider` runs in the Kubernetes control-plane encryption path. Treat vulnerability reports as sensitive even when they appear to be configuration mistakes.

## Reporting A Vulnerability

Use GitHub private vulnerability reporting for issues that could expose plaintext, bypass decrypt validation, weaken key identity, leak credentials, or alter release artifacts.

If private reporting is not available, open a minimal public issue that asks
maintainers to establish a private channel. Do not include exploit details,
plaintext, JSON Web Tokens (JWTs), OpenBao tokens, full Transit ciphertexts,
kubeconfigs, logs with secrets, or environment-specific credentials.

## Supported Versions

Security fixes are provided for the latest released preview line. Stable support windows will be documented before a stable production-ready release line is introduced.

See [Support Policy](docs/reference/support-policy.md) and [Release Policy](docs/reference/release-policy.md).

## Security Scope

Security-sensitive areas include:

- Kubernetes KMS v2 protocol behavior.
- Key ID, annotation, and additional authenticated data (AAD) derivation.
- OpenBao Transit request construction.
- JWT authentication and OpenBao token lifecycle.
- Unix socket permissions and stale socket handling.
- Logs, metrics, debug output, and panic recovery.
- CI, release, signing, software bill of materials (SBOM), and provenance workflows.
