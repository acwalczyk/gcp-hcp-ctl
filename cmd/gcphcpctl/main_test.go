package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMainExitsNonzeroOnCommandFailure checks that main() surfaces a failing
// command's error to stderr exactly once (no cobra usage dump) and exits
// non-zero. It uses the natural "no endpoint available" failure: `cluster list`
// with neither a discovery environment nor an explicit endpoint fails in pre-run,
// before any network call or credential lookup, so the test is hermetic and
// deterministic.
func TestMainExitsNonzeroOnCommandFailure(t *testing.T) {
	if os.Getenv("GCPHCPCTL_MAIN_TEST_CHILD") == "1" {
		os.Args = []string{"gcphcpctl", "--config", os.Getenv("GCPHCPCTL_MAIN_TEST_CONFIG"), "cluster", "list"}
		main()
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainExitsNonzeroOnCommandFailure$")
	tmp := t.TempDir()
	// Give the child a minimal, hermetic environment rather than inheriting the
	// shell's: no GCPHCPCTL_* can leak in to supply an endpoint or environment, so
	// the child has neither. config/HOME point at an empty temp dir.
	cmd.Env = []string{
		"GCPHCPCTL_MAIN_TEST_CHILD=1",
		"GCPHCPCTL_MAIN_TEST_CONFIG=" + filepath.Join(tmp, "missing-config.yaml"),
		"HOME=" + tmp,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("exit = %v, stdout = %q, stderr = %q", err, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	// The command error is surfaced to stderr once, with no usage dump
	// (SilenceUsage/SilenceErrors) and no successful-list output.
	got := stderr.String()
	if strings.Count(got, "no endpoint available") != 1 || strings.Contains(got, "Usage:") || strings.Contains(got, "No clusters found") {
		t.Fatalf("unexpected command diagnostic: %q", got)
	}
}
