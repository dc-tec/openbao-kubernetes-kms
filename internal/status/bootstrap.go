package status

import (
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const autoBootstrapAllowedReason = "initial Transit metadata is eligible for auto-bootstrap"

// AutoBootstrapAssessment describes whether absent local registry state may be synthesized.
type AutoBootstrapAssessment struct {
	Allowed bool
	Reason  string
}

// AssessAutoBootstrapState reports whether absent local state may be synthesized from Transit metadata.
func AssessAutoBootstrapState(profile openbao.KeyProfile) AutoBootstrapAssessment {
	switch {
	case profile.LatestVersion != initialTransitVersion:
		return AutoBootstrapAssessment{
			Allowed: false,
			Reason: fmt.Sprintf(
				"Transit latest_version=%d is not initial version %d",
				profile.LatestVersion,
				initialTransitVersion,
			),
		}
	case profile.MinAvailableVersion > initialTransitVersion:
		return AutoBootstrapAssessment{
			Allowed: false,
			Reason: fmt.Sprintf(
				"Transit min_available_version=%d excludes initial version %d",
				profile.MinAvailableVersion,
				initialTransitVersion,
			),
		}
	case profile.MinDecryptionVersion > initialTransitVersion:
		return AutoBootstrapAssessment{
			Allowed: false,
			Reason: fmt.Sprintf(
				"Transit min_decryption_version=%d excludes initial version %d",
				profile.MinDecryptionVersion,
				initialTransitVersion,
			),
		}
	default:
		return AutoBootstrapAssessment{
			Allowed: true,
			Reason:  autoBootstrapAllowedReason,
		}
	}
}

// CanAutoBootstrapState reports whether absent local state may be synthesized from Transit metadata.
func CanAutoBootstrapState(profile openbao.KeyProfile) bool {
	return AssessAutoBootstrapState(profile).Allowed
}
