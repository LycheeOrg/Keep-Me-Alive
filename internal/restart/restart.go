// Package restart executes shell commands used to restart local sites.
package restart

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Run executes command via "sh -c" inside workingDir, bounded by timeout.
// It returns the combined stdout+stderr output and a non-nil error on a
// nonzero exit code, exec failure, or timeout.
func Run(ctx context.Context, command, workingDir string, timeout time.Duration) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = workingDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("restart: command %q in %q: %w", command, workingDir, err)
	}

	return string(output), nil
}
