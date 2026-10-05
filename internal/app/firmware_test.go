package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) Firmware(context.Context) (tr064.Firmware, error) {
	current, offered, state, build := "8.00", "8.10", "UpdateAvailable", "Release"
	mode, when, last, result := "important", "2026-01-30T03:00:00+01:00", "7.90", "succeeded"
	available := true
	return tr064.Firmware{CurrentVersion: &current, UpdateAvailable: &available, OfferedVersion: &offered, UpdateState: &state, BuildType: &build, AutoUpdateMode: &mode, UpdateTime: &when, LastVersion: &last, UpdateSuccessful: &result}, f.err
}

type unknownFirmwareReader struct{ fakeReader }

func (unknownFirmwareReader) Firmware(context.Context) (tr064.Firmware, error) {
	return tr064.Firmware{}, nil
}

func TestFirmwareOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"firmware"}, "firmware:\n  current_version: 8.00\n  update_available: true\n  offered_version: 8.10\n  update_state: UpdateAvailable\n  build_type: Release\n  auto_update_mode: important\n  update_time: 2026-01-30T03:00:00+01:00\n  last_version: 7.90\n  update_successful: succeeded\n"},
		{[]string{"firmware", "--json"}, `{"current_version":"8.00","update_available":true,"offered_version":"8.10","update_state":"UpdateAvailable","build_type":"Release","auto_update_mode":"important","update_time":"2026-01-30T03:00:00+01:00","last_version":"7.90","update_successful":"succeeded"}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout, stderr)
		}
	}
}

func TestFirmwareUnknownOutput(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"firmware"}, "firmware:\n  current_version: unknown\n  update_available: unknown\n  offered_version: unknown\n  update_state: unknown\n  build_type: unknown\n  auto_update_mode: unknown\n  update_time: unknown\n  last_version: unknown\n  update_successful: unknown\n"},
		{[]string{"firmware", "--json"}, `{"current_version":null,"update_available":null,"offered_version":null,"update_state":null,"build_type":null,"auto_update_mode":null,"update_time":null,"last_version":null,"update_successful":null}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return unknownFirmwareReader{}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestFirmwareHelpAndFlags(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("help or invalid input contacted router"); return nil, nil }, func(string) string { t.Fatal("help or invalid input read credentials"); return "" })
	for _, args := range [][]string{{"firmware", "--help"}, {"firmware", "--confirm"}, {"firmware", "--all"}, {"firmware", "--unknown"}, {"firmware", "check"}, {"firmware", "update"}} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if args[1] == "--help" {
			if code != ExitOK || !strings.Contains(stdout.String(), "UserInterface:GetInfo and X_AVM-DE_GetInfo") || !strings.Contains(stdout.String(), "router-axi firmware --json") {
				t.Fatalf("code=%d stdout=%q", code, stdout.String())
			}
		} else if code != ExitUsage {
			t.Fatalf("args=%q code=%d stdout=%q", args, code, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr=%q", stderr.String())
		}
	}
}

func TestFirmwareStructuredErrors(t *testing.T) {
	for _, test := range []struct {
		kind string
		exit int
	}{{"auth", ExitAuth}, {"network", ExitNetwork}, {"unsupported", ExitUnsupported}, {"protocol", ExitRouter}, {"router", ExitRouter}} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(Config) (Reader, error) {
				return fakeReader{err: &tr064.Error{Kind: test.kind, Operation: "firmware", Message: "firmware status inspection failed"}}, nil
			}, func(string) string { return "" })
			args := []string{"firmware"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != test.exit {
				t.Fatalf("json=%t code=%d", jsonOutput, code)
			}
			assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
		}
	}
}
