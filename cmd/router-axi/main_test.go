package main

import (
	"bytes"
	"strings"
	"testing"
)

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
