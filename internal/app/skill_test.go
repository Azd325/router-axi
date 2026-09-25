package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/skill"
	"gopkg.in/yaml.v3"
)

const (
	helpBeginMarker = "<!-- router-axi-help:begin -->"
	helpEndMarker   = "<!-- router-axi-help:end -->"
)

func TestSkillHelpNotStale(t *testing.T) {
	raw := string(skill.Bytes())
	begin := strings.Index(raw, helpBeginMarker)
	end := strings.Index(raw, "\n"+helpEndMarker)
	if begin < 0 || end < 0 || end < begin || !strings.HasPrefix(raw[begin+len(helpBeginMarker):], "\n") {
		t.Fatalf("SKILL.md is missing well-formed %s/%s markers", helpBeginMarker, helpEndMarker)
	}
	block := raw[begin+len(helpBeginMarker)+1 : end]
	var stdout, stderr bytes.Buffer
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	if code := application.Run(t.Context(), []string{"help"}, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
		t.Fatalf("router-axi help: exit = %d, stderr = %q", code, stderr.String())
	}
	if want := stdout.String(); block != want {
		t.Fatalf("SKILL.md help block is stale; regenerate it from router-axi help.\nwant:\n%q\ngot:\n%q", want, block)
	}
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func parseSkillFrontmatter(raw []byte) (skillFrontmatter, error) {
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return skillFrontmatter{}, fmt.Errorf("frontmatter must start with ---")
	}
	end := strings.Index(text, "\n---\n")
	if end < 0 {
		return skillFrontmatter{}, fmt.Errorf("frontmatter must end with ---")
	}
	var frontmatter skillFrontmatter
	decoder := yaml.NewDecoder(strings.NewReader(text[4:end]))
	decoder.KnownFields(true)
	if err := decoder.Decode(&frontmatter); err != nil {
		return skillFrontmatter{}, err
	}
	if frontmatter.Name == "" || frontmatter.Description == "" {
		return skillFrontmatter{}, fmt.Errorf("frontmatter name and description must not be empty")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return skillFrontmatter{}, fmt.Errorf("frontmatter contains multiple documents")
		}
		return skillFrontmatter{}, err
	}
	return frontmatter, nil
}

func TestSkillFrontmatter(t *testing.T) {
	frontmatter, err := parseSkillFrontmatter(skill.Bytes())
	if err != nil {
		t.Fatalf("parse SKILL.md frontmatter: %v", err)
	}
	if frontmatter.Name != skill.Name {
		t.Fatalf("frontmatter name = %q, want %q", frontmatter.Name, skill.Name)
	}
	if frontmatter.Description == "" {
		t.Fatal("frontmatter description must not be empty")
	}
}

func TestSkillFrontmatterRejectsInvalidYAML(t *testing.T) {
	for _, raw := range []string{
		"---\nname: router-axi\ndescription: [unterminated\n---\n",
		"---\nname: router-axi\ndescription: \"\"\n---\n",
		"---\nname: router-axi\nunknown: value\ndescription: valid\n---\n",
	} {
		if _, err := parseSkillFrontmatter([]byte(raw)); err == nil {
			t.Fatalf("parseSkillFrontmatter(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestSkillInstall(t *testing.T) {
	dir := t.TempDir()
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	application.homeDir = func() (string, error) { return dir, nil }
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"skill", "install"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	path := filepath.Join(dir, ".agents", "skills", skill.Name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	if !bytes.Equal(data, skill.Bytes()) {
		t.Fatal("installed SKILL.md differs from the bundled skill")
	}
	if want := "skill:\n  path: " + strconv.Quote(path) + "\n  installed: true\n"; stdout.String() != want {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestSkillInstallPathFlag(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	application.homeDir = func() (string, error) { return "", os.ErrNotExist }
	code := application.Run(t.Context(), []string{"skill", "install", "--path", dir, "--json"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	path := filepath.Join(dir, skill.Name, "SKILL.md")
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("skill not installed under --path: %v", err)
	}
	if want := `{"skill":{"path":` + strconv.Quote(path) + `,"installed":true}}` + "\n"; stdout.String() != want {
		t.Fatalf("unexpected JSON stdout: %q", stdout.String())
	}
}

func TestSkillInstallRejectsUnrelatedFlagsBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "unexpected")
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	application.homeDir = func() (string, error) { return dir, nil }
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"skill", "install", "--output", output, "--force"}, &stdout, &stderr)
	if code != ExitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--output is valid only with backup") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("unrelated output path was written: %v", err)
	}
}

func TestSkillInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	application.homeDir = func() (string, error) { return dir, nil }
	var stdout, stderr bytes.Buffer
	for round := range 3 {
		if code := application.Run(t.Context(), []string{"skill", "install"}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("round %d: exit = %d, stderr = %s", round, code, stderr.String())
		}
		if round > 0 && (stdout.Len() > 0 || stderr.Len() > 0) {
			t.Fatalf("repeated install was not a silent no-op: stdout = %q, stderr = %q", stdout.String(), stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
	}
}

func TestSkillInstallOverwritesChangedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "skills", skill.Name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	application.homeDir = func() (string, error) { return dir, nil }
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"skill", "install"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, skill.Bytes()) {
		t.Fatalf("stale skill content was not replaced: %v", err)
	}
}

func TestSkillUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"missing action", []string{"skill"}, "skill requires the action install"},
		{"unknown action", []string{"skill", "uninstall"}, "skill accepts one action: install"},
		{"path outside skill", []string{"wifi", "enable", "--path", "/tmp"}, "--path is valid only with skill install"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, stderr := runTest(t, testCase.args...)
			if !strings.Contains(stderr, testCase.code) {
				t.Fatalf("stderr = %q, want code %q", stderr, testCase.code)
			}
		})
	}
}

func TestSkillHelp(t *testing.T) {
	if code, stdout, _ := runTest(t, "skill", "--help"); code != ExitOK || !strings.Contains(stdout, "usage: router-axi skill install") {
		t.Fatalf("skill help: exit %d, stdout %q", code, stdout)
	}
}
