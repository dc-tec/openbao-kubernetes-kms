package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	kmsapi "k8s.io/kms/apis/v2"
)

type socketProbeOptions struct {
	socketPath    string
	expectedKeyID string
	timeout       time.Duration
}

func newProbeCommand() *cobra.Command {
	opts := socketProbeOptions{}
	var output string
	cmd := &cobra.Command{
		Use: "probe", Short: "Check a running KMS v2 Unix socket without loading credentials",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateProbeOptions(opts); err != nil {
				return cli.WithExitCode(cli.ExitUsage, err)
			}
			ctx, cancel := context.WithTimeout(commandContext(cmd), opts.timeout)
			defer cancel()
			report := runSocketProbe(ctx, opts)
			if err := printCLIReport(cmd.OutOrStdout(), report, output); err != nil {
				return err
			}
			return reportError(report, "running provider probe failed")
		},
	}
	cmd.Flags().StringVar(&opts.socketPath, "socket", "/run/openbao-kms/kms.sock", "Absolute Unix socket path")
	cmd.Flags().StringVar(&opts.expectedKeyID, "expected-key-id", "", "Require this active key ID from a verified peer")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 10*time.Second, "Deadline for the complete probe")
	addOutputFlag(cmd, &output)
	return cmd
}

func validateProbeOptions(opts socketProbeOptions) error {
	if !filepath.IsAbs(opts.socketPath) {
		return errors.New("--socket must be an absolute Unix socket path")
	}
	if opts.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if opts.expectedKeyID != "" {
		if _, err := keyregistry.ParseKeyID(opts.expectedKeyID); err != nil {
			return errors.New("--expected-key-id must be a provider key ID")
		}
	}
	return nil
}

func runSocketProbe(ctx context.Context, opts socketProbeOptions) cli.Report {
	report := cli.Report{Name: "probe"}
	groups, err := os.Getgroups()
	if err != nil {
		report.Fail("client.identity", "Caller identity", "cannot read supplementary groups; check the host identity")
		return report
	}
	identity := fmt.Sprintf("uid=%d gid=%d groups=%v", os.Geteuid(), os.Getegid(), groups)
	if os.Geteuid() == 0 {
		report.Warn("client.identity", "Caller identity", identity+"; root does not prove non-root socket access")
	} else {
		report.Pass("client.identity", "Caller identity", identity)
	}
	info, err := os.Lstat(opts.socketPath)
	if errors.Is(err, os.ErrPermission) {
		report.Fail("socket.path", "Unix socket", "access denied; check caller groups and parent-directory search permission")
		return report
	}
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		report.Fail("socket.path", "Unix socket", "socket missing or wrong file type; check the path and provider readiness")
		return report
	}
	report.Pass("socket.path", "Unix socket", "path is a Unix socket, not a symlink")
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int64(os.Geteuid()) == int64(stat.Uid) {
		report.Warn("socket.access_scope", "Socket access scope",
			"caller owns the socket; this round trip does not verify access through the socket group")
	}
	dialer := &net.Dialer{}
	connection, err := grpc.NewClient("passthrough:///kms-probe",
		// Unix permissions authenticate local access; this dialer cannot connect over TCP.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", opts.socketPath)
		}),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64*1024)),
	)
	if err != nil {
		report.Fail("socket.connect", "Socket client", "cannot initialize the Unix socket client")
		return report
	}
	defer func() { _ = connection.Close() }()
	probeKMSRoundTrip(ctx, &report, kmsapi.NewKeyManagementServiceClient(connection), opts.expectedKeyID)
	return report
}

func probeKMSRoundTrip(
	ctx context.Context, report *cli.Report, client kmsapi.KeyManagementServiceClient, expected string,
) {
	status, err := client.Status(ctx, &kmsapi.StatusRequest{})
	if err != nil {
		report.Fail("kms.status", "Running provider Status",
			"Status request failed; check provider readiness, caller socket-group access, and the deadline")
		return
	}
	if status.GetVersion() != kmsv2.APIVersion || status.GetHealthz() != kmsv2.HealthOK {
		report.Fail("kms.status", "Running provider Status",
			"provider is unhealthy or not KMS v2; inspect provider diagnostics")
		return
	}
	keyID, err := keyregistry.ParseKeyID(status.GetKeyId())
	if err != nil {
		report.Fail("kms.key_id", "Active key ID", "provider returned an invalid key ID; check the selected socket")
		return
	}
	report.Pass("kms.status", "Running provider Status", "KMS v2 provider reports healthy")
	if expected != "" && expected != keyID {
		report.Fail("kms.key_id", "Peer key agreement", "active key differs; check node identity and rotation convergence")
		return
	}
	probeKMSCiphertext(ctx, report, client, keyID)
}

func probeKMSCiphertext(
	ctx context.Context, report *cli.Report, client kmsapi.KeyManagementServiceClient, keyID string,
) {
	material, err := randomBytes(48)
	if err != nil {
		report.Fail("kms.encrypt", "Running provider Encrypt", "cannot generate probe bytes; check host randomness")
		return
	}
	plaintext, uid := material[:32], hex.EncodeToString(material[32:])
	encrypted, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{Uid: uid + "-encrypt", Plaintext: plaintext})
	if err != nil || len(encrypted.GetCiphertext()) == 0 {
		report.Fail("kms.encrypt", "Running provider Encrypt", "Encrypt failed; check provider readiness and OpenBao access")
		return
	}
	if encrypted.GetKeyId() != keyID {
		report.Fail("kms.key_id", "Active key agreement", "key changed during probe; wait for rotation convergence and retry")
		return
	}
	report.Pass("kms.encrypt", "Running provider Encrypt", "fresh probe bytes encrypted through the live socket")
	decrypted, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Uid: uid + "-decrypt", Ciphertext: encrypted.GetCiphertext(),
		KeyId: encrypted.GetKeyId(), Annotations: encrypted.GetAnnotations(),
	})
	if err != nil || !bytes.Equal(decrypted.GetPlaintext(), plaintext) {
		report.Fail("kms.decrypt", "Running provider Decrypt",
			"Decrypt failed or returned different bytes; check provider diagnostics and Transit decrypt permissions")
		return
	}
	report.Pass("kms.decrypt", "Running provider Decrypt", "live round trip returned the original probe bytes")
	report.Pass("kms.key_id", "Active key ID", keyID)
}
