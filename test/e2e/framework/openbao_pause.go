//go:build e2e

package framework

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PauseContainer suspends OpenBao without closing its network connections.
func (f *OpenBaoEnvironment) PauseContainer(ctx context.Context) error {
	return f.setContainerPaused(ctx, true)
}

// ResumeContainer resumes a container suspended by PauseContainer.
func (f *OpenBaoEnvironment) ResumeContainer(ctx context.Context) error {
	return f.setContainerPaused(ctx, false)
}

func (f *OpenBaoEnvironment) setContainerPaused(ctx context.Context, paused bool) error {
	action := "unpause"
	if paused {
		action = "pause"
	}
	cmd := exec.CommandContext(ctx, f.dockerBinary, action, f.containerName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s OpenBao environment container: %w: %s", action, err, strings.TrimSpace(string(output)))
	}
	return nil
}
