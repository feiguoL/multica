package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestProbeAgentCLIs_PiAndOmpRegisterSideBySide is the daemon-level
// integration test the review asked for: it places fake `pi` and `omp`
// binaries on PATH, runs the real probeAgentCLIs(), and asserts that two
// separate runtimes are discovered with the correct commands. This
// exercises the descriptor-driven probe loop in agents_probe.go — not just
// backend construction.
func TestProbeAgentCLIs_PiAndOmpRegisterSideBySide(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakeDir := t.TempDir()
	for _, name := range []string{"pi", "omp"} {
		fakePath := filepath.Join(fakeDir, name)
		writeDaemonTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))
	}

	// Stub the login-shell resolver so it doesn't try to fork a shell.
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{}
	}
	resetShellResolveCacheForTest(t)

	t.Setenv("PATH", fakeDir)
	t.Setenv("MULTICA_PI_PATH", "")
	t.Setenv("MULTICA_OMP_PATH", "")

	agents := probeAgentCLIs()

	// Both pi and omp should be discovered as separate entries.
	piEntry, piOk := agents["pi"]
	if !piOk {
		t.Fatal("pi was not discovered by probeAgentCLIs; expected both pi and omp")
	}
	ompEntry, ompOk := agents["omp"]
	if !ompOk {
		t.Fatal("omp was not discovered by probeAgentCLIs; expected both pi and omp")
	}

	if piEntry.Command != "pi" {
		t.Errorf("pi command = %q, want %q", piEntry.Command, "pi")
	}
	if ompEntry.Command != "omp" {
		t.Errorf("omp command = %q, want %q", ompEntry.Command, "omp")
	}

	// Verify the omp descriptor's display name is what the daemon would
	// surface in the registration payload (detectBuiltinRuntimes calls
	// providerDisplayName for each agent).
	ompDisplayName := providerDisplayName("omp")
	if ompDisplayName != "Oh-My-Pi" {
		t.Errorf("omp display name = %q, want %q", ompDisplayName, "Oh-My-Pi")
	}

	// Verify the pi descriptor is NOT a built-in runtime (it's a protocol
	// family), while omp IS a built-in runtime identity.
	if agent.IsBuiltinRuntime("pi") {
		t.Error("pi should not be a built-in runtime (it is a protocol family)")
	}
	if !agent.IsBuiltinRuntime("omp") {
		t.Error("omp should be a built-in runtime identity")
	}
}

// writeDaemonTestExecutable is a helper for the daemon package's test files.
func writeDaemonTestExecutable(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
