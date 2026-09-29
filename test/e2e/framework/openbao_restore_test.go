//go:build e2e

package framework

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSnapshotRestoreWaitsForSealedTarget(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: restoreSealTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/sys/seal-status" {
			t.Fatalf("unexpected seal-status request: %s %s", request.Method, request.URL.Path)
		}
		calls++
		body := `{"sealed":false}`
		if calls == 3 {
			body = `{"sealed":true}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	environment := &OpenBaoEnvironment{Address: "https://restore.example.invalid"}
	if err := environment.waitUntilSealed(context.Background(), client, 45*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("restore proceeded before the target sealed: %d seal-status requests", calls)
	}
}

func TestSnapshotRestoreSealWaitFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unsealed", http.StatusOK, `{"sealed":false}`},
		{"missing sealed state", http.StatusOK, `{}`},
		{"invalid response", http.StatusOK, `invalid`},
		{"endpoint failure", http.StatusServiceUnavailable, `{"sealed":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: restoreSealTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			environment := &OpenBaoEnvironment{Address: "https://restore.example.invalid"}
			err := environment.waitUntilSealed(context.Background(), client, 10*time.Millisecond)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("restore did not stop at its deadline: %v", err)
			}
		})
	}
}

type restoreSealTransport func(*http.Request) (*http.Response, error)

func (transport restoreSealTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
