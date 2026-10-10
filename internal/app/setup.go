package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	sessionHookMarker        = "router-axi-session-hook"
	sessionProgramName       = "router-axi"
	sessionCommandTail       = " session dashboard 2>/dev/null || true # " + sessionHookMarker
	legacySessionCommandTail = " session dashboard # " + sessionHookMarker
	openCodePluginDependency = "^1.18.30"

	openCodePluginPrefix = "// " + sessionHookMarker + "\nimport { execFileSync } from \"node:child_process\";\nimport type { Plugin } from \"@opencode-ai/plugin\";\n\nconst injected = new Set<string>();\n\nexport const RouterAxiPlugin: Plugin = async () => ({\n  \"experimental.chat.system.transform\": async (input, output) => {\n    if (input.sessionID && injected.has(input.sessionID)) return;\n    let context = \"\";\n    try {\n      context = execFileSync("
	openCodePluginSuffix = ", { encoding: \"utf8\", shell: true, timeout: 5000, stdio: [\"ignore\", \"pipe\", \"ignore\"] });\n    } catch {\n      return;\n    }\n    if (!context.trim()) return;\n    if (input.sessionID) injected.add(input.sessionID);\n    output.system.push(context);\n  },\n});\n\nexport default RouterAxiPlugin;\n"

	legacyOpenCodePluginPrefix = "// " + sessionHookMarker + "\nimport { execFileSync } from \"node:child_process\";\nimport type { Plugin } from \"@opencode-ai/plugin\";\n\nconst injected = new Set<string>();\n\nexport const RouterAxiPlugin: Plugin = async () => ({\n  \"experimental.chat.system.transform\": async (input, output) => {\n    if (input.sessionID && injected.has(input.sessionID)) return;\n    const context = execFileSync("
	legacyOpenCodePluginSuffix = ", { encoding: \"utf8\", shell: true, stdio: [\"ignore\", \"pipe\", \"ignore\"] });\n    if (input.sessionID) injected.add(input.sessionID);\n    output.system.push(context);\n  },\n});\n\nexport default RouterAxiPlugin;\n"
)

type setupResult struct {
	Agent             string `json:"agent"`
	State             string `json:"state"`
	Command           string `json:"command,omitempty"`
	CodexHooksFeature string `json:"codex_hooks_feature,omitempty"`
}

type ownerRecord struct {
	Command            string `json:"command"`
	ShapeHash          string `json:"shape_hash"`
	OpenCodeDependency string `json:"opencode_dependency,omitempty"`
}

var (
	errHookConfigNotJSON         = errors.New("managed hook configuration is not valid JSON")
	errHookConfigNotObject       = errors.New("managed hook configuration must be a JSON object")
	errHookConfigHooks           = errors.New("managed hook configuration has incompatible hooks value")
	errHookConfigSessionStart    = errors.New("managed hook configuration has incompatible SessionStart value")
	errHookAmbiguous             = errors.New("multiple managed router-axi session hooks found; refuse ambiguous configuration")
	errHookNoOwner               = errors.New("managed session hook has no owner record; refuse ambiguous configuration")
	errOpenCodePathOccupied      = errors.New("OpenCode plugin path is occupied by unmanaged content")
	errOpenCodeManifestNotObject = errors.New("OpenCode package.json is not a JSON object")
	errOpenCodeDependencies      = errors.New("OpenCode package.json has incompatible dependencies")
	errOpenCodePluginDependency  = errors.New("OpenCode package.json has incompatible @opencode-ai/plugin dependency")
)

type hookLocation struct {
	agent        string
	path         string
	configPath   string
	manifestPath string
	markerPath   string
}

func (a *App) runSession(opts options, stdout io.Writer) int {
	if opts.action != "dashboard" {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", "session accepts one action: dashboard", "router-axi session dashboard")
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
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", "setup requires --agent claude, codex, opencode, or all", "router-axi setup "+opts.action+" --agent all")
	}
	agents, err := setupAgents(opts.agent)
	if err != nil {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", err.Error(), "router-axi setup "+opts.action+" --agent all")
	}
	if opts.action == "" {
		return writeError(stdout, opts.json, ExitUsage, "invalid_arguments", "setup requires one action: install, check, or uninstall", "router-axi setup check --agent "+opts.agent)
	}
	results := make([]setupResult, 0, len(agents))
	for _, agent := range agents {
		result, err := a.setupAgent(opts.action, agent)
		if err != nil {
			return writeError(stdout, opts.json, ExitInternal, "setup_failed", a.setupFailureMessage(err), a.setupFailureHint(err, opts.action, agent))
		}
		results = append(results, result)
	}
	if opts.json {
		return writeJSON(stdout, map[string]any{"setup": results})
	}
	if len(results) > 1 {
		if err := writeSetupTable(stdout, results); err != nil {
			return ExitInternal
		}
		return ExitOK
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
		if result.CodexHooksFeature != "" {
			if _, err := fmt.Fprintf(stdout, "  codex_hooks_feature: %s\n", result.CodexHooksFeature); err != nil {
				return ExitInternal
			}
		}
	}
	return ExitOK
}

func writeSetupTable(w io.Writer, results []setupResult) error {
	return writeTable(w, "setup", results, []tableColumn[setupResult]{
		{"agent", func(r setupResult) string { return toon(r.Agent) }},
		{"state", func(r setupResult) string { return toon(r.State) }},
		{"command", func(r setupResult) string { return toon(r.Command) }},
		{"codex_hooks_feature", func(r setupResult) string { return toon(r.CodexHooksFeature) }},
	}, []string{"agent", "state", "command", "codex_hooks_feature"}, nil)
}

func (a *App) setupFailureMessage(err error) string {
	home, _ := a.homeDir()
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &pathErr):
		return fileFailureMessage(pathErr.Op, pathErr.Path, pathErr.Err, home)
	case errors.As(err, &linkErr):
		return fileFailureMessage("rename", linkErr.New, linkErr.Err, home)
	case errors.Unwrap(err) != nil:
		return "setup failed for an unexpected reason"
	}
	return err.Error()
}

func fileFailureMessage(op, path string, cause error, home string) string {
	name := displayPath(path, home)
	switch {
	case errors.Is(cause, fs.ErrPermission):
		return "permission denied for " + name
	case op == "open" || op == "read" || op == "readfile" || op == "stat" || op == "lstat" || op == "readlink":
		return "cannot read " + name
	}
	return "cannot write " + name
}

func displayPath(path, home string) string {
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok && home != "" {
		return "~/" + filepath.ToSlash(rest)
	}
	return filepath.Base(path)
}

func (a *App) setupFailureHint(err error, action, agent string) string {
	retry := ", then run router-axi setup " + action + " --agent " + agent
	home, _ := a.homeDir()
	location := hookLocationFor(home, agent)
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &pathErr) && errors.Is(pathErr.Err, fs.ErrPermission):
		return "fix the permissions of " + displayPath(pathErr.Path, home) + retry
	case errors.As(err, &pathErr):
		return "repair " + displayPath(pathErr.Path, home) + retry
	case errors.As(err, &linkErr):
		return "repair " + displayPath(linkErr.New, home) + retry
	case errors.Is(err, errHookConfigNotJSON), errors.Is(err, errHookConfigNotObject), errors.Is(err, errHookConfigHooks), errors.Is(err, errHookConfigSessionStart):
		return "correct " + displayPath(location.path, home) + " so that it is a JSON object with compatible hooks" + retry
	case errors.Is(err, errHookAmbiguous), errors.Is(err, errHookNoOwner):
		return "remove the router-axi session hook entries from " + displayPath(location.path, home) + retry
	case errors.Is(err, errOpenCodePathOccupied):
		return "move " + displayPath(location.path, home) + " away" + retry
	case errors.Is(err, errOpenCodeManifestNotObject), errors.Is(err, errOpenCodeDependencies), errors.Is(err, errOpenCodePluginDependency):
		return "correct " + displayPath(location.manifestPath, home) + " so that it is a JSON object with compatible dependencies" + retry
	case errors.Is(err, errCodexConfigUnsafe):
		return "make " + displayPath(location.configPath, home) + " define features once, as one [features] table with the line hooks = true or as the single line features.hooks = true" + retry
	}
	return "router-axi setup --help"
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
	location := hookLocationFor(home, agent)
	owner, err := readOwner(location.markerPath)
	if err != nil {
		return setupResult{}, err
	}
	if agent == "opencode" {
		if action == "install" {
			command, err := a.sessionCommand()
			if err != nil {
				return setupResult{}, err
			}
			return manageOpenCodePlugin(action, location, command, owner, a.commandStale)
		}
		return manageOpenCodePlugin(action, location, "", owner, a.commandStale)
	}
	command := ""
	if action == "install" {
		command, err = a.sessionCommand()
		if err != nil {
			return setupResult{}, err
		}
	}
	if agent != "codex" {
		return manageHookJSON(action, location, command, owner, a.commandStale)
	}
	return manageCodexHook(action, location, command, owner, a.commandStale)
}

func hookLocationFor(home, agent string) hookLocation {
	location := hookLocation{agent: agent}
	switch agent {
	case "claude":
		location.path = filepath.Join(home, ".claude", "settings.json")
		location.markerPath = filepath.Join(home, ".claude", ".router-axi-session-hook")
	case "codex":
		location.path = filepath.Join(home, ".codex", "hooks.json")
		location.configPath = filepath.Join(home, ".codex", "config.toml")
		location.markerPath = filepath.Join(home, ".codex", ".router-axi-session-hook")
	case "opencode":
		location.path = filepath.Join(home, ".config", "opencode", "plugins", "router-axi.ts")
		location.manifestPath = filepath.Join(home, ".config", "opencode", "package.json")
		location.markerPath = filepath.Join(home, ".config", "opencode", "plugins", ".router-axi-session-hook")
	}
	return location
}

func manageCodexHook(action string, location hookLocation, command string, owner ownerRecord, stale func(string) bool) (setupResult, error) {
	var config []byte
	var feature codexHooksUpdate
	if action == "install" {
		var err error
		if config, err = readCodexConfig(location.configPath); err != nil {
			return setupResult{}, err
		}
		if feature, err = enableCodexHooks(string(config)); err != nil {
			return setupResult{}, err
		}
	}
	result, err := manageHookJSON(action, location, command, owner, stale)
	if err != nil {
		return setupResult{}, err
	}
	switch action {
	case "install":
		if feature.Content != string(config) {
			if err := writeCodexConfig(location.configPath, []byte(feature.Content)); err != nil {
				return setupResult{}, err
			}
		}
		result.CodexHooksFeature = feature.State
	case "check":
		data, err := readCodexConfig(location.configPath)
		if err != nil {
			return setupResult{}, err
		}
		result.CodexHooksFeature = codexHooksState(string(data))
	}
	return result, nil
}

func (a *App) sessionCommand() (string, error) {
	path, err := a.executable()
	if err != nil || path == "" {
		return "", errors.New("resolve router-axi executable")
	}
	program := shellQuote(path)
	if a.pathResolvesTo(path) {
		program = sessionProgramName
	}
	return program + sessionCommandTail, nil
}

func (a *App) pathResolvesTo(path string) bool {
	found, err := a.lookPath(sessionProgramName)
	if err != nil {
		return false
	}
	foundInfo, err := os.Stat(found)
	if err != nil {
		return false
	}
	pathInfo, err := os.Stat(path)
	return err == nil && os.SameFile(foundInfo, pathInfo)
}

func (a *App) commandStale(command string) bool {
	program, ok := sessionProgram(command)
	if !ok {
		return false
	}
	if program == sessionProgramName {
		_, err := a.lookPath(program)
		return err != nil
	}
	return !a.programExists(program)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func manageHookJSON(action string, location hookLocation, command string, owner ownerRecord, stale func(string) bool) (setupResult, error) {
	root := map[string]any{}
	data, err := os.ReadFile(location.path)
	if err != nil && !os.IsNotExist(err) {
		return setupResult{}, err
	}
	if len(data) > 0 && json.Unmarshal(data, &root) != nil {
		return setupResult{}, errHookConfigNotJSON
	}
	if root == nil {
		return setupResult{}, errHookConfigNotObject
	}
	hooksValue, hooksPresent := root["hooks"]
	if hooksPresent {
		if _, ok := hooksValue.(map[string]any); !ok {
			return setupResult{}, errHookConfigHooks
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
			return setupResult{}, errHookConfigSessionStart
		}
	}
	managed := make([]int, 0, 1)
	for i, entry := range entries {
		if isManagedHook(entry, owner) {
			managed = append(managed, i)
		}
	}
	if len(managed) > 1 {
		return setupResult{}, errHookAmbiguous
	}
	state := "missing"
	if len(managed) == 1 {
		state = "installed"
		if hookCommand, _ := managedEntryCommand(entries[managed[0]]); stale(hookCommand) {
			state = "stale"
		}
	}
	switch action {
	case "check":
		return setupResult{Agent: location.agent, State: state, Command: command}, nil
	case "install":
		if owner.ShapeHash == "" && hasSessionMarker(entries) {
			return setupResult{}, errHookNoOwner
		}
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
			removeMarker(location.markerPath)
			return setupResult{Agent: location.agent, State: "missing"}, nil
		}
		entries = append(entries[:managed[0]], entries[managed[0]+1:]...)
		state = "removed"
	default:
		return setupResult{}, errors.New("setup accepts one action: install, check, or uninstall")
	}
	hooks["SessionStart"] = entries
	var shapeHash string
	if action == "install" {
		if len(managed) == 1 {
			shapeHash, _ = hookShapeHash(entries[managed[0]])
		} else {
			shapeHash, _ = hookShapeHash(entries[len(entries)-1])
		}
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return setupResult{}, err
	}
	if err := atomicWrite(location.path, append(encoded, '\n'), 0o600); err != nil {
		return setupResult{}, err
	}
	switch action {
	case "uninstall":
		removeMarker(location.markerPath)
	case "install":
		if err := writeOwner(location.markerPath, ownerRecord{Command: command, ShapeHash: shapeHash}); err != nil {
			if restoreErr := restoreFile(location.path, data, len(data) > 0); restoreErr != nil {
				return setupResult{}, fmt.Errorf("write managed session owner: %w; rollback configuration: %v", err, restoreErr)
			}
			return setupResult{}, err
		}
	}
	return setupResult{Agent: location.agent, State: state, Command: command}, nil
}

func isManagedHook(value any, owner ownerRecord) bool {
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
	shapeHash, valid := hookShapeHash(entry)
	return ok && valid && shapeHash == owner.ShapeHash && isManagedCommand(command, owner)
}

func managedEntryCommand(value any) (string, bool) {
	entry, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	hooks, ok := entry["hooks"].([]any)
	if !ok || len(hooks) != 1 {
		return "", false
	}
	hook, ok := hooks[0].(map[string]any)
	if !ok {
		return "", false
	}
	command, ok := hook["command"].(string)
	return command, ok
}

func isManagedCommand(command string, owner ownerRecord) bool {
	return owner.ShapeHash != "" && (command == owner.Command || isSessionCommand(command))
}

func isSessionCommand(command string) bool {
	_, ok := sessionProgram(command)
	return ok
}

func sessionProgram(command string) (string, bool) {
	for _, tail := range []string{sessionCommandTail, legacySessionCommandTail} {
		head, found := strings.CutSuffix(command, tail)
		if !found {
			continue
		}
		if head == sessionProgramName {
			return head, true
		}
		if !strings.HasPrefix(head, "'") || !strings.HasSuffix(head, "'") || len(head) < 2 {
			continue
		}
		path := strings.ReplaceAll(head[1:len(head)-1], "'\\''", "'")
		if shellQuote(path) == head {
			return path, true
		}
	}
	return "", false
}

func hookShapeHash(value any) (string, bool) {
	entry, ok := value.(map[string]any)
	if !ok || entry["matcher"] != "" {
		return "", false
	}
	hooks, ok := entry["hooks"].([]any)
	if !ok || len(hooks) != 1 {
		return "", false
	}
	hook, ok := hooks[0].(map[string]any)
	if !ok || hook["type"] != "command" {
		return "", false
	}
	normalized := map[string]any{"matcher": "", "hooks": []any{map[string]any{"type": "command", "command": sessionHookMarker}}}
	data, err := json.Marshal(normalized)
	return contentHash(data), err == nil
}

func hasSessionMarker(entries []any) bool {
	for _, entry := range entries {
		value, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hooks, ok := value["hooks"].([]any)
		if !ok {
			continue
		}
		for _, hook := range hooks {
			item, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			command, ok := item["command"].(string)
			if ok && strings.Contains(command, "# "+sessionHookMarker) {
				return true
			}
		}
	}
	return false
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
		return false, errOpenCodeManifestNotObject
	}
	dependencies, ok := root["dependencies"].(map[string]any)
	if !ok {
		return false, nil
	}
	_, ok = dependencies["@opencode-ai/plugin"].(string)
	return ok, nil
}

func ensureOpenCodeDependency(path string) (bool, error) {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if len(data) > 0 && (json.Unmarshal(data, &root) != nil || root == nil) {
		return false, errOpenCodeManifestNotObject
	}
	dependencies, ok := root["dependencies"].(map[string]any)
	if !ok {
		if _, exists := root["dependencies"]; exists {
			return false, errOpenCodeDependencies
		}
		dependencies = map[string]any{}
		root["dependencies"] = dependencies
	}
	if dependency, exists := dependencies["@opencode-ai/plugin"]; exists {
		if _, ok := dependency.(string); ok {
			return false, nil
		}
		return false, errOpenCodePluginDependency
	}
	dependencies["@opencode-ai/plugin"] = openCodePluginDependency
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	if err := atomicWrite(path, append(encoded, '\n'), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func openCodeDependencyVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	root := map[string]any{}
	if json.Unmarshal(data, &root) != nil || root == nil {
		return ""
	}
	dependencies, ok := root["dependencies"].(map[string]any)
	if !ok {
		return ""
	}
	version, _ := dependencies["@opencode-ai/plugin"].(string)
	return version
}

func removeOwnedOpenCodeDependency(path string, owner ownerRecord) error {
	if owner.OpenCodeDependency == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	root := map[string]any{}
	if json.Unmarshal(data, &root) != nil || root == nil {
		return nil
	}
	dependencies, ok := root["dependencies"].(map[string]any)
	if !ok || dependencies["@opencode-ai/plugin"] != owner.OpenCodeDependency {
		return nil
	}
	delete(dependencies, "@opencode-ai/plugin")
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(encoded, '\n'), 0o600)
}

func manageOpenCodePlugin(action string, location hookLocation, command string, owner ownerRecord, stale func(string) bool) (setupResult, error) {
	data, err := os.ReadFile(location.path)
	if err != nil && !os.IsNotExist(err) {
		return setupResult{}, err
	}
	managed := isManagedOpenCodePlugin(data, owner)
	if len(data) > 0 && !managed {
		return setupResult{}, errOpenCodePathOccupied
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
			if pluginCommand, _, _ := openCodePluginParts(data); stale(pluginCommand) {
				state = "stale"
			}
		}
		return setupResult{Agent: location.agent, State: state, Command: command}, nil
	case "uninstall":
		if !managed {
			return setupResult{Agent: location.agent, State: "missing"}, nil
		}
		if err := removeOwnedOpenCodeDependency(location.manifestPath, owner); err != nil {
			return setupResult{}, err
		}
		if err := os.Remove(location.path); err != nil {
			return setupResult{}, err
		}
		removeMarker(location.markerPath)
		return setupResult{Agent: location.agent, State: "removed"}, nil
	case "install":
		manifestData, manifestErr := os.ReadFile(location.manifestPath)
		manifestExists := manifestErr == nil
		if manifestErr != nil && !os.IsNotExist(manifestErr) {
			return setupResult{}, manifestErr
		}
		dependencyAdded, err := ensureOpenCodeDependency(location.manifestPath)
		if err != nil {
			return setupResult{}, err
		}
		plugin := openCodePluginPrefix + jsonStringCommand(command) + openCodePluginSuffix
		if err := atomicWrite(location.path, []byte(plugin), 0o600); err != nil {
			if restoreErr := restoreFile(location.manifestPath, manifestData, manifestExists); restoreErr != nil {
				return setupResult{}, fmt.Errorf("write plugin: %w; rollback manifest: %v", err, restoreErr)
			}
			return setupResult{}, err
		}
		_, shapeHash, _ := openCodePluginParts([]byte(plugin))
		dependencyOwner := ""
		if dependencyAdded || owner.OpenCodeDependency == openCodePluginDependency && openCodeDependencyVersion(location.manifestPath) == openCodePluginDependency {
			dependencyOwner = openCodePluginDependency
		}
		if err := writeOwner(location.markerPath, ownerRecord{Command: command, ShapeHash: shapeHash, OpenCodeDependency: dependencyOwner}); err != nil {
			pluginErr := restoreFile(location.path, data, len(data) > 0)
			manifestErr := restoreFile(location.manifestPath, manifestData, manifestExists)
			if pluginErr != nil || manifestErr != nil {
				return setupResult{}, fmt.Errorf("write managed session owner: %w; rollback plugin: %v; rollback manifest: %v", err, pluginErr, manifestErr)
			}
			return setupResult{}, err
		}
		return setupResult{Agent: location.agent, State: "installed", Command: command}, nil
	default:
		return setupResult{}, errors.New("setup accepts one action: install, check, or uninstall")
	}
}

func isManagedOpenCodePlugin(data []byte, owner ownerRecord) bool {
	command, shapeHash, ok := openCodePluginParts(data)
	return ok && shapeHash == owner.ShapeHash && isManagedCommand(command, owner)
}

func openCodePluginParts(data []byte) (string, string, bool) {
	content := string(data)
	for _, shape := range [][2]string{{openCodePluginPrefix, openCodePluginSuffix}, {legacyOpenCodePluginPrefix, legacyOpenCodePluginSuffix}} {
		prefix, suffix := shape[0], shape[1]
		if !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, suffix) {
			continue
		}
		encodedCommand := strings.TrimSuffix(strings.TrimPrefix(content, prefix), suffix)
		var command string
		if json.Unmarshal([]byte(encodedCommand), &command) != nil {
			continue
		}
		return command, contentHash([]byte(prefix + jsonStringCommand("") + suffix)), true
	}
	return "", "", false
}

func jsonStringCommand(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	writeErr := func(err error) error {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return &fs.PathError{Op: "write", Path: path, Err: err}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".router-axi-*")
	if err != nil {
		return writeErr(err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return writeErr(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return writeErr(err)
	}
	if err := tmp.Close(); err != nil {
		return writeErr(err)
	}
	return os.Rename(tmpPath, path)
}

func readOwner(path string) (ownerRecord, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var owner ownerRecord
		if json.Unmarshal(data, &owner) != nil || owner.Command == "" || owner.ShapeHash == "" {
			return ownerRecord{}, nil
		}
		return owner, nil
	}
	if os.IsNotExist(err) {
		return ownerRecord{}, nil
	}
	return ownerRecord{}, err
}

func writeOwner(path string, owner ownerRecord) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o600)
}

func contentHash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func removeMarker(path string) {
	_ = os.Remove(path)
}

func restoreFile(path string, data []byte, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return atomicWrite(path, data, 0o600)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
