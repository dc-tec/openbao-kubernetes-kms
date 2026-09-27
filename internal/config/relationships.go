package config

import (
	"net"
	"net/netip"
	"strconv"
	"strings"
)

func validateRelationships(problems *[]ValidationProblem, cfg Config) {
	if cfg.Status.ProbeInterval > 0 && cfg.Status.StatusMaxStaleness > 0 &&
		cfg.Status.ProbeInterval >= cfg.Status.StatusMaxStaleness {
		appendProblem(problems, "status.probeInterval", "must be shorter than status.statusMaxStaleness")
	}
	if cfg.Auth.TokenRenewalIncrement > 0 && cfg.Auth.LoginBeforeTokenExpiry > 0 &&
		cfg.Auth.TokenRenewalIncrement <= cfg.Auth.LoginBeforeTokenExpiry {
		appendProblem(problems, "auth.tokenRenewalIncrement", "must exceed auth.loginBeforeTokenExpiry")
	}
	if sameFixedListener(cfg.Server.HealthAddress, cfg.Server.MetricsAddress) {
		appendProblem(problems, "server.metricsAddress", "must differ from server.healthAddress for a fixed port")
	}
}

// Compare configured endpoints without DNS lookups or binding sockets. Port zero
// asks the OS to allocate separate ports and empty addresses disable listeners.
func sameFixedListener(first, second string) bool {
	firstHost, firstPort, firstErr := net.SplitHostPort(first)
	secondHost, secondPort, secondErr := net.SplitHostPort(second)
	if firstErr != nil || secondErr != nil {
		return false
	}
	firstPort, secondPort = canonicalPort(firstPort), canonicalPort(secondPort)
	if firstPort == "0" || firstPort != secondPort {
		return false
	}
	firstIP, firstIPErr := netip.ParseAddr(firstHost)
	secondIP, secondIPErr := netip.ParseAddr(secondHost)
	if firstIPErr == nil && secondIPErr == nil {
		return firstIP.Unmap() == secondIP.Unmap()
	}
	return strings.EqualFold(firstHost, secondHost)
}

func canonicalPort(port string) string {
	if number, err := strconv.ParseUint(port, 10, 16); err == nil {
		return strconv.FormatUint(number, 10)
	}
	return port
}
