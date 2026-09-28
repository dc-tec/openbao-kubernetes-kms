package oauth2

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestRequestErrorRetainsTypedCauseWithoutLeakingURL(t *testing.T) {
	cfg := testConfig(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected network call") })
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cause := &net.DNSError{Name: "private-issuer.example", Err: "sensitive details", IsNotFound: true}
	client.http.Transport = failingTransport{err: cause}
	_, err = client.Token(t.Context())
	var dns *net.DNSError
	if !errors.Is(err, ErrRequest) || !errors.As(err, &dns) || dns != cause {
		t.Fatalf("lost typed transport cause: %v", err)
	}
	if strings.Contains(err.Error(), "private-issuer") || strings.Contains(err.Error(), "tenant") ||
		strings.Contains(err.Error(), "sensitive") {
		t.Fatal("transport error exposed endpoint or remote data")
	}
}

func TestCredentialFailuresHaveBoundedReasons(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secret")
	if err := os.WriteFile(target, []byte("synthetic-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want CredentialReason
	}{
		{link, CredentialSymlink},
		{dir, CredentialType},
		{filepath.Join(dir, "missing"), CredentialMissing},
		{"relative", CredentialPath},
	} {
		_, err := ReadClientSecret(tc.path)
		var detail *CredentialError
		if !errors.Is(err, ErrCredential) || !errors.As(err, &detail) || detail.Reason != tc.want {
			t.Fatalf("wrong credential reason: %v", err)
		}
		if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("credential error leaked path or data")
		}
	}
}
