package openbao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReadKeyProfileParsesMetadataAndFindings(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/v1/transit/keys/k8s")
		_, _ = w.Write([]byte(`{
			"data": {
				"allow_plaintext_backup": true,
				"deletion_allowed": true,
				"derived": true,
				"exportable": true,
				"imported_key": false,
				"keys": {"1": 1778277601},
				"latest_version": 1,
				"min_available_version": 0,
				"min_decryption_version": 2,
				"min_encryption_version": 2,
				"name": "k8s",
				"soft_deleted": false,
				"supports_decryption": true,
				"supports_derivation": true,
				"supports_encryption": true,
				"supports_signing": false,
				"type": "aes256-gcm96",
				"convergent_encryption": true
			}
		}`))
	}))
	client := newTestClient(t, server)
	profile, err := client.ReadKeyProfile(context.Background(), testMountPath, testKeyName)
	if err != nil {
		t.Fatalf("read key profile: %v", err)
	}

	if profile.Name != testKeyName || profile.Type != TransitKeyTypeAES256GCM96 || profile.LatestVersion != 1 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if len(profile.VersionCreationTimes) != 1 || profile.VersionCreationTimes[0].Version != 1 {
		t.Fatalf("unexpected version metadata: %#v", profile.VersionCreationTimes)
	}
	findings := findingCodes(AssessKeyProfile(profile))
	for _, want := range []string{
		"exportable",
		"plaintext_backup",
		"deletion_allowed",
		"derived",
		"convergent",
		"min_encryption_version",
		"min_decryption_version",
	} {
		if !slices.Contains(findings, want) {
			t.Fatalf("expected finding %q in %#v", want, findings)
		}
	}
	byCode := findingsByCode(AssessKeyProfile(profile))
	if byCode[findingCodeExportable].Impact != KeyProfileFindingImpactCryptographicSafety {
		t.Fatalf("unexpected exportable finding impact: %#v", byCode[findingCodeExportable])
	}
	if byCode[findingCodeDeletionAllowed].Impact != KeyProfileFindingImpactAvailability {
		t.Fatalf("unexpected deletion finding impact: %#v", byCode[findingCodeDeletionAllowed])
	}
	if got := openBaoFindingSeverity(byCode[findingCodeDeletionAllowed]); got != KeyProfileFindingSeverityBlocking {
		t.Fatalf("unexpected deletion finding severity: %s", got)
	}
}

func TestAssessKeyProfileFlagsUnsupportedKeyType(t *testing.T) {
	findings := findingCodes(AssessKeyProfile(KeyProfile{
		Type:               "chacha20-poly1305",
		LatestVersion:      1,
		SupportsEncryption: true,
		SupportsDecryption: true,
	}))
	if !slices.Contains(findings, findingCodeUnsupportedType) {
		t.Fatalf("expected unsupported key type finding in %#v", findings)
	}
}

func TestBlockingKeyProfileFindingsIgnoresWarnings(t *testing.T) {
	findings := BlockingKeyProfileFindings([]KeyProfileFinding{
		{
			Code:     "future_warning",
			Message:  "future warning",
			Severity: KeyProfileFindingSeverityWarning,
		},
		{
			Code:     "blocking",
			Message:  "blocking",
			Severity: KeyProfileFindingSeverityBlocking,
		},
	})
	if len(findings) != 1 || findings[0].Code != "blocking" {
		t.Fatalf("unexpected blocking findings: %#v", findings)
	}
}

func TestEncryptSendsExplicitKeyVersionAndAAD(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPost, "/v1/transit/encrypt/k8s")
		var body encryptRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.KeyVersion != 3 {
			t.Fatalf("expected explicit key version 3, got %d", body.KeyVersion)
		}
		if body.Plaintext != base64.StdEncoding.EncodeToString([]byte("plain")) {
			t.Fatalf("unexpected plaintext encoding: %s", body.Plaintext)
		}
		if body.AssociatedData != base64.StdEncoding.EncodeToString([]byte("aad")) {
			t.Fatalf("unexpected AAD encoding: %s", body.AssociatedData)
		}
		_, _ = w.Write([]byte(`{"data":{"ciphertext":"vault:v3:ciphertext","key_version":3}}`))
	}))
	client := newTestClient(t, server)
	result, err := client.Encrypt(context.Background(), EncryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Plaintext:      []byte(testPlaintext),
		AssociatedData: []byte(testAAD),
		KeyVersion:     3,
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if result.Ciphertext != "vault:v3:ciphertext" || result.KeyVersion != 3 {
		t.Fatalf("unexpected encrypt result: %#v", result)
	}
}

func TestEncryptRejectsImplicitKeyVersion(t *testing.T) {
	client := newTestClient(t, httptest.NewTLSServer(http.NotFoundHandler()))

	_, err := client.Encrypt(context.Background(), EncryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Plaintext:      []byte(testPlaintext),
		AssociatedData: []byte(testAAD),
	})
	if err == nil {
		t.Fatal("expected missing key version to fail")
	}
}

func TestTransitRequiresAssociatedData(t *testing.T) {
	client := newTestClient(t, httptest.NewTLSServer(http.NotFoundHandler()))

	_, err := client.Encrypt(context.Background(), EncryptRequest{
		MountPath:  testMountPath,
		KeyName:    testKeyName,
		Plaintext:  []byte(testPlaintext),
		KeyVersion: 1,
	})
	if err == nil {
		t.Fatal("expected encrypt without AAD to fail")
	}

	_, err = client.Decrypt(context.Background(), DecryptRequest{
		MountPath:  testMountPath,
		KeyName:    testKeyName,
		Ciphertext: testCiphertext,
	})
	if err == nil {
		t.Fatal("expected decrypt without AAD to fail")
	}

	_, err = client.BatchDecrypt(context.Background(), BatchDecryptRequest{
		MountPath: testMountPath,
		KeyName:   testKeyName,
		Items: []BatchDecryptItem{{
			Ciphertext: testCiphertext,
		}},
	})
	if err == nil {
		t.Fatal("expected batch decrypt without AAD to fail")
	}
}

func TestDecryptDecodesPlaintextAndClassifiesAADMismatch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPost, "/v1/transit/decrypt/k8s")
		var body decryptRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.AssociatedData == base64.StdEncoding.EncodeToString([]byte("wrong")) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["cipher: message authentication failed"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"plaintext":"cGxhaW4="}}`))
	}))
	client := newTestClient(t, server)
	result, err := client.Decrypt(context.Background(), DecryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Ciphertext:     testCiphertext,
		AssociatedData: []byte(testAAD),
	})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(result.Plaintext) != testPlaintext {
		t.Fatalf("unexpected plaintext: %q", result.Plaintext)
	}

	_, err = client.Decrypt(context.Background(), DecryptRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		Ciphertext:     testCiphertext,
		AssociatedData: []byte("wrong"),
	})
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassDecryptFailed {
		t.Fatalf("expected decrypt failed class, got %v", err)
	}
	if got := openBaoErr.Error(); got == "" || strings.Contains(got, "cipher: message authentication failed") {
		t.Fatalf("unexpected redacted error: %q", got)
	}
}

func TestBatchDecryptPreservesAADAndReference(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPost, "/v1/transit/decrypt/k8s")
		var body batchDecryptRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.BatchInput) != 1 {
			t.Fatalf("unexpected batch length: %d", len(body.BatchInput))
		}
		if body.BatchInput[0].Reference != "item-1" {
			t.Fatalf("unexpected reference: %q", body.BatchInput[0].Reference)
		}
		if body.BatchInput[0].AssociatedData != base64.StdEncoding.EncodeToString([]byte("aad-1")) {
			t.Fatalf("unexpected item AAD: %s", body.BatchInput[0].AssociatedData)
		}
		_, _ = w.Write([]byte(`{"data":{"batch_results":[{"plaintext":"cGxhaW4=","reference":"item-1"}]}}`))
	}))
	client := newTestClient(t, server)
	result, err := client.BatchDecrypt(context.Background(), BatchDecryptRequest{
		MountPath: testMountPath,
		KeyName:   testKeyName,
		Items: []BatchDecryptItem{{
			Ciphertext:     testCiphertext,
			AssociatedData: []byte("aad-1"),
			Reference:      "item-1",
		}},
	})
	if err != nil {
		t.Fatalf("batch decrypt: %v", err)
	}
	if len(result.Results) != 1 || string(result.Results[0].Plaintext) != testPlaintext {
		t.Fatalf("unexpected batch result: %#v", result)
	}
}

func TestCapabilitiesAndDisableUpsert(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sys/capabilities-self":
			var body capabilitiesRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode capabilities request: %v", err)
			}
			if !slices.Equal(body.Paths, []string{"transit/keys/k8s"}) {
				t.Fatalf("unexpected capability paths: %#v", body.Paths)
			}
			_, _ = w.Write([]byte(`{"data":{"transit/keys/k8s":["read"]}}`))
		case "/v1/transit/config/keys":
			_, _ = w.Write([]byte(`{"data":{"disable_upsert":true}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	client := newTestClient(t, server)
	caps, err := client.Capabilities(context.Background(), []string{"transit/keys/k8s"})
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if !slices.Equal(caps.ByPath["transit/keys/k8s"], []string{"read"}) {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	disableUpsert, err := client.ReadDisableUpsert(context.Background(), testMountPath)
	if err != nil {
		t.Fatalf("read disable_upsert: %v", err)
	}
	if !disableUpsert {
		t.Fatal("expected disable_upsert to be true")
	}
}

func TestProbeEncryptDecrypt(t *testing.T) {
	var ciphertext string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/transit/encrypt/k8s":
			var body encryptRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode encrypt request: %v", err)
			}
			ciphertext = "vault:v1:" + body.Plaintext
			_, _ = w.Write([]byte(`{"data":{"ciphertext":"` + ciphertext + `","key_version":1}}`))
		case "/v1/transit/decrypt/k8s":
			var body decryptRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode decrypt request: %v", err)
			}
			if body.Ciphertext != ciphertext {
				t.Fatalf("unexpected ciphertext: %s", body.Ciphertext)
			}
			plaintext := ciphertext[len("vault:v1:"):]
			_, _ = w.Write([]byte(`{"data":{"plaintext":"` + plaintext + `"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	client := newTestClient(t, server)
	result, err := client.ProbeEncryptDecrypt(context.Background(), ProbeRequest{
		MountPath:      testMountPath,
		KeyName:        testKeyName,
		KeyVersion:     1,
		AssociatedData: []byte("probe-aad"),
	})
	if err != nil {
		t.Fatalf("probe encrypt/decrypt: %v", err)
	}
	if string(result.Ciphertext) != ciphertext || result.KeyVersion != 1 {
		t.Fatalf("unexpected probe result: %#v", result)
	}
}

func TestTLSConfigRequiresCAAndServerName(t *testing.T) {
	if _, err := NewTLSConfig("", "bao.example.internal"); err == nil {
		t.Fatal("expected missing CA to fail")
	}
	if _, err := NewTLSConfig("ca.pem", ""); err == nil {
		t.Fatal("expected missing server name to fail")
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	t.Cleanup(server.Close)

	client, err := NewClientWithHTTPClient(ClientConfig{
		Address:     server.URL,
		Namespace:   testNamespace,
		TokenSource: StaticTokenSource{TokenValue: testToken},
		Timeout:     time.Second,
	}, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func assertRequest(t *testing.T, r *http.Request, method string, requestPath string) {
	t.Helper()

	if r.Method != method {
		t.Fatalf("unexpected method: %s", r.Method)
	}
	if r.URL.Path != requestPath {
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}
	if r.Header.Get(vaultTokenHeader) != testToken {
		t.Fatalf("missing token header")
	}
	if r.Header.Get(vaultNamespaceHeader) != testNamespace {
		t.Fatalf("missing namespace header")
	}
}

func findingCodes(findings []KeyProfileFinding) []string {
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	return codes
}

func findingsByCode(findings []KeyProfileFinding) map[string]KeyProfileFinding {
	byCode := make(map[string]KeyProfileFinding, len(findings))
	for _, finding := range findings {
		byCode[finding.Code] = finding
	}
	return byCode
}

func openBaoFindingSeverity(finding KeyProfileFinding) KeyProfileFindingSeverity {
	if finding.Severity == "" {
		return KeyProfileFindingSeverityBlocking
	}
	return finding.Severity
}
