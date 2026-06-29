package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestStartupRejectsMissingJWTSecret(t *testing.T) {
	if os.Getenv("MONITORING_STARTUP_TEST") == "1" {
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestStartupRejectsMissingJWTSecret$") // #nosec G204 -- re-executes this test binary.
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "MONITORING_STARTUP_TEST=1", "JWT_SECRET=", "MONITOR_AUTHORIZATION_ENCRYPTION_KEY=")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "JWT_SECRET is required") {
		t.Fatalf("expected startup failure for missing JWT_SECRET, err=%v output=%s", err, output)
	}
}
