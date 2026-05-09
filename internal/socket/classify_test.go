package socket

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestClassifyDialErrConnectionRefusedIsDead(t *testing.T) {
	if got := classifyDialErr(syscall.ECONNREFUSED); got != probeDead {
		t.Fatalf("ECONNREFUSED: want probeDead, got %v", got)
	}
}

func TestClassifyDialErrConnectionRefusedWrappedIsDead(t *testing.T) {
	wrapped := &net.OpError{
		Op:  "dial",
		Net: "unix",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}
	if got := classifyDialErr(wrapped); got != probeDead {
		t.Fatalf("wrapped ECONNREFUSED: want probeDead, got %v", got)
	}
}

func TestClassifyDialErrPermissionDeniedIsUncertain(t *testing.T) {
	if got := classifyDialErr(syscall.EACCES); got != probeUncertain {
		t.Fatalf("EACCES: want probeUncertain, got %v", got)
	}
}

func TestClassifyDialErrTimeoutIsUncertain(t *testing.T) {
	if got := classifyDialErr(context.DeadlineExceeded); got != probeUncertain {
		t.Fatalf("deadline exceeded: want probeUncertain, got %v", got)
	}
}

func TestClassifyDialErrUnrelatedErrorIsUncertain(t *testing.T) {
	if got := classifyDialErr(errors.New("some other dial failure")); got != probeUncertain {
		t.Fatalf("other error: want probeUncertain, got %v", got)
	}
}

func TestProbeSocketReportsLiveListener(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "kmsprb-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := dir + "/probe.sock"
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	if got := probeSocket(socketPath, 250*time.Millisecond); got != probeLive {
		t.Fatalf("live socket: want probeLive, got %v", got)
	}
}

func TestProbeSocketReportsDeadOrphan(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "kmsprb-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := dir + "/probe.sock"
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := probeSocket(socketPath, 250*time.Millisecond); got != probeDead {
		t.Fatalf("orphan socket: want probeDead, got %v", got)
	}
}
