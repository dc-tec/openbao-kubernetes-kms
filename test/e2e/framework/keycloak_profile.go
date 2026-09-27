//go:build e2e

package framework

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// These DTOs cover the maintained client provisioning profile and the isolated
// realm import. Credentials exist only in the private fixture directory.
type keycloakClientProfile struct {
	ID       string `json:"id,omitempty"`
	ClientID string `json:"clientId"`
	// #nosec G117 -- ephemeral fixture credential, written only to a private temporary import file.
	Secret                    string                   `json:"secret,omitempty"`
	Enabled                   bool                     `json:"enabled"`
	Protocol                  string                   `json:"protocol"`
	PublicClient              bool                     `json:"publicClient"`
	ClientAuthenticatorType   string                   `json:"clientAuthenticatorType"`
	ServiceAccountsEnabled    bool                     `json:"serviceAccountsEnabled"`
	StandardFlowEnabled       bool                     `json:"standardFlowEnabled"`
	ImplicitFlowEnabled       bool                     `json:"implicitFlowEnabled"`
	DirectAccessGrantsEnabled bool                     `json:"directAccessGrantsEnabled"`
	FullScopeAllowed          bool                     `json:"fullScopeAllowed"`
	Attributes                map[string]string        `json:"attributes"`
	ProtocolMappers           []keycloakProtocolMapper `json:"protocolMappers"`
}

type keycloakProtocolMapper struct {
	Name           string            `json:"name"`
	Protocol       string            `json:"protocol"`
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config"`
}

type keycloakRealmImport struct {
	Realm               string                   `json:"realm"`
	Enabled             bool                     `json:"enabled"`
	AccessTokenLifespan int                      `json:"accessTokenLifespan"`
	Clients             []keycloakClientProfile  `json:"clients"`
	Users               []keycloakServiceAccount `json:"users"`
}

type keycloakServiceAccount struct {
	ID                     string `json:"id"`
	Username               string `json:"username"`
	Enabled                bool   `json:"enabled"`
	ServiceAccountClientID string `json:"serviceAccountClientId"`
}

func (f *KeycloakEnvironment) writeRealm() error {
	// E2E binaries run in test/e2e. Consume the operator-facing profile so the
	// qualified service-account settings cannot drift from the installation kit.
	profile, err := os.ReadFile(filepath.Join("..", "..", "deploy", "config", "keycloak-client.json"))
	if err != nil {
		return fmt.Errorf("read Keycloak provisioning profile: %w", err)
	}
	var client keycloakClientProfile
	decoder := json.NewDecoder(bytes.NewReader(profile))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&client); err != nil {
		return fmt.Errorf("decode Keycloak provisioning profile: %w", err)
	}
	client.ID, client.ClientID, client.Secret = keycloakClientUUID, f.ClientID, f.ClientSecret
	realm := keycloakRealmImport{
		Realm: "kms-e2e", Enabled: true, AccessTokenLifespan: 300,
		Clients: []keycloakClientProfile{client},
		Users: []keycloakServiceAccount{{
			ID: f.Subject, Username: "service-account-" + f.ClientID,
			Enabled: true, ServiceAccountClientID: f.ClientID,
		}},
	}
	content, err := json.Marshal(realm)
	if err != nil {
		return fmt.Errorf("encode Keycloak fixture: %w", err)
	}
	return os.WriteFile(filepath.Join(f.dir, "kms-e2e-realm.json"), content, 0o600)
}
