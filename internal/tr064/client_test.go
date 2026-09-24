package tr064

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

//go:embed testdata/wifi-description.xml
var wifiDescriptionFixture string

//go:embed testdata/wifi-info.xml
var wifiInfoFixture string

//go:embed testdata/wifi-channel.xml
var wifiChannelFixture string

//go:embed testdata/wifi-associations.xml
var wifiAssociationsFixture string

//go:embed testdata/wifi-security.xml
var wifiSecurityFixture string

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

type wifiResponse struct {
	body   string
	status int
}

func wifiFixtureClient(t *testing.T, description string, overrides map[string]wifiResponse) (*Client, <-chan string) {
	t.Helper()
	requests := make(chan string, 100)
	responses := map[string]string{
		"GetInfo":              wifiInfoFixture,
		"GetChannelInfo":       wifiChannelFixture,
		"GetTotalAssociations": wifiAssociationsFixture,
		"GetBeaconType":        wifiSecurityFixture,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == descriptionPath {
			if r.Method != http.MethodGet {
				t.Error("description request was not GET")
			}
			_, _ = w.Write([]byte(description))
			return
		}
		if r.URL.Path == "/device" && r.Header.Get("SOAPAction") == `"urn:dslforum-org:service:DeviceInfo:1#GetInfo"` {
			requests <- "/device#GetInfo"
			_, _ = w.Write([]byte(deviceFixture))
			return
		}
		header := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		serviceType, action, _ := strings.Cut(header, "#")
		requests <- r.URL.Path + "#" + action
		response, ok := responses[action]
		if !ok || r.Method != http.MethodPost {
			t.Errorf("forbidden Wi-Fi request: %s", action)
			http.Error(w, "forbidden action", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		want := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + serviceType + `"></u:` + action + `></s:Body></s:Envelope>`
		if string(body) != want {
			t.Errorf("unexpected SOAP request: %s", body)
		}
		override, ok := overrides[r.URL.Path+"#"+action]
		if !ok {
			override, ok = overrides[action]
		}
		if ok {
			response = override.body
			if override.status != 0 {
				w.WriteHeader(override.status)
			}
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, requests
}

func TestWiFiEnumeratesNestedInstancesInNumericOrder(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run(version, func(t *testing.T) {
			description := strings.Replace(wifiDescriptionFixture, "WLANConfiguration:1", "WLANConfiguration:"+version, 1)
			client, requests := wifiFixtureClient(t, description, map[string]wifiResponse{
				"/wifi1#GetChannelInfo":       {body: strings.Replace(wifiChannelFixture, ">36<", ">0<", 1)},
				"/wifi2#GetTotalAssociations": {body: strings.Replace(wifiAssociationsFixture, ">2<", ">65535<", 1)},
			})
			radios, err := client.WiFi(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := []Radio{
				{wlanIDPrefix + "1", "synthetic-ap", true, 0, "5000", "ax", 2, "11iandWPA3"},
				{wlanIDPrefix + "2", "synthetic-ap", true, 36, "5000", "ax", 65535, "11iandWPA3"},
				{wlanIDPrefix + "10", "synthetic-ap", true, 36, "5000", "ax", 2, "11iandWPA3"},
			}
			if !reflect.DeepEqual(radios, want) {
				t.Fatalf("radios = %#v, want %#v", radios, want)
			}
			if len(client.allServices) != 4 || len(requests) != 12 {
				t.Fatalf("services=%d requests=%d", len(client.allServices), len(requests))
			}
			for _, path := range []string{"/wifi1", "/wifi2", "/wifi10"} {
				for _, action := range []string{"GetInfo", "GetChannelInfo", "GetTotalAssociations", "GetBeaconType"} {
					if got := <-requests; got != path+"#"+action {
						t.Fatalf("request = %q", got)
					}
				}
			}
			encoded, err := json.Marshal(radios[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != `{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration1","ssid":"synthetic-ap","enabled":true,"channel":0,"band":"5000","standard":"ax","associated_devices":2,"security_mode":"11iandWPA3"}` {
				t.Fatalf("radio JSON = %s", encoded)
			}
		})
	}
}

func TestWiFiRejectsInvalidIdentifiersBeforeActions(t *testing.T) {
	for _, id := range []string{"", "private-value", wlanIDPrefix, wlanIDPrefix + "01", wlanIDPrefix + "-1", wlanIDPrefix + "+1", wlanIDPrefix + "1 ", wlanIDPrefix + "18446744073709551616", wlanIDPrefix + "2"} {
		t.Run(id, func(t *testing.T) {
			description := strings.Replace(wifiDescriptionFixture, "<serviceId>"+wlanIDPrefix+"1</serviceId>", "<serviceId>"+id+"</serviceId>", 1)
			client, requests := wifiFixtureClient(t, description, nil)
			radios, err := client.WiFi(t.Context())
			var protocolErr *Error
			if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("radios=%#v error=%v", radios, err)
			}
			if len(requests) != 0 {
				t.Fatal("invalid identifiers triggered SOAP actions")
			}
		})
	}
}

func TestWiFiMissingService(t *testing.T) {
	client, requests := wifiFixtureClient(t, descriptionFixture, nil)
	radios, err := client.WiFi(t.Context())
	var protocolErr *Error
	if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || !strings.Contains(protocolErr.Message, wifiRemediation) || len(requests) != 0 {
		t.Fatalf("radios=%#v error=%v", radios, err)
	}
}

func TestWiFiValidatesEnableState(t *testing.T) {
	for _, value := range []string{"0", "1", "", "2", "-1", "true", "private-value", "missing"} {
		t.Run(value, func(t *testing.T) {
			original := "<NewEnable>1</NewEnable>"
			replacement := "<NewEnable>" + value + "</NewEnable>"
			if value == "missing" {
				replacement = ""
			}
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
				"GetInfo": {body: strings.Replace(wifiInfoFixture, original, replacement, 1)},
			})
			radios, err := client.WiFi(t.Context())
			if value == "0" || value == "1" {
				if err != nil || len(radios) != 3 || radios[0].Enabled != (value == "1") {
					t.Fatalf("radios=%#v error=%v", radios, err)
				}
				return
			}
			var protocolErr *Error
			if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("radios=%#v error=%v", radios, err)
			}
		})
	}
}

func TestWiFiValidatesNumericFields(t *testing.T) {
	for _, field := range []struct{ action, fixture, tag, original, overflow, maximum string }{
		{"GetChannelInfo", wifiChannelFixture, "NewChannel", "36", "256", "255"},
		{"GetTotalAssociations", wifiAssociationsFixture, "NewTotalAssociations", "2", "65536", "65535"},
	} {
		for _, value := range []string{"", "-1", "+1", "1.5", "private-value", "18446744073709551616", field.overflow, "missing", "0", field.maximum} {
			t.Run(field.action+"/"+value, func(t *testing.T) {
				original := "<" + field.tag + ">" + field.original + "</" + field.tag + ">"
				replacement := "<" + field.tag + ">" + value + "</" + field.tag + ">"
				if value == "missing" {
					replacement = ""
				}
				client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
					field.action: {body: strings.Replace(field.fixture, original, replacement, 1)},
				})
				radios, err := client.WiFi(t.Context())
				if value == "0" || value == field.maximum {
					if err != nil || len(radios) != 3 {
						t.Fatalf("radios=%#v error=%v", radios, err)
					}
					return
				}
				var protocolErr *Error
				if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "private-value") {
					t.Fatalf("radios=%#v error=%v", radios, err)
				}
			})
		}
	}
}

func TestWiFiAllowlistsBandStandardAndSecurity(t *testing.T) {
	for _, band := range []string{"2400", "5000", "6000", "unknown", "private-value", ""} {
		t.Run("band/"+band, func(t *testing.T) {
			replacement := "<NewX_AVM-DE_FrequencyBand>" + band + "</NewX_AVM-DE_FrequencyBand>"
			if band == "" {
				replacement = ""
			}
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
				"GetInfo": {body: strings.Replace(wifiInfoFixture, "<NewX_AVM-DE_FrequencyBand>5000</NewX_AVM-DE_FrequencyBand>", replacement, 1)},
			})
			radios, err := client.WiFi(t.Context())
			want := band
			if band == "" || band == "private-value" {
				want = "unknown"
			}
			if err != nil || len(radios) != 3 || radios[0].Band != want {
				t.Fatalf("radios=%#v error=%v", radios, err)
			}
		})
	}
	for _, standard := range []string{"b", "g", "n", "ac", "ax", "be", "", "private-value"} {
		t.Run("standard/"+standard, func(t *testing.T) {
			replacement := "<NewStandard>" + standard + "</NewStandard>"
			if standard == "" {
				replacement = ""
			}
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
				"GetInfo": {body: strings.Replace(wifiInfoFixture, "<NewStandard>ax</NewStandard>", replacement, 1)},
			})
			radios, err := client.WiFi(t.Context())
			want := standard
			if standard == "" || standard == "private-value" {
				want = "unknown"
			}
			if err != nil || len(radios) != 3 || radios[0].Standard != want {
				t.Fatalf("radios=%#v error=%v", radios, err)
			}
		})
	}
	for _, mode := range []string{"None", "Basic", "WPA", "11i", "WPAand11i", "WPA3", "11iandWPA3", "OWE", "OWETrans", "private-value"} {
		t.Run("security/"+mode, func(t *testing.T) {
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
				"GetBeaconType": {body: strings.Replace(wifiSecurityFixture, ">11iandWPA3<", ">"+mode+"<", 1)},
			})
			radios, err := client.WiFi(t.Context())
			want := mode
			if mode == "private-value" {
				want = "unknown"
			}
			if err != nil || len(radios) != 3 || radios[0].SecurityMode != want {
				t.Fatalf("radios=%#v error=%v", radios, err)
			}
		})
	}
}

func TestWiFiReportsSSIDAndDiscardsBSSID(t *testing.T) {
	client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
		"GetInfo": {body: strings.Replace(wifiInfoFixture, "<NewSSID>synthetic-ap</NewSSID>", "<NewSSID>synthetic, \"quoted\" ap</NewSSID>", 1)},
	})
	radios, err := client.WiFi(t.Context())
	if err != nil || len(radios) != 3 || radios[0].SSID != "synthetic, \"quoted\" ap" {
		t.Fatalf("radios=%#v error=%v", radios, err)
	}
	for _, radio := range radios {
		encoded, err := json.Marshal(radio)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "synthetic-sensitive-bssid") {
			t.Fatal("radio JSON leaked the BSSID")
		}
	}
}

func TestWiFiRequiresSecurityMode(t *testing.T) {
	for _, replacement := range []string{"", "<NewBeaconType></NewBeaconType>"} {
		client, _ := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
			"/wifi10#GetBeaconType": {body: strings.Replace(wifiSecurityFixture, "<NewBeaconType>11iandWPA3</NewBeaconType>", replacement, 1)},
		})
		radios, err := client.WiFi(t.Context())
		var protocolErr *Error
		if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" {
			t.Fatalf("radios=%#v error=%v", radios, err)
		}
	}
}

func TestWiFiLateErrorsAreAtomicAndSanitized(t *testing.T) {
	for _, test := range []struct {
		name, body, kind string
		status           int
	}{
		{"auth", "private-value", "auth", http.StatusUnauthorized},
		{"fault", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusInternalServerError},
		{"fault-success-status", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusOK},
		{"invalid-xml", "<private-value", "protocol", http.StatusOK},
		{"invalid-xml-error-status", "<private-value", "protocol", http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{
				"/wifi10#GetBeaconType": {body: test.body, status: test.status},
			})
			radios, err := client.WiFi(t.Context())
			var protocolErr *Error
			if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.StatusCode != test.status || protocolErr.FaultCode != "" || protocolErr.Operation != "wifi" {
				t.Fatalf("radios=%#v error=%#v", radios, err)
			}
			if strings.Contains(fmt.Sprintf("%#v", err), "private") || strings.Contains(err.Error(), "/wifi") || len(requests) != 12 {
				t.Fatalf("unsanitized or premature failure: %#v", err)
			}
		})
	}
}

type wifiFailingTransport struct{}

func (wifiFailingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private-network-error")
}

func TestWiFiDiscoveryErrorsAreSanitized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private-value"))
			}))
			t.Cleanup(server.Close)
			client, err := New(server.URL, "", "", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			radios, err := client.WiFi(t.Context())
			kind := "router"
			if status == http.StatusUnauthorized {
				kind = "auth"
			}
			var protocolErr *Error
			if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.StatusCode != status || protocolErr.Operation != "wifi" || protocolErr.FaultCode != "" || strings.Contains(fmt.Sprintf("%#v", err), "private") {
				t.Fatalf("radios=%#v error=%#v", radios, err)
			}
		})
	}
}

func TestWiFiNetworkErrorsAreSanitized(t *testing.T) {
	for _, discovered := range []bool{false, true} {
		t.Run(fmt.Sprint(discovered), func(t *testing.T) {
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, nil)
			if discovered {
				if err := client.discover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			client.http = &http.Client{Transport: wifiFailingTransport{}}
			radios, err := client.WiFi(t.Context())
			var protocolErr *Error
			if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "network" || protocolErr.Operation != "wifi" || protocolErr.StatusCode != 0 || strings.Contains(fmt.Sprintf("%#v", err), "private") || strings.Contains(err.Error(), client.base.Host) {
				t.Fatalf("radios=%#v error=%#v", radios, err)
			}
		})
	}
}

func TestDoctorAdvertisesWiFiWithoutWiFiActions(t *testing.T) {
	client, requests := wifiFixtureClient(t, wifiDescriptionFixture, nil)
	report, err := client.Doctor(t.Context())
	if err != nil || report.Capabilities.WiFi.State != "advertised" || len(requests) != 1 || <-requests != "/device#GetInfo" {
		t.Fatalf("report=%#v error=%v", report, err)
	}
	encoded, err := json.Marshal(report.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(encoded), `,"wifi":{"state":"advertised"}}`) || strings.Index(string(encoded), `"devices"`) > strings.Index(string(encoded), `"wifi"`) {
		t.Fatalf("capabilities JSON = %s", encoded)
	}
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
	if report.Capabilities.WiFi.State != "unsupported" || report.Capabilities.WiFi.Remediation == "" || report.Capabilities.WAN.Remediation == "" || report.Capabilities.Devices.Remediation == "" {
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

func TestDevicesRejectsOversizedHostCount(t *testing.T) {
	var entryRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tr64desc.xml" {
			_, _ = w.Write([]byte(descriptionFixture))
			return
		}
		if strings.Contains(r.Header.Get("SOAPAction"), "GetGenericHostEntry") {
			entryRequests++
			_, _ = w.Write([]byte(hostEntryZeroFixture))
			return
		}
		_, _ = w.Write([]byte(strings.Replace(hostCountFixture, ">2<", ">4294967295<", 1)))
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Devices(t.Context())
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || protocolErr.Operation != "GetHostNumberOfEntries" {
		t.Fatalf("error = %#v", err)
	}
	if entryRequests != 0 {
		t.Fatalf("GetGenericHostEntry requests = %d, want 0", entryRequests)
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
