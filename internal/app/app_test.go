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
type emptyDevicesReader struct{ fakeReader }
type unsupportedDevicesReader struct{ fakeReader }
type forwardsReader struct {
	fakeReader
	forwards []tr064.Forward
}

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

func (manyDevicesReader) Devices(context.Context) ([]tr064.Device, error) {
	devices := make([]tr064.Device, 23)
	for i := range devices {
		devices[i] = tr064.Device{IPAddress: fmt.Sprintf("192.0.2.%d", i+1), MACAddress: fmt.Sprintf("02:00:00:00:00:%02x", i+1), InterfaceType: "Ethernet", Active: true}
	}
	return devices, nil
}

func (emptyDevicesReader) Devices(context.Context) ([]tr064.Device, error) {
	return []tr064.Device{}, nil
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
		Capabilities: tr064.DoctorCapabilities{Status: advertised, Overview: advertised, WAN: advertised, Traffic: advertised, Calls: advertised, Devices: advertised, WiFi: advertised, Forwards: advertised},
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
func (f fakeReader) Calls(context.Context) ([]tr064.Call, error) {
	return []tr064.Call{{ID: "12", Direction: "incoming", Remote: "+4930123456", Name: "Alice", Date: "10.03.24 12:34", Duration: "0:02"}}, f.err
}
func (f fakeReader) Devices(context.Context) ([]tr064.Device, error) {
	return []tr064.Device{{Name: "sanitized-device", IPAddress: "192.0.2.10", MACAddress: "02:00:00:00:00:10", InterfaceType: "Ethernet", Active: true}}, f.err
}

func (f fakeReader) WiFi(context.Context) ([]tr064.Radio, error) {
	return []tr064.Radio{{ServiceID: "urn:WLANConfiguration-com:serviceId:WLANConfiguration1", SSID: "synthetic-ap", Enabled: true, Channel: 6, Band: "2400", Standard: "ax", AssociatedDevices: 2, SecurityMode: "11i"}}, f.err
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
		{"wifi", "radios[1]{service_id,ssid,enabled,channel,band,standard,associated_devices,security_mode}:"},
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
	if code != ExitOK || !strings.HasPrefix(stdout, "router:\n") || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestDoctorJSONIsDeterministic(t *testing.T) {
	code, stdout, stderr := runTest(t, "doctor", "--json")
	want := `{"endpoint":"http://router.test:49000","reachability":{"state":"reachable"},"protocol":{"state":"available"},"authentication":{"state":"authenticated"},"model":"FRITZ!Box 7590 AX","firmware":"8.02","capabilities":{"status":{"state":"advertised"},"overview":{"state":"advertised"},"wan":{"state":"advertised"},"traffic":{"state":"advertised"},"calls":{"state":"advertised"},"devices":{"state":"advertised"},"wifi":{"state":"advertised"},"forwards":{"state":"advertised"}}}` + "\n"
	if code != ExitOK || stdout != want || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestDoctorPreservesPartialReportOnFailure(t *testing.T) {
	application := New(func(Config) (Reader, error) {
		return fakeReader{err: &tr064.Error{Kind: "auth", Message: "router rejected credentials"}}, nil
	}, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"doctor"}, &stdout, &stderr)
	if code != ExitAuth || !strings.Contains(stdout.String(), "doctor:\n") || !strings.Contains(stderr.String(), "authentication_failed") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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

func TestDevicesAreCompactByDefault(t *testing.T) {
	application := New(func(Config) (Reader, error) { return manyDevicesReader{}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"devices"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "devices[20]") || !strings.Contains(stdout.String(), "omitted: 3") || !strings.Contains(stdout.String(), "devices --all") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	code = application.Run(t.Context(), []string{"devices", "--all"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "devices[23]") || strings.Contains(stdout.String(), "omitted:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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
	if code != ExitUnsupported || stdout.Len() != 0 || !strings.Contains(stderr.String(), "code: unsupported_capability") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
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
			} else if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("partial output=%q stderr=%q", stdout.String(), stderr.String())
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
				if code != ExitRouter || stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
			} else if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "synthetic-ap") || !strings.Contains(stdout.String(), "2400") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		}
	}
}

func TestWiFiFlagsAndHelp(t *testing.T) {
	for _, flag := range []string{"--all", "--reveal", "--ssid"} {
		code, stdout, _ := runTest(t, "wifi", flag)
		if code != ExitUsage || stdout != "" {
			t.Fatalf("flag=%s code=%d stdout=%q", flag, code, stdout)
		}
	}
	code, stdout, stderr := runTest(t, "wifi", "--help")
	if code != ExitOK || stdout != "usage: router-axi wifi [--host ADDRESS] [--json]\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
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
	forwards := make([]tr064.Forward, 23)
	for i := range forwards {
		forwards[i] = tr064.Forward{Protocol: "TCP", ExternalPort: uint64(8000 + i), InternalClient: fmt.Sprintf("192.0.2.%d", i+1), InternalPort: 80, Description: "synthetic", RemoteHost: "198.51.100.1"}
	}
	application := New(func(Config) (Reader, error) { return forwardsReader{forwards: forwards}, nil }, func(string) string { return "" })
	for _, test := range []struct {
		args     []string
		contains []string
		absent   string
	}{
		{[]string{"forwards"}, []string{"forwards[20]", "omitted: 3", "forwards --all"}, "forwards[23]"},
		{[]string{"forwards", "--all"}, []string{"forwards[23]"}, "omitted:"},
		{[]string{"forwards", "--json"}, []string{`"total":23,"omitted":3`}, `"external_port":8020`},
		{[]string{"forwards", "--all", "--json"}, []string{`"total":23,"omitted":0`, `"external_port":8020`}, ""},
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
			if code != test.exit || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("json=%t code=%d stdout=%q stderr=%q", jsonOutput, code, stdout.String(), stderr.String())
			}
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
					_, _ = io.WriteString(w, `<root><service><serviceType>urn:dslforum-org:service:WANIPConnection:1</serviceType><controlURL>/wan</controlURL><SCPDURL>/wan.xml</SCPDURL></service></root>`)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/wan.xml" {
					_, _ = io.WriteString(w, `<scpd><actionList><action><name>GetPortMappingNumberOfEntries</name></action><action><name>GetGenericPortMappingEntry</name></action></actionList></scpd>`)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/wan" {
					t.Error("unexpected forwards request")
				}
				switch r.Header.Get("SOAPAction") {
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
				if code != ExitRouter || stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatal("failed enumeration produced partial success")
				}
			} else if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "192.0.2.10") || !strings.Contains(stdout.String(), "synthetic-service") {
				t.Fatal("successful enumeration did not expose operational fields")
			}
		}
	}
}

func TestForwardsFlagsAndHelp(t *testing.T) {
	for _, flag := range []string{"--reveal", "--ssid"} {
		code, stdout, _ := runTest(t, "forwards", flag)
		if code != ExitUsage || stdout != "" {
			t.Fatalf("flag=%s code=%d stdout=%q", flag, code, stdout)
		}
	}
	code, stdout, stderr := runTest(t, "forwards", "--help")
	if code != ExitOK || stdout != "usage: router-axi forwards [--host ADDRESS] [--json] [--all]\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestFactoryFailureIsConfigurationError(t *testing.T) {
	for _, command := range []string{"status", "wifi"} {
		application := New(func(Config) (Reader, error) { return nil, errors.New("bad address") }, func(string) string { return "" })
		var stderr bytes.Buffer
		code := application.Run(t.Context(), []string{command}, &bytes.Buffer{}, &stderr)
		if code != ExitUsage || !strings.Contains(stderr.String(), "invalid_configuration") || !strings.Contains(stderr.String(), "bad address") {
			t.Fatalf("command=%s code=%d stderr=%q", command, code, stderr.String())
		}
	}
}
