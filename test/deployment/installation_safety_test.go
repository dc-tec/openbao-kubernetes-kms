package deployment_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallChecksRefuseHostExecution(t *testing.T) {
	for _, name := range []string{"systemd-install.sh", "static-pod-install.sh", "native-package-install.sh"} {
		t.Run(name, func(t *testing.T) {
			// #nosec G204 -- run a repository test harness with its container permission flag disabled.
			cmd := exec.Command("bash", repoPath("test/deployment/"+name))
			cmd.Env = append(os.Environ(), "KMS_INSTALL_TEST_CONTAINER=")
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "run through make") {
				t.Fatalf("installation check did not refuse host execution: %v: %s", err, output)
			}
		})
	}
}
