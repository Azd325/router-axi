package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupApp(t *testing.T, home string) *App {
	t.Helper()
	application := New(func(Config) (Reader, error) {
		t.Fatal("setup or session constructed a router reader")
		return nil, nil
	}, func(string) string { return "" })
	application.homeDir = func() (string, error) { return home, nil }
	application.executable = func() (string, error) { return "/opt/router-axi/router-axi", nil }
	return application
}

func TestSetupInstallCheckAndUninstall(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	for _, agent := range []string{"claude", "codex", "opencode"} {
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), []string{"setup", "install", "--agent", agent}, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("install %s: code=%d stdout=%q stderr=%q", agent, code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "state: installed") {
			t.Fatalf("install %s stdout=%q", agent, stdout.String())
		}
		stdout.Reset()
		if code := application.Run(t.Context(), []string{"setup", "check", "--agent", agent}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "state: installed") {
			t.Fatalf("check %s: code=%d stdout=%q", agent, code, stdout.String())
		}
		stdout.Reset()
		if code := application.Run(t.Context(), []string{"setup", "uninstall", "--agent", agent}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "state: removed") {
			t.Fatalf("uninstall %s: code=%d stdout=%q", agent, code, stdout.String())
		}
	}
}

func TestSetupPreservesUnrelatedClaudeHooksAndRepairsPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"keep-me"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	application := setupApp(t, home)
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("install: code=%d stdout=%q", code, stdout.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("keep-me")) || !bytes.Contains(data, []byte(sessionHookMarker)) || !bytes.Contains(data, []byte("/opt/router-axi/router-axi")) {
		t.Fatalf("settings lost unrelated hook or managed path: %s", data)
	}
	application.executable = func() (string, error) { return "/new/router-axi", nil }
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("repair: code=%d stdout=%q", code, stdout.String())
	}
	data, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("/new/router-axi")) || bytes.Contains(data, []byte("/opt/router-axi/router-axi")) {
		t.Fatalf("managed path was not repaired: %s, %v", data, err)
	}
}

func TestSetupRejectsMalformedConfiguration(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := setupApp(t, home)
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "codex"}, &stdout, &stderr); code != ExitInternal || !strings.Contains(stdout.String(), "not valid JSON") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "not json" {
		t.Fatalf("malformed configuration was modified: %q", data)
	}
}

func TestSessionDashboardIsOffline(t *testing.T) {
	application := setupApp(t, t.TempDir())
	application.Version = "v0.5.0"
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"session", "dashboard"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "context: offline") || !strings.Contains(stdout.String(), "version: v0.5.0") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSetupUsage(t *testing.T) {
	for _, args := range [][]string{{"setup"}, {"setup", "install"}, {"setup", "install", "--agent", "other"}, {"session"}, {"session", "other"}} {
		code, stdout, stderr := runTest(t, args...)
		if code != ExitUsage || stderr != "" {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}
