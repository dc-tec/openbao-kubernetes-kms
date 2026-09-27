---
title: OAuth 2.0 client credentials
description: "Obtain JWT access tokens directly from an independent OAuth authorization server and exchange them for OpenBao tokens."
eyebrow: Configure · Authentication
weight: 15
verifiedBy:
  - internal/oauth2
  - internal/auth/oauth2.go
  - test/e2e/oauth2_test.go
  - test/e2e/kind_oauth2_test.go
---

Set `auth.jwt.source: oauth2` to obtain JWT access tokens with the OAuth 2.0
client credentials grant. The provider requests a new access token before each
OpenBao JWT login. It keeps access tokens and OpenBao tokens in memory.

The authorization server, its database, DNS, routing, and TLS trust must work
while the protected Kubernetes API is unavailable. Provision the client secret
on each control-plane host through an independent process. Fetching it from a
Secret in the protected cluster creates a startup and recovery dependency.
A separate management cluster can host the issuer if it recovers independently.

## Supported protocol

The integration uses [OAuth 2.0 client credentials](https://www.rfc-editor.org/rfc/rfc6749#section-4.4)
and has no provider-specific endpoint paths or realm configuration.

| Setting or behavior | Contract |
|---|---|
| Token endpoint | Administrator-configured HTTPS URL, including fixed query parameters; verified TLS; redirects are rejected. |
| Client authentication | `client_secret_basic` or `client_secret_post`, selected in configuration. No automatic method fallback. |
| Request parameters | `scope`, optional `audience`, and repeated `resource` parameters from [RFC 8707](https://www.rfc-editor.org/rfc/rfc8707). |
| Successful response | HTTP 200 with a JSON body containing `access_token` and Bearer `token_type`. Unknown response fields are ignored. |
| Access token | A compact, signed JWT with `exp` and matching configured claims. OpenBao verifies its signature and role bindings. |
| Expiration | JWT `exp` is required. If `expires_in` is present, the provider uses the shorter lifetime. |
| Client secret | Absolute path to a regular file; no symlink, group write, world access, or execute permission. The file is reread before each grant. |
| Other flows | Refresh-token grants, interactive login, opaque-token introspection, client assertions, and mutual TLS client authentication are unavailable. |

The optional `audience` request parameter is an extension supported by some
authorization servers. It does not replace `expectedAudience`, which checks the
JWT returned by the server. Configure only the request parameters your issuer
accepts. Scopes and resource indicators depend on the registered API and client.

Keycloak service accounts can issue JWT access tokens through this grant.
Other OAuth providers can use the same integration when they meet this contract.
Protocol tests exercise both client authentication methods with an HTTPS test
issuer. OpenBao integration tests exercise signature validation and Transit.
The [Kind OAuth lane](/contribute/e2e-framework/) uses Keycloak `26.6.3`, OpenBao
`2.6.0`, and the generated provider static pod. It checks both authentication
methods, Kubernetes Secret encryption in etcd, credential rotation without a
provider restart, issuer outages, and cold provider recovery while the protected
API server is stopped. Images are pinned by digest in `.ci/versions.yaml`.
This qualifies the tested Keycloak flow; other providers and issuer deployment
topologies require their own validation.

## Configure the source

Register a confidential client with permission to request a token for the
OpenBao audience. Record the actual token issuer, audience, and service-account
subject. Configure the OpenBao JWT role to bind that identity and grant only the
provider policy; see [OpenBao auth and policy](/docs/configure/openbao-auth/).

Replace the JWT section in the provider configuration:

```yaml
auth:
  method: jwt
  loginTimeout: 10s
  jwt:
    source: oauth2
    mountPath: auth/k8s-workload-a-jwt
    role: openbao-kms-control-plane
    minRemainingTtl: 2m
    clockSkewLeeway: 30s
    expectedIssuer: https://identity.example.com/realms/platform
    expectedAudience:
      - openbao-kms-production
    expectedSubject: "<service-account-subject>"
    oauth2:
      tokenUrl: https://identity.example.com/realms/platform/protocol/openid-connect/token
      clientId: kms-control-plane
      authMethod: client_secret_basic
      clientSecretFile: /etc/openbao-kms/credentials/client-secret
      caCertFile: /etc/openbao-kms/issuer/ca.pem
```

Omit `caCertFile` to use system certificate roots. If you set it, the file is
the trust bundle for the token endpoint. OpenBao has separate TLS trust settings
and separate issuer trust for JWT verification.

`expectedIssuer` and at least one `expectedAudience` are required for this source.
Set `expectedSubject` when the issuer provides a stable subject. If identity
binding uses other claims, configure those bindings on the OpenBao role.
The `init` setup script requires a subject to generate its bound JWT role.
Omit `jwtFile`; configuring both sources is rejected.

For issuers that require request parameters, add the applicable fields under
`oauth2`. These are independent examples; use the values registered with your issuer:

```yaml
scopes: ["https://api.example.com/.default"]
audience: https://api.example.com
resources: ["https://api.example.com"]
```

Use a dedicated directory for the client secret. For systemd, install the
credential with owner `root`, group `openbao-kms`, and mode `0640`; give the
service user directory traversal permission. A single trailing newline is
accepted. Keep the secret out of configuration YAML, command arguments, and logs.

For static pods, `init --model static-pod` mounts the credential directory
read-only so atomic replacement is visible without a restart. The container UID
must be able to read the secret. No Kubernetes Secret or TokenRequest is needed.

Run `bao-kms-provider doctor --config /etc/openbao-kms/config.yaml` as the service
user. `oauth2.local` checks the credential file. `oauth2.acquire` reports token
acquisition, followed by OpenBao JWT login and the usual Transit checks.

## Lifetime and recovery

`auth.loginTimeout` covers token acquisition and OpenBao login together, including
an attempted OpenBao renewal before fallback login. Set it for both network hops.
Concurrent requests share one login attempt and failed attempts use the existing
exponential backoff.

Acquire tokens with more remaining lifetime than `auth.jwt.minRemainingTtl`
(default two minutes). The provider rejects a token at or below this threshold,
even if the issuer has just minted it. Lower the threshold deliberately when
using an issuer with a shorter token lifetime.

The provider renews its OpenBao token when renewal is available. Renewal does
not acquire a new issuer token. An issuer outage can therefore leave an existing
OpenBao token usable. New logins require the issuer; an expired or rejected
OpenBao token cannot fall back to a JWT file or another authentication method.

Disabling the OAuth client does not revoke OpenBao tokens already issued to it.
Use bounded OpenBao token lifetimes and an OpenBao revocation procedure when
immediate removal of access is required.

To rotate the client secret, install the replacement as a private temporary
file in the same directory, set its ownership and permissions, and rename it
over `clientSecretFile`. The next grant reads the replacement. Use the issuer's
credential overlap procedure when available, and verify a new login before
retiring the old secret. Changes to the endpoint or CA configuration require a
provider restart.
