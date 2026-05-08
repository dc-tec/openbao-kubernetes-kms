//go:build e2e

package framework

import "os"

const (
	DefaultOpenBaoTransitMount = "transit-ws03"
	DefaultOpenBaoTransitKey   = "ws03-shape"
)

func EnvDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
