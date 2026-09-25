package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

type fakeReader struct{ err error }
type partialReader struct{ fakeReader }
type manyCallsReader struct{ fakeReader }
type manyDevicesReader struct{ fakeReader }
type boundedDevicesReader struct {
	count int
	fakeReader
}
type emptyDevicesReader struct{ fakeReader }
type unsupportedDevicesReader struct{ fakeReader }
type manyLeasesReader struct{ fakeReader }
type emptyLeasesReader struct{ fakeReader }
type unsupportedLeasesReader struct{ fakeReader }

func (manyLeasesReader) Leases(context.Context) ([]tr064.Lease, error) {
	leases := make([]tr064.Lease, 103)
	for i := range leases {
		remaining := int64(3600)
		leases[i] = tr064.Lease{IPAddress: fmt.Sprintf("192.0.2.%d", i+1), MACAddress: fmt.Sprintf("02:00:00:00:00:%02x", i+1), AddressSource: "DHCP", LeaseTimeRemaining: &remaining, InterfaceType: "802.11", Active: i == 0}
	}
	return leases, nil
}

func (emptyLeasesReader) Leases(context.Context) ([]tr064.Lease, error) {
	return nil, nil
}

func (unsupportedLeasesReader) Leases(context.Context) ([]tr064.Lease, error) {
	return nil, &tr064.Error{Kind: "unsupported", Operation: "GetHostNumberOfEntries", Message: "router does not advertise the required TR-064 service"}
}

type forwardsReader struct {
	fakeReader
	forwards []tr064.Forward
}

func (partialReader) Overview(context.Context) (tr064.Overview, error) {
	return tr064.Overview{}, &tr064.Error{Kind: "unsupported", Message: "WAN unavailable"}
}

func (manyCallsReader) Calls(context.Context) ([]tr064.Call, error) {
	calls := make([]tr064.Call, 103)
	for i := range calls {
		calls[i] = tr064.Call{ID: fmt.Sprint(i + 1), Direction: "incoming", Remote: "123", Date: "10.03.24 12:34", Duration: "0:01"}
	}
	return calls, nil
}

func (manyDevicesReader) Devices(context.Context) ([]tr064.Device, error) {
	devices := make([]tr064.Device, 103)
	for i := range devices {
		devices[i] = tr064.Device{IPAddress: fmt.Sprintf("192.0.2.%d", i+1), MACAddress: fmt.Sprintf("02:00:00:00:00:%02x", i+1), InterfaceType: "Ethernet", Active: true}
	}
	return devices, nil
}

func (emptyDevicesReader) Devices(context.Context) ([]tr064.Device, error) {
	return []tr064.Device{}, nil
}

func (r boundedDevicesReader) Devices(context.Context) ([]tr064.Device, error) {
	devices := make([]tr064.Device, r.count)
	for i := range devices {
		devices[i] = tr064.Device{IPAddress: fmt.Sprintf("192.0.2.%d", i+1), MACAddress: fmt.Sprintf("02:00:00:00:00:%02x", i+1), InterfaceType: "Ethernet", Active: true}
	}
	return devices, nil
}

func (unsupportedDevicesReader) Devices(context.Context) ([]tr064.Device, error) {
	return nil, &tr064.Error{Kind: "unsupported", Operation: "GetHostNumberOfEntries", Message: "router does not advertise the required TR-064 service"}
}

func (f fakeReader) Doctor(context.Context) (tr064.Doctor, error) {
	advertised := tr064.DoctorCheck{State: "advertised"}
	return tr064.Doctor{
		Endpoint: "http://router.test:49000", Reachability: tr064.DoctorCheck{State: "reachable"},
		Protocol: tr064.DoctorCheck{State: "available"}, Authentication: tr064.DoctorCheck{State: "authenticated"},
		Model: "FRITZ!Box 7590 AX", Firmware: "8.02",
		Capabilities: tr064.DoctorCapabilities{Status: advertised, Overview: advertised, WAN: advertised, Traffic: advertised, Calls: advertised, Devices: advertised, Leases: advertised, WiFi: advertised, Forwards: advertised, Reboot: advertised, Backup: advertised},
	}, f.err
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
func (f fakeReader) WatchSnapshot(context.Context) (tr064.WatchSnapshot, error) {
	return tr064.WatchSnapshot{}, f.err
}

func (f fakeReader) Calls(context.Context) ([]tr064.Call, error) {
	return []tr064.Call{{ID: "12", Direction: "incoming", Remote: "+4930123456", Name: "Alice", Date: "10.03.24 12:34", Duration: "0:02"}}, f.err
}
func (f fakeReader) Devices(context.Context) ([]tr064.Device, error) {
	return []tr064.Device{{Name: "sanitized-device", IPAddress: "192.0.2.10", MACAddress: "02:00:00:00:00:10", InterfaceType: "Ethernet", Active: true}}, f.err
}

func (f fakeReader) Leases(context.Context) ([]tr064.Lease, error) {
	remaining := int64(3600)
	return []tr064.Lease{{Name: "sanitized-static-device", IPAddress: "192.0.2.10", MACAddress: "02:00:00:00:00:10", AddressSource: "Static", InterfaceType: "Ethernet", Active: true}, {IPAddress: "192.0.2.20", MACAddress: "02:00:00:00:00:20", AddressSource: "DHCP", LeaseTimeRemaining: &remaining, InterfaceType: "802.11"}}, f.err
}

func (f fakeReader) WiFi(context.Context) ([]tr064.Radio, error) {
	return []tr064.Radio{{ServiceID: "urn:WLANConfiguration-com:serviceId:WLANConfiguration1", SSID: "synthetic-ap", Enabled: true, Channel: 6, Band: "2400", Standard: "ax", AssociatedDevices: 2, SecurityMode: "11i"}}, f.err
}

func (f fakeReader) GuestWiFi(context.Context) ([]tr064.GuestNetwork, error) {
	return []tr064.GuestNetwork{{ServiceID: "urn:WLANConfiguration-com:serviceId:WLANConfiguration2", SSID: "synthetic-guest", Enabled: true, Channel: 36, Band: "5000", Standard: "ax", AssociatedClients: 1, SecurityMode: "11iandWPA3"}}, f.err
}

func (f fakeReader) Reboot(_ context.Context, confirm bool) (tr064.RebootResult, error) {
	return tr064.RebootResult{Endpoint: "http://router.test:49000", Preview: !confirm, Accepted: confirm}, f.err
}

const syntheticBackupPayload = "**** SYNTHETIC ROUTER-AXI TEST EXPORT ****\nsynthetic-configuration-export-content\n**** END OF SYNTHETIC EXPORT ****"

func (f fakeReader) ConfigExport(context.Context, string) ([]byte, error) {
	return []byte(syntheticBackupPayload), f.err
}

func (f fakeReader) WiFiMutation(context.Context, uint64, bool, bool) (tr064.WiFiMutation, error) {
	return tr064.WiFiMutation{}, f.err
}

type wifiMutationReader struct {
	fakeReader
	instance uint64
	enable   bool
	confirm  bool
	result   tr064.WiFiMutation
}

func (f *wifiMutationReader) WiFiMutation(_ context.Context, instance uint64, enable, confirm bool) (tr064.WiFiMutation, error) {
	f.instance, f.enable, f.confirm = instance, enable, confirm
	if f.err != nil {
		return tr064.WiFiMutation{}, f.err
	}
	if !confirm {
		n := instance
		if n == 0 {
			n = 1
		}
		return tr064.WiFiMutation{Instance: "urn:WLANConfiguration-com:serviceId:WLANConfiguration" + fmt.Sprint(n), Action: actionName(enable), Current: true, Intended: enable, Preview: true}, nil
	}
	if f.result.Instance == "" {
		f.result.Instance = "urn:WLANConfiguration-com:serviceId:WLANConfiguration" + fmt.Sprint(instance)
		f.result.Action = actionName(enable)
		f.result.Previous, f.result.Current, f.result.Changed = !enable, enable, true
	}
	return f.result, nil
}

func actionName(enable bool) string {
	if enable {
		return "enable"
	}
	return "disable"
}

func (f fakeReader) Forwards(context.Context) ([]tr064.Forward, error) {
	return []tr064.Forward{{Enabled: true, Protocol: "TCP", ExternalPort: 8443, InternalClient: "192.0.2.10", InternalPort: 443, Description: "synthetic service", RemoteHost: "198.51.100.10"}}, f.err
}

func (f forwardsReader) Forwards(context.Context) ([]tr064.Forward, error) {
	return f.forwards, f.err
}

type wifiReader struct {
	fakeReader
	radios []tr064.Radio
}

func (f wifiReader) WiFi(context.Context) ([]tr064.Radio, error) {
	return f.radios, f.err
}

type guestReader struct {
	fakeReader
	guests []tr064.GuestNetwork
}

func (f guestReader) GuestWiFi(context.Context) ([]tr064.GuestNetwork, error) {
	return f.guests, f.err
}

func assertStructuredError(t *testing.T, stdout, stderr string, jsonOutput bool, contains string) {
	t.Helper()
	if stderr != "" {
		t.Fatalf("error written to stderr: %q", stderr)
	}
	if jsonOutput && !json.Valid([]byte(stdout)) {
		t.Fatalf("invalid JSON error: %q", stdout)
	}
	if !strings.Contains(stdout, contains) {
		t.Fatalf("error output=%q missing=%q", stdout, contains)
	}
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
		{"doctor", "forwards: advertised"},
		{"status", "model: FRITZ!Box 7590 AX"},
		{"overview", "traffic: 12.35 GB downloaded, 0.99 GB uploaded"},
		{"wan", "ip_family: ipv4"},
		{"traffic", "observed_at: 2025-03-08T09:11:12Z"},
		{"calls", "calls[1]{id,direction,remote,name,date,duration}:"},
		{"devices", "devices[1]{name,ip_address,mac_address,interface_type,active}:"},
		{"leases", "leases[2]{name,ip_address,mac_address,address_source,lease_time_remaining,interface_type,active}:"},
		{"wifi", "radios[1]{service_id,ssid,enabled,channel,band,standard,associated_devices,security_mode}:"},
		{"guest", "guests[1]{service_id,ssid,enabled,channel,band,standard,associated_clients,security_mode}:"},
		{"forwards", "forwards[1]{enabled,protocol,external_port,internal_client,internal_port,description,remote_host,lease_duration}:"},
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
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "bin: ") || !strings.Contains(stdout, "\ndescription: ") {
		t.Fatalf("status compact output missing self-identification: %q", stdout)
	}
	if !strings.Contains(stdout, "\nrouter:\n") {
		t.Fatalf("status compact output missing router block: %q", stdout)
	}
}

func TestStatusCompactIdentifiesTheCLI(t *testing.T) {
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	application.executable = func() (string, error) { return "/home/tester/tools/router-axi", nil }
	application.homeDir = func() (string, error) { return "/home/tester", nil }
	for _, args := range [][]string{{}, {"status"}} {
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), args, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
		want := "bin: ~/tools/router-axi\ndescription: " + cliDescription + "\nrouter:\n"
		if !strings.HasPrefix(stdout.String(), want) {
			t.Fatalf("args=%v stdout=%q want-prefix=%q", args, stdout.String(), want)
		}
	}
}

func TestStatusJSONKeepsExactShapeWithoutSelfIdentification(t *testing.T) {
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"status", "--json"}, &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 || strings.Contains(stdout.String(), "\"bin\"") || strings.HasPrefix(stdout.String(), "{") == false {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["manufacturer"]; !ok {
		t.Fatalf("unexpected status JSON shape: %s", stdout.String())
	}
}

func TestVersionFlagsAnswerBeforeRouterWork(t *testing.T) {
	factoryCalled := false
	application := New(func(Config) (Reader, error) { factoryCalled = true; return fakeReader{}, nil }, func(string) string { return "" })
	application.Version = "1.2.3"
	for _, args := range [][]string{{"--version"}, {"-v"}, {"-V"}, {"version"}} {
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), args, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
		want := "1.2.3\n"
		if args[0] == "version" {
			want = "version: 1.2.3\n"
		}
		if stdout.String() != want {
			t.Fatalf("args=%v stdout=%q want=%q", args, stdout.String(), want)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"version", "--json"}, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 || stdout.String() != "{\"version\":\"1.2.3\"}\n" {
		t.Fatalf("version --json code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if factoryCalled {
		t.Fatal("version fast path contacted the router")
	}
}

func TestVersionAliasesRejectTrailingArguments(t *testing.T) {
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"--version", "--bogus"}, want: "unknown option: --bogus"},
		{args: []string{"-v", "--host", "router.test"}, want: "--version, -v, and -V must be used alone"},
	} {
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), test.args, &stdout, &stderr); code != ExitUsage {
			t.Fatalf("args=%v code=%d", test.args, code)
		}
		assertStructuredError(t, stdout.String(), stderr.String(), false, test.want)
	}
}

func TestUnknownFlagErrorIsSelfCorrecting(t *testing.T) {
	for _, test := range []struct{ args, hint []string }{
		{[]string{"wan", "--stat"}, []string{"valid flags for wan: --host, --json, --help"}},
		{[]string{"watch", "--stats"}, []string{"valid flags for watch: --host, --json, --interval, --count, --help"}},
		{[]string{"wifi", "enable", "--conform"}, []string{"valid flags for wifi: --host, --json, --instance, --confirm, --help"}},
		{[]string{"backup", "--out"}, []string{"valid flags for backup: --host, --json, --output, --force, --help"}},
		{[]string{"version", "--bogus"}, []string{"valid flags for version: --json, --help"}},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		unknown := "unknown option: " + test.args[len(test.args)-1]
		if code != ExitUsage || !strings.Contains(stdout, unknown) || !strings.Contains(stdout, "  hint: "+test.hint[0]) || stderr != "" {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", test.args, code, stdout, stderr)
		}
	}
}

func TestContextualFlagErrorsRespectCommandAction(t *testing.T) {
	for _, test := range []struct {
		args       []string
		want       string
		unwanted   string
		requireMsg bool
	}{
		{args: []string{"version", "--host", "router.test"}, want: "--host is not valid for version", requireMsg: true},
		{args: []string{"wifi", "--bogus"}, want: "valid flags for wifi: --host, --json, --help", unwanted: "--instance, --confirm"},
		{args: []string{"wifi", "enable", "--bogus"}, want: "valid flags for wifi: --host, --json, --instance, --confirm, --help", requireMsg: true},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitUsage || !strings.Contains(stdout, test.want) || stderr != "" {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q want=%q", test.args, code, stdout, stderr, test.want)
		}
		if test.unwanted != "" && strings.Contains(stdout, test.unwanted) {
			t.Fatalf("args=%v stdout=%q unexpectedly contains %q", test.args, stdout, test.unwanted)
		}
		if test.requireMsg && !strings.Contains(stdout, "message: ") {
			t.Fatalf("args=%v stdout=%q missing structured message", test.args, stdout)
		}
	}
}

func TestRunAcceptsRepresentativeFlagsAndHelp(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"status", "--host", "router.test", "--json", "--help"}, want: "usage: router-axi status"},
		{args: []string{"watch", "--interval", "1s", "--count", "1", "--help"}, want: "usage: router-axi watch"},
		{args: []string{"calls", "--all", "--help"}, want: "usage: router-axi calls"},
		{args: []string{"wifi", "enable", "--instance", "1", "--confirm", "--help"}, want: "usage: router-axi wifi enable"},
		{args: []string{"backup", "--output", "router.export", "--force", "--help"}, want: "usage: router-axi backup"},
		{args: []string{"version", "--json", "--help"}, want: "usage: router-axi version"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stderr != "" || !strings.Contains(stdout, test.want) {
			t.Errorf("args=%v code=%d stdout=%q stderr=%q want=%q", test.args, code, stdout, stderr, test.want)
		}
	}
}

func TestTopLevelHelpListsPublicCommands(t *testing.T) {
	code, stdout, stderr := runTest(t, "help")
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, command := range []string{"doctor", "status", "overview", "wan", "traffic", "watch", "calls", "devices", "leases", "wifi", "guest", "forwards", "reboot", "backup", "skill", "version"} {
		if !strings.Contains(stdout, "\n  "+command+" ") {
			t.Fatalf("top-level help does not list %q: %q", command, stdout)
		}
	}
}

func TestDoctorJSONIsDeterministic(t *testing.T) {
	code, stdout, stderr := runTest(t, "doctor", "--json")
	want := `{"endpoint":"http://router.test:49000","reachability":{"state":"reachable"},"protocol":{"state":"available"},"authentication":{"state":"authenticated"},"model":"FRITZ!Box 7590 AX","firmware":"8.02","capabilities":{"status":{"state":"advertised"},"overview":{"state":"advertised"},"wan":{"state":"advertised"},"traffic":{"state":"advertised"},"calls":{"state":"advertised"},"devices":{"state":"advertised"},"leases":{"state":"advertised"},"wifi":{"state":"advertised"},"forwards":{"state":"advertised"},"reboot":{"state":"advertised"},"backup":{"state":"advertised"}}}` + "\n"
	if code != ExitOK || stdout != want || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestDoctorPreservesPartialReportOnFailure(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		application := New(func(Config) (Reader, error) {
			return fakeReader{err: &tr064.Error{Kind: "auth", Message: "router rejected credentials"}}, nil
		}, func(string) string { return "" })
		args := []string{"doctor"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitAuth || stderr.Len() != 0 {
			t.Fatalf("json=%t code=%d stdout=%q stderr=%q", jsonOutput, code, stdout.String(), stderr.String())
		}
		if jsonOutput {
			var envelope struct {
				Doctor struct {
					Endpoint     string `json:"endpoint"`
					Model        string `json:"model"`
					Capabilities struct {
						Status struct {
							State string `json:"state"`
						} `json:"status"`
					} `json:"capabilities"`
				} `json:"doctor"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("json=%t invalid envelope: %v", jsonOutput, err)
			}
			if envelope.Doctor.Endpoint != "http://router.test:49000" || envelope.Doctor.Model != "FRITZ!Box 7590 AX" || envelope.Doctor.Capabilities.Status.State != "advertised" {
				t.Fatalf("json=%t envelope lost the partial report: %s", jsonOutput, stdout.String())
			}
			if envelope.Error.Code != "authentication_failed" {
				t.Fatalf("json=%t envelope error code=%q stdout=%q", jsonOutput, envelope.Error.Code, stdout.String())
			}
			continue
		}
		if !strings.Contains(stdout.String(), "doctor:\n") || !strings.Contains(stdout.String(), "authentication_failed") {
			t.Fatalf("stdout=%q", stdout.String())
		}
	}
}

func TestOverviewJSONIsDeterministic(t *testing.T) {
	code, stdout, stderr := runTest(t, "overview", "--json")
	want := `{"router":{"manufacturer":"AVM","model":"FRITZ!Box 7590 AX","serial":"","software_version":"8.02","hardware_version":"","uptime_seconds":93784},"wan":{"status":"Connected","external_ip":"203.0.113.42","ip_family":"ipv4","uptime_seconds":86400,"last_error":"ERROR_NONE"},"traffic":{"total_download_bytes":12345678901,"total_upload_bytes":987654321,"observed_at":"2025-03-08T09:11:12Z"}}` + "\n"
	if code != ExitOK || stdout != want || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestOverviewFailureProducesStructuredError(t *testing.T) {
	application := New(func(Config) (Reader, error) {
		return partialReader{}, nil
	}, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"overview"}, &stdout, &stderr)
	if code != ExitUnsupported {
		t.Fatalf("code=%d", code)
	}
	assertStructuredError(t, stdout.String(), stderr.String(), false, "code: unsupported_capability")
}

func TestCallsAreCompactByDefault(t *testing.T) {
	application := New(func(Config) (Reader, error) { return manyCallsReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"calls"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "calls[100]") || !strings.Contains(stdout.String(), "omitted: 3") || !strings.Contains(stdout.String(), "calls --all") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	code = application.Run(t.Context(), []string{"calls", "--all"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "calls[103]") || strings.Contains(stdout.String(), "omitted:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDevicesAreCompactByDefault(t *testing.T) {
	application := New(func(Config) (Reader, error) { return manyDevicesReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"devices"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "devices[100]") || !strings.Contains(stdout.String(), "omitted: 3") || !strings.Contains(stdout.String(), "devices --all") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	code = application.Run(t.Context(), []string{"devices", "--all"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "devices[103]") || strings.Contains(stdout.String(), "omitted:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDefaultListLimitBoundary(t *testing.T) {
	for _, test := range []struct {
		count    int
		header   string
		omitted  string
		nextHint string
	}{
		{100, "devices[100]", "", ""},
		{101, "devices[100]", "omitted: 1", "next: router-axi devices --all"},
	} {
		application := New(func(Config) (Reader, error) {
			return boundedDevicesReader{count: test.count}, nil
		}, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), []string{"devices"}, &stdout, &stderr)
		out := stdout.String()
		if code != ExitOK || stderr.Len() != 0 || !strings.Contains(out, test.header) {
			t.Fatalf("count=%d code=%d stdout=%q stderr=%q", test.count, code, out, stderr.String())
		}
		if test.omitted == "" {
			if strings.Contains(out, "omitted:") || strings.Contains(out, "next:") {
				t.Fatalf("count=%d unexpected truncation hint: %q", test.count, out)
			}
		} else if !strings.Contains(out, test.omitted) || !strings.Contains(out, test.nextHint) {
			t.Fatalf("count=%d missing truncation hint: %q", test.count, out)
		}
	}
}

func TestDevicesEmptyOutputIsDefinitive(t *testing.T) {
	application := New(func(Config) (Reader, error) { return emptyDevicesReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"devices"}, &stdout, &stderr)
	if code != ExitOK || stdout.String() != "devices[0]: no devices found\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	code = application.Run(t.Context(), []string{"devices", "--json"}, &stdout, &stderr)
	if code != ExitOK || stdout.String() != "{\"devices\":[],\"total\":0,\"omitted\":0}\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDevicesCapabilityAbsenceIsExplicit(t *testing.T) {
	application := New(func(Config) (Reader, error) { return unsupportedDevicesReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"devices"}, &stdout, &stderr)
	if code != ExitUnsupported {
		t.Fatalf("code=%d", code)
	}
	assertStructuredError(t, stdout.String(), stderr.String(), false, "code: unsupported_capability")
}

func TestDevicesJSONIsDeterministic(t *testing.T) {
	code, stdout, stderr := runTest(t, "devices", "--json")
	want := `{"devices":[{"name":"sanitized-device","ip_address":"192.0.2.10","mac_address":"02:00:00:00:00:10","interface_type":"Ethernet","active":true}],"total":1,"omitted":0}` + "\n"
	if code != ExitOK || stdout != want || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
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
	code, stdoutText, stderr := runTest(t, "delete")
	if code != ExitUsage || stderr != "" || !strings.Contains(stdoutText, "code: unknown_command") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdoutText, stderr)
	}

	var stdout bytes.Buffer
	var errout bytes.Buffer
	application := New(func(Config) (Reader, error) {
		return fakeReader{err: &tr064.Error{Kind: "auth", Operation: "GetInfo", Message: "router rejected credentials"}}, nil
	}, func(string) string { return "" })
	code = application.Run(t.Context(), []string{"status"}, &stdout, &errout)
	if code != ExitAuth || errout.Len() != 0 || !strings.Contains(stdout.String(), "code: authentication_failed") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), errout.String())
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

func TestWiFiOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"wifi"}, "radios[1]{service_id,ssid,enabled,channel,band,standard,associated_devices,security_mode}:\n  urn:WLANConfiguration-com:serviceId:WLANConfiguration1,synthetic-ap,true,6,2400,ax,2,11i\n"},
		{[]string{"wifi", "--json"}, `{"radios":[{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration1","ssid":"synthetic-ap","enabled":true,"channel":6,"band":"2400","standard":"ax","associated_devices":2,"security_mode":"11i"}],"total":1}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
}

func TestGuestOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"guest"}, "guests[1]{service_id,ssid,enabled,channel,band,standard,associated_clients,security_mode}:\n  urn:WLANConfiguration-com:serviceId:WLANConfiguration2,synthetic-guest,true,36,5000,ax,1,11iandWPA3\n"},
		{[]string{"guest", "--json"}, `{"guests":[{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration2","ssid":"synthetic-guest","enabled":true,"channel":36,"band":"5000","standard":"ax","associated_clients":1,"security_mode":"11iandWPA3"}],"total":1}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
}

func TestGuestEmptyAndFailureOutput(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, test := range []struct {
			kind string
			exit int
		}{
			{"", ExitOK}, {"unsupported", ExitUnsupported}, {"auth", ExitAuth}, {"network", ExitNetwork}, {"protocol", ExitRouter}, {"router", ExitRouter},
		} {
			reader := guestReader{}
			if test.kind != "" {
				reader.err = &tr064.Error{Kind: test.kind, Message: "guest Wi-Fi inspection failed"}
				reader.guests = []tr064.GuestNetwork{{SSID: "must-not-appear"}}
			}
			application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
			args := []string{"guest"}
			want := "guests[0]: no guest Wi-Fi networks found\n"
			if jsonOutput {
				args = append(args, "--json")
				want = "{\"guests\":[],\"total\":0}\n"
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != test.exit {
				t.Fatalf("kind=%s code=%d", test.kind, code)
			}
			if test.kind == "" {
				if stdout.String() != want || stderr.Len() != 0 {
					t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			} else {
				assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
				if strings.Contains(stdout.String(), "must-not-appear") {
					t.Fatalf("stdout=%q leaked discarded guest data", stdout.String())
				}
			}
		}
	}
}

func TestGuestFlagsAndHelp(t *testing.T) {
	for _, args := range [][]string{{"guest", "--all"}, {"guest", "--instance", "2"}, {"guest", "enable"}, {"wifi", "guest"}} {
		code, stdout, stderr := runTest(t, args...)
		if code != ExitUsage || stderr != "" || !strings.Contains(stdout, "error") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runTest(t, "guest", "--help")
	if code != ExitOK || stdout != "usage: router-axi guest [--host ADDRESS] [--json] [--help]\nRead-only documented guest Wi-Fi inspection: public SSID and aggregate radio state only; never keys, BSSIDs, or client details.\nexamples: router-axi guest; router-axi guest --json\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestWiFiEmptyAndFailureOutput(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, test := range []struct {
			kind string
			exit int
		}{
			{"", ExitOK}, {"unsupported", ExitUnsupported}, {"auth", ExitAuth}, {"network", ExitNetwork}, {"protocol", ExitRouter}, {"router", ExitRouter},
		} {
			reader := wifiReader{}
			if test.kind != "" {
				reader.err = &tr064.Error{Kind: test.kind, Message: "Wi-Fi inspection failed"}
				reader.radios = []tr064.Radio{{Channel: 6}}
			}
			application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
			args := []string{"wifi"}
			want := "radios[0]: no Wi-Fi services found\n"
			if jsonOutput {
				args = append(args, "--json")
				want = "{\"radios\":[],\"total\":0}\n"
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != test.exit {
				t.Fatalf("kind=%s code=%d", test.kind, code)
			}
			if test.kind == "" {
				if stdout.String() != want || stderr.Len() != 0 {
					t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			} else {
				assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
			}
		}
	}
}

func TestWiFiClientPrivacyBoundary(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			var actions []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml" {
					_, _ = io.WriteString(w, `<root><device><serviceList><service><serviceType>urn:dslforum-org:service:WLANConfiguration:1</serviceType><serviceId>urn:WLANConfiguration-com:serviceId:WLANConfiguration1</serviceId><controlURL>/wlan1</controlURL></service></serviceList></device></root>`)
					return
				}
				action := r.Header.Get("SOAPAction")
				actions = append(actions, action)
				if r.Method != http.MethodPost || r.URL.Path != "/wlan1" {
					t.Error("unexpected Wi-Fi request")
				}
				fields := ""
				switch action {
				case `"urn:dslforum-org:service:WLANConfiguration:1#GetInfo"`:
					fields = "<NewEnable>1</NewEnable><NewStatus>Up</NewStatus><NewSSID>synthetic-ap</NewSSID><NewStandard>ax</NewStandard><NewX_AVM-DE_FrequencyBand>2400</NewX_AVM-DE_FrequencyBand><NewBSSID>synthetic-sensitive-bssid</NewBSSID>"
				case `"urn:dslforum-org:service:WLANConfiguration:1#GetChannelInfo"`:
					fields = "<NewChannel>6</NewChannel><NewX_AVM-DE_FrequencyBand>synthetic-sensitive-band</NewX_AVM-DE_FrequencyBand>"
				case `"urn:dslforum-org:service:WLANConfiguration:1#GetTotalAssociations"`:
					fields = "<NewTotalAssociations>0</NewTotalAssociations>"
				case `"urn:dslforum-org:service:WLANConfiguration:1#GetBeaconType"`:
					fields = "<NewBeaconType>11i</NewBeaconType>"
					if fail {
						w.WriteHeader(http.StatusInternalServerError)
						fields = "<errorCode>501</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription>"
					}
				default:
					t.Error("forbidden Wi-Fi action")
					w.WriteHeader(http.StatusBadRequest)
				}
				_, _ = io.WriteString(w, "<Envelope><Body>"+fields+"</Body></Envelope>")
			}))
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"wifi"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if strings.Contains(stdout.String()+stderr.String(), "synthetic-sensitive") || strings.Contains(stdout.String()+stderr.String(), server.URL) {
				t.Fatal("Wi-Fi output leaked untrusted data")
			}
			if len(actions) != 4 {
				t.Fatalf("action count=%d", len(actions))
			}
			if fail {
				assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
			} else if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "synthetic-ap") || !strings.Contains(stdout.String(), "2400") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		}
	}
}

// TestEverySubcommandHelpDepth asserts the AXI help contract: every
// subcommand help must include a usage line, flags, and concrete examples.
func TestEverySubcommandHelpDepth(t *testing.T) {
	for _, command := range []string{"doctor", "status", "overview", "wan", "traffic", "calls", "devices", "leases", "wifi", "guest", "forwards", "watch", "reboot", "backup"} {
		text := help(command, "")
		if !strings.HasPrefix(text, "usage: router-axi "+command) || !strings.Contains(text, "--host ADDRESS") || !strings.Contains(text, "--json") || !strings.Contains(text, "examples: router-axi "+command) && !strings.Contains(text, "Examples: router-axi "+command) {
			t.Fatalf("help for %s lacks usage, flags, or examples: %q", command, text)
		}
	}
	for _, action := range []string{"enable", "disable"} {
		text := help("wifi", action)
		if !strings.HasPrefix(text, "usage: router-axi wifi "+action) || !strings.Contains(text, "--confirm") || !strings.Contains(text, "examples: router-axi wifi "+action) {
			t.Fatalf("help for wifi %s lacks usage, flags, or examples: %q", action, text)
		}
	}
}

func TestWiFiFlagsAndHelp(t *testing.T) {
	for _, flag := range []string{"--all", "--reveal", "--ssid"} {
		code, stdout, stderr := runTest(t, "wifi", flag)
		if code != ExitUsage || stderr != "" || !strings.Contains(stdout, "error") {
			t.Fatalf("flag=%s code=%d stdout=%q stderr=%q", flag, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runTest(t, "wifi", "--help")
	if code != ExitOK || stdout != "usage: router-axi wifi [--host ADDRESS] [--json] [enable|disable [--instance N] --confirm] [--help]\nRead-only Wi-Fi radio inspection. wifi enable|disable changes one radio: --instance N (1 or greater; required when the router advertises more than one radio), preview without --confirm.\nexamples: router-axi wifi; router-axi wifi disable --instance 1 --confirm\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for action, want := range map[string]string{
		"enable":  "usage: router-axi wifi enable [--instance N] --confirm [--host ADDRESS] [--json] [--help]\nEnables or disables one WLANConfiguration radio. Without --confirm: preview only, nothing changes.\nWith --confirm: idempotent change; the router must confirm the new state. --instance N is required when the router advertises more than one radio; bounds N 1 or greater.\nNo prompts or retries; SSIDs, BSSIDs, and keys are never read or printed.\nexamples: router-axi wifi enable; router-axi wifi enable --instance 1 --confirm; router-axi wifi enable --instance 2 --confirm --json\n",
		"disable": "usage: router-axi wifi disable [--instance N] --confirm [--host ADDRESS] [--json] [--help]\nEnables or disables one WLANConfiguration radio. Without --confirm: preview only, nothing changes.\nWith --confirm: idempotent change; the router must confirm the new state. --instance N is required when the router advertises more than one radio; bounds N 1 or greater.\nNo prompts or retries; SSIDs, BSSIDs, and keys are never read or printed.\nexamples: router-axi wifi disable; router-axi wifi disable --instance 1 --confirm; router-axi wifi disable --instance 2 --confirm --json\n",
	} {
		code, stdout, stderr := runTest(t, "wifi", action, "--help")
		if code != ExitOK || stdout != want || stderr != "" {
			t.Fatalf("action=%s code=%d stdout=%q stderr=%q", action, code, stdout, stderr)
		}
	}
}

func TestCallsAndDevicesHelp(t *testing.T) {
	for command, want := range map[string]string{
		"calls":   "usage: router-axi calls [--all] [--host ADDRESS] [--json] [--help]\nRead-only call history. Defaults to the 100 most recent entries; --all lists everything. No required arguments.\nexamples: router-axi calls; router-axi calls --all; router-axi calls --all --json\n",
		"devices": "usage: router-axi devices [--all] [--host ADDRESS] [--json] [--help]\nRead-only connected and remembered LAN clients. Defaults to 100 entries; --all lists everything. No required arguments.\nexamples: router-axi devices; router-axi devices --all --json\n",
	} {
		code, stdout, stderr := runTest(t, command, "--help")
		if code != ExitOK || stdout != want || stderr != "" {
			t.Fatalf("command=%s code=%d stdout=%q stderr=%q", command, code, stdout, stderr)
		}
	}
}

func TestForwardsOutputContract(t *testing.T) {
	zero := uint64(0)
	reader := forwardsReader{forwards: []tr064.Forward{
		{Enabled: true, Protocol: "TCP,UDP", ExternalPort: 8443, InternalClient: "192.0.2.10", InternalPort: 443, Description: "synthetic, \"service\"", RemoteHost: "198.51.100.10"},
		{Enabled: false, Protocol: "UDP", ExternalPort: 5353, InternalClient: "192.0.2.11", InternalPort: 5353, Description: "synthetic\nservice", RemoteHost: "", LeaseDuration: &zero},
	}}
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"forwards"}, "forwards[2]{enabled,protocol,external_port,internal_client,internal_port,description,remote_host,lease_duration}:\n  true,\"TCP,UDP\",8443,192.0.2.10,443,\"synthetic, \\\"service\\\"\",198.51.100.10,unknown\n  false,UDP,5353,192.0.2.11,5353,\"synthetic\\nservice\",\"\",0\n"},
		{[]string{"forwards", "--json"}, `{"forwards":[{"enabled":true,"protocol":"TCP,UDP","external_port":8443,"internal_client":"192.0.2.10","internal_port":443,"description":"synthetic, \"service\"","remote_host":"198.51.100.10"},{"enabled":false,"protocol":"UDP","external_port":5353,"internal_client":"192.0.2.11","internal_port":5353,"description":"synthetic\nservice","remote_host":"","lease_duration":0}],"total":2,"omitted":0}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestForwardsAreBoundedAndEmptyIsExplicit(t *testing.T) {
	forwards := make([]tr064.Forward, 103)
	for i := range forwards {
		forwards[i] = tr064.Forward{Protocol: "TCP", ExternalPort: uint64(8000 + i), InternalClient: fmt.Sprintf("192.0.2.%d", i+1), InternalPort: 80, Description: "synthetic", RemoteHost: "198.51.100.1"}
	}
	application := New(func(Config) (Reader, error) { return forwardsReader{forwards: forwards}, nil }, func(string) string { return "" })
	for _, test := range []struct {
		args     []string
		contains []string
		absent   string
	}{
		{[]string{"forwards"}, []string{"forwards[100]", "omitted: 3", "forwards --all"}, "forwards[103]"},
		{[]string{"forwards", "--all"}, []string{"forwards[103]"}, "omitted:"},
		{[]string{"forwards", "--json"}, []string{`"total":103,"omitted":3`}, `"external_port":8102`},
		{[]string{"forwards", "--all", "--json"}, []string{`"total":103,"omitted":0`, `"external_port":8102`}, ""},
	} {
		var out, errout bytes.Buffer
		code := application.Run(t.Context(), test.args, &out, &errout)
		stdout, stderr := out.String(), errout.String()
		if code != ExitOK || stderr != "" {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout, stderr)
		}
		for _, want := range test.contains {
			if !strings.Contains(stdout, want) {
				t.Fatalf("args=%q stdout=%q missing=%q", test.args, stdout, want)
			}
		}
		if test.absent != "" && strings.Contains(stdout, test.absent) {
			t.Fatalf("args=%q stdout=%q unexpectedly contains=%q", test.args, stdout, test.absent)
		}
	}

	empty := New(func(Config) (Reader, error) { return forwardsReader{}, nil }, func(string) string { return "" })
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"forwards"}, "forwards[0]: no port forwards found\n"},
		{[]string{"forwards", "--json"}, "{\"forwards\":[],\"total\":0,\"omitted\":0}\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := empty.Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestForwardsFailuresAreAtomic(t *testing.T) {
	for _, test := range []struct {
		err  error
		exit int
	}{
		{&tr064.Error{Kind: "auth", Message: "synthetic failure"}, ExitAuth},
		{&tr064.Error{Kind: "network", Message: "synthetic failure"}, ExitNetwork},
		{&tr064.Error{Kind: "unsupported", Message: "synthetic failure"}, ExitUnsupported},
		{&tr064.Error{Kind: "protocol", Message: "synthetic failure"}, ExitRouter},
		{&tr064.Error{Kind: "router", Message: "synthetic failure"}, ExitRouter},
		{errors.New("synthetic failure"), ExitInternal},
	} {
		for _, jsonOutput := range []bool{false, true} {
			reader := forwardsReader{forwards: []tr064.Forward{{Protocol: "TCP"}}}
			reader.err = test.err
			application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
			args := []string{"forwards"}
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

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic write failure") }

func TestForwardsOutputWriteFailure(t *testing.T) {
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
	for _, args := range [][]string{{"forwards"}, {"forwards", "--json"}} {
		var stderr bytes.Buffer
		if code := application.Run(t.Context(), args, failingWriter{}, &stderr); code != ExitInternal || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestForwardsClientOutputBoundary(t *testing.T) {
	for _, fail := range []bool{false, true} {
		for _, jsonOutput := range []bool{false, true} {
			countReads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml" {
					_, _ = io.WriteString(w, `<root><service><serviceType>urn:dslforum-org:service:WANIPConnection:1</serviceType><serviceId>urn:synthetic:wan</serviceId><controlURL>/wan</controlURL><SCPDURL>/wan.xml</SCPDURL></service><service><serviceType>urn:dslforum-org:service:Layer3Forwarding:1</serviceType><controlURL>/layer3</controlURL></service></root>`)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/wan.xml" {
					_, _ = io.WriteString(w, `<scpd><actionList><action><name>GetPortMappingNumberOfEntries</name></action><action><name>GetGenericPortMappingEntry</name></action></actionList></scpd>`)
					return
				}
				if r.Method != http.MethodPost || (r.URL.Path != "/wan" && r.URL.Path != "/layer3") {
					t.Error("unexpected forwards request")
				}
				switch r.Header.Get("SOAPAction") {
				case `"urn:dslforum-org:service:Layer3Forwarding:1#GetDefaultConnectionService"`:
					_, _ = io.WriteString(w, `<Envelope><NewDefaultConnectionService>urn:synthetic:wan</NewDefaultConnectionService></Envelope>`)
				case `"urn:dslforum-org:service:WANIPConnection:1#GetPortMappingNumberOfEntries"`:
					countReads++
					if fail && countReads == 2 {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = io.WriteString(w, `<Fault><errorCode>501</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription></Fault>`)
						return
					}
					_, _ = io.WriteString(w, `<Envelope><NewPortMappingNumberOfEntries>1</NewPortMappingNumberOfEntries></Envelope>`)
				case `"urn:dslforum-org:service:WANIPConnection:1#GetGenericPortMappingEntry"`:
					_, _ = io.WriteString(w, `<Envelope><NewEnabled>1</NewEnabled><NewProtocol>TCP</NewProtocol><NewExternalPort>8443</NewExternalPort><NewInternalClient>192.0.2.10</NewInternalClient><NewInternalPort>443</NewInternalPort><NewPortMappingDescription>synthetic-service</NewPortMappingDescription><NewRemoteHost></NewRemoteHost><NewSerialNumber>synthetic-sensitive-serial</NewSerialNumber></Envelope>`)
				default:
					t.Error("forbidden forwards action")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"forwards"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if strings.Contains(stdout.String()+stderr.String(), "synthetic-sensitive") || strings.Contains(stdout.String()+stderr.String(), server.URL) {
				t.Fatal("forwards output leaked discarded response data")
			}
			if fail {
				if code != ExitRouter {
					t.Fatalf("code=%d", code)
				}
				assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
			} else if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "192.0.2.10") || !strings.Contains(stdout.String(), "synthetic-service") {
				t.Fatal("successful enumeration did not expose operational fields")
			}
		}
	}
}

func TestForwardsFlagsAndHelp(t *testing.T) {
	for _, flag := range []string{"--reveal", "--ssid"} {
		code, stdout, stderr := runTest(t, "forwards", flag)
		if code != ExitUsage || stderr != "" || !strings.Contains(stdout, "error") {
			t.Fatalf("flag=%s code=%d stdout=%q stderr=%q", flag, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runTest(t, "forwards", "--help")
	if code != ExitOK || stdout != "usage: router-axi forwards [--all] [--host ADDRESS] [--json] [--help]\nRead-only port-forwarding rules. Defaults to 100 entries; --all lists everything. No required arguments.\nexamples: router-axi forwards; router-axi forwards --all --json\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestFactoryFailureIsConfigurationError(t *testing.T) {
	for _, command := range []string{"status", "wifi"} {
		application := New(func(Config) (Reader, error) { return nil, errors.New("bad address") }, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), []string{command}, &stdout, &stderr)
		if code != ExitUsage || stderr.Len() != 0 || !strings.Contains(stdout.String(), "invalid_configuration") || !strings.Contains(stdout.String(), "bad address") {
			t.Fatalf("command=%s code=%d stdout=%q stderr=%q", command, code, stdout.String(), stderr.String())
		}
	}
}

func TestLeasesOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"leases"}, "leases[2]{name,ip_address,mac_address,address_source,lease_time_remaining,interface_type,active}:\n  sanitized-static-device,192.0.2.10,02:00:00:00:00:10,Static,unknown,Ethernet,true\n  \"\",192.0.2.20,02:00:00:00:00:20,DHCP,3600,802.11,false\n"},
		{[]string{"leases", "--json"}, `{"leases":[{"name":"sanitized-static-device","ip_address":"192.0.2.10","mac_address":"02:00:00:00:00:10","address_source":"Static","interface_type":"Ethernet","active":true},{"ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:20","address_source":"DHCP","lease_time_remaining":3600,"interface_type":"802.11","active":false}],"total":2,"omitted":0}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestLeasesAreBounded(t *testing.T) {
	application := New(func(Config) (Reader, error) { return manyLeasesReader{}, nil }, func(string) string { return "" })
	for _, test := range []struct {
		args     []string
		contains []string
		absent   string
	}{
		{[]string{"leases"}, []string{"leases[100]", "omitted: 3", "leases --all"}, "leases[103]"},
		{[]string{"leases", "--all"}, []string{"leases[103]"}, "omitted:"},
	} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), test.args, &stdout, &stderr)
		for _, want := range test.contains {
			if code != ExitOK || !strings.Contains(stdout.String(), want) {
				t.Fatalf("args=%q want=%q stdout=%q", test.args, want, stdout.String())
			}
		}
		if strings.Contains(stdout.String(), test.absent) {
			t.Fatalf("args=%q found %q", test.args, test.absent)
		}
	}
}

func TestLeasesJSONBounds(t *testing.T) {
	application := New(func(Config) (Reader, error) { return manyLeasesReader{}, nil }, func(string) string { return "" })
	for _, all := range []bool{false, true} {
		args := []string{"leases", "--json"}
		count := 100
		if all {
			args = append(args, "--all")
			count = 103
		}
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		var result leaseResult
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if code != ExitOK || stderr.Len() != 0 || len(result.Leases) != count || result.Total != 103 || result.Omitted != 103-count {
			t.Fatal("JSON list bound or aggregate mismatch")
		}
	}
}

func TestLeasesErrorsDiscardPartialResults(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, test := range []struct {
			kind string
			exit int
		}{{"unsupported", ExitUnsupported}, {"auth", ExitAuth}, {"network", ExitNetwork}, {"protocol", ExitRouter}, {"router", ExitRouter}} {
			application := New(func(Config) (Reader, error) {
				return fakeReader{err: &tr064.Error{Kind: test.kind, Message: "lease observation failed"}}, nil
			}, func(string) string { return "" })
			args := []string{"leases"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != test.exit {
				t.Fatalf("kind=%s code=%d", test.kind, code)
			}
			assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
		}
	}
}

func TestLeasesEmptyOutputIsDefinitive(t *testing.T) {
	application := New(func(Config) (Reader, error) { return emptyLeasesReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"leases"}, &stdout, &stderr)
	if code != ExitOK || stdout.String() != "leases[0]: no host observations found\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = application.Run(t.Context(), []string{"leases", "--json"}, &stdout, &stderr)
	if code != ExitOK || stdout.String() != "{\"leases\":[],\"total\":0,\"omitted\":0}\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestLeasesCapabilityAbsenceIsExplicit(t *testing.T) {
	application := New(func(Config) (Reader, error) { return unsupportedLeasesReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"leases"}, &stdout, &stderr)
	if code != ExitUnsupported || stderr.Len() != 0 || !strings.Contains(stdout.String(), "code: unsupported_capability") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestLeasesClientOutputBoundary(t *testing.T) {
	for _, fail := range []bool{false, true} {
		for _, jsonOutput := range []bool{false, true} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml" {
					_, _ = io.WriteString(w, `<root><device><serviceList><service><serviceType>urn:dslforum-org:service:Hosts:1</serviceType><controlURL>/hosts</controlURL></service></serviceList></device></root>`)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/hosts" {
					t.Error("unexpected lease observation request")
				}
				switch r.Header.Get("SOAPAction") {
				case `"urn:dslforum-org:service:Hosts:1#GetHostNumberOfEntries"`:
					_, _ = io.WriteString(w, `<Envelope><NewHostNumberOfEntries>2</NewHostNumberOfEntries></Envelope>`)
				case `"urn:dslforum-org:service:Hosts:1#GetGenericHostEntry"`:
					body, _ := io.ReadAll(r.Body)
					if fail && strings.Contains(string(body), "<NewIndex>1</NewIndex>") {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = io.WriteString(w, `<Fault><errorCode>501</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription></Fault>`)
						return
					}
					_, _ = io.WriteString(w, `<Envelope><NewHostName>synthetic, "client"</NewHostName><NewIPAddress>192.0.2.10</NewIPAddress><NewMACAddress>02:00:00:00:00:10</NewMACAddress><NewAddressSource>DHCP</NewAddressSource><NewLeaseTimeRemaining>60</NewLeaseTimeRemaining><NewInterfaceType>Ethernet</NewInterfaceType><NewActive>1</NewActive><NewSerialNumber>synthetic-sensitive-serial</NewSerialNumber></Envelope>`)
				default:
					t.Error("forbidden lease observation action")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"leases"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if strings.Contains(stdout.String()+stderr.String(), "synthetic-sensitive") || strings.Contains(stdout.String()+stderr.String(), server.URL) {
				t.Fatal("lease diagnostics leaked discarded response data")
			}
			if fail {
				if code != ExitRouter {
					t.Fatalf("code=%d", code)
				}
				assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
			} else if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "synthetic, \\\"client\\\"") || !strings.Contains(stdout.String(), "192.0.2.10") || !strings.Contains(stdout.String(), "02:00:00:00:00:10") {
				t.Fatalf("successful observation did not expose escaped operational fields: %s", stdout.String())
			}
		}
	}
}

func TestLeasesFlagsAndHelp(t *testing.T) {
	for _, flag := range []string{"--reveal", "--ssid"} {
		code, stdout, stderr := runTest(t, "leases", flag)
		if code != ExitUsage || stderr != "" || !strings.Contains(stdout, "error") {
			t.Fatalf("flag=%s code=%d stdout=%q stderr=%q", flag, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runTest(t, "leases", "--help")
	if code != ExitOK || stdout != "usage: router-axi leases [--all] [--host ADDRESS] [--json] [--help]\nRead-only observed lease metadata from the Hosts table: name, addresses, address source, and remaining lease time. Defaults to 100 entries; --all lists everything. No required arguments.\nexamples: router-axi leases; router-axi leases --all --json\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestWiFiMutationPreviewRequiresExplicitConfirmation(t *testing.T) {
	for _, args := range [][]string{{"wifi", "disable"}, {"wifi", "disable", "--json"}} {
		reader := &wifiMutationReader{confirm: false}
		application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
		if args[1] != "disable" || reader.enable {
			t.Fatal("intended state was not propagated")
		}
		want := "wifi:\n  action: disable\n  instance: urn:WLANConfiguration-com:serviceId:WLANConfiguration1\n  current: enabled\n  intended: disabled\n  changed: false\nnext: router-axi wifi disable --instance 1 --confirm\n"
		if args[len(args)-1] == "--json" {
			want = `{"wifi":{"instance":"urn:WLANConfiguration-com:serviceId:WLANConfiguration1","action":"disable","current":true,"intended":false,"preview":true}}` + "\n"
		}
		if stdout.String() != want {
			t.Fatalf("preview stdout=%q want=%q", stdout.String(), want)
		}
	}
}

func TestWiFiMutationPreviewHintRepeatsExplicitHost(t *testing.T) {
	for host, want := range map[string]string{
		"192.168.178.2":      " --host 192.168.178.2",
		"http://[::1]:49000": " --host 'http://[::1]:49000'",
		"it's.test":          ` --host 'it'\''s.test'`,
	} {
		application := New(func(config Config) (Reader, error) {
			if config.Host != host {
				t.Fatalf("host=%q want %q", config.Host, host)
			}
			return &wifiMutationReader{}, nil
		}, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), []string{"--host", host, "wifi", "disable"}, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("host=%q code=%d stderr=%q", host, code, stderr.String())
		}
		if !strings.HasSuffix(stdout.String(), "\nnext: router-axi wifi disable --instance 1 --confirm"+want+"\n") {
			t.Fatalf("host=%q stdout=%q", host, stdout.String())
		}
	}
}

func TestWiFiMutationConfirmedFlagsReachTheReader(t *testing.T) {
	reader := &wifiMutationReader{}
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"wifi", "enable", "--instance", "3", "--confirm", "--json"}, &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if reader.instance != 3 || !reader.enable || !reader.confirm {
		t.Fatalf("reader saw instance=%d enable=%t confirm=%t", reader.instance, reader.enable, reader.confirm)
	}
	want := `{"wifi":{"instance":"urn:WLANConfiguration-com:serviceId:WLANConfiguration3","action":"enable","previous":false,"current":true,"changed":true}}` + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout=%q want=%q", stdout.String(), want)
	}
}

func TestWiFiMutationCompactResult(t *testing.T) {
	reader := &wifiMutationReader{result: tr064.WiFiMutation{Instance: "urn:WLANConfiguration-com:serviceId:WLANConfiguration1", Action: "disable", Previous: true, Current: false, Changed: true}}
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"wifi", "disable", "--confirm"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	want := "wifi:\n  action: disable\n  instance: urn:WLANConfiguration-com:serviceId:WLANConfiguration1\n  previous: enabled\n  current: disabled\n  changed: true\nnext: router-axi wifi\n"
	if stdout.String() != want {
		t.Fatalf("stdout=%q want=%q", stdout.String(), want)
	}
}

func TestWiFiMutationIdempotentNoChange(t *testing.T) {
	reader := &wifiMutationReader{result: tr064.WiFiMutation{Instance: "urn:WLANConfiguration-com:serviceId:WLANConfiguration1", Action: "enable", Previous: true, Current: true, Changed: false}}
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"wifi", "enable", "--confirm"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "changed: false") {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
}

func TestWiFiMutationRefusesAmbiguousInstance(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		reader := fakeReader{err: &tr064.Error{Kind: "usage", Code: "ambiguous_instance", Message: "router advertises 3 WLAN instances; specify the target with --instance"}}
		application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
		args := []string{"wifi", "disable", "--confirm"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitUsage || stderr.Len() != 0 || !strings.Contains(stdout.String(), "ambiguous_instance") {
			t.Fatalf("json=%t code=%d stdout=%q stderr=%q", jsonOutput, code, stdout.String(), stderr.String())
		}
	}
}

func TestWiFiMutationUsageErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"wifi", "enable", "--instance", "0"}, "--instance requires a WLANConfiguration number of 1 or greater"},
		{[]string{"wifi", "enable", "--confirm", "--all"}, "--all is valid only for calls, devices, leases, or forwards"},
		{[]string{"status", "--confirm"}, "--confirm is valid only with reboot, wifi enable, or wifi disable"},
		{[]string{"wifi", "enable", "disable"}, "wifi accepts one action: enable or disable"},
		{[]string{"wifi", "restart"}, "exactly one command is required"},
		{[]string{"wan", "enable"}, "exactly one command is required"},
	}
	for _, test := range tests {
		_, stdout, stderr := runTest(t, test.args...)
		if stderr != "" || !strings.Contains(stdout, test.want) {
			t.Fatalf("args=%v stdout=%q stderr=%q want=%q", test.args, stdout, stderr, test.want)
		}
	}
}

func TestWiFiMutationProtocolAndAuthErrors(t *testing.T) {
	tests := []struct {
		kind     string
		codeWant string
		exit     int
	}{
		{"auth", "authentication_failed", ExitAuth},
		{"network", "router_unreachable", ExitNetwork},
		{"unsupported", "unsupported_capability", ExitUnsupported},
		{"protocol", "router_protocol_error", ExitRouter},
	}
	for _, test := range tests {
		reader := fakeReader{err: &tr064.Error{Kind: test.kind, Message: "Wi-Fi radio change failed"}}
		application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), []string{"wifi", "disable", "--confirm"}, &stdout, &stderr)
		if code != test.exit || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.codeWant) {
			t.Fatalf("kind=%s code=%d stdout=%q stderr=%q", test.kind, code, stdout.String(), stderr.String())
		}
	}
}

func TestStatusDistinguishesUntrustedCertificateFromUnreachableRouter(t *testing.T) {
	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an untrusted certificate completed a request")
	}))
	defer untrusted.Close()
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()
	for _, test := range []struct {
		address, code, hint string
	}{
		{untrusted.URL, "tls_untrusted", "certificate"},
		{unreachable.URL, "router_unreachable", "check --host"},
	} {
		application := New(func(Config) (Reader, error) { return tr064.New(test.address, "", "", nil) }, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), []string{"status", "--json"}, &stdout, &stderr)
		var payload struct {
			Error struct{ Code, Hint string } `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if code != ExitNetwork || stderr.Len() != 0 || payload.Error.Code != test.code || !strings.Contains(payload.Error.Hint, test.hint) {
			t.Fatalf("want=%s code=%d stdout=%q", test.code, code, stdout.String())
		}
	}
}
