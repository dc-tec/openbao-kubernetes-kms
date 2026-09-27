package config

import (
	"reflect"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/oauth2"
)

func validateJWTSource(problems *[]ValidationProblem, jwt JWTAuthConfig) {
	switch jwt.Source {
	case JWTSourceFile:
		validateAbsolutePath(problems, "auth.jwt.jwtFile", jwt.JWTFile)
		if !reflect.DeepEqual(jwt.OAuth2, OAuth2Config{}) {
			appendProblem(problems, "auth.jwt.oauth2", "must be omitted for source file")
		}
	case JWTSourceOAuth2:
		if jwt.JWTFile != "" {
			appendProblem(problems, "auth.jwt.jwtFile", "must be omitted for source oauth2")
		}
		if err := oauth2.ValidateConfig(jwt.OAuth2.ClientConfig(time.Second)); err != nil {
			appendProblem(problems, "auth.jwt.oauth2", err.Error())
		}
		appendRequired(problems, "auth.jwt.expectedIssuer", jwt.ExpectedIssuer)
		if len(jwt.ExpectedAudience) == 0 {
			appendProblem(problems, "auth.jwt.expectedAudience", "must contain at least one audience for source oauth2")
		}
	default:
		appendProblem(problems, "auth.jwt.source", "must be file or oauth2")
	}
}
