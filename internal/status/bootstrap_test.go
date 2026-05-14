package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestAssessAutoBootstrapState(t *testing.T) {
	clock := newFakeClock()

	tests := []struct {
		name        string
		mutate      func(profileForTest) profileForTest
		wantAllowed bool
		wantReason  string
	}{
		{
			name:        "initial metadata",
			wantAllowed: true,
			wantReason:  "initial Transit metadata",
		},
		{
			name: "latest version advanced",
			mutate: func(profile profileForTest) profileForTest {
				profile.latestVersion = 2
				return profile
			},
			wantReason: "latest_version=2",
		},
		{
			name: "minimum available excludes version one",
			mutate: func(profile profileForTest) profileForTest {
				profile.minAvailableVersion = 2
				return profile
			},
			wantReason: "min_available_version=2",
		},
		{
			name: "minimum decryption excludes version one",
			mutate: func(profile profileForTest) profileForTest {
				profile.minDecryptionVersion = 2
				return profile
			},
			wantReason: "min_decryption_version=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := profileForTest{latestVersion: 1}
			if tt.mutate != nil {
				profile = tt.mutate(profile)
			}
			transitProfile := profileForLatest(profile.latestVersion, clock.Now().Add(time.Minute))
			transitProfile.MinAvailableVersion = profile.minAvailableVersion
			transitProfile.MinDecryptionVersion = profile.minDecryptionVersion

			assessment := status.AssessAutoBootstrapState(transitProfile)
			if assessment.Allowed != tt.wantAllowed {
				t.Fatalf("unexpected allowed value: want %t got %t", tt.wantAllowed, assessment.Allowed)
			}
			if !strings.Contains(assessment.Reason, tt.wantReason) {
				t.Fatalf("assessment reason %q does not contain %q", assessment.Reason, tt.wantReason)
			}
			if status.CanAutoBootstrapState(transitProfile) != tt.wantAllowed {
				t.Fatalf("boolean compatibility helper disagrees with assessment")
			}
		})
	}
}

type profileForTest struct {
	latestVersion        int
	minAvailableVersion  int
	minDecryptionVersion int
}
