//go:build certauth_spiffe

package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/proto/spiffe/workload"
	"google.golang.org/grpc"
)

func TestSPIFFECertificateProviderReadsWorkloadAPISVID(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, signer := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(2 * time.Hour),
		URIs:     []string{testSPIFFEID},
	})
	addr := startFakeSPIFFEWorkloadAPI(t, workloadSVIDResponse(t, cert, signer, testSPIFFEID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := NewSPIFFECertificateProvider(ctx, SPIFFEProviderConfig{
		WorkloadAPISocket: addr,
		SPIFFEID:          testSPIFFEID,
		TrustDomain:       testTrustDomain,
	})
	if err != nil {
		t.Fatalf("new spiffe provider: %v", err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Fatalf("close spiffe provider: %v", err)
		}
	})

	tlsCert, err := provider.CurrentCertificate(ctx)
	if err != nil {
		t.Fatalf("current certificate: %v", err)
	}
	if tlsCert.Leaf == nil || tlsCert.Leaf.URIs[0].String() != testSPIFFEID {
		t.Fatalf("unexpected SVID leaf: %#v", tlsCert.Leaf)
	}
	tlsSigner, ok := tlsCert.PrivateKey.(crypto.Signer)
	if !ok {
		t.Fatalf("SVID private key is not a signer: %T", tlsCert.PrivateKey)
	}
	if err := ValidateCertificateSigner(tlsCert.Leaf, tlsSigner); err != nil {
		t.Fatalf("validate SVID signer: %v", err)
	}
}

func TestSPIFFECertificateProviderRejectsUnexpectedSVID(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	wrongID := "spiffe://example.org/openbao-kms/other"
	_, cert, signer := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(2 * time.Hour),
		URIs:     []string{wrongID},
	})
	addr := startFakeSPIFFEWorkloadAPI(t, workloadSVIDResponse(t, cert, signer, wrongID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := NewSPIFFECertificateProvider(ctx, SPIFFEProviderConfig{
		WorkloadAPISocket: addr,
		SPIFFEID:          testSPIFFEID,
		TrustDomain:       testTrustDomain,
	})
	if err == nil {
		t.Fatal("expected provider initialization to reject an unexpected SVID")
	}
}

func workloadSVIDResponse(
	t *testing.T,
	cert *x509.Certificate,
	signer *rsa.PrivateKey,
	spiffeID string,
) *workload.X509SVIDResponse {
	t.Helper()

	keyDER, err := x509.MarshalPKCS8PrivateKey(signer)
	if err != nil {
		t.Fatalf("marshal SVID private key: %v", err)
	}
	return &workload.X509SVIDResponse{
		Svids: []*workload.X509SVID{
			{
				SpiffeId:    spiffeID,
				X509Svid:    cert.Raw,
				X509SvidKey: keyDER,
				Bundle:      cert.Raw,
			},
		},
	}
}

func startFakeSPIFFEWorkloadAPI(t *testing.T, response *workload.X509SVIDResponse) string {
	t.Helper()

	socketDir, err := os.MkdirTemp("/tmp", "obk-spiffe-")
	if err != nil {
		t.Fatalf("create fake Workload API socket directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(socketDir); err != nil {
			t.Fatalf("remove fake Workload API socket directory: %v", err)
		}
	})
	socketPath := filepath.Join(socketDir, "api.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake Workload API socket: %v", err)
	}
	server := grpc.NewServer()
	workload.RegisterSpiffeWorkloadAPIServer(server, fakeSPIFFEWorkloadAPIServer{response: response})
	t.Cleanup(func() {
		server.Stop()
	})
	go func() {
		_ = server.Serve(listener)
	}()
	return "unix://" + socketPath
}

type fakeSPIFFEWorkloadAPIServer struct {
	workload.UnimplementedSpiffeWorkloadAPIServer
	response *workload.X509SVIDResponse
}

func (s fakeSPIFFEWorkloadAPIServer) FetchX509SVID(
	_ *workload.X509SVIDRequest,
	stream grpc.ServerStreamingServer[workload.X509SVIDResponse],
) error {
	return stream.Send(s.response)
}
