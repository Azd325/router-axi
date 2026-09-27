package app

import (
	"bytes"
	"encoding/json"
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

type persistedHookConfig struct {
	Hooks struct {
		SessionStart []struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"SessionStart"`
	} `json:"hooks"`
}

func readPersistedHookConfig(t *testing.T, path string) persistedHookConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config persistedHookConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
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
		if agent == "opencode" {
			manifest := filepath.Join(home, ".config", "opencode", "package.json")
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var packageJSON struct {
				Dependencies map[string]string `json:"dependencies"`
			}
			if err := json.Unmarshal(data, &packageJSON); err != nil || packageJSON.Dependencies["@opencode-ai/plugin"] == "" {
				t.Fatalf("OpenCode dependency metadata: %s, %v", data, err)
			}
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
	config := readPersistedHookConfig(t, path)
	if len(config.Hooks.SessionStart) != 2 || config.Hooks.SessionStart[0].Hooks[0].Command != "keep-me" || config.Hooks.SessionStart[1].Hooks[0].Command != "'/opt/router-axi/router-axi' session dashboard # "+sessionHookMarker {
		t.Fatalf("settings lost unrelated hook or managed path: %+v", config)
	}
	application.executable = func() (string, error) { return "/new/router-axi", nil }
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("repair: code=%d stdout=%q", code, stdout.String())
	}
	config = readPersistedHookConfig(t, path)
	if len(config.Hooks.SessionStart) != 2 || config.Hooks.SessionStart[1].Hooks[0].Command != "'/new/router-axi' session dashboard # "+sessionHookMarker {
		t.Fatalf("managed path was not repaired: %+v", config)
	}
}

func TestSetupCheckAndUninstallDoNotRequireExecutable(t *testing.T) {
	application := setupApp(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("install: code=%d stdout=%q", code, stdout.String())
	}
	application.executable = func() (string, error) { return "", os.ErrNotExist }
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "check", "--agent", "claude"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "state: installed") {
		t.Fatalf("check: code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "uninstall", "--agent", "claude"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "state: removed") {
		t.Fatalf("uninstall: code=%d stdout=%q", code, stdout.String())
	}
}

func TestSetupAllJSONIsOneDocument(t *testing.T) {
	application := setupApp(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "check", "--agent", "all", "--json"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("check: code=%d stdout=%q", code, stdout.String())
	}
	var result struct {
		Setup []setupResult `json:"setup"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || len(result.Setup) != 3 {
		t.Fatalf("invalid aggregate: err=%v stdout=%q", err, stdout.String())
	}
}

func TestSetupRejectsIncompatibleHookValuesAndUnmanagedMarker(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"SessionStart":"keep"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	application := setupApp(t, home)
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitInternal {
		t.Fatalf("incompatible hooks: code=%d stdout=%q", code, stdout.String())
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"'/unrelated/script' session dashboard # router-axi-session-hook"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "uninstall", "--agent", "claude"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "state: missing") {
		t.Fatalf("unmanaged marker: code=%d stdout=%q", code, stdout.String())
	}
}

func TestSetupRejectsSpoofedOpenCodeMarker(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "opencode", "plugins", "router-axi.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// "+sessionHookMarker+"\nexport default function unrelated() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := setupApp(t, home)
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "check", "--agent", "opencode"}, &stdout, &stderr); code != ExitInternal || !strings.Contains(stdout.String(), "occupied by unmanaged content") {
		t.Fatalf("spoofed plugin: code=%d stdout=%q", code, stdout.String())
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
