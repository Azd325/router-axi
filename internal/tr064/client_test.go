package tr064

import (
	_ "embed"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/tr64desc.xml
var descriptionFixture string

//go:embed testdata/device-info.xml
var deviceFixture string

//go:embed testdata/wan-status.xml
var wanStatusFixture string

//go:embed testdata/wan-ip.xml
var wanIPFixture string

//go:embed testdata/traffic.xml
var trafficFixture string

//go:embed testdata/call-list-url.xml
var callListURLFixture string

//go:embed testdata/calls.xml
var callsFixture string

//go:embed testdata/host-count.xml
var hostCountFixture string

//go:embed testdata/host-entry-0.xml
var hostEntryZeroFixture string

//go:embed testdata/host-entry-1.xml
var hostEntryOneFixture string

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	responses := map[string]string{
		"/tr64desc.xml": descriptionFixture,
		"/calllist.lua": callsFixture,
	}
	actions := map[string]string{
		"GetInfo":                deviceFixture,
		"GetStatusInfo":          wanStatusFixture,
		"GetExternalIPAddress":   wanIPFixture,
		"GetTotalBytesReceived":  trafficFixture,
		"GetTotalBytesSent":      trafficFixture,
		"GetCallList":            callListURLFixture,
		"GetHostNumberOfEntries": hostCountFixture,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if response, ok := responses[r.URL.Path]; ok {
			_, _ = w.Write([]byte(response))
			return
		}
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		if i := strings.LastIndex(action, "#"); i >= 0 {
			action = action[i+1:]
		}
		if action == "GetGenericHostEntry" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "could not read request", http.StatusBadRequest)
				return
			}
			switch {
			case strings.Contains(string(body), "<NewIndex>0</NewIndex>"):
				_, _ = w.Write([]byte(hostEntryZeroFixture))
			case strings.Contains(string(body), "<NewIndex>1</NewIndex>"):
				_, _ = w.Write([]byte(hostEntryOneFixture))
			default:
				http.Error(w, "unexpected host index", http.StatusBadRequest)
			}
			return
		}
		response, ok := actions[action]
		if !ok {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(response))
	}))
}

func TestNewDefaultsToTR064Ports(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{"fritz.box", "http://fritz.box:49000"},
		{"https://fritz.box", "https://fritz.box:49443"},
		{"http://fritz.box:12345", "http://fritz.box:12345"},
	}

	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			client, err := New(test.address, "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := client.base.String(); got != test.want {
				t.Fatalf("base = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClientReadOnlyCommands(t *testing.T) {
	server := fixtureServer(t)
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.Date(2025, 3, 8, 10, 11, 12, 0, time.FixedZone("test", 3600)) }

	status, err := client.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.Model != "FRITZ!Box 7590 AX" || status.UptimeSeconds != 93784 {
		t.Fatalf("status = %#v", status)
	}

	wan, err := client.WAN(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if wan.Status != "Connected" || wan.ExternalIP != "2001:db8::42" || wan.IPFamily != "ipv6" {
		t.Fatalf("wan = %#v", wan)
	}

	traffic, err := client.Traffic(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if traffic.TotalDownloadBytes != 12345678901 || traffic.TotalUploadBytes != 987654321 || traffic.ObservedAt != "2025-03-08T09:11:12Z" {
		t.Fatalf("traffic = %#v", traffic)
	}

	calls, err := client.Calls(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Direction != "incoming" || calls[1].Direction != "missed" {
		t.Fatalf("calls = %#v", calls)
	}

	devices, err := client.Devices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].Name != "sanitized-device" || devices[0].MACAddress != "02:00:00:00:00:10" || !devices[0].Active || devices[1].Active {
		t.Fatalf("devices = %#v", devices)
	}
}

func TestDoctorUsesFixtureBackedDescriptionAndDeviceInfo(t *testing.T) {
	server := fixtureServer(t)
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Endpoint != server.URL || report.Reachability.State != "reachable" || report.Protocol.State != "available" || report.Authentication.State != "authenticated" {
		t.Fatalf("doctor checks = %#v", report)
	}
	if report.Model != "FRITZ!Box 7590 AX" || report.Firmware != "8.02" {
		t.Fatalf("doctor identity = %#v", report)
	}
	if report.Capabilities.Status.State != "advertised" || report.Capabilities.Overview.State != "advertised" || report.Capabilities.WAN.State != "advertised" || report.Capabilities.Traffic.State != "advertised" || report.Capabilities.Calls.State != "advertised" || report.Capabilities.Devices.State != "advertised" {
		t.Fatalf("doctor capabilities = %#v", report.Capabilities)
	}
}

func TestDoctorReportsOptionalUnsupportedCapabilities(t *testing.T) {
	const description = `<root><device><serviceList><service><serviceType>urn:dslforum-org:service:DeviceInfo:1</serviceType><controlURL>/device</controlURL></service></serviceList></device></root>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == descriptionPath {
			_, _ = w.Write([]byte(description))
			return
		}
		_, _ = w.Write([]byte(deviceFixture))
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Capabilities.Status.State != "advertised" || report.Capabilities.WAN.State != "unsupported" || report.Capabilities.Overview.State != "unsupported" {
		t.Fatalf("capabilities = %#v", report.Capabilities)
	}
	if report.Capabilities.WAN.Remediation == "" || report.Capabilities.Devices.Remediation == "" {
		t.Fatal("unsupported capability has no remediation")
	}
}

func TestDoctorReportsAuthenticationFailureWithoutSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == descriptionPath {
			_, _ = w.Write([]byte(descriptionFixture))
			return
		}
		w.Header().Set("WWW-Authenticate", `Digest realm="router", nonce="nonce", qop="auth"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := New(server.URL, "private-user", "private-password", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Doctor(t.Context())
	if err == nil || report.Authentication.State != "unauthenticated" {
		t.Fatalf("report=%#v error=%v", report, err)
	}
	combined := report.Authentication.Remediation + err.Error()
	if strings.Contains(combined, "private-user") || strings.Contains(combined, "private-password") {
		t.Fatalf("diagnosis leaked credentials: %q", combined)
	}
}

func TestDoctorReportsDisabledTR064(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Doctor(t.Context())
	if err == nil || report.Reachability.State != "reachable" || report.Protocol.State != "disabled" || report.Protocol.Remediation == "" {
		t.Fatalf("report=%#v error=%v", report, err)
	}
}

func TestOverviewUsesFixtureBackedReadOperations(t *testing.T) {
	server := fixtureServer(t)
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.Date(2025, 3, 8, 9, 11, 12, 0, time.UTC) }

	overview, err := client.Overview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Router.Model != "FRITZ!Box 7590 AX" || overview.WAN.Status != "Connected" || overview.Traffic.TotalDownloadBytes != 12345678901 {
		t.Fatalf("overview = %#v", overview)
	}
}

func TestIPFamilyUsesReturnedAddress(t *testing.T) {
	tests := map[string]string{
		"192.0.2.1":         "ipv4",
		"::ffff:192.0.2.1":  "ipv4",
		"2001:db8::1":       "ipv6",
		"":                  "unknown",
		"not-an-ip-address": "unknown",
	}
	for address, want := range tests {
		if got := ipFamily(address); got != want {
			t.Errorf("ipFamily(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestDiscoveryFindsNestedServices(t *testing.T) {
	const nestedDescription = `<?xml version="1.0"?>
<root><device><serviceList>
  <service><serviceType>urn:dslforum-org:service:DeviceInfo:1</serviceType><controlURL>/upnp/control/deviceinfo</controlURL></service>
</serviceList><deviceList><device><serviceList>
  <service><serviceType>urn:dslforum-org:service:WANIPConnection:1</serviceType><controlURL>/upnp/control/wanipconn1</controlURL></service>
  <service><serviceType>urn:dslforum-org:service:WANCommonInterfaceConfig:1</serviceType><controlURL>/upnp/control/wancommonifconfig1</controlURL></service>
</serviceList></device></deviceList></device></root>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(nestedDescription))
	}))
	defer server.Close()

	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.discover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.services["urn:dslforum-org:service:WANIPConnection:1"]; !ok {
		t.Fatalf("services = %#v", client.services)
	}
	if _, ok := client.services["urn:dslforum-org:service:WANCommonInterfaceConfig:1"]; !ok {
		t.Fatalf("services = %#v", client.services)
	}
}

func TestWANSupportsPPPConnectionService(t *testing.T) {
	pppDescription := strings.ReplaceAll(descriptionFixture, "WANIPConnection", "WANPPPConnection")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tr64desc.xml" {
			_, _ = w.Write([]byte(pppDescription))
			return
		}
		action := r.Header.Get("SOAPAction")
		if strings.Contains(action, "GetStatusInfo") {
			_, _ = w.Write([]byte(wanStatusFixture))
			return
		}
		_, _ = w.Write([]byte(wanIPFixture))
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	wan, err := client.WAN(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if wan.Status != "Connected" {
		t.Fatalf("WAN = %#v", wan)
	}
}

func TestDevicesReportMissingHostsCapability(t *testing.T) {
	const description = `<root><device><serviceList><service><serviceType>urn:dslforum-org:service:DeviceInfo:1</serviceType><controlURL>/device</controlURL></service></serviceList></device></root>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(description))
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Devices(t.Context())
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || protocolErr.Operation != "GetHostNumberOfEntries" {
		t.Fatalf("error = %#v", err)
	}
}

func TestCallsRejectsCrossOriginURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tr64desc.xml" {
			_, _ = w.Write([]byte(descriptionFixture))
			return
		}
		_, _ = w.Write([]byte(strings.Replace(callListURLFixture, "/calllist.lua?sid=redacted", "http://example.com/calls.xml", 1)))
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Calls(t.Context())
	if err == nil || !strings.Contains(err.Error(), "router origin") {
		t.Fatalf("error = %v", err)
	}
}
