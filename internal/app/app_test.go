package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

type fakeReader struct{ err error }
type partialReader struct{ fakeReader }
type manyCallsReader struct{ fakeReader }

func (partialReader) Overview(context.Context) (tr064.Overview, error) {
	return tr064.Overview{}, &tr064.Error{Kind: "unsupported", Message: "WAN unavailable"}
}

func (manyCallsReader) Calls(context.Context) ([]tr064.Call, error) {
	calls := make([]tr064.Call, 23)
	for i := range calls {
		calls[i] = tr064.Call{ID: fmt.Sprint(i + 1), Direction: "incoming", Remote: "123", Date: "10.03.24 12:34", Duration: "0:01"}
	}
	return calls, nil
}

func (f fakeReader) Status(context.Context) (tr064.Status, error) {
	return tr064.Status{Manufacturer: "AVM", Model: "FRITZ!Box 7590 AX", Software: "8.02", UptimeSeconds: 93784}, f.err
}
func (f fakeReader) Overview(ctx context.Context) (tr064.Overview, error) {
	router, err := f.Status(ctx)
	if err != nil {
		return tr064.Overview{}, err
	}
	wan, err := f.WAN(ctx)
	if err != nil {
		return tr064.Overview{}, err
	}
	traffic, err := f.Traffic(ctx)
	return tr064.Overview{Router: router, WAN: wan, Traffic: traffic}, err
}
func (f fakeReader) WAN(context.Context) (tr064.WAN, error) {
	return tr064.WAN{Status: "Connected", ExternalIP: "203.0.113.42", IPFamily: "ipv4", UptimeSeconds: 86400, LastError: "ERROR_NONE"}, f.err
}
func (f fakeReader) Traffic(context.Context) (tr064.Traffic, error) {
	return tr064.Traffic{TotalDownloadBytes: 12345678901, TotalUploadBytes: 987654321, ObservedAt: "2025-03-08T09:11:12Z"}, f.err
}
func (f fakeReader) Calls(context.Context) ([]tr064.Call, error) {
	return []tr064.Call{{ID: "12", Direction: "incoming", Remote: "+4930123456", Name: "Alice", Date: "10.03.24 12:34", Duration: "0:02"}}, f.err
}

func runTest(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	code := application.Run(t.Context(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCompactCommands(t *testing.T) {
	tests := []struct{ command, contains string }{
		{"status", "model: FRITZ!Box 7590 AX"},
		{"overview", "traffic: 12.35 GB downloaded, 0.99 GB uploaded"},
		{"wan", "ip_family: ipv4"},
		{"traffic", "observed_at: 2025-03-08T09:11:12Z"},
		{"calls", "calls[1]{id,direction,remote,name,date,duration}:"},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			code, stdout, stderr := runTest(t, test.command)
			if code != ExitOK || !strings.Contains(stdout, test.contains) || stderr != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestNoCommandRunsStatus(t *testing.T) {
	code, stdout, stderr := runTest(t)
	if code != ExitOK || !strings.HasPrefix(stdout, "router:\n") || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestOverviewJSONIsDeterministic(t *testing.T) {
	code, stdout, stderr := runTest(t, "overview", "--json")
	want := `{"router":{"manufacturer":"AVM","model":"FRITZ!Box 7590 AX","serial":"","software_version":"8.02","hardware_version":"","uptime_seconds":93784},"wan":{"status":"Connected","external_ip":"203.0.113.42","ip_family":"ipv4","uptime_seconds":86400,"last_error":"ERROR_NONE"},"traffic":{"total_download_bytes":12345678901,"total_upload_bytes":987654321,"observed_at":"2025-03-08T09:11:12Z"}}` + "\n"
	if code != ExitOK || stdout != want || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestOverviewFailureProducesNoPartialOutput(t *testing.T) {
	application := New(func(Config) (Reader, error) {
		return partialReader{}, nil
	}, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"overview"}, &stdout, &stderr)
	if code != ExitUnsupported || stdout.Len() != 0 || !strings.Contains(stderr.String(), "code: unsupported_capability") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCallsAreCompactByDefault(t *testing.T) {
	application := New(func(Config) (Reader, error) { return manyCallsReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"calls"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "calls[20]") || !strings.Contains(stdout.String(), "omitted: 3") || !strings.Contains(stdout.String(), "calls --all") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	code = application.Run(t.Context(), []string{"calls", "--all"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "calls[23]") || strings.Contains(stdout.String(), "omitted:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTrafficMakesMissingObservationTimeExplicit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	application := New(func(Config) (Reader, error) { return fakeReaderWithoutObservation{}, nil }, func(string) string { return "" })
	code := application.Run(t.Context(), []string{"traffic"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "observed_at: unknown") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type fakeReaderWithoutObservation struct{ fakeReader }

func (fakeReaderWithoutObservation) Traffic(context.Context) (tr064.Traffic, error) {
	return tr064.Traffic{TotalDownloadBytes: 1, TotalUploadBytes: 2}, nil
}

func TestJSONIsExplicit(t *testing.T) {
	code, stdout, stderr := runTest(t, "wan", "--json")
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var value tr064.WAN
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if value.Status != "Connected" {
		t.Fatalf("WAN = %#v", value)
	}
}

func TestUsageAndStructuredErrors(t *testing.T) {
	code, _, stderr := runTest(t, "delete")
	if code != ExitUsage || !strings.Contains(stderr, "code: unknown_command") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}

	var stdout bytes.Buffer
	var errout bytes.Buffer
	application := New(func(Config) (Reader, error) {
		return fakeReader{err: &tr064.Error{Kind: "auth", Operation: "GetInfo", Message: "router rejected credentials"}}, nil
	}, func(string) string { return "" })
	code = application.Run(t.Context(), []string{"status"}, &stdout, &errout)
	if code != ExitAuth || !strings.Contains(errout.String(), "code: authentication_failed") {
		t.Fatalf("code=%d stderr=%q", code, errout.String())
	}
}

func TestCredentialsComeFromEnvironment(t *testing.T) {
	var got Config
	application := New(func(config Config) (Reader, error) { got = config; return fakeReader{}, nil }, func(key string) string {
		values := map[string]string{"ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "agent", "ROUTER_AXI_PASSWORD": "secret"}
		return values[key]
	})
	code := application.Run(t.Context(), []string{"status"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	if got.Host != "router.test" || got.Username != "agent" || got.Password != "secret" {
		t.Fatalf("config = %#v", got)
	}
}

func TestFactoryFailureIsConfigurationError(t *testing.T) {
	application := New(func(Config) (Reader, error) { return nil, errors.New("bad address") }, func(string) string { return "" })
	var stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"status"}, &bytes.Buffer{}, &stderr)
	if code != ExitUsage || !strings.Contains(stderr.String(), "invalid_configuration") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
