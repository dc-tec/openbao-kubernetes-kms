package oauth2

import "fmt"

// CredentialError reports a bounded corrective reason without a path or secret.
type CredentialError struct {
	Reason CredentialReason
}

// CredentialReason identifies a local credential preparation failure.
type CredentialReason string

const (
	// CredentialPath requires an absolute path.
	CredentialPath CredentialReason = "absolute_path_required"
	// CredentialMissing means the configured file does not exist.
	CredentialMissing CredentialReason = "file_missing"
	// CredentialUnreadable means the runtime identity cannot inspect or read the file.
	// #nosec G101 -- bounded diagnostic reason, not a credential.
	CredentialUnreadable CredentialReason = "file_unreadable"
	// CredentialSymlink rejects symlinked credentials, including projected Secret files.
	// #nosec G101 -- bounded diagnostic reason, not a credential.
	CredentialSymlink CredentialReason = "symlink_unsupported"
	// CredentialType requires a regular file.
	// #nosec G101 -- bounded diagnostic reason, not a credential.
	CredentialType CredentialReason = "regular_file_required"
	// CredentialPermissions requires private file permissions.
	CredentialPermissions CredentialReason = "unsafe_permissions"
	// CredentialChanged means replacement raced with reading the file; retry after provisioning completes.
	CredentialChanged CredentialReason = "file_changed"
	// CredentialSize means the secret exceeds the supported bound.
	CredentialSize CredentialReason = "file_too_large"
	// CredentialContent requires a nonempty, single-line secret.
	CredentialContent CredentialReason = "invalid_content"
)

func (e *CredentialError) Error() string { return fmt.Sprintf("%s: %s", ErrCredential, e.Reason) }
func (e *CredentialError) Unwrap() error { return ErrCredential }

// requestError retains typed transport causes for diagnostics while excluding
// URLs, response bodies, and other remote data from its printable message.
type requestError struct{ cause error }

func (e *requestError) Error() string        { return ErrRequest.Error() }
func (e *requestError) Unwrap() error        { return e.cause }
func (e *requestError) Is(target error) bool { return target == ErrRequest }
