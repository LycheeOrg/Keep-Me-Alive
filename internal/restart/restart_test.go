package restart

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRun_Success(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "restarted")

	_, err := Run(context.Background(), "touch restarted", dir, time.Second)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("expected file %s to exist: %v", target, statErr)
	}
}

func TestRun_NonZeroExit(t *testing.T) {
	dir := t.TempDir()

	output, err := Run(context.Background(), "exit 1", dir, time.Second)
	if err == nil {
		t.Fatal("Run() expected error for nonzero exit, got nil")
	}
	_ = output
}

func TestRun_BogusWorkingDir(t *testing.T) {
	_, err := Run(context.Background(), "true", "/nonexistent/does/not/exist", time.Second)
	if err == nil {
		t.Fatal("Run() expected error for bogus working dir, got nil")
	}
}

func TestRun_Timeout(t *testing.T) {
	dir := t.TempDir()

	_, err := Run(context.Background(), "sleep 1", dir, 20*time.Millisecond)
	if err == nil {
		t.Fatal("Run() expected error for timeout, got nil")
	}
}
