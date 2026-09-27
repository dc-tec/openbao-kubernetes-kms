package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidationRejectsContradictoryDurations(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
		fields []string
	}{
		{
			"probe at staleness",
			func(cfg *Config) { cfg.Status.ProbeInterval = cfg.Status.StatusMaxStaleness },
			[]string{"status.probeInterval", "status.statusMaxStaleness"},
		},
		{
			"probe after staleness",
			func(cfg *Config) { cfg.Status.ProbeInterval = cfg.Status.StatusMaxStaleness + time.Second },
			[]string{"status.probeInterval", "status.statusMaxStaleness"},
		},
		{
			"renewal at refresh lead",
			func(cfg *Config) { cfg.Auth.TokenRenewalIncrement = cfg.Auth.LoginBeforeTokenExpiry },
			[]string{"auth.tokenRenewalIncrement", "auth.loginBeforeTokenExpiry"},
		},
		{
			"renewal before refresh lead",
			func(cfg *Config) { cfg.Auth.TokenRenewalIncrement = cfg.Auth.LoginBeforeTokenExpiry - time.Second },
			[]string{"auth.tokenRenewalIncrement", "auth.loginBeforeTokenExpiry"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadValidConfig(t)
			tt.change(&cfg)
			err := Validate(cfg, ValidationOptions{})
			if err == nil {
				t.Fatal("expected contradictory durations to fail validation")
			}
			for _, field := range tt.fields {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("error %q does not identify %s", err, field)
				}
			}
		})
	}

	t.Run("strict boundaries accepted", func(t *testing.T) {
		cfg := loadValidConfig(t)
		cfg.Status.ProbeInterval = cfg.Status.StatusMaxStaleness - time.Nanosecond
		cfg.Auth.TokenRenewalIncrement = cfg.Auth.LoginBeforeTokenExpiry + time.Nanosecond
		if err := Validate(cfg, ValidationOptions{}); err != nil {
			t.Fatalf("valid duration boundaries: %v", err)
		}
	})
}

func TestValidationChecksFixedListenersWithoutResolvingNames(t *testing.T) {
	tests := []struct {
		name    string
		health  string
		metrics string
		invalid bool
	}{
		{"same IPv4", "127.0.0.1:8081", "127.0.0.1:8081", true},
		{"same IPv6", "[::1]:8081", "[0:0:0:0:0:0:0:1]:8081", true},
		{"same scoped IPv6", "[fe80::1%eth0]:8081", "[fe80:0:0:0:0:0:0:1%eth0]:8081", true},
		{"different IPv6 zones", "[fe80::1%eth0]:8081", "[fe80::1%ETH0]:8081", false},
		{"same IPv4 mapped address", "127.0.0.1:8081", "[::ffff:127.0.0.1]:8081", true},
		{"same hostname", "LOCALHOST:8081", "localhost:8081", true},
		{"same port spelling", "127.0.0.1:08081", "127.0.0.1:8081", true},
		{"different ports", "127.0.0.1:8081", "127.0.0.1:8082", false},
		{"different addresses", "127.0.0.1:8081", "127.0.0.2:8081", false},
		{"dynamic ports", "127.0.0.1:0", "127.0.0.1:0", false},
		{"dynamic port spelling", "127.0.0.1:00", "127.0.0.1:0", false},
		{"health disabled", "", "127.0.0.1:8081", false},
		{"metrics disabled", "127.0.0.1:8081", "", false},
		{"both disabled", "", "", false},
		{"unresolved hosts", "health.invalid:8081", "metrics.invalid:8081", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadValidConfig(t)
			cfg.Server.HealthAddress = tt.health
			cfg.Server.MetricsAddress = tt.metrics
			err := Validate(cfg, ValidationOptions{})
			if (err != nil) != tt.invalid {
				t.Fatalf("Validate() = %v, invalid = %v", err, tt.invalid)
			}
			if tt.invalid && (!strings.Contains(err.Error(), "server.healthAddress") ||
				!strings.Contains(err.Error(), "server.metricsAddress")) {
				t.Fatalf("error does not identify both listeners: %v", err)
			}
		})
	}
}

func TestListenerCrossCheckIncludesEnvironmentOverrides(t *testing.T) {
	t.Setenv("BAO_KMS_PROVIDER_SERVER_METRICS_ADDRESS", "127.0.0.1:8082")
	if err := Validate(loadValidConfig(t), ValidationOptions{}); err == nil {
		t.Fatal("expected environment override that duplicates the health listener to fail validation")
	}
}
