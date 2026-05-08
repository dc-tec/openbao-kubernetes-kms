package openbao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testAuthMountPath     = "auth/k8s-workload-a-jwt"
	testAuthLoginPath     = "/v1/auth/k8s-workload-a-jwt/login"
	testAuthRole          = "openbao-kms-control-plane"
	testJWT               = "header.payload.signature"
	testAuthToken         = "bao-client-token"
	testAuthPolicy        = "openbao-kms"
	testRenewSelfPath     = "/v1/auth/token/renew-self"
	testRenewIncrement    = "300s"
	testLoginLeaseSeconds = 600
	testRenewLeaseSeconds = 900
)

func TestAuthClientLoginJWT(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testAuthLoginPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get(vaultTokenHeader) != "" {
			t.Fatal("JWT login must not send an OpenBao token header")
		}
		if r.Header.Get(vaultNamespaceHeader) != testNamespace {
			t.Fatalf("missing namespace header")
		}
		var body jwtLoginRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode login body: %v", err)
		}
		if body.Role != testAuthRole || body.JWT != testJWT {
			t.Fatalf("unexpected login body")
		}
		_, _ = w.Write([]byte(`{
			"auth": {
				"client_token": "` + testAuthToken + `",
				"lease_duration": 600,
				"renewable": true,
				"policies": ["` + testAuthPolicy + `"],
				"token_policies": ["` + testAuthPolicy + `"]
			}
		}`))
	}))
	client := newTestAuthClient(t, server)

	token, err := client.LoginJWT(context.Background(), JWTLoginRequest{
		MountPath: " /" + testAuthMountPath + "/ ",
		Role:      " " + testAuthRole + " ",
		JWT:       "\n" + testJWT + "\n",
	})
	if err != nil {
		t.Fatalf("login jwt: %v", err)
	}
	if token.ClientToken != testAuthToken ||
		token.LeaseDuration != time.Duration(testLoginLeaseSeconds)*time.Second ||
		!token.Renewable {
		t.Fatalf("unexpected token response: %#v", token)
	}
}

func TestAuthClientRenewSelfToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testRenewSelfPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get(vaultTokenHeader) != testAuthToken {
			t.Fatal("renew-self did not send the supplied token")
		}
		var body renewSelfRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode renew body: %v", err)
		}
		if body.Increment != testRenewIncrement {
			t.Fatalf("unexpected increment: %q", body.Increment)
		}
		_, _ = w.Write([]byte(`{
			"auth": {
				"client_token": "` + testAuthToken + `",
				"lease_duration": 900,
				"renewable": true
			}
		}`))
	}))
	client := newTestAuthClient(t, server)

	token, err := client.RenewSelfToken(context.Background(), testAuthToken, 5*time.Minute)
	if err != nil {
		t.Fatalf("renew self: %v", err)
	}
	if token.LeaseDuration != time.Duration(testRenewLeaseSeconds)*time.Second || !token.Renewable {
		t.Fatalf("unexpected renewal response: %#v", token)
	}
}

func TestFormatDurationSecondsRoundsUp(t *testing.T) {
	if got := formatDurationSeconds(1500 * time.Millisecond); got != "2s" {
		t.Fatalf("unexpected rounded duration: %q", got)
	}
}

func TestAuthClientLoginFailureIsClassifiedAndRedacted(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":["permission denied for jwt header.payload.signature"]}`))
	}))
	client := newTestAuthClient(t, server)

	_, err := client.LoginJWT(context.Background(), JWTLoginRequest{
		MountPath: testAuthMountPath,
		Role:      "wrong-role",
		JWT:       testJWT,
	})
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassPermissionDenied {
		t.Fatalf("expected permission denied class, got %v", err)
	}
	if strings.Contains(openBaoErr.Error(), testJWT) {
		t.Fatalf("error leaked JWT: %q", openBaoErr.Error())
	}
}

func TestNewAuthClientRejectsUnsafeAddress(t *testing.T) {
	_, err := NewAuthClientWithHTTPClient(AuthClientConfig{Address: "http://bao.example.internal"}, &http.Client{})
	if err == nil {
		t.Fatal("expected unsafe address to fail")
	}
}

func newTestAuthClient(t *testing.T, server *httptest.Server) *AuthClient {
	t.Helper()
	t.Cleanup(server.Close)

	client, err := NewAuthClientWithHTTPClient(AuthClientConfig{
		Address:   server.URL,
		Namespace: testNamespace,
		Timeout:   time.Second,
	}, server.Client())
	if err != nil {
		t.Fatalf("new auth client: %v", err)
	}
	return client
}
