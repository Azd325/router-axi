package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"version"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if stdout.String() != "version: dev\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "version: dev\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"unknown"}, &stdout, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "code: not_implemented") {
		t.Fatalf("stderr = %q, want structured error code", stderr.String())
	}
}
