package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildVersionUsesTaggedModuleVersion(t *testing.T) {
	original := readBuildInfo
	t.Cleanup(func() { readBuildInfo = original })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}}, true
	}
	if got := buildVersion(); got != "v0.4.0" {
		t.Fatalf("buildVersion() = %q, want v0.4.0", got)
	}
}

func TestBuildVersionFallsBackToLinkerVersion(t *testing.T) {
	originalReadBuildInfo, originalVersion := readBuildInfo, version
	t.Cleanup(func() {
		readBuildInfo, version = originalReadBuildInfo, originalVersion
	})
	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	}
	if got := buildVersion(); got != "dev" {
		t.Fatalf("buildVersion() = %q, want dev", got)
	}
}

func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}, {"-V"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(args, &stdout, &stderr)
		if exitCode != 0 {
			t.Fatalf("args=%v exit code = %d, want 0", args, exitCode)
		}
		want := "version: dev\n"
		if args[0] != "version" {
			want = "dev\n"
		}
		if stdout.String() != want {
			t.Fatalf("args=%v stdout = %q, want %q", args, stdout.String(), want)
		}
		if stderr.Len() != 0 {
			t.Fatalf("args=%v stderr = %q, want empty", args, stderr.String())
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"unknown"}, &stdout, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want non-zero")
	}
	if !strings.Contains(stdout.String(), "code: unknown_command") {
		t.Fatalf("stdout = %q, want structured error code", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
