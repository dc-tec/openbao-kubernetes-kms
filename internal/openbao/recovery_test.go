package openbao

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type recoveringTokenSource struct {
	replacement string
	recoveryErr error
	recoveries  atomic.Int32
	rejected    string
}

func (s *recoveringTokenSource) RejectToken(token string) { s.rejected = token }

func (*recoveringTokenSource) Token(context.Context) (string, error) { return testToken, nil }
func (s *recoveringTokenSource) RecoverToken(_ context.Context, rejected string) (string, error) {
	if rejected != testToken {
		return "", errors.New("recovered wrong credential")
	}
	s.recoveries.Add(1)
	return s.replacement, s.recoveryErr
}

func TestClientRetriesOnlyAuthenticationRejectionsOnce(t *testing.T) {
	for _, tc := range []struct {
		name           string
		firstStatus    int
		retryStatus    int
		replacement    string
		recoveryErr    error
		wantCalls      int32
		wantRecoveries int32
		wantClass      ErrorClass
	}{
		{
			name:           "unauthorized recovered",
			firstStatus:    401,
			retryStatus:    200,
			replacement:    "replacement",
			wantCalls:      2,
			wantRecoveries: 1,
		},
		{
			name:           "forbidden recovered",
			firstStatus:    403,
			retryStatus:    200,
			replacement:    "replacement",
			wantCalls:      2,
			wantRecoveries: 1,
		},
		{
			name:           "policy denied",
			firstStatus:    403,
			retryStatus:    403,
			replacement:    "replacement",
			wantCalls:      2,
			wantRecoveries: 1,
			wantClass:      ErrorClassPermissionDenied,
		},
		{
			name:           "replacement rejected",
			firstStatus:    401,
			retryStatus:    401,
			replacement:    "replacement",
			wantCalls:      2,
			wantRecoveries: 1,
			wantClass:      ErrorClassUnauthenticated,
		},
		{name: "cooldown", firstStatus: 403, wantCalls: 1, wantRecoveries: 1, wantClass: ErrorClassPermissionDenied},
		{
			name:           "unchanged credential",
			firstStatus:    403,
			replacement:    testToken,
			wantCalls:      1,
			wantRecoveries: 1,
			wantClass:      ErrorClassPermissionDenied,
		},
		{name: "login failed", firstStatus: 403, recoveryErr: ErrAuthentication, wantCalls: 1, wantRecoveries: 1},
		{name: "bad input", firstStatus: 400, wantCalls: 1, wantClass: ErrorClassInvalidRequest},
		{name: "unavailable", firstStatus: 503, wantCalls: 1, wantClass: ErrorClassUnavailable},
		{name: "rate limited", firstStatus: 429, wantCalls: 1, wantClass: ErrorClassRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status := tc.firstStatus
				token := testToken
				if calls.Add(1) > 1 {
					status = tc.retryStatus
					token = tc.replacement
				}
				if r.Header.Get("X-Vault-Token") != token {
					t.Error("wrong credential sent")
				}
				w.WriteHeader(status)
				if status == 200 {
					_, _ = w.Write([]byte(`{"data":{"disable_upsert":true}}`))
				} else {
					_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
				}
			}))
			defer server.Close()
			source := &recoveringTokenSource{replacement: tc.replacement, recoveryErr: tc.recoveryErr}
			client, err := NewClientWithHTTPClient(ClientConfig{Address: server.URL, TokenSource: source}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ReadDisableUpsert(t.Context(), testMountPath)
			if tc.wantClass != "" {
				if !errors.Is(err, &Error{Class: tc.wantClass}) {
					t.Fatalf("wrong error class: %v", err)
				}
			} else if !errors.Is(err, tc.recoveryErr) {
				t.Fatalf("wrong recovery result: %v", err)
			}
			if calls.Load() != tc.wantCalls || source.recoveries.Load() != tc.wantRecoveries {
				t.Fatalf("unexpected calls/recoveries: %d/%d", calls.Load(), source.recoveries.Load())
			}
			wantRejected := ""
			if tc.wantCalls == 2 && (tc.retryStatus == 401 || tc.retryStatus == 403) {
				wantRejected = tc.replacement
			}
			if source.rejected != wantRejected {
				t.Fatalf("retry rejection was not invalidated: %q", source.rejected)
			}
		})
	}
}
