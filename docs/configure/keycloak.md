---
title: Provision a Keycloak client
description: "Provision an independent Keycloak service account, bind its identity in OpenBao, and install its client secret on provider hosts."
eyebrow: Configure · Authentication
weight: 16
verifiedBy:
  - deploy/config/keycloak-client.json
  - test/e2e/framework/keycloak_environment.go
  - test/e2e/kind_oauth2_test.go
---

Use this recipe with an existing Keycloak realm that remains available while
the protected Kubernetes API is stopped. The provider's Keycloak qualification
covers the client-credentials flow, both client authentication methods,
credential replacement, issuer outages, and cold provider recovery. It does
not qualify your issuer deployment, database, network, or backup process.
See [OAuth client credentials](/docs/configure/oauth2/) for the protocol and
[E2E framework](/contribute/e2e-framework/) for the pinned test targets.

## Before provisioning

Use the selected release's `deploy/config/keycloak-client.json` and
`deploy/config/init-values-oauth2.yaml`. The client profile enables service
accounts, disables browser and password grants, adds the `bao-kms-provider`
audience, and selects a five-minute access-token lifetime. Give this client
no realm administration roles. OpenBao authorizes its bound subject through
the generated provider policy.

The following Bash commands require the matching Keycloak Admin CLI
(`kcadm.sh`), `jq`, `curl`, and a realm administrator. Configure their TLS trust
for your issuer before starting. Do not disable certificate verification.
Run without shell tracing. The temporary Admin CLI configuration contains
administrator tokens and must not be copied to provider hosts.

## Create the client

Set the public HTTPS server URL, realm, and administrator name. The CLI prompts
for the administrator password:

```sh
set -euo pipefail
umask 077
KC_SERVER=https://identity.example.com
KC_REALM=platform
KC_ADMIN=realm-admin
KC_WORK=$(mktemp -d)
trap 'rm -rf "$KC_WORK"' EXIT
KC_CONFIG="$KC_WORK/kcadm.config"
kcadm.sh config credentials --config "$KC_CONFIG" \
  --server "$KC_SERVER" --realm "$KC_REALM" --user "$KC_ADMIN"

CLIENT_UUID=$(kcadm.sh create clients --config "$KC_CONFIG" -r "$KC_REALM" \
  -f deploy/config/keycloak-client.json -i)
SUBJECT=$(kcadm.sh get "clients/$CLIENT_UUID/service-account-user" \
  --config "$KC_CONFIG" -r "$KC_REALM" --fields id | jq -er '.id')

curl --fail --silent --show-error \
  "$KC_SERVER/realms/$KC_REALM/.well-known/openid-configuration" > "$KC_WORK/discovery.json"
jq '{issuer, token_endpoint}' "$KC_WORK/discovery.json"
printf 'Service-account subject: %s\n' "$SUBJECT"
```

Client creation must succeed once. If the client already exists, stop and
review its settings and ownership before reusing it. Do not recreate it:
the service-account subject can change. The subject is the service-account
user ID, not the client ID or the client resource UUID.

Record the discovery document's `issuer` and `token_endpoint`, the returned
subject, client ID `kms-control-plane`, and audience `bao-kms-provider` in the
OAuth values file. Keep `authMethod: client_secret_basic`. The qualified
alternative is `client_secret_post`; do not configure a fallback between them.
The five-minute token lifetime exceeds the default two-minute
`minRemainingTtl`. Recheck this relationship if realm policy changes it.

## Install the credential

Write the secret into the private temporary directory without displaying it:

```sh
kcadm.sh get "clients/$CLIENT_UUID/client-secret" --config "$KC_CONFIG" \
  -r "$KC_REALM" --fields value | jq -er '.value | select(length > 0)' > "$KC_WORK/client-secret"
```

Transfer this file through the credential provisioning channel that works
without the protected API. Never add it to values YAML, Git, a command argument,
or a Kubernetes Secret in the protected cluster.

On a systemd host, after creating the service identity:

```sh
sudo install -d -o root -g openbao-kms -m 0750 /etc/openbao-kms/credentials
sudo install -o root -g openbao-kms -m 0640 client-secret /etc/openbao-kms/credentials/client-secret.new
sudo mv /etc/openbao-kms/credentials/client-secret.new /etc/openbao-kms/credentials/client-secret
```

For a static-pod host, use numeric group `65532` instead of `openbao-kms`.
Keep the temporary file on the same filesystem as the destination so the
rename is atomic. Restrict the containing directory to root and the provider
identity. The generated pod mounts this directory read-only; do not bind-mount
the secret file alone. Remove the transferred staging copy after installation.

Generate the installation files, then have the OpenBao administrator review
and apply `openbao-setup.sh`. The resulting JWT role binds the recorded issuer,
audience, and subject. Keycloak's CA trust for the token endpoint and OpenBao's
CA trust for JWT discovery are separate inputs; configure both when needed.

## Verify renewal and recovery

Run `doctor` under the provider identity. Its `oauth2.acquire` and
`openbao.auth` checks must pass. Check the actual running provider separately;
a successful local diagnostic does not establish API-server activation.

In a disposable acceptance environment, verify:

1. The provider continues through OpenBao token renewal and fresh login.
2. An issuer outage prevents fresh login without switching to another source.
3. After issuer recovery, a cold provider starts while the protected API is stopped.
4. After atomic client-secret replacement, fresh login succeeds and stored data
   remains readable. An already-issued OpenBao token does not prove the new
   client secret works; use a fresh diagnostic login.

Use your Keycloak credential rotation policy to control overlap. This recipe
does not promise overlap when regenerating a client secret. Disabling a client
does not revoke OpenBao tokens it already obtained. Recovery requires the
issuer database and signing keys, client identity, host secret, TLS trust, and
OpenBao to remain available independently of the protected API.

For administrator operations, see the upstream
[Keycloak Admin CLI guide](https://www.keycloak.org/docs/latest/server_admin/index.html#admin-cli)
and [service-account user API](https://www.keycloak.org/docs-api/latest/rest-api/index.html#_get_adminrealmsrealmclientsclient_uuidservice_account_user).
