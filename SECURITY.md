# Security Policy

This repository is pre-release and does not yet provide a production-supported binary.

## Reporting A Vulnerability

Do not open public issues with exploit details, plaintext data, tokens, JWTs, Transit ciphertexts, or environment-specific secrets.

Until a dedicated private contact is published, use GitHub private vulnerability reporting if it is enabled for this repository. If private reporting is not available, open a minimal public issue asking maintainers to establish a private channel and omit technical details.

## Security Scope

Security-sensitive areas include:

- Kubernetes KMS v2 protocol behavior.
- Key ID, annotation, and AAD derivation.
- OpenBao Transit request construction.
- JWT authentication and OpenBao token lifecycle.
- Unix socket permissions and stale socket handling.
- Logs, metrics, debug output, and panic recovery.
- CI, release, signing, SBOM, and provenance workflows.

## Supported Versions

No stable release line exists yet. v0.1 is planned as an engineering preview only.
