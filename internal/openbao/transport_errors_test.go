package openbao

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

func TestTransportFailuresAreClassifiedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		class ErrorClass
	}{
		{"untrusted CA", x509.UnknownAuthorityError{}, ErrorClassTLSFailed},
		{"hostname", x509.HostnameError{Host: "sensitive-host"}, ErrorClassTLSFailed},
		{"expired certificate", x509.CertificateInvalidError{Reason: x509.Expired}, ErrorClassTLSFailed},
		{"verification", &tls.CertificateVerificationError{Err: errors.New("sensitive certificate")}, ErrorClassTLSFailed},
		{"TLS record", tls.RecordHeaderError{Msg: "sensitive record"}, ErrorClassTLSFailed},
		{"TLS alert", tls.AlertError(42), ErrorClassTLSFailed},
		{"DNS", &net.OpError{Op: "dial", Err: &net.DNSError{Name: "sensitive-host", IsNotFound: true}}, ErrorClassDNSFailed},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, ErrorClassConnectionFailed},
		{"reset", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, ErrorClassConnectionFailed},
		{"unknown", errors.New("sensitive transport failure"), ErrorClassUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := &fakeRequestObserver{}
			client, err := NewClientWithHTTPClient(ClientConfig{
				Address:     "https://sensitive-host",
				TokenSource: StaticTokenSource{TokenValue: testToken},
				Observer:    observer,
			}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("sensitive wrapper: %w", tc.cause)
			})})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Encrypt(context.Background(), EncryptRequest{
				MountPath: testMountPath, KeyName: "sensitive-key", Plaintext: []byte(testPlaintext),
				KeyVersion: 1, AssociatedData: []byte(testAAD),
			})
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Class != tc.class {
				t.Fatalf("error = %v, want class %s", err, tc.class)
			}
			if strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), testToken) {
				t.Fatalf("unredacted error: %v", err)
			}
			if len(observer.requests) != 1 || observer.requests[0].ErrorClass != tc.class ||
				observer.requests[0].Status != string(tc.class) {
				t.Fatalf("observations = %#v", observer.requests)
			}
		})
	}
}

func TestWrappedTransportContextErrorsAreRedacted(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		client, err := NewClientWithHTTPClient(ClientConfig{
			Address: "https://sensitive-host", TokenSource: StaticTokenSource{TokenValue: testToken},
		}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("sensitive transport: %w", cause)
		})})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ReadDisableUpsert(context.Background(), testMountPath)
		if err != cause {
			t.Fatalf("error = %v, want redacted sentinel %v", err, cause)
		}
	}
}
