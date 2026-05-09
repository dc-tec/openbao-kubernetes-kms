package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/socket"
)

const (
	internalParentMode      = os.FileMode(0o750)
	internalSocketMode      = os.FileMode(0o660)
	internalShutdownTimeout = 500 * time.Millisecond
)

func TestRunReturnsServeErrorWhenSocketListenerCloses(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "kmsrt-internal-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	if err := os.Chmod(dir, internalParentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}

	rt, err := New(Options{
		Socket: socket.Options{
			Path: filepath.Join(dir, "kms.sock"),
			Mode: internalSocketMode,
			GID:  -1,
		},
		GRPCServer:      grpc.NewServer(),
		ShutdownTimeout: internalShutdownTimeout,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(ctx) }()

	if err := rt.socketListener.Close(); err != nil {
		t.Fatalf("close socket listener: %v", err)
	}

	select {
	case err := <-runErr:
		if err == nil {
			t.Fatal("Run returned nil error")
		}
		if !strings.Contains(err.Error(), "grpc server") {
			t.Fatalf("Run error should identify grpc server, got %v", err)
		}
		if errors.Is(err, grpc.ErrServerStopped) {
			t.Fatalf("Run returned expected shutdown error instead of serve failure: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after listener close")
	}
}
