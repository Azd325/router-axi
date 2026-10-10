package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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
	application.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	application.programExists = func(string) bool { return true }
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
		if agent == "opencode" {
			manifest := filepath.Join(home, ".config", "opencode", "package.json")
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var packageJSON struct {
				Dependencies map[string]string `json:"dependencies"`
			}
			if err := json.Unmarshal(data, &packageJSON); err != nil {
				t.Fatal(err)
			}
			if _, exists := packageJSON.Dependencies["@opencode-ai/plugin"]; exists || packageJSON.Dependencies == nil {
				t.Fatalf("owned dependency was not removed without preserving its container: %s", data)
			}
		}
	}
}

func TestSetupUninstallOpenCodePreservesUnownedDependencies(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		mutate   func(*testing.T, string)
		version  string
	}{
		{
			name:     "pre-existing",
			manifest: `{"name":"keep","dependencies":{"other":"1.0.0","@opencode-ai/plugin":"^1.18.30"}}`,
			version:  "^1.18.30",
		},
		{
			name:    "changed after install",
			version: "^2.0.0",
			mutate: func(t *testing.T, manifest string) {
				t.Helper()
				data, err := os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				var root map[string]any
				if err := json.Unmarshal(data, &root); err != nil {
					t.Fatal(err)
				}
				root["dependencies"].(map[string]any)["@opencode-ai/plugin"] = "^2.0.0"
				data, err = json.Marshal(root)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifest, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:    "legacy owner record",
			version: "^1.18.30",
			mutate: func(t *testing.T, manifest string) {
				t.Helper()
				marker := filepath.Join(filepath.Dir(manifest), "plugins", ".router-axi-session-hook")
				owner, err := readOwner(marker)
				if err != nil {
					t.Fatal(err)
				}
				owner.OpenCodeDependency = ""
				if err := writeOwner(marker, owner); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			manifest := filepath.Join(home, ".config", "opencode", "package.json")
			if test.manifest != "" {
				if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifest, []byte(test.manifest), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			application := setupApp(t, home)
			var stdout, stderr bytes.Buffer
			if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "opencode"}, &stdout, &stderr); code != ExitOK {
				t.Fatalf("install: code=%d stdout=%q", code, stdout.String())
			}
			if test.mutate != nil {
				test.mutate(t, manifest)
			}
			stdout.Reset()
			if code := application.Run(t.Context(), []string{"setup", "uninstall", "--agent", "opencode"}, &stdout, &stderr); code != ExitOK {
				t.Fatalf("uninstall: code=%d stdout=%q", code, stdout.String())
			}
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatal(err)
			}
			dependencies := root["dependencies"].(map[string]any)
			if dependencies["@opencode-ai/plugin"] != test.version {
				t.Fatalf("unowned dependency changed: %s", data)
			}
			if test.manifest != "" && (root["name"] != "keep" || dependencies["other"] != "1.0.0") {
				t.Fatalf("unrelated manifest content changed: %s", data)
			}
		})
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
	if len(config.Hooks.SessionStart) != 2 || config.Hooks.SessionStart[0].Hooks[0].Command != "keep-me" || config.Hooks.SessionStart[1].Hooks[0].Command != "'/opt/router-axi/router-axi' session dashboard 2>/dev/null || true # "+sessionHookMarker {
		t.Fatalf("settings lost unrelated hook or managed path: %+v", config)
	}
	application.executable = func() (string, error) { return "/new/router-axi", nil }
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("repair: code=%d stdout=%q", code, stdout.String())
	}
	config = readPersistedHookConfig(t, path)
	if len(config.Hooks.SessionStart) != 2 || config.Hooks.SessionStart[1].Hooks[0].Command != "'/new/router-axi' session dashboard 2>/dev/null || true # "+sessionHookMarker {
		t.Fatalf("managed path was not repaired: %+v", config)
	}
}

func TestSetupUninstallOpenCodeIgnoresMalformedManifest(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "opencode"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("install: code=%d stdout=%q", code, stdout.String())
	}
	manifest := filepath.Join(home, ".config", "opencode", "package.json")
	if err := os.WriteFile(manifest, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "uninstall", "--agent", "opencode"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "state: removed") {
		t.Fatalf("uninstall: code=%d stdout=%q", code, stdout.String())
	}
	data, err := os.ReadFile(manifest)
	if err != nil || string(data) != "not json" {
		t.Fatalf("malformed manifest changed: %q, %v", data, err)
	}
}

func TestSetupInstallOpenCodePreservesMalformedDependency(t *testing.T) {
	for _, test := range []struct {
		name       string
		dependency string
	}{
		{name: "null", dependency: "null"},
		{name: "number", dependency: "7"},
		{name: "object", dependency: `{"version":"^1.18.30"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			manifest := filepath.Join(home, ".config", "opencode", "package.json")
			original := []byte(`{"name":"keep","dependencies":{"@opencode-ai/plugin":` + test.dependency + `}}`)
			if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, original, 0o600); err != nil {
				t.Fatal(err)
			}
			application := setupApp(t, home)
			var stdout, stderr bytes.Buffer
			if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "opencode"}, &stdout, &stderr); code != ExitInternal {
				t.Fatalf("install: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, original) {
				t.Fatalf("malformed dependency manifest changed: got %q want %q", data, original)
			}
		})
	}
}

func TestSetupRollsBackWhenOwnerRecordCannotBeWritten(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	for _, agent := range []string{"claude", "opencode"} {
		marker := filepath.Join(home, "."+agent, ".router-axi-session-hook")
		if agent == "opencode" {
			marker = filepath.Join(home, ".config", "opencode", "plugins", ".router-axi-session-hook")
		}
		if err := os.MkdirAll(marker, 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), []string{"setup", "install", "--agent", agent}, &stdout, &stderr); code != ExitInternal {
			t.Fatalf("install %s: code=%d stdout=%q", agent, code, stdout.String())
		}
		path := filepath.Join(home, "."+agent, "settings.json")
		if agent == "opencode" {
			path = filepath.Join(home, ".config", "opencode", "plugins", "router-axi.ts")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("install %s left artifact: err=%v", agent, err)
		}
		if agent == "opencode" {
			manifest := filepath.Join(home, ".config", "opencode", "package.json")
			if _, err := os.Stat(manifest); !os.IsNotExist(err) {
				t.Fatalf("install %s left manifest: err=%v", agent, err)
			}
		}
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
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "install", "--agent", "claude"}, &stdout, &stderr); code != ExitInternal || !strings.Contains(stdout.String(), "no owner record") {
		t.Fatalf("orphaned marker: code=%d stdout=%q", code, stdout.String())
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

func runSetup(t *testing.T, application *App, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), append([]string{"setup"}, args...), &stdout, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	return code, stdout.String()
}

func codexConfigPath(home string) string { return filepath.Join(home, ".codex", "config.toml") }

func writeCodexConfigFixture(t *testing.T, home, content string) {
	t.Helper()
	path := codexConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestSetupCodexInstallEnablesHooksFeature(t *testing.T) {
	for _, test := range []struct {
		name, before, after, state string
	}{
		{"missing file", "", "[features]\nhooks = true\n", "added"},
		{"blank file", "\n\n", "[features]\nhooks = true\n", "added"},
		{"no features table", "model = \"x\" # keep\n\n[tools]\nweb = true", "model = \"x\" # keep\n\n[tools]\nweb = true\n\n[features]\nhooks = true\n", "added"},
		{"CRLF file", "model = \"x\"\r\n", "model = \"x\"\r\n\r\n[features]\r\nhooks = true\r\n", "added"},
		{"features without hooks", "# top\n[features]\nother = true\n\n# next\n[tools]\nweb = true\n", "# top\n[features]\nother = true\nhooks = true\n\n# next\n[tools]\nweb = true\n", "added"},
		{"features at end without newline", "[features]\nother = true", "[features]\nother = true\nhooks = true\n", "added"},
		{"false", "[features]\nhooks = false # off\nother = 1\n", "[features]\nhooks = true # off\nother = 1\n", "changed_from_false"},
		{"true", "[features]\nhooks = true\n", "[features]\nhooks = true\n", "enabled"},
		{"dotted true", "features.hooks = true\n", "features.hooks = true\n", "enabled"},
		{"dotted false", "features.hooks = false\n", "features.hooks = true\n", "changed_from_false"},
		{"multiline string", "text = \"\"\"\n[features]\nhooks = false\n\"\"\"\n", "text = \"\"\"\n[features]\nhooks = false\n\"\"\"\n\n[features]\nhooks = true\n", "added"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			if test.before != "" {
				writeCodexConfigFixture(t, home, test.before)
			}
			code, stdout := runSetup(t, setupApp(t, home), "install", "--agent", "codex")
			if code != ExitOK || !strings.Contains(stdout, "codex_hooks_feature: "+test.state+"\n") {
				t.Fatalf("code=%d stdout=%q", code, stdout)
			}
			data, err := os.ReadFile(codexConfigPath(home))
			if err != nil || string(data) != test.after {
				t.Fatalf("config=%q err=%v want %q", data, err, test.after)
			}
			if test.before != "" {
				info, _ := os.Stat(codexConfigPath(home))
				if info.Mode().Perm() != 0o640 {
					t.Fatalf("mode=%v", info.Mode().Perm())
				}
			}
		})
	}
}

func TestSetupCodexCheckReportsFeatureAndUninstallLeavesIt(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	code, stdout := runSetup(t, application, "check", "--agent", "codex", "--json")
	if code != ExitOK || !strings.Contains(stdout, `"codex_hooks_feature":"missing"`) {
		t.Fatalf("missing: code=%d stdout=%q", code, stdout)
	}
	writeCodexConfigFixture(t, home, "[features]\nhooks = false\n")
	if _, stdout = runSetup(t, application, "check", "--agent", "codex"); !strings.Contains(stdout, "codex_hooks_feature: disabled") {
		t.Fatalf("disabled: %q", stdout)
	}
	if code, stdout = runSetup(t, application, "install", "--agent", "codex"); code != ExitOK || !strings.Contains(stdout, "codex_hooks_feature: changed_from_false") {
		t.Fatalf("install: code=%d stdout=%q", code, stdout)
	}
	if _, stdout = runSetup(t, application, "check", "--agent", "codex"); !strings.Contains(stdout, "state: installed") || !strings.Contains(stdout, "codex_hooks_feature: enabled") {
		t.Fatalf("enabled: %q", stdout)
	}
	if code, stdout = runSetup(t, application, "uninstall", "--agent", "codex"); code != ExitOK || strings.Contains(stdout, "codex_hooks_feature") {
		t.Fatalf("uninstall: code=%d stdout=%q", code, stdout)
	}
	data, _ := os.ReadFile(codexConfigPath(home))
	if string(data) != "[features]\nhooks = true\n" {
		t.Fatalf("uninstall changed config: %q", data)
	}
}

func TestSetupCodexRefusesUnsafeConfigWithoutWriting(t *testing.T) {
	for name, content := range map[string]string{
		"inline table":     "features = { hooks = false }\n",
		"other dotted key": "features.other = true\n",
		"string value":     "[features]\nhooks = \"yes\"\n",
		"duplicate hooks":  "[features]\nhooks = true\nhooks = false\n",
		"array of tables":  "[[features]]\nhooks = true\n",
		"duplicate table":  "[features]\na = 1\n[features]\nb = 1\n",
		"hooks sub table":  "[features]\nhooks.x = 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			writeCodexConfigFixture(t, home, content)
			code, stdout := runSetup(t, setupApp(t, home), "install", "--agent", "codex")
			if code != ExitInternal || !strings.Contains(stdout, "set [features].hooks = true manually") {
				t.Fatalf("code=%d stdout=%q", code, stdout)
			}
			data, _ := os.ReadFile(codexConfigPath(home))
			if string(data) != content {
				t.Fatalf("config changed: %q", data)
			}
			if _, err := os.Stat(filepath.Join(home, ".codex", "hooks.json")); !os.IsNotExist(err) {
				t.Fatalf("hook written despite refusal: %v", err)
			}
			if _, stdout = runSetup(t, setupApp(t, home), "check", "--agent", "codex"); !strings.Contains(stdout, "codex_hooks_feature: unverifiable") {
				t.Fatalf("check: %q", stdout)
			}
		})
	}
}

func TestSetupCodexOtherAgentsNeverTouchConfig(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	for _, agent := range []string{"claude", "opencode"} {
		if code, stdout := runSetup(t, application, "install", "--agent", agent); code != ExitOK || strings.Contains(stdout, "codex_hooks_feature") {
			t.Fatalf("%s: code=%d stdout=%q", agent, code, stdout)
		}
	}
	if _, err := os.Stat(codexConfigPath(home)); !os.IsNotExist(err) {
		t.Fatalf("config.toml created: %v", err)
	}
}

func TestSetupHookCommandUsesBinaryNameOnlyWhenPathResolvesToExecutable(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "real-router-axi")
	other := filepath.Join(dir, "other-router-axi")
	for _, path := range []string{binary, other} {
		if err := os.WriteFile(path, []byte(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "router-axi")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, found, want string
	}{
		{"same file through symlink", link, "router-axi session dashboard 2>/dev/null || true # " + sessionHookMarker},
		{"different file", other, shellQuote(binary) + " session dashboard 2>/dev/null || true # " + sessionHookMarker},
		{"not on PATH", "", shellQuote(binary) + " session dashboard 2>/dev/null || true # " + sessionHookMarker},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			application := setupApp(t, home)
			application.executable = func() (string, error) { return binary, nil }
			application.lookPath = func(string) (string, error) {
				if test.found == "" {
					return "", exec.ErrNotFound
				}
				return test.found, nil
			}
			if code, stdout := runSetup(t, application, "install", "--agent", "claude"); code != ExitOK {
				t.Fatalf("code=%d stdout=%q", code, stdout)
			}
			config := readPersistedHookConfig(t, filepath.Join(home, ".claude", "settings.json"))
			if got := config.Hooks.SessionStart[0].Hooks[0].Command; got != test.want {
				t.Fatalf("command=%q want %q", got, test.want)
			}
			if code, stdout := runSetup(t, application, "check", "--agent", "claude"); code != ExitOK || !strings.Contains(stdout, "state: installed") {
				t.Fatalf("check: code=%d stdout=%q", code, stdout)
			}
		})
	}
}

func TestSetupRepairsLegacyHookAndPluginShapes(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	if code, stdout := runSetup(t, application, "install", "--agent", "claude"); code != ExitOK {
		t.Fatalf("install: code=%d stdout=%q", code, stdout)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	data, _ := os.ReadFile(settings)
	legacy := strings.ReplaceAll(strings.ReplaceAll(string(data), " 2\\u003e/dev/null || true", ""), " 2>/dev/null || true", "")
	if legacy == string(data) {
		t.Fatal("fixture was not rewritten to the legacy command")
	}
	if err := os.WriteFile(settings, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout := runSetup(t, application, "check", "--agent", "claude"); code != ExitOK || !strings.Contains(stdout, "state: installed") {
		t.Fatalf("legacy check: code=%d stdout=%q", code, stdout)
	}
	if code, _ := runSetup(t, application, "install", "--agent", "claude"); code != ExitOK {
		t.Fatalf("legacy repair failed")
	}
	if config := readPersistedHookConfig(t, settings); len(config.Hooks.SessionStart) != 1 || !strings.Contains(config.Hooks.SessionStart[0].Hooks[0].Command, "2>/dev/null || true") {
		t.Fatalf("legacy hook not repaired: %+v", config)
	}

	plugin := filepath.Join(home, ".config", "opencode", "plugins", "router-axi.ts")
	if code, stdout := runSetup(t, application, "install", "--agent", "opencode"); code != ExitOK {
		t.Fatalf("install: code=%d stdout=%q", code, stdout)
	}
	command, err := application.sessionCommand()
	if err != nil {
		t.Fatal(err)
	}
	legacyCommand := strings.ReplaceAll(command, " 2>/dev/null || true", "")
	legacyPlugin := legacyOpenCodePluginPrefix + jsonStringCommand(legacyCommand) + legacyOpenCodePluginSuffix
	if err := os.WriteFile(plugin, []byte(legacyPlugin), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := readOwner(filepath.Join(filepath.Dir(plugin), ".router-axi-session-hook"))
	if err != nil {
		t.Fatal(err)
	}
	_, owner.ShapeHash, _ = openCodePluginParts([]byte(legacyPlugin))
	if err := writeOwner(filepath.Join(filepath.Dir(plugin), ".router-axi-session-hook"), owner); err != nil {
		t.Fatal(err)
	}
	if code, stdout := runSetup(t, application, "check", "--agent", "opencode"); code != ExitOK || !strings.Contains(stdout, "state: installed") {
		t.Fatalf("legacy plugin check: code=%d stdout=%q", code, stdout)
	}
	if code, stdout := runSetup(t, application, "install", "--agent", "opencode"); code != ExitOK {
		t.Fatalf("legacy plugin repair: code=%d stdout=%q", code, stdout)
	}
	data, _ = os.ReadFile(plugin)
	if !strings.Contains(string(data), "try {") || !strings.Contains(string(data), "timeout: 5000") || !strings.Contains(string(data), "} catch {") {
		t.Fatalf("plugin lacks failure guard: %s", data)
	}
}

func TestSetupCheckReportsStaleWhenProgramIsGone(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			home := t.TempDir()
			application := setupApp(t, home)
			if code, stdout := runSetup(t, application, "install", "--agent", agent); code != ExitOK {
				t.Fatalf("install: code=%d stdout=%q", code, stdout)
			}
			application.programExists = func(string) bool { return false }
			if code, stdout := runSetup(t, application, "check", "--agent", agent); code != ExitOK || !strings.Contains(stdout, "state: stale") {
				t.Fatalf("check: code=%d stdout=%q", code, stdout)
			}
			application.programExists = func(string) bool { return true }
			if _, stdout := runSetup(t, application, "check", "--agent", agent); !strings.Contains(stdout, "state: installed") {
				t.Fatalf("check after restore: %q", stdout)
			}
		})
	}
}

func TestSetupCheckReportsStaleWhenBinaryNameLeavesPath(t *testing.T) {
	home := t.TempDir()
	application := setupApp(t, home)
	binary := filepath.Join(t.TempDir(), "router-axi")
	if err := os.WriteFile(binary, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	application.executable = func() (string, error) { return binary, nil }
	application.lookPath = func(string) (string, error) { return binary, nil }
	if code, stdout := runSetup(t, application, "install", "--agent", "claude"); code != ExitOK {
		t.Fatalf("install: code=%d stdout=%q", code, stdout)
	}
	application.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if _, stdout := runSetup(t, application, "check", "--agent", "claude"); !strings.Contains(stdout, "state: stale") {
		t.Fatalf("check: %q", stdout)
	}
}

func TestSetupHookCommandFailsSafe(t *testing.T) {
	application := setupApp(t, t.TempDir())
	application.executable = func() (string, error) { return "/nonexistent/router-axi", nil }
	command, err := application.sessionCommand()
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "-c", command).CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("hook command failed loudly: err=%v out=%q", err, out)
	}
}
