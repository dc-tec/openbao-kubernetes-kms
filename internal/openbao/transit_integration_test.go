//go:build integration

package openbao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOpenBaoTransitIntegration(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/transit/keys/k8s":
			assertRequest(t, r, http.MethodGet, "/v1/transit/keys/k8s")
			_, _ = w.Write([]byte(`{
				"data": {
					"allow_plaintext_backup": false,
					"deletion_allowed": false,
					"derived": false,
					"exportable": false,
					"imported_key": false,
					"keys": {"1": 1778277601, "2": 1778277602},
					"latest_version": 2,
					"min_available_version": 0,
					"min_decryption_version": 1,
					"min_encryption_version": 0,
					"name": "k8s",
					"soft_deleted": false,
					"supports_decryption": true,
					"supports_derivation": true,
					"supports_encryption": true,
					"supports_signing": false,
					"type": "aes256-gcm96"
				}
			}`))
		case "/v1/transit/config/keys":
			assertRequest(t, r, http.MethodGet, "/v1/transit/config/keys")
			_, _ = w.Write([]byte(`{"data":{"disable_upsert":true}}`))
		case "/v1/sys/capabilities-self":
			assertRequest(t, r, http.MethodPost, "/v1/sys/capabilities-self")
			var body capabilitiesRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode capabilities request: %v", err)
			}
			if !slices.Equal(body.Paths, []string{"transit/keys/k8s"}) {
				t.Fatalf("unexpected capability paths: %#v", body.Paths)
			}
			_, _ = w.Write([]byte(`{"data":{"transit/keys/k8s":["read"]}}`))
		case "/v1/transit/encrypt/k8s":
			assertRequest(t, r, http.MethodPost, "/v1/transit/encrypt/k8s")
			var body encryptRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode encrypt request: %v", err)
			}
			if body.KeyVersion != 2 {
				t.Fatalf("expected key version 2, got %d", body.KeyVersion)
			}
			ciphertext := "vault:v2:" + body.Plaintext
			_, _ = w.Write([]byte(`{"data":{"ciphertext":"` + ciphertext + `","key_version":2}}`))
		case "/v1/transit/decrypt/k8s":
			assertRequest(t, r, http.MethodPost, "/v1/transit/decrypt/k8s")
			handleDecryptIntegrationRequest(t, w, r)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	caFile := writeServerCAFile(t, server)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	client, err := NewClient(ClientConfig{
		Address:       server.URL,
		Namespace:     testNamespace,
		CACertFile:    caFile,
		TLSServerName: parsed.Hostname(),
		Timeout:       time.Second,
		TokenSource:   StaticTokenSource{TokenValue: testToken},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	profile, err := client.ReadKeyProfile(ctx, testMountPath, testKeyName)
	if err != nil {
		t.Fatalf("read key profile: %v", err)
	}
	if profile.LatestVersion != 2 {
		t.Fatalf("unexpected key profile: %#v", profile)
	}
	disableUpsert, err := client.ReadDisableUpsert(ctx, testMountPath)
	if err != nil {
		t.Fatalf("read disable_upsert: %v", err)
	}
	if !disableUpsert {
		t.Fatal("expected disable_upsert")
	}

	keyPath := path.Join(testMountPath, transitPathSegmentKeys, testKeyName)
	capabilities, err := client.Capabilities(ctx, []string{keyPath})
	if err != nil {
		t.Fatalf("read capabilities: %v", err)
	}
	if !slices.Equal(capabilities.ByPath[keyPath], []string{"read"}) {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}

	encrypted, err := client.Encrypt(ctx, EncryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Plaintext:      []byte(testPlaintext),
		AssociatedData: []byte(testAAD),
		KeyVersion:     profile.LatestVersion,
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decrypted, err := client.Decrypt(ctx, DecryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: []byte(testAAD),
	})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted.Plaintext) != testPlaintext {
		t.Fatalf("unexpected plaintext: %q", decrypted.Plaintext)
	}

	_, err = client.Decrypt(ctx, DecryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: []byte("wrong-aad"),
	})
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassDecryptFailed {
		t.Fatalf("expected decrypt failure for AAD mismatch, got %v", err)
	}

	batch, err := client.BatchDecrypt(ctx, BatchDecryptRequest{
		MountPath: testMountPath,
		KeyName:   testKeyName,
		Items: []BatchDecryptItem{{
			Ciphertext:     encrypted.Ciphertext,
			AssociatedData: []byte(testAAD),
			Reference:      "integration-1",
		}},
	})
	if err != nil {
		t.Fatalf("batch decrypt: %v", err)
	}
	if len(batch.Results) != 1 || string(batch.Results[0].Plaintext) != testPlaintext {
		t.Fatalf("unexpected batch decrypt result: %#v", batch)
	}

	if err := client.ProbeEncryptDecrypt(ctx, ProbeRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		KeyVersion:     profile.LatestVersion,
		AssociatedData: []byte("probe-aad"),
	}); err != nil {
		t.Fatalf("probe encrypt/decrypt: %v", err)
	}
}

func handleDecryptIntegrationRequest(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read decrypt request: %v", err)
	}
	var batch batchDecryptRequestBody
	if err := json.Unmarshal(bodyBytes, &batch); err != nil {
		t.Fatalf("decode batch decrypt request: %v", err)
	}
	if len(batch.BatchInput) > 0 {
		result := batchDecryptIntegrationItem(t, batch.BatchInput[0])
		_, _ = w.Write([]byte(`{"data":{"batch_results":[` + result + `]}}`))
		return
	}

	var body decryptRequestBody
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("decode decrypt request: %v", err)
	}
	if body.AssociatedData == base64.StdEncoding.EncodeToString([]byte("wrong-aad")) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":["cipher: message authentication failed"]}`))
		return
	}
	plaintext := strings.TrimPrefix(body.Ciphertext, "vault:v2:")
	_, _ = w.Write([]byte(`{"data":{"plaintext":"` + plaintext + `"}}`))
}

func batchDecryptIntegrationItem(t *testing.T, item batchDecryptRequestItem) string {
	t.Helper()

	plaintext := strings.TrimPrefix(item.Ciphertext, "vault:v2:")
	return `{"plaintext":"` + plaintext + `","reference":"` + item.Reference + `"}`
}
