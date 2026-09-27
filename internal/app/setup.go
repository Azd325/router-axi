package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const sessionHookMarker = "router-axi-session-hook"

type setupResult struct {
	Agent   string `json:"agent"`
	State   string `json:"state"`
	Command string `json:"command,omitempty"`
}

type hookLocation struct {
	agent        string
	path         string
	manifestPath string
}

func (a *App) runSession(opts options, stdout io.Writer) int {
	if opts.action != "dashboard" {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", "session accepts one action: dashboard", "router-axi session --help")
	}
	if opts.json {
		return writeJSON(stdout, map[string]any{"session": map[string]string{"context": "offline", "version": a.Version}})
	}
	_, err := fmt.Fprintf(stdout, "session:\n  context: offline\n  version: %s\nnext:\n  run: router-axi status\n  diagnose: router-axi doctor\n", a.Version)
	if err != nil {
		return ExitInternal
	}
	return ExitOK
}

func (a *App) runSetup(opts options, stdout io.Writer) int {
	if opts.agent == "" {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", "setup requires --agent claude, codex, opencode, or all", "router-axi setup --help")
	}
	agents, err := setupAgents(opts.agent)
	if err != nil {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", err.Error(), "router-axi setup --help")
	}
	if opts.action == "" {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", "setup requires one action: install, check, or uninstall", "router-axi setup --help")
	}
	results := make([]setupResult, 0, len(agents))
	for _, agent := range agents {
		result, err := a.setupAgent(opts.action, agent)
		if err != nil {
			return writeError(stdout, opts.json, ExitInternal, "setup_failed", err.Error(), "router-axi setup check --agent "+agent)
		}
		results = append(results, result)
	}
	if opts.json {
		return writeJSON(stdout, map[string]any{"setup": results})
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(stdout, "setup:\n  agent: %s\n  state: %s\n", result.Agent, result.State); err != nil {
			return ExitInternal
		}
		if result.Command != "" {
			if _, err := fmt.Fprintf(stdout, "  command: %s\n", result.Command); err != nil {
				return ExitInternal
			}
		}
	}
	return ExitOK
}

func setupAgents(value string) ([]string, error) {
	if value == "all" {
		return []string{"claude", "codex", "opencode"}, nil
	}
	if value == "claude" || value == "codex" || value == "opencode" {
		return []string{value}, nil
	}
	return nil, errors.New("--agent must be claude, codex, opencode, or all")
}

func (a *App) setupAgent(action, agent string) (setupResult, error) {
	home, err := a.homeDir()
	if err != nil {
		return setupResult{}, fmt.Errorf("resolve home directory: %w", err)
	}
	location := hookLocation{agent: agent}
	switch agent {
	case "claude":
		location.path = filepath.Join(home, ".claude", "settings.json")
	case "codex":
		location.path = filepath.Join(home, ".codex", "hooks.json")
	case "opencode":
		location.path = filepath.Join(home, ".config", "opencode", "plugins", "router-axi.ts")
		location.manifestPath = filepath.Join(home, ".config", "opencode", "package.json")
	}
	if agent == "opencode" {
		if action == "install" {
			command, err := a.sessionCommand()
			if err != nil {
				return setupResult{}, err
			}
			return manageOpenCodePlugin(action, location, command)
		}
		return manageOpenCodePlugin(action, location, "")
	}
	command := ""
	if action == "install" {
		command, err = a.sessionCommand()
		if err != nil {
			return setupResult{}, err
		}
	}
	return manageHookJSON(action, location, command)
}

func (a *App) sessionCommand() (string, error) {
	path, err := a.executable()
	if err != nil || path == "" {
		return "", errors.New("resolve router-axi executable")
	}
	return shellQuote(path) + " session dashboard # " + managedCommandMarker(path), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func manageHookJSON(action string, location hookLocation, command string) (setupResult, error) {
	root := map[string]any{}
	data, err := os.ReadFile(location.path)
	if err != nil && !os.IsNotExist(err) {
		return setupResult{}, err
	}
	if len(data) > 0 && json.Unmarshal(data, &root) != nil {
		return setupResult{}, errors.New("managed hook configuration is not valid JSON")
	}
	if root == nil {
		return setupResult{}, errors.New("managed hook configuration must be a JSON object")
	}
	hooksValue, hooksPresent := root["hooks"]
	if hooksPresent {
		if _, ok := hooksValue.(map[string]any); !ok {
			return setupResult{}, errors.New("managed hook configuration has incompatible hooks value")
		}
	} else {
		hooksValue = map[string]any{}
	}
	hooks := hooksValue.(map[string]any)
	root["hooks"] = hooks
	entriesValue, entriesPresent := hooks["SessionStart"]
	entries := []any(nil)
	if entriesPresent {
		var ok bool
		entries, ok = entriesValue.([]any)
		if !ok {
			return setupResult{}, errors.New("managed hook configuration has incompatible SessionStart value")
		}
	}
	managed := make([]int, 0, 1)
	for i, entry := range entries {
		if isManagedHook(entry) {
			managed = append(managed, i)
		}
	}
	if len(managed) > 1 {
		return setupResult{}, errors.New("multiple managed router-axi session hooks found; refuse ambiguous configuration")
	}
	state := "missing"
	if len(managed) == 1 {
		state = "installed"
	}
	switch action {
	case "check":
		return setupResult{Agent: location.agent, State: state, Command: command}, nil
	case "install":
		entry := map[string]any{"matcher": "", "hooks": []any{map[string]any{"type": "command", "command": command}}}
		if len(managed) == 1 {
			entries[managed[0]] = entry
			state = "installed"
		} else {
			entries = append(entries, entry)
			state = "installed"
		}
	case "uninstall":
		if len(managed) == 0 {
			return setupResult{Agent: location.agent, State: "missing"}, nil
		}
		entries = append(entries[:managed[0]], entries[managed[0]+1:]...)
		state = "removed"
	default:
		return setupResult{}, errors.New("setup accepts one action: install, check, or uninstall")
	}
	hooks["SessionStart"] = entries
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return setupResult{}, err
	}
	if err := atomicWrite(location.path, append(encoded, '\n'), 0o600); err != nil {
		return setupResult{}, err
	}
	return setupResult{Agent: location.agent, State: state, Command: command}, nil
}

func isManagedHook(value any) bool {
	entry, ok := value.(map[string]any)
	if !ok || len(entry) != 2 || entry["matcher"] != "" {
		return false
	}
	hooks, ok := entry["hooks"].([]any)
	if !ok || len(hooks) != 1 {
		return false
	}
	hook, ok := hooks[0].(map[string]any)
	if !ok || len(hook) != 2 || hook["type"] != "command" {
		return false
	}
	command, ok := hook["command"].(string)
	return ok && isManagedCommand(command)
}

func isManagedCommand(command string) bool {
	const separator = "' session dashboard # " + sessionHookMarker + ":"
	if !strings.HasPrefix(command, "'") {
		return false
	}
	separatorIndex := strings.LastIndex(command, separator)
	if separatorIndex < 1 {
		return false
	}
	encodedPath := command[1:separatorIndex]
	path := strings.ReplaceAll(encodedPath, "'\\''", "'")
	return shellQuote(path)+" session dashboard # "+managedCommandMarker(path) == command
}

func managedCommandMarker(path string) string {
	digest := sha256.Sum256([]byte(path))
	return sessionHookMarker + ":" + hex.EncodeToString(digest[:])
}

func openCodeDependency(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return false, errors.New("OpenCode package.json is not a JSON object")
	}
	dependencies, ok := root["dependencies"].(map[string]any)
	if !ok {
		return false, nil
	}
	_, ok = dependencies["@opencode-ai/plugin"].(string)
	return ok, nil
}

func ensureOpenCodeDependency(path string) error {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 && (json.Unmarshal(data, &root) != nil || root == nil) {
		return errors.New("OpenCode package.json is not a JSON object")
	}
	dependencies, ok := root["dependencies"].(map[string]any)
	if !ok {
		if _, exists := root["dependencies"]; exists {
			return errors.New("OpenCode package.json has incompatible dependencies")
		}
		dependencies = map[string]any{}
		root["dependencies"] = dependencies
	}
	if _, ok := dependencies["@opencode-ai/plugin"].(string); ok {
		return nil
	}
	dependencies["@opencode-ai/plugin"] = "^1.18.30"
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(encoded, '\n'), 0o600)
}

func manageOpenCodePlugin(action string, location hookLocation, command string) (setupResult, error) {
	data, err := os.ReadFile(location.path)
	if err != nil && !os.IsNotExist(err) {
		return setupResult{}, err
	}
	managed := isManagedOpenCodePlugin(data)
	if len(data) > 0 && !managed {
		return setupResult{}, errors.New("OpenCode plugin path is occupied by unmanaged content")
	}
	switch action {
	case "check":
		dependency, err := openCodeDependency(location.manifestPath)
		if err != nil {
			return setupResult{}, err
		}
		state := "missing"
		if managed && dependency {
			state = "installed"
		}
		return setupResult{Agent: location.agent, State: state, Command: command}, nil
	case "uninstall":
		if !managed {
			return setupResult{Agent: location.agent, State: "missing"}, nil
		}
		if err := os.Remove(location.path); err != nil {
			return setupResult{}, err
		}
		return setupResult{Agent: location.agent, State: "removed"}, nil
	case "install":
		if err := ensureOpenCodeDependency(location.manifestPath); err != nil {
			return setupResult{}, err
		}
		plugin := "// " + sessionHookMarker + "\nimport { execFileSync } from \"node:child_process\";\nimport type { Plugin } from \"@opencode-ai/plugin\";\n\nconst injected = new Set<string>();\n\nexport const RouterAxiPlugin: Plugin = async () => ({\n  \"experimental.chat.system.transform\": async (input, output) => {\n    if (input.sessionID && injected.has(input.sessionID)) return;\n    const context = execFileSync(" + jsonStringCommand(command) + ", { encoding: \"utf8\", shell: true, stdio: [\"ignore\", \"pipe\", \"ignore\"] });\n    if (input.sessionID) injected.add(input.sessionID);\n    output.system.push(context);\n  },\n});\n\nexport default RouterAxiPlugin;\n"
		if err := atomicWrite(location.path, []byte(plugin), 0o600); err != nil {
			return setupResult{}, err
		}
		return setupResult{Agent: location.agent, State: "installed", Command: command}, nil
	default:
		return setupResult{}, errors.New("setup accepts one action: install, check, or uninstall")
	}
}

func isManagedOpenCodePlugin(data []byte) bool {
	content := string(data)
	prefix := "// " + sessionHookMarker + "\nimport { execFileSync } from \"node:child_process\";\nimport type { Plugin } from \"@opencode-ai/plugin\";\n\nconst injected = new Set<string>();\n\nexport const RouterAxiPlugin: Plugin = async () => ({\n  \"experimental.chat.system.transform\": async (input, output) => {\n    if (input.sessionID && injected.has(input.sessionID)) return;\n    const context = execFileSync("
	suffix := ", { encoding: \"utf8\", shell: true, stdio: [\"ignore\", \"pipe\", \"ignore\"] });\n    if (input.sessionID) injected.add(input.sessionID);\n    output.system.push(context);\n  },\n});\n\nexport default RouterAxiPlugin;\n"
	if !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, suffix) {
		return false
	}
	encodedCommand := strings.TrimSuffix(strings.TrimPrefix(content, prefix), suffix)
	var command string
	return json.Unmarshal([]byte(encodedCommand), &command) == nil && isManagedCommand(command)
}

func jsonStringCommand(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".router-axi-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
