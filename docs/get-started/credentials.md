---
title: Choose credentials
description: "Choose how each provider obtains a renewable JWT before installing it on a control-plane host."
eyebrow: Get started · Step 3
weight: 30
---

Choose the credential source before generating files. Each control-plane node
must recover its credentials while the protected Kubernetes API is unavailable.
That includes the issuer, its storage, DNS, routing, TLS trust, and the process
that installs the initial credential.

| Source | Required integration | Values example |
|---|---|---|
| Existing host JWT agent | An independently managed agent that writes and renews a JWT file on each host. The provider reads the file; it does not obtain replacement host JWTs. | `deploy/config/init-values-file.yaml` |
| Native OAuth client credentials | An independent issuer that returns signed JWT access tokens, plus a provisioned client secret. The provider requests tokens itself. | `deploy/config/init-values-oauth2.yaml` |

Both sources exchange a JWT for a scoped OpenBao token. Bind the expected
issuer, audience, and subject in the values file and the OpenBao role. Do not
use a ServiceAccount token issued only by the protected cluster as the recovery
credential.

## Existing host JWT agent

Use this path when your platform already has a maintained host identity agent.
Before installation, verify that the agent:

- Starts and renews without calling the protected Kubernetes API.
- Obtains JWTs with the expected issuer, audience, and subject, and enough
  lifetime to exceed `auth.jwt.minRemainingTtl` plus renewal and outage margin.
- Writes a regular file at the configured `auth.jwt.jwtFile`, without symlinks.
- Uses a private temporary file in the same directory, sets ownership and
  permissions, then renames it over the destination for atomic replacement.
- Reports renewal failure and has a documented way to recover its own identity.

For systemd, use `root:openbao-kms` and mode `0640` for the JWT. The service
user needs directory traversal permission. For static pods, use a dedicated
credential directory readable by UID/GID `65532`, separate from the socket and
state directories. Do not grant the API-server socket group credential access.
See [Linux identity model](/docs/security/linux-identity-model/).

Test a renewal and a cold provider start with the protected API stopped in a
disposable environment. This file contract does not qualify every host agent;
keep evidence for the agent and issuer your platform uses.

## Native OAuth client credentials

Use this path when an independent issuer already supports the required grant.
Provision the client secret as a regular private file on every host. Record
its path, token endpoint, client ID, and token claims in the values file.
Never store the client secret itself in that file or a command argument.

The [OAuth guide](/docs/configure/oauth2/) describes the supported protocol,
file ownership, credential rotation, issuer outages, and the tested Keycloak
flow. It also distinguishes issuer-token acquisition from OpenBao token renewal.
Follow [Provision a Keycloak client](/docs/configure/keycloak/) for client setup
and secret placement. Use only issuer-specific support claims covered by
qualification.

Native OAuth and `init` are unreleased relative to preview.2. Use a candidate
built from the selected Next commit when evaluating this workflow. For
preview.2 artifacts, select the released documentation and the host JWT path.
