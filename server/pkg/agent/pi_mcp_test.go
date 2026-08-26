package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"log/slog"
)

// A managed mcp_config must be materialised as .mcp.json in the workdir before
// the Pi process starts. Both Pi and OMP discover MCP servers from this file.
func TestPiExecuteWritesMcpConfigToWorkdir(t *testing.T) {
	workDir := t.TempDir()

	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+
		"test -f .mcp.json || exit 1\n"+
		"cat > /dev/null\n"+
		"printf '%s\\n' '{\"type\":\"agent_start\"}'\n"+
		"printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"ok\"}],\"usage\":{\"input\":1,\"output\":1}}}'\n"))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	mcpConfig := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`)
	session, err := backend.Execute(t.Context(), "test prompt", ExecOptions{
		Cwd:       workDir,
		McpConfig: mcpConfig,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("expected .mcp.json to be written before execution completes, got: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("invalid .mcp.json: %v", err)
	}
	servers, ok := written["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcpServers key, got: %v", written)
	}
	if _, ok := servers["fetch"]; !ok {
		t.Errorf("expected 'fetch' MCP server in written config, got: %v", servers)
	}

	// Drain the session so the cleanup runs.
	for range session.Messages {
	}
	<-session.Result
	if _, err := os.Stat(filepath.Join(workDir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("expected managed .mcp.json to be removed after execution, got err=%v", err)
	}
}

// An empty/nil mcp_config must NOT write .mcp.json, so Pi's own inheritance
// path (project-level .mcp.json the user may have) stays intact.
func TestPiExecuteNoMcpConfigLeavesWorkdirUntouched(t *testing.T) {
	workDir := t.TempDir()

	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript([]string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input":1,"output":1}}}`,
	})))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	session, err := backend.Execute(t.Context(), "test prompt", ExecOptions{
		Cwd: workDir,
		// No McpConfig — nil means inherit the runtime's own config.
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	<-session.Result

	if _, err := os.Stat(filepath.Join(workDir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("expected no .mcp.json when mcp_config is nil, got err=%v", err)
	}
}

// An existing .mcp.json in the workdir must not be silently overwritten —
// the managed config would clobber the user's project-level MCP setup.
func TestPiExecuteFailsClosedOnExistingMcpJson(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, ".mcp.json"), []byte(`{"mcpServers":{"user-server":{"command":"echo"}}}`), 0o644); err != nil {
		t.Fatalf("seed .mcp.json: %v", err)
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript([]string{
		`{"type":"agent_start"}`,
	})))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	mcpConfig := json.RawMessage(`{"mcpServers":{"managed":{"command":"uvx"}}}`)
	_, err = backend.Execute(t.Context(), "test prompt", ExecOptions{
		Cwd:       workDir,
		McpConfig: mcpConfig,
	})
	if err == nil {
		t.Fatal("expected error when .mcp.json already exists, got nil")
	}

	// Verify the user's file was NOT overwritten.
	data, err := os.ReadFile(filepath.Join(workDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("invalid .mcp.json: %v", err)
	}
	servers, _ := written["mcpServers"].(map[string]any)
	if _, ok := servers["user-server"]; !ok {
		t.Errorf("user's .mcp.json was overwritten, got: %v", written)
	}
	if _, ok := servers["managed"]; ok {
		t.Errorf("managed server leaked into user's .mcp.json, got: %v", written)
	}
}
