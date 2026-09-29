package config

// AuthMethod selects the OpenBao authentication method. Validate rejects values
// other than the declared methods before the provider starts.
type AuthMethod string

// OpenBao authentication method names.
const (
	AuthMethodJWT  AuthMethod = "jwt"
	AuthMethodCert AuthMethod = "cert"
)

// CertificateSource selects the certificate provider for OpenBao cert auth.
// Available sources also depend on the build tags and compatibility policy.
type CertificateSource string

// Certificate provider names.
const (
	CertificateSourcePKCS11 CertificateSource = "pkcs11"
	CertificateSourceSPIFFE CertificateSource = "spiffe"
)
