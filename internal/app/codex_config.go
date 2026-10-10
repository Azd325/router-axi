package app

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	codexHooksEnabled          = "enabled"
	codexHooksAdded            = "added"
	codexHooksChangedFromFalse = "changed_from_false"
	codexHooksDisabled         = "disabled"
	codexHooksMissing          = "missing"
	codexHooksUnverifiable     = "unverifiable"
)

var (
	tomlTableHeader      = regexp.MustCompile(`^\s*(\[\[?)\s*([A-Za-z0-9_."' -]+?)\s*(\]\]?)\s*(?:#.*)?$`)
	tomlHooksBool        = regexp.MustCompile(`^(\s*hooks\s*=\s*)(true|false)(\s*(?:#.*)?)$`)
	tomlDottedHooks      = regexp.MustCompile(`^(\s*features\s*\.\s*hooks\s*=\s*)(true|false)(\s*(?:#.*)?)$`)
	tomlHooksKey         = regexp.MustCompile(`^\s*["']?hooks["']?\s*[.=]`)
	tomlFeaturesKey      = regexp.MustCompile(`^\s*["']?features["']?\s*[.=]`)
	errCodexConfigUnsafe = errors.New("codex config.toml cannot be edited safely; set [features].hooks = true manually")
)

type codexHooksUpdate struct {
	Content string
	State   string
}

type tomlLine struct{ text, eol string }

type codexConfigScan struct {
	lines         []tomlLine
	hooksLine     int
	hooksValue    string
	hooksPattern  *regexp.Regexp
	featuresFound bool
	featuresLast  int
}

func readCodexConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return data, nil
}

func splitTOMLLines(content string) []tomlLine {
	parts := strings.SplitAfter(content, "\n")
	lines := make([]tomlLine, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		line := tomlLine{text: part}
		switch {
		case strings.HasSuffix(part, "\r\n"):
			line = tomlLine{text: strings.TrimSuffix(part, "\r\n"), eol: "\r\n"}
		case strings.HasSuffix(part, "\n"):
			line = tomlLine{text: strings.TrimSuffix(part, "\n"), eol: "\n"}
		}
		lines = append(lines, line)
	}
	return lines
}

func scanCodexConfig(content string) (codexConfigScan, error) {
	scan := codexConfigScan{lines: splitTOMLLines(content), hooksLine: -1, featuresLast: -1}
	inFeatures, inMultiline, inTable := false, false, false
	for i, line := range scan.lines {
		text := line.text
		quotes := strings.Count(text, `"""`) + strings.Count(text, `'''`)
		if inMultiline {
			if quotes%2 == 1 {
				inMultiline = false
			}
			if inFeatures {
				scan.featuresLast = i
			}
			continue
		}
		if quotes%2 == 1 {
			inMultiline = true
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if header := tomlTableHeader.FindStringSubmatch(text); header != nil && len(header[1]) == len(header[3]) {
			inTable = true
			name := strings.Trim(header[2], `"' `)
			inFeatures = name == "features"
			if inFeatures {
				if len(header[1]) == 2 || scan.featuresFound {
					return scan, errCodexConfigUnsafe
				}
				scan.featuresFound = true
				scan.featuresLast = i
			}
			continue
		}
		if inFeatures {
			scan.featuresLast = i
			if tomlHooksKey.MatchString(text) {
				if err := scan.recordHooks(i, text, tomlHooksBool); err != nil {
					return scan, err
				}
			}
			continue
		}
		if tomlFeaturesKey.MatchString(text) && !inTable {
			if err := scan.recordHooks(i, text, tomlDottedHooks); err != nil {
				return scan, err
			}
		}
	}
	return scan, nil
}

func (scan *codexConfigScan) recordHooks(index int, text string, pattern *regexp.Regexp) error {
	match := pattern.FindStringSubmatch(text)
	if match == nil || scan.hooksLine >= 0 {
		return errCodexConfigUnsafe
	}
	scan.hooksLine, scan.hooksValue, scan.hooksPattern = index, match[2], pattern
	return nil
}

func codexHooksState(content string) string {
	scan, err := scanCodexConfig(content)
	switch {
	case err != nil:
		return codexHooksUnverifiable
	case scan.hooksLine < 0:
		return codexHooksMissing
	case scan.hooksValue == "true":
		return codexHooksEnabled
	}
	return codexHooksDisabled
}

func enableCodexHooks(content string) (codexHooksUpdate, error) {
	scan, err := scanCodexConfig(content)
	if err != nil {
		return codexHooksUpdate{}, err
	}
	newline := "\n"
	for _, line := range scan.lines {
		if line.eol != "" {
			newline = line.eol
			break
		}
	}
	switch {
	case scan.hooksLine >= 0 && scan.hooksValue == "true":
		return codexHooksUpdate{Content: content, State: codexHooksEnabled}, nil
	case scan.hooksLine >= 0:
		line := &scan.lines[scan.hooksLine]
		match := scan.hooksPattern.FindStringSubmatch(line.text)
		line.text = match[1] + "true" + match[3]
		return codexHooksUpdate{Content: joinTOMLLines(scan.lines), State: codexHooksChangedFromFalse}, nil
	case scan.featuresFound:
		return codexHooksUpdate{Content: insertTOMLLine(scan.lines, scan.featuresLast+1, "hooks = true", newline), State: codexHooksAdded}, nil
	case strings.TrimSpace(content) == "":
		return codexHooksUpdate{Content: "[features]" + newline + "hooks = true" + newline, State: codexHooksAdded}, nil
	}
	lines := append([]tomlLine(nil), scan.lines...)
	if lines[len(lines)-1].eol == "" {
		lines[len(lines)-1].eol = newline
	}
	if strings.TrimSpace(lines[len(lines)-1].text) != "" {
		lines = append(lines, tomlLine{eol: newline})
	}
	lines = append(lines, tomlLine{text: "[features]", eol: newline})
	return codexHooksUpdate{Content: insertTOMLLine(lines, len(lines), "hooks = true", newline), State: codexHooksAdded}, nil
}

func insertTOMLLine(lines []tomlLine, index int, text, newline string) string {
	out := append([]tomlLine(nil), lines...)
	if index > 0 && out[index-1].eol == "" {
		out[index-1].eol = newline
	}
	out = append(out[:index], append([]tomlLine{{text: text, eol: newline}}, out[index:]...)...)
	return joinTOMLLines(out)
}

func joinTOMLLines(lines []tomlLine) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line.text)
		b.WriteString(line.eol)
	}
	return b.String()
}

func writeCodexConfig(path string, data []byte) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return atomicWrite(path, data, mode)
}
