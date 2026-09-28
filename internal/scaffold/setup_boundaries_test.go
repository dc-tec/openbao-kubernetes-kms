package scaffold

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

func TestSetupRejectsUnsafeNamesAndLease(t *testing.T) {
	for _, name := range []string{"p,root", "p,admin", "../p", "a/b", "root", "default", "-option", "p\nq"} {
		if _, err := RenderOpenBaoSetup(loadConfig(t, systemdSamplePath), SetupOptions{PolicyName: name}); err == nil {
			t.Fatalf("accepted unsafe policy name %q", name)
		}
	}
	for _, role := range []string{"../../../sys/policies/acl/x", "a/b", "a%2fb", ".", "..", "a?b"} {
		cfg := loadConfig(t, systemdSamplePath)
		cfg.Auth.JWT.Role = role
		if _, err := RenderOpenBaoSetup(cfg, SetupOptions{PolicyName: "p"}); err == nil {
			t.Fatalf("accepted unsafe role %q", role)
		}
	}
	for _, ttl := range []time.Duration{30 * time.Minute, 40 * time.Minute} {
		cfg := loadConfig(t, systemdSamplePath)
		cfg.Auth.LoginBeforeTokenExpiry = ttl
		_, err := RenderOpenBaoSetup(cfg, SetupOptions{PolicyName: "p"})
		if err == nil || !strings.Contains(err.Error(), "30m") {
			t.Fatalf("expected an actionable generated lease error, got %v", err)
		}
	}
}

func TestSetupPhasePreservesJSONBindings(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	cfg.Auth.JWT.ExpectedAudience = []string{"one,two", "urn:example:'quoted'"}
	cfg.Auth.JWT.ExpectedSubject = "$(touch NEVER); it's a literal"
	script, err := RenderOpenBaoSetup(cfg, SetupOptions{PolicyName: "provider"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// The mock rejects every command except the selected phase and records its JSON stdin.
	cli := "#!/bin/sh\nset -eu\ntest \"$1\" = write\ntest \"$2\" = " +
		shellQuote(cfg.Auth.JWT.MountPath+"/role/"+cfg.Auth.JWT.Role) +
		"\ntest \"$3\" = -\ncat > \"$ROLE_OUTPUT\"\n"
	// #nosec G306 -- executable mock CLI in a private test directory.
	if err := os.WriteFile(filepath.Join(dir, "bao"), []byte(cli), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "setup.sh")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "role.json")
	// #nosec G204 -- execute only the generated test script with a fixed phase and mock CLI.
	cmd := exec.Command("sh", path, "auth-role")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "ROLE_OUTPUT="+output)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run role phase: %v: %s", err, out)
	}
	// #nosec G304 -- mock output in the private test directory.
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var role setupJWTRole
	if err := json.Unmarshal(data, &role); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(role.BoundAudiences, cfg.Auth.JWT.ExpectedAudience) ||
		role.BoundSubject != cfg.Auth.JWT.ExpectedSubject || !reflect.DeepEqual(role.TokenPolicies, []string{"provider"}) {
		t.Fatalf("role bindings changed: %+v", role)
	}
	if _, err := os.Stat(filepath.Join(dir, "NEVER")); !os.IsNotExist(err) {
		t.Fatal("configuration was executed as shell input")
	}
}

func TestStaticPodRejectsBroadAndOverlappingMounts(t *testing.T) {
	for _, path := range []string{
		"/ca.crt", "/etc/ca.crt", "/etc/openbao-kms/ca.crt",
		"/var/lib/openbao-kms/ca.crt", "/var/lib/openbao-kms/state/ca.crt",
		"/var/lib/openbao-kms/state/nested/ca.crt", "/run/openbao-kms/ca.crt",
	} {
		for _, source := range []string{"CA", "OAuth credential", "OAuth CA"} {
			cfg := loadConfig(t, staticPodSamplePath)
			switch source {
			case "CA":
				cfg.OpenBao.CACertFile = path
			case "OAuth credential":
				cfg.Auth.JWT.Source = config.JWTSourceOAuth2
				cfg.Auth.JWT.OAuth2.ClientSecretFile = path
			case "OAuth CA":
				cfg.Auth.JWT.Source = config.JWTSourceOAuth2
				cfg.Auth.JWT.OAuth2.ClientSecretFile = "/etc/openbao-kms/credentials/secret"
				cfg.Auth.JWT.OAuth2.CACertFile = path
			}
			if _, err := RenderStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234}); err == nil {
				t.Fatalf("accepted %s path %s", source, path)
			}
		}
	}
}
