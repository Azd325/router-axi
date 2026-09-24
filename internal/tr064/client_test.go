package tr064

import (
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
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

//go:embed testdata/port-mapping-description.xml
var portMappingDescriptionFixture string

//go:embed testdata/port-mapping-scpd.xml
var portMappingSCPDFixture string

//go:embed testdata/port-mapping-count.xml
var portMappingCountFixture string

//go:embed testdata/port-mapping-entry.xml
var portMappingEntryFixture string

//go:embed testdata/config-file-url.xml
var configFileURLFixture string

//go:embed testdata/config-export.txt
var configExportFixture string

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
	if !strings.Contains(string(encoded), `,"wifi":{"state":"advertised"},"forwards":{"state":"unsupported","remediation":`) || strings.Index(string(encoded), `"devices"`) > strings.Index(string(encoded), `"wifi"`) {
		t.Fatalf("capabilities JSON = %s", encoded)
	}
}

type forwardExchange struct {
	path, action, index, body string
	status                    int
	location                  string
	optional                  bool
}

func forwardFixtureClient(t *testing.T, exchanges []forwardExchange) *Client {
	t.Helper()
	pending := make(chan forwardExchange, len(exchanges))
	for _, exchange := range exchanges {
		pending <- exchange
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var exchange forwardExchange
		select {
		case exchange = <-pending:
		default:
			t.Errorf("unexpected request: %s %s %s", r.Method, r.URL.Path, r.Header.Get("SOAPAction"))
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		method := http.MethodGet
		serviceType := "urn:dslforum-org:service:WANIPConnection:1"
		if exchange.path == "/ppp1" {
			serviceType = "urn:dslforum-org:service:WANPPPConnection:1"
		}
		if exchange.path == "/device" {
			serviceType = "urn:dslforum-org:service:DeviceInfo:1"
		}
		if exchange.path == "/layer3" {
			serviceType = "urn:dslforum-org:service:Layer3Forwarding:1"
		}
		var wantBody, wantAction string
		if exchange.action != "" {
			method = http.MethodPost
			if exchange.action != "GetPortMappingNumberOfEntries" && exchange.action != "GetGenericPortMappingEntry" && (exchange.path != "/device" || exchange.action != "GetInfo") && (exchange.path != "/layer3" || exchange.action != "GetDefaultConnectionService") {
				t.Errorf("test permitted forbidden action %q", exchange.action)
			}
			argument := ""
			if exchange.action == "GetGenericPortMappingEntry" {
				argument = "<NewPortMappingIndex>" + exchange.index + "</NewPortMappingIndex>"
			}
			wantAction = `"` + serviceType + "#" + exchange.action + `"`
			wantBody = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + exchange.action + ` xmlns:u="` + serviceType + `">` + argument + `</u:` + exchange.action + `></s:Body></s:Envelope>`
		} else if exchange.path != descriptionPath && exchange.path != "/ip1.xml" && exchange.path != "/ip2.xml" && exchange.path != "/ppp1.xml" {
			t.Errorf("test permitted forbidden GET %q", exchange.path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != method || r.URL.RequestURI() != exchange.path || r.Header.Get("SOAPAction") != wantAction || string(body) != wantBody {
			t.Errorf("request = %s %s %q %s; want %s %s %q %s", r.Method, r.URL.RequestURI(), r.Header.Get("SOAPAction"), body, method, exchange.path, wantAction, wantBody)
		}
		if exchange.location != "" {
			w.Header().Set("Location", exchange.location)
		}
		if exchange.status != 0 {
			w.WriteHeader(exchange.status)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(exchange.body, "__ORIGIN__", "http://"+r.Host)))
	}))
	t.Cleanup(func() {
		server.Close()
		unused := 0
		close(pending)
		for exchange := range pending {
			if !exchange.optional {
				unused++
			}
		}
		if unused != 0 {
			t.Errorf("%d expected requests were not made", unused)
		}
	})
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func forwardScript(defaultService, activePath string, tables ...[]string) []forwardExchange {
	exchanges := []forwardExchange{
		{path: descriptionPath, body: portMappingDescriptionFixture},
		{path: "/layer3", action: "GetDefaultConnectionService", body: `<Envelope><NewDefaultConnectionService>` + defaultService + `</NewDefaultConnectionService></Envelope>`},
		{path: activePath + ".xml", body: portMappingSCPDFixture},
	}
	count := forwardExchange{path: activePath, action: "GetPortMappingNumberOfEntries", body: strings.Replace(portMappingCountFixture, ">2<", fmt.Sprintf(">%d<", entriesFor(tables)), 1)}
	exchanges = append(exchanges, count)
	for index, entry := range allEntries(tables) {
		exchanges = append(exchanges, forwardExchange{path: activePath, action: "GetGenericPortMappingEntry", index: fmt.Sprint(index), body: entry})
	}
	exchanges = append(exchanges, count)
	return exchanges
}

func entriesFor(tables [][]string) int { return len(allEntries(tables)) }

func allEntries(tables [][]string) []string {
	entries := []string{}
	for _, table := range tables {
		entries = append(entries, table...)
	}
	return entries
}

func activeIPScript(tables ...[]string) []forwardExchange {
	return forwardScript("urn:WANIPConnection-com:serviceId:WANIPConnection1", "/ip1", tables...)
}

func mappingField(body, tag, value string) string {
	start := strings.Index(body, "<"+tag+">")
	end := strings.Index(body, "</"+tag+">") + len(tag) + 3
	replacement := ""
	if value != "missing" {
		var escaped strings.Builder
		_ = xml.EscapeText(&escaped, []byte(value))
		replacement = "<" + tag + ">" + escaped.String() + "</" + tag + ">"
	}
	return body[:start] + replacement + body[end:]
}

func mappingEntry(forward Forward) string {
	body := portMappingEntryFixture
	for tag, value := range map[string]string{
		"NewRemoteHost": forward.RemoteHost, "NewExternalPort": fmt.Sprint(forward.ExternalPort),
		"NewProtocol": forward.Protocol, "NewInternalPort": fmt.Sprint(forward.InternalPort),
		"NewInternalClient": forward.InternalClient, "NewEnabled": fmt.Sprint(forward.Enabled),
		"NewPortMappingDescription": forward.Description,
	} {
		body = mappingField(body, tag, value)
	}
	lease := "missing"
	if forward.LeaseDuration != nil {
		lease = fmt.Sprint(*forward.LeaseDuration)
	}
	return mappingField(body, "NewLeaseDuration", lease)
}

func assertForwardError(t *testing.T, client *Client, kind string, status int) *Error {
	t.Helper()
	forwards, err := client.Forwards(t.Context())
	var protocolErr *Error
	if forwards != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "forwards" || protocolErr.StatusCode != status || protocolErr.FaultCode != "" {
		t.Fatalf("forwards=%#v error=%#v", forwards, err)
	}
	for _, secret := range []string{"private", client.base.Host, "/ip1", "/ip2", "/ppp1"} {
		if strings.Contains(fmt.Sprintf("%#v %s", protocolErr, err), secret) {
			t.Fatalf("unsanitized error: %#v", protocolErr)
		}
	}
	return protocolErr
}

func TestForwardsSortsEntriesOfTheActiveService(t *testing.T) {
	zero, two, ten, maximum := uint64(0), uint64(2), uint64(10), uint64(4294967295)
	want := []Forward{
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "", nil},
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "", &zero},
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "", &two},
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "", &ten},
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "", &maximum},
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-b", "", nil},
		{true, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "", nil},
		{false, "TCP", 2, "192.0.2.10", 10, "synthetic-a", "", nil},
		{false, "TCP", 2, "192.0.2.20", 2, "synthetic-a", "", nil},
		{false, "TCP", 2, "192.0.2.10", 2, "synthetic-a", "198.51.100.2", nil},
		{false, "TCP", 10, "192.0.2.10", 2, "synthetic-a", "", nil},
		{true, "TCP", 65535, "192.0.2.10", 65535, "synthetic, \"quoted\" & web", "", &zero},
		{false, "UDP", 1, "192.0.2.10", 1, "", "", nil},
	}
	for _, shift := range []int{0, 4, 9} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			// Every entry lives on the single active WAN service; enumeration
			// order may vary, so the full-tuple sort stays observable.
			table := make([]string, len(want))
			for i := range want {
				table[i] = mappingEntry(want[(len(want)-1-i+shift)%len(want)])
			}
			client := forwardFixtureClient(t, activeIPScript(table))
			got, err := client.Forwards(t.Context())
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("forwards=%#v want=%#v error=%v", got, want, err)
			}
		})
	}
}

func TestForwardsZeroCountsDoNotFetchEntries(t *testing.T) {
	client := forwardFixtureClient(t, activeIPScript())
	got, err := client.Forwards(t.Context())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("forwards=%#v error=%v", got, err)
	}
}

func TestForwardsSelectsDefaultConnectionService(t *testing.T) {
	for _, test := range []struct {
		name, defaultService, activePath string
	}{
		{"ip-by-id", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "/ip1"},
		{"second-ip-by-id", "urn:WANIPConnection-com:serviceId:WANIPConnection2", "/ip2"},
		{"ppp-by-id", "urn:WANPPPConnection-com:serviceId:WANPPPConnection1", "/ppp1"},
		{"ppp-by-type", "urn:dslforum-org:service:WANPPPConnection:1", "/ppp1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := mappingEntry(Forward{true, "TCP", 443, "192.0.2.10", 8443, "synthetic-web", "", nil})
			client := forwardFixtureClient(t, forwardScript(test.defaultService, test.activePath, []string{entry}))
			got, err := client.Forwards(t.Context())
			if err != nil || len(got) != 1 || !got[0].Enabled || got[0].ExternalPort != 443 {
				t.Fatalf("forwards=%#v error=%v", got, err)
			}
		})
	}
}

func TestForwardsSelectsUpnpStyleDefaultConnectionService(t *testing.T) {
	// Live FRITZ routers report default identifiers that do not literally
	// equal the advertised dslforum-style service identifiers: upnp-org
	// families, uuid-shaped identifiers, and the dot-separated
	// N.WANIPConnection.N that FRITZ!OS returns. Each selects the one
	// advertised WAN service of the same family and instance.
	for _, test := range []struct {
		name, defaultService, activePath string
	}{
		{"ip-family", "urn:upnp-org:serviceId:WANIPConnection1", "/ip1"},
		{"ppp-family", "urn:upnp-org:serviceId:WANPPPConnection1", "/ppp1"},
		{"uuid-ip-family", "uuid:4d69648d-6c2f-4e46-bf4e-1a2b3c4d5e6f:WANIPConnection.2", "/ip2"},
		{"avm-live-shape", "1.WANIPConnection.1", "/ip1"},
		{"avm-live-shape-ppp", "1.WANPPPConnection.1", "/ppp1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := forwardScript(test.defaultService, test.activePath, []string{portMappingEntryFixture})
			client := forwardFixtureClient(t, script)
			gots, err := client.Forwards(t.Context())
			if err != nil || len(gots) != 1 || !gots[0].Enabled || gots[0].ExternalPort != 443 || gots[0].InternalClient != "192.0.2.10" {
				t.Fatalf("forwards=%#v error=%v", gots, err)
			}
		})
	}
}

func TestForwardsRejectsUnsupportedUpnpStyleDefaultConnectionService(t *testing.T) {
	for _, test := range []struct{ name, defaultService string }{
		// No advertised WAN service carries the default's family.
		{"unknown-family", "urn:upnp-org:serviceId:WANCommonInterfaceConfig"},
		// An additional colon is not a documented default identifier.
		{"additional-colon", "urn:upnp-org:serviceId:WANIPConnection:1"},
		// A malformed family that names no WAN connection service.
		{"malformed-family", "urn:upnp-org:serviceId:WANConnection"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := forwardScript(test.defaultService, "/ip1")
			err := assertForwardError(t, forwardFixtureClient(t, script[:2]), "unsupported", 0)
			if !strings.Contains(err.Message, forwardsRemediation) {
				t.Fatalf("missing remediation: %#v", err)
			}
		})
	}
}

func TestActiveWANServiceIDMatchesAdvertisedFamily(t *testing.T) {
	for _, test := range []struct {
		name, advertisedType, advertisedID, defaultService string
		want                                               bool
	}{
		{"ip-family-ip-service", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "urn:upnp-org:serviceId:WANIPConnection1", true},
		{"ip-family-other-instance", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection2", "urn:upnp-org:serviceId:WANIPConnection1", false},
		{"ppp-family-ppp-service", "urn:dslforum-org:service:WANPPPConnection:1", "urn:WANPPPConnection-com:serviceId:WANPPPConnection2", "urn:upnp-org:serviceId:WANPPPConnection2", true},
		{"family-mismatch", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "urn:upnp-org:serviceId:WANPPPConnection1", false},
		{"non-wan-service", "urn:dslforum-org:service:Hosts:1", "urn:Hosts-com:serviceId:Hosts1", "urn:upnp-org:serviceId:WANIPConnection1", false},
		{"additional-colon", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "urn:upnp-org:serviceId:WANIPConnection:1", false},
		{"malformed-family", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "urn:upnp-org:serviceId:WANConnection", false},
		{"family-without-instance", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "urn:upnp-org:serviceId:WANIPConnection", false},
		{"uuid-ip-family", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "uuid:4d69648d-6c2f-4e46-bf4e-1a2b3c4d5e6f:WANIPConnection.1", true},
		{"uuid-dot-before-family", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "uuid:4d69648d-6c2f-4e46-bf4e-1a2b3c4d5e6f.WANIPConnection.1", false},
		{"non-numeric-dot-head", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "a.WANIPConnection.1", false},
		{"bare-dot-head", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", ".WANIPConnection.1", false},
		{"avm-live-shape", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "1.WANIPConnection.1", true},
		{"avm-live-shape-instance-mismatch", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection2", "1.WANIPConnection.1", false},
		{"uuid-ppp-family", "urn:dslforum-org:service:WANPPPConnection:1", "urn:WANPPPConnection-com:serviceId:WANPPPConnection1", "uuid:4d69648d-6c2f-4e46-bf4e-1a2b3c4d5e6f:WANPPPConnection.1", true},
		{"uuid-family-mismatch", "urn:dslforum-org:service:WANIPConnection:1", "urn:WANIPConnection-com:serviceId:WANIPConnection1", "uuid:4d69648d-6c2f-4e46-bf4e-1a2b3c4d5e6f:WANPPPConnection.1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := activeWANServiceID(test.advertisedType, test.advertisedID, test.defaultService); got != test.want {
				t.Fatalf("activeWANServiceID(%q, %q, %q) = %v, want %v", test.advertisedType, test.advertisedID, test.defaultService, got, test.want)
			}
		})
	}
}

func TestForwardsSkipsInactiveAdvertisedServices(t *testing.T) {
	// The fixture advertises ip1, ip2, and ppp1; only the default WAN
	// service may receive a port-mapping SOAP request.
	client := forwardFixtureClient(t, activeIPScript([]string{portMappingEntryFixture}))
	got, err := client.Forwards(t.Context())
	if err != nil || len(got) != 1 {
		t.Fatalf("forwards=%#v error=%v", got, err)
	}
}

func TestForwardsRequiresUsableDefaultConnectionService(t *testing.T) {
	for _, test := range []struct {
		name, defaultService, remediation string
		idless                            bool
	}{
		{"missing-layer3", "", "Layer3Forwarding:GetDefaultConnectionService", false},
		{"whitespace", " \n ", "Layer3Forwarding:GetDefaultConnectionService", false},
		{"empty-with-idless-service", "", "Layer3Forwarding:GetDefaultConnectionService", true},
		{"invalid-value", "urn:synthetic:unknown", forwardsRemediation, false},
		{"non-wan", "urn:dslforum-org:service:WANCommonInterfaceConfig:1", forwardsRemediation, false},
		{"ambiguous-type", "urn:dslforum-org:service:WANIPConnection:1", forwardsRemediation, false},
		{"family-without-instance", "urn:upnp-org:serviceId:WANIPConnection", forwardsRemediation, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := forwardScript(test.defaultService, "/ip1")[:2]
			if test.idless {
				script[0].body = strings.Replace(script[0].body, "<serviceId>urn:WANIPConnection-com:serviceId:WANIPConnection1</serviceId>", "", 1)
			}
			err := assertForwardError(t, forwardFixtureClient(t, script), "unsupported", 0)
			if !strings.Contains(err.Message, test.remediation) {
				t.Fatalf("missing remediation: %#v", err)
			}
		})
	}
}

func TestForwardsLayer3ErrorsAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name, body, kind string
		status           int
	}{
		{"auth", "private-value", "auth", http.StatusUnauthorized},
		{"router", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusServiceUnavailable},
		{"internal-500", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "unsupported", http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := activeIPScript()
			script[1].body, script[1].status = test.body, test.status
			err := assertForwardError(t, forwardFixtureClient(t, script[:2]), test.kind, test.status)
			if (test.kind == "unsupported") != strings.Contains(err.Message, "Layer3Forwarding:GetDefaultConnectionService") {
				t.Fatalf("remediation mismatch: %#v", err)
			}
		})
	}
}

func TestForwardsLayer3NetworkErrorsAreSanitized(t *testing.T) {
	script := activeIPScript()[:2]
	for i := range script {
		script[i].optional = true
	}
	client := forwardFixtureClient(t, script)
	client.http = &http.Client{Transport: layer3FailingTransport{next: client.http.Transport}}
	err := assertForwardError(t, client, "network", 0)
	if !strings.Contains(err.Message, "active WAN service") || strings.Contains(err.Message, "Layer3Forwarding") {
		t.Fatalf("missing remediation: %#v", err)
	}
}

type layer3FailingTransport struct{ next http.RoundTripper }

func (t layer3FailingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/layer3" {
		return nil, errors.New("private-network-error")
	}
	return t.next.RoundTrip(r)
}

func TestForwardsValidatesRequiredFields(t *testing.T) {
	for _, field := range []struct {
		tag    string
		values []string
	}{
		{"NewExternalPort", []string{"missing", "", "0", "-1", "+1", "1.5", "private-value", "65536"}},
		{"NewInternalPort", []string{"missing", "", "0", "-1", "private-value", "65536"}},
		{"NewRemoteHost", []string{"missing"}},
		{"NewEnabled", []string{"missing", "", "2", "private-value"}},
		{"NewProtocol", []string{"missing", "", "tcp", "ICMP", "private-value"}},
		{"NewInternalClient", []string{"missing", "", " "}},
		{"NewLeaseDuration", []string{"", "-1", "1.5", "private-value", "4294967296"}},
	} {
		for _, value := range field.values {
			t.Run(field.tag+"/"+value, func(t *testing.T) {
				script := activeIPScript([]string{portMappingEntryFixture, mappingField(portMappingEntryFixture, field.tag, value)})
				_ = assertForwardError(t, forwardFixtureClient(t, script[:6]), "protocol", 0)
			})
		}
	}
}

func TestForwardsValidatesCountsBeforeAndAfterEnumeration(t *testing.T) {
	for _, value := range []string{"missing", "", "-1", "+1", "1.5", "private-value", "4097", "65536", "18446744073709551616"} {
		for _, position := range []int{3, 6} {
			t.Run(fmt.Sprintf("%s/%d", value, position), func(t *testing.T) {
				script := activeIPScript([]string{portMappingEntryFixture, portMappingEntryFixture})
				script[position].body = mappingField(portMappingCountFixture, "NewPortMappingNumberOfEntries", value)
				_ = assertForwardError(t, forwardFixtureClient(t, script[:position+1]), "protocol", 0)
			})
		}
	}
}

func TestForwardsEnforcesAggregateLimit(t *testing.T) {
	for _, secondCount := range []int{0, 1} {
		t.Run(fmt.Sprint(secondCount), func(t *testing.T) {
			entries := make([]string, 4096)
			for i := range entries {
				entries[i] = portMappingEntryFixture
			}
			script := activeIPScript(entries)
			if secondCount == 1 {
				position := 4 + len(entries)
				script[position].body = mappingField(portMappingCountFixture, "NewPortMappingNumberOfEntries", "1")
				_ = assertForwardError(t, forwardFixtureClient(t, script[:position+1]), "protocol", 0)
				return
			}
			got, err := forwardFixtureClient(t, script).Forwards(t.Context())
			if err != nil || len(got) != 4096 {
				t.Fatalf("count=%d error=%v", len(got), err)
			}
		})
	}
}

func TestForwardsUnsupportedCapabilitiesDoNotSendSOAP(t *testing.T) {
	for _, missing := range []string{"service", "SCPDURL", "GetPortMappingNumberOfEntries", "GetGenericPortMappingEntry"} {
		t.Run(missing, func(t *testing.T) {
			script := activeIPScript()
			switch missing {
			case "service":
				script[0].body = wifiDescriptionFixture
				script = script[:1]
			case "SCPDURL":
				script[0].body = strings.Replace(portMappingDescriptionFixture, "<SCPDURL>/ip1.xml</SCPDURL>", "", 1)
				script = script[:2]
			default:
				script[2].body = strings.Replace(portMappingSCPDFixture, "<action><name>"+missing+"</name></action>", "", 1)
				script = script[:3]
			}
			err := assertForwardError(t, forwardFixtureClient(t, script), "unsupported", 0)
			if !strings.Contains(err.Message, forwardsRemediation) {
				t.Fatalf("missing remediation: %#v", err)
			}
		})
	}
}

func TestForwardsChangedCountsAreAtomic(t *testing.T) {
	for _, count := range []string{"0", "3"} {
		t.Run(count, func(t *testing.T) {
			script := activeIPScript([]string{portMappingEntryFixture}, []string{portMappingEntryFixture})
			script[6].body = mappingField(portMappingCountFixture, "NewPortMappingNumberOfEntries", count)
			err := assertForwardError(t, forwardFixtureClient(t, script[:7]), "protocol", 0)
			if !strings.Contains(err.Message, "retry") {
				t.Fatalf("missing retry guidance: %#v", err)
			}
		})
	}
}

func TestForwardsLateErrorsAreAtomicAndSanitized(t *testing.T) {
	for _, test := range []struct {
		name, body, kind string
		status           int
	}{
		{"auth", "private-value", "auth", http.StatusUnauthorized},
		{"indexed-713", "<Fault><errorCode>713</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusInternalServerError},
		{"unsupported-401", "<Fault><errorCode>401</errorCode><errorDescription>private-value</errorDescription></Fault>", "unsupported", http.StatusInternalServerError},
		{"fault", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusInternalServerError},
		{"fault-success-status", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusOK},
		{"invalid-xml", "<private-value", "protocol", http.StatusOK},
		{"invalid-xml-error-status", "<private-value", "protocol", http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := activeIPScript([]string{portMappingEntryFixture}, []string{portMappingEntryFixture, portMappingEntryFixture})
			script[6].body, script[6].status = test.body, test.status
			err := assertForwardError(t, forwardFixtureClient(t, script[:7]), test.kind, test.status)
			if test.kind == "unsupported" && !strings.Contains(err.Message, forwardsRemediation) {
				t.Fatalf("missing remediation: %#v", err)
			}
		})
	}
}

func TestForwardsDescriptionErrorsAreSanitized(t *testing.T) {
	for _, position := range []int{0, 2} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable, http.StatusOK} {
			t.Run(fmt.Sprintf("%d/%d", position, status), func(t *testing.T) {
				script := activeIPScript()[:position+1]
				script[position].body, script[position].status = "<private-value", status
				kind, wantStatus := "router", status
				switch status {
				case http.StatusUnauthorized:
					kind = "auth"
				case http.StatusOK:
					kind, wantStatus = "protocol", 0
				}
				_ = assertForwardError(t, forwardFixtureClient(t, script), kind, wantStatus)
			})
		}
	}
}

func TestForwardsNetworkErrorsAreSanitized(t *testing.T) {
	for _, discovered := range []bool{false, true} {
		t.Run(fmt.Sprint(discovered), func(t *testing.T) {
			var script []forwardExchange
			if discovered {
				script = activeIPScript()[:1]
			}
			client := forwardFixtureClient(t, script)
			if discovered {
				if err := client.discover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			client.http = &http.Client{Transport: wifiFailingTransport{}}
			_ = assertForwardError(t, client, "network", 0)
		})
	}
}

func TestForwardsRejectsUnsafeServiceURLs(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalRequests.Add(1)
	}))
	defer external.Close()
	for _, tag := range []string{"controlURL", "SCPDURL"} {
		for _, address := range []string{external.URL + "/private-endpoint", "//" + strings.TrimPrefix(external.URL, "http://") + "/private-endpoint", "http://private-user:private-password@192.0.2.1/private-endpoint", "__ORIGIN__/private-endpoint#private-fragment", ""} {
			t.Run(tag+"/"+address, func(t *testing.T) {
				script := activeIPScript()[:2]
				original := "/ip1"
				if tag == "SCPDURL" {
					original += ".xml"
				}
				script[0].body = strings.Replace(script[0].body, "<"+tag+">"+original+"</"+tag+">", "<"+tag+">"+address+"</"+tag+">", 1)
				kind := "protocol"
				if address == "" && tag == "SCPDURL" {
					kind = "unsupported"
				}
				_ = assertForwardError(t, forwardFixtureClient(t, script), kind, 0)
			})
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("cross-origin service URL reached external server")
	}
}

func TestForwardsAcceptsSameOriginAbsoluteServiceURLs(t *testing.T) {
	script := activeIPScript([]string{portMappingEntryFixture})
	script[0].body = strings.ReplaceAll(script[0].body, ">/ip1", ">__ORIGIN__/ip1")
	got, err := forwardFixtureClient(t, script).Forwards(t.Context())
	if err != nil || len(got) != 1 || got[0].InternalClient != "192.0.2.10" || !got[0].Enabled {
		t.Fatalf("forwards=%#v error=%v", got, err)
	}
}

func TestForwardsRefusesRedirects(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalRequests.Add(1)
	}))
	defer external.Close()
	for _, position := range []int{0, 1, 2, 3} {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
			t.Run(fmt.Sprintf("%d/%d", position, status), func(t *testing.T) {
				script := activeIPScript([]string{portMappingEntryFixture})[:position+1]
				script[position].status = status
				script[position].location = external.URL + "/private-endpoint"
				client := forwardFixtureClient(t, script)
				_ = assertForwardError(t, client, "network", 0)
				if client.http.CheckRedirect != nil {
					t.Fatal("Forwards changed the caller's redirect policy")
				}
			})
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("redirect reached external server")
	}
}

func TestDoctorAdvertisesForwardsWithoutSCPDOrEnumeration(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		t.Run(fmt.Sprint(advertised), func(t *testing.T) {
			description := wifiDescriptionFixture
			if advertised {
				description = strings.ReplaceAll(portMappingDescriptionFixture, "SCPDURL", "unused")
			}
			client := forwardFixtureClient(t, []forwardExchange{
				{path: descriptionPath, body: description},
				{path: "/device", action: "GetInfo", body: deviceFixture},
			})
			report, err := client.Doctor(t.Context())
			want := DoctorCheck{State: "unsupported", Remediation: forwardsRemediation}
			if advertised {
				want = DoctorCheck{State: "advertised"}
			}
			if err != nil || report.Capabilities.Forwards != want {
				t.Fatalf("report=%#v error=%v", report, err)
			}
			encoded, err := json.Marshal(report.Capabilities)
			if err != nil || strings.Index(string(encoded), `"wifi"`) > strings.Index(string(encoded), `"forwards"`) {
				t.Fatalf("capabilities JSON=%s error=%v", encoded, err)
			}
		})
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

type hostExchange struct {
	action, index, body string
	status              int
	location            string
}

func hostScript(entries ...string) []hostExchange {
	script := []hostExchange{
		{body: descriptionFixture},
		{action: "GetHostNumberOfEntries", body: mappingField(hostCountFixture, "NewHostNumberOfEntries", fmt.Sprint(len(entries)))},
	}
	for i, entry := range entries {
		script = append(script, hostExchange{action: "GetGenericHostEntry", index: fmt.Sprint(i), body: entry})
	}
	return script
}

func hostFixtureClient(t *testing.T, script []hostExchange) *Client {
	t.Helper()
	pending := make(chan hostExchange, len(script))
	for _, exchange := range script {
		pending <- exchange
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var exchange hostExchange
		select {
		case exchange = <-pending:
		default:
			t.Error("unexpected host request")
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		method, path, action, body := http.MethodGet, descriptionPath, "", ""
		if exchange.action != "" {
			method, path = http.MethodPost, "/upnp/control/hosts"
			argument := ""
			switch exchange.action {
			case "GetHostNumberOfEntries":
			case "GetGenericHostEntry":
				argument = "<NewIndex>" + exchange.index + "</NewIndex>"
			default:
				t.Errorf("forbidden Hosts action: %s", exchange.action)
			}
			action = `"urn:dslforum-org:service:Hosts:1#` + exchange.action + `"`
			body = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + exchange.action + ` xmlns:u="urn:dslforum-org:service:Hosts:1">` + argument + `</u:` + exchange.action + `></s:Body></s:Envelope>`
		}
		gotBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != method || r.URL.RequestURI() != path || r.Header.Get("SOAPAction") != action || string(gotBody) != body {
			t.Errorf("unexpected Hosts request: %s %s %q %s", r.Method, r.URL.RequestURI(), r.Header.Get("SOAPAction"), gotBody)
		}
		if exchange.location != "" {
			w.Header().Set("Location", exchange.location)
		}
		if exchange.status != 0 {
			w.WriteHeader(exchange.status)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(exchange.body, "__ORIGIN__", "http://"+r.Host)))
	}))
	t.Cleanup(func() {
		server.Close()
		if len(pending) != 0 {
			t.Errorf("%d expected Hosts requests were not made", len(pending))
		}
	})
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertLeaseError(t *testing.T, client *Client, kind string, status int) *Error {
	t.Helper()
	leases, err := client.Leases(t.Context())
	var protocolErr *Error
	if leases != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "leases" || protocolErr.StatusCode != status || protocolErr.FaultCode != "" {
		t.Fatalf("leases=%#v error=%#v", leases, err)
	}
	for _, secret := range []string{"private", client.base.Host, "/upnp/control/hosts"} {
		if strings.Contains(fmt.Sprintf("%#v %s", protocolErr, err), secret) {
			t.Fatalf("unsanitized error: %#v", protocolErr)
		}
	}
	if kind == "unsupported" && !strings.Contains(protocolErr.Message, leasesRemediation) {
		t.Fatalf("missing remediation: %#v", protocolErr)
	}
	return protocolErr
}

func TestLeasesObserveAddressSourceAndRemainder(t *testing.T) {
	client := hostFixtureClient(t, hostScript(hostEntryZeroFixture, hostEntryOneFixture))
	leases, err := client.Leases(t.Context())
	remaining := int64(3600)
	want := []Lease{
		{"sanitized-device", "192.0.2.10", "02:00:00:00:00:10", "Static", nil, "Ethernet", true},
		{"", "192.0.2.20", "02:00:00:00:00:20", "DHCP", &remaining, "802.11", false},
	}
	if err != nil || !reflect.DeepEqual(leases, want) {
		t.Fatalf("leases=%#v want=%#v error=%v", leases, want, err)
	}
	encoded, err := json.Marshal(leases[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:20","address_source":"DHCP","lease_time_remaining":3600,"interface_type":"802.11","active":false}` {
		t.Fatalf("lease JSON = %s", encoded)
	}
}

func TestLeasesPreserveOnlyDocumentedAddressSources(t *testing.T) {
	for _, source := range []string{"DHCP", "Static", "", "missing", "AutoIP", "dhcp", "static", "private-value"} {
		t.Run(source, func(t *testing.T) {
			entry := mappingField(hostEntryZeroFixture, "NewAddressSource", source)
			client := hostFixtureClient(t, hostScript(entry))
			leases, err := client.Leases(t.Context())
			want := "unknown"
			if source == "DHCP" || source == "Static" {
				want = source
			}
			if err != nil || len(leases) != 1 || leases[0].AddressSource != want || leases[0].LeaseTimeRemaining == nil || *leases[0].LeaseTimeRemaining != 3600 {
				t.Fatalf("leases=%#v error=%v", leases, err)
			}
		})
	}
}

func TestLeasesValidateLeaseTimeRemaining(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
		valid bool
	}{
		{"missing", 0, true}, {"", 0, true}, {" \n ", 0, true}, {"0", 0, true}, {"-1", 0, true},
		{"2147483647", 0, true}, {"4294967295", 0, true}, {" 4294967295 ", 0, true},
		{"1", 1, true}, {"+1", 1, true}, {" 3600 ", 3600, true}, {"2147483646", 2147483646, true},
		{"-2", 0, false}, {"-2147483648", 0, false}, {"-2147483649", 0, false},
		{"1.5", 0, false}, {"private-value", 0, false}, {"2147483648", 0, false},
		{"4294967294", 0, false}, {"4294967296", 0, false}, {"18446744073709551616", 0, false},
	} {
		t.Run(test.value, func(t *testing.T) {
			entry := mappingField(hostEntryZeroFixture, "NewLeaseTimeRemaining", test.value)
			client := hostFixtureClient(t, hostScript(hostEntryOneFixture, entry))
			if !test.valid {
				_ = assertLeaseError(t, client, "protocol", 0)
				return
			}
			leases, err := client.Leases(t.Context())
			if err != nil || len(leases) != 2 {
				t.Fatalf("leases=%#v error=%v", leases, err)
			}
			got := leases[1].LeaseTimeRemaining
			if test.want == 0 {
				if got != nil {
					t.Fatalf("remaining=%d, want nil", *got)
				}
				encoded, err := json.Marshal(leases[1])
				if err != nil || strings.Contains(string(encoded), "lease_time_remaining") {
					t.Fatalf("JSON=%s error=%v", encoded, err)
				}
			} else if got == nil || *got != test.want {
				t.Fatalf("remaining=%v, want %d", got, test.want)
			}
		})
	}
}

func TestLeasesLateErrorsAreAtomicAndSanitized(t *testing.T) {
	for _, test := range []struct {
		name, body, kind string
		status           int
	}{
		{"auth", "private-value", "auth", http.StatusUnauthorized},
		{"fault", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusInternalServerError},
		{"fault-success-status", "<Fault><errorCode>private-code</errorCode><errorDescription>private-value</errorDescription></Fault>", "router", http.StatusOK},
		{"unsupported", "<Fault><errorCode>401</errorCode><errorDescription>private-value</errorDescription></Fault>", "unsupported", http.StatusInternalServerError},
		{"unsupported-success-status", "<Fault><errorCode>401</errorCode><errorDescription>private-value</errorDescription></Fault>", "unsupported", http.StatusOK},
		{"invalid-xml", "<private-value", "protocol", http.StatusOK},
		{"invalid-xml-error-status", "<private-value", "protocol", http.StatusBadGateway},
	} {
		for _, position := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s/%d", test.name, position), func(t *testing.T) {
				script := hostScript(hostEntryZeroFixture, hostEntryOneFixture)[:position+1]
				script[position].body, script[position].status = test.body, test.status
				_ = assertLeaseError(t, hostFixtureClient(t, script), test.kind, test.status)
			})
		}
	}
}

func TestLeasesMissingHostsCapability(t *testing.T) {
	client := hostFixtureClient(t, []hostExchange{{body: wifiDescriptionFixture}})
	_ = assertLeaseError(t, client, "unsupported", 0)
}

func TestLeasesEmptyTable(t *testing.T) {
	leases, err := hostFixtureClient(t, hostScript()).Leases(t.Context())
	if err != nil || leases == nil || len(leases) != 0 {
		t.Fatalf("leases=%#v error=%v", leases, err)
	}
}

func TestLeasesRejectMalformedHostTable(t *testing.T) {
	for _, value := range []string{"missing", "", "private-value", "-1", "1.5", "4097", "4294967296"} {
		t.Run("count/"+value, func(t *testing.T) {
			script := hostScript()
			script[1].body = mappingField(hostCountFixture, "NewHostNumberOfEntries", value)
			_ = assertLeaseError(t, hostFixtureClient(t, script), "protocol", 0)
		})
	}
	for _, value := range []string{"missing", "", "private-value", "-1", "2"} {
		t.Run("active/"+value, func(t *testing.T) {
			entry := mappingField(hostEntryOneFixture, "NewActive", value)
			_ = assertLeaseError(t, hostFixtureClient(t, hostScript(hostEntryZeroFixture, entry)), "protocol", 0)
		})
	}
}

func TestLeasesSortFullTuple(t *testing.T) {
	two, ten := int64(2), int64(10)
	want := []Lease{
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "DHCP", nil, "802.11", false},
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "DHCP", &two, "802.11", false},
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "DHCP", &ten, "802.11", false},
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "DHCP", nil, "802.11", true},
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "DHCP", nil, "Ethernet", false},
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "Static", nil, "802.11", false},
		{"a", "192.0.2.10", "02:00:00:00:00:AA", "unknown", nil, "802.11", false},
		{"a", "192.0.2.10", "02:00:00:00:00:aa", "DHCP", nil, "802.11", false},
		{"b", "192.0.2.10", "02:00:00:00:00:aa", "DHCP", nil, "802.11", false},
		{"a", "192.0.2.20", "02:00:00:00:00:aa", "DHCP", nil, "802.11", false},
		{"a", "192.0.2.10", "02:00:00:00:00:bb", "DHCP", nil, "802.11", false},
	}
	for _, shift := range []int{0, 3, 7} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			entries := make([]string, len(want))
			for i := range entries {
				lease := want[(len(want)-1-i+shift)%len(want)]
				body := hostEntryZeroFixture
				for tag, value := range map[string]string{
					"NewHostName": lease.Name, "NewIPAddress": lease.IPAddress, "NewMACAddress": lease.MACAddress,
					"NewAddressSource": lease.AddressSource, "NewInterfaceType": lease.InterfaceType, "NewActive": fmt.Sprint(lease.Active),
				} {
					body = mappingField(body, tag, value)
				}
				remaining := "missing"
				if lease.LeaseTimeRemaining != nil {
					remaining = fmt.Sprint(*lease.LeaseTimeRemaining)
				}
				entries[i] = mappingField(body, "NewLeaseTimeRemaining", remaining)
			}
			leases, err := hostFixtureClient(t, hostScript(entries...)).Leases(t.Context())
			if err != nil || !reflect.DeepEqual(leases, want) {
				t.Fatalf("leases=%#v want=%#v error=%v", leases, want, err)
			}
		})
	}
}

func TestDevicesPreserveRequestsAndIgnoreLeaseMetadata(t *testing.T) {
	entry := mappingField(hostEntryZeroFixture, "NewLeaseTimeRemaining", "private-value")
	entry = mappingField(entry, "NewAddressSource", "private-value")
	client := hostFixtureClient(t, hostScript(entry, hostEntryOneFixture))
	devices, err := client.Devices(t.Context())
	want := []Device{
		{"sanitized-device", "192.0.2.10", "02:00:00:00:00:10", "Ethernet", true},
		{"", "192.0.2.20", "02:00:00:00:00:20", "802.11", false},
	}
	if err != nil || !reflect.DeepEqual(devices, want) {
		t.Fatalf("devices=%#v want=%#v error=%v", devices, want, err)
	}
	encoded, err := json.Marshal(devices)
	if err != nil || string(encoded) != `[{"name":"sanitized-device","ip_address":"192.0.2.10","mac_address":"02:00:00:00:00:10","interface_type":"Ethernet","active":true},{"ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:20","interface_type":"802.11","active":false}]` {
		t.Fatalf("devices JSON=%s error=%v", encoded, err)
	}
}

func TestDevicesPreserveOriginalHostFaults(t *testing.T) {
	for _, position := range []int{1, 3} {
		t.Run(fmt.Sprint(position), func(t *testing.T) {
			script := hostScript(hostEntryZeroFixture, hostEntryOneFixture)[:position+1]
			script[position].body = "<Fault><errorCode>401</errorCode><errorDescription>synthetic-original-message</errorDescription></Fault>"
			script[position].status = http.StatusInternalServerError
			devices, err := hostFixtureClient(t, script).Devices(t.Context())
			var protocolErr *Error
			want := Error{Kind: "router", Operation: script[position].action, StatusCode: http.StatusInternalServerError, FaultCode: "401", Message: "synthetic-original-message"}
			if devices != nil || !errors.As(err, &protocolErr) || *protocolErr != want {
				t.Fatalf("devices=%#v error=%#v want=%#v", devices, err, want)
			}
		})
	}
}

func TestLeasesRefuseRedirectsWithoutChangingDevices(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalRequests.Add(1)
		_, _ = w.Write([]byte(descriptionFixture))
	}))
	defer external.Close()
	for _, position := range []int{0, 1, 3} {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
			t.Run(fmt.Sprintf("%d/%d", position, status), func(t *testing.T) {
				script := hostScript(hostEntryZeroFixture, hostEntryOneFixture)[:position+1]
				script[position].status, script[position].location = status, external.URL+"/private-endpoint"
				devicesScript := hostScript(hostEntryZeroFixture, hostEntryOneFixture)
				devicesScript[0].status, devicesScript[0].location = status, external.URL+"/description"
				client := hostFixtureClient(t, append(script, devicesScript...))
				originalHTTP := client.http
				var redirects int
				originalHTTP.CheckRedirect = func(*http.Request, []*http.Request) error {
					redirects++
					return nil
				}
				before := externalRequests.Load()
				_ = assertLeaseError(t, client, "network", 0)
				if externalRequests.Load() != before || redirects != 0 || client.http != originalHTTP {
					t.Fatal("Leases followed a redirect or changed the shared HTTP client")
				}
				devices, err := client.Devices(t.Context())
				if err != nil || len(devices) != 2 || redirects != 1 || externalRequests.Load() != before+1 {
					t.Fatalf("devices=%#v redirects=%d error=%v", devices, redirects, err)
				}
			})
		}
	}
}

func TestLeasesAcceptSameHostsDescriptionsAsDevices(t *testing.T) {
	duplicate := `<service><serviceType>urn:dslforum-org:service:Hosts:1</serviceType><controlURL>/upnp/control/hosts</controlURL></service>`
	description := strings.Replace(descriptionFixture, "</serviceList>", duplicate+"</serviceList>", 1)
	script := append(hostScript(hostEntryZeroFixture), hostScript(hostEntryZeroFixture)...)
	script[0].body, script[3].body = description, description
	client := hostFixtureClient(t, script)
	leases, err := client.Leases(t.Context())
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases=%#v error=%v", leases, err)
	}
	devices, err := client.Devices(t.Context())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices=%#v error=%v", devices, err)
	}
}

type hostFailingTransport struct {
	next      http.RoundTripper
	remaining int
}

func (t *hostFailingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.remaining == 0 {
		return nil, errors.New("private-network-error")
	}
	t.remaining--
	return t.next.RoundTrip(r)
}

func TestLeasesNetworkErrorsAreSanitized(t *testing.T) {
	for _, position := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(position), func(t *testing.T) {
			client := hostFixtureClient(t, hostScript(hostEntryZeroFixture, hostEntryOneFixture)[:position])
			client.http.Transport = &hostFailingTransport{next: client.http.Transport, remaining: position}
			_ = assertLeaseError(t, client, "network", 0)
		})
	}
}

func TestLeasesDiscoveryErrorsAreSanitized(t *testing.T) {
	for _, test := range []struct {
		status, wantStatus int
		kind               string
	}{
		{http.StatusUnauthorized, http.StatusUnauthorized, "auth"},
		{http.StatusServiceUnavailable, http.StatusServiceUnavailable, "router"},
		{http.StatusOK, 0, "protocol"},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			client := hostFixtureClient(t, []hostExchange{{body: "<private-value", status: test.status}})
			_ = assertLeaseError(t, client, test.kind, test.wantStatus)
		})
	}
}

func TestDevicesPreserveOriginalNetworkErrors(t *testing.T) {
	client := hostFixtureClient(t, nil)
	client.http.Transport = wifiFailingTransport{}
	devices, err := client.Devices(t.Context())
	var protocolErr *Error
	if devices != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "network" || protocolErr.Operation != "GET "+descriptionPath || !strings.Contains(protocolErr.Message, "private-network-error") {
		t.Fatalf("devices=%#v error=%#v", devices, err)
	}
}

func TestDoctorReportsLeasesCapabilityAlongsideDevices(t *testing.T) {
	server := fixtureServer(t)
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	report, err := client.Doctor(t.Context())
	if err != nil || report.Capabilities.Leases != (DoctorCheck{State: "advertised"}) {
		t.Fatalf("report=%#v error=%v", report, err)
	}
	encoded, err := json.Marshal(report.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	devicesIndex, leasesIndex := strings.Index(string(encoded), `"devices"`), strings.Index(string(encoded), `"leases"`)
	if leasesIndex < devicesIndex || leasesIndex > strings.Index(string(encoded), `"wifi"`) {
		t.Fatalf("capabilities JSON = %s", encoded)
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

type mutationExchange struct {
	action  string
	body    string
	status  int
	payload string
}

// mutationFixtureClient serves a two-instance WLAN description plus scripted
// GetInfo/SetEnable exchanges. Exchanges are consumed in order per control
// path; the returned channel records every SOAP action that was invoked.
func mutationFixtureClient(t *testing.T, exchanges map[string][]mutationExchange) (*Client, <-chan string) {
	t.Helper()
	requests := make(chan string, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == descriptionPath {
			_, _ = w.Write([]byte(wifiDescriptionFixture))
			return
		}
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		if index := strings.LastIndex(action, "#"); index >= 0 {
			action = action[index+1:]
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		select {
		case requests <- r.URL.Path + "#" + action:
		default:
		}
		queue := exchanges[r.URL.Path]
		if len(queue) == 0 {
			t.Errorf("unexpected request %s#%s", r.URL.Path, action)
			http.Error(w, "forbidden action", http.StatusBadRequest)
			return
		}
		next := queue[0]
		exchanges[r.URL.Path] = queue[1:]
		if next.action != action {
			t.Errorf("expected action %s on %s, got %s", next.action, r.URL.Path, action)
			http.Error(w, "unexpected action", http.StatusBadRequest)
			return
		}
		if next.body != "" && !strings.Contains(string(body), next.body) {
			t.Errorf("expected %s request body to contain %s, got %s", next.action, next.body, body)
		}
		w.Header().Set("Content-Type", "text/xml")
		if next.status != 0 {
			w.WriteHeader(next.status)
		}
		_, _ = w.Write([]byte(next.payload))
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, requests
}

func enabledInfo(enable string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetInfoResponse xmlns:u="urn:dslforum-org:service:WLANConfiguration:1"><NewEnable>` + enable + `</NewEnable></u:GetInfoResponse></s:Body></s:Envelope>`
}

func mutationRequestCount(requests <-chan string) int {
	count := 0
	for {
		select {
		case <-requests:
			count++
		default:
			return count
		}
	}
}

const rebootServiceFixture = `<service><serviceType>urn:dslforum-org:service:DeviceConfig:1</serviceType><controlURL>/config</controlURL></service>`
const rebootDescriptionFixture = `<root><device><serviceList>` + rebootServiceFixture + `<service><serviceType>urn:dslforum-org:service:DeviceInfo:1</serviceType><controlURL>/device</controlURL></service></serviceList></device></root>`
const rebootResponseFixture = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:RebootResponse xmlns:u="urn:dslforum-org:service:DeviceConfig:1"/></s:Body></s:Envelope>`
const rebootDigestChallenge = `Digest realm="synthetic", nonce="synthetic-nonce", qop="auth"`

type rebootExchange struct {
	path, action, body, challenge, location string
	status                                  int
	auth, drop                              bool
}

func rebootFixtureClient(t *testing.T, script []rebootExchange, credentials bool) (*Client, *atomic.Int64) {
	t.Helper()
	pending := make(chan rebootExchange, len(script))
	for _, exchange := range script {
		pending <- exchange
	}
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.Header.Get("SOAPAction"), "#Reboot") {
			posts.Add(1)
		}
		var exchange rebootExchange
		select {
		case exchange = <-pending:
		default:
			t.Error("unexpected request after scripted preflight or reboot")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, action := http.MethodGet, ""
		if exchange.action != "" {
			method = http.MethodPost
			serviceType := "urn:dslforum-org:service:DeviceInfo:1"
			if exchange.action == "Reboot" {
				serviceType = "urn:dslforum-org:service:DeviceConfig:1"
			}
			action = `"` + serviceType + "#" + exchange.action + `"`
			body, err := io.ReadAll(r.Body)
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + exchange.action + ` xmlns:u="` + serviceType + `"></u:` + exchange.action + `></s:Body></s:Envelope>`
			if err != nil || string(body) != want {
				t.Error("unexpected SOAP request body")
			}
		}
		if r.Method != method || r.URL.RequestURI() != exchange.path || r.Header.Get("SOAPAction") != action {
			t.Errorf("request=%s %s %s, want=%s %s %s", r.Method, r.URL.RequestURI(), r.Header.Get("SOAPAction"), method, exchange.path, action)
		}
		auth := r.Header.Get("Authorization")
		if exchange.auth {
			params := map[string]string{}
			for _, part := range strings.Split(strings.TrimPrefix(auth, "Digest "), ",") {
				key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
				params[key] = strings.Trim(value, `"`)
			}
			nc := "00000001"
			if exchange.action == "Reboot" {
				nc = "00000002"
			}
			want := md5hex(md5hex("private-user:synthetic:private-password") + ":synthetic-nonce:" + nc + ":" + params["cnonce"] + ":auth:" + md5hex(method+":"+exchange.path))
			if !strings.HasPrefix(auth, "Digest ") || params["uri"] != exchange.path || params["nc"] != nc || params["response"] != want || params["cnonce"] == "" {
				t.Error("invalid digest authorization for request URI")
			}
		} else if auth != "" {
			t.Error("unexpected authorization")
		}
		if exchange.drop {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		if exchange.challenge != "" {
			w.Header().Set("WWW-Authenticate", exchange.challenge)
		}
		if exchange.location != "" {
			w.Header().Set("Location", exchange.location)
		}
		if exchange.status != 0 {
			w.WriteHeader(exchange.status)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(exchange.body, "__ORIGIN__", "http://"+r.Host)))
	}))
	t.Cleanup(func() {
		server.Close()
		if len(pending) != 0 {
			t.Errorf("%d scripted requests were not made", len(pending))
		}
	})
	username, password := "", ""
	if credentials {
		username, password = "private-user", "private-password"
	}
	client, err := New(server.URL, username, password, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, &posts
}

func rebootScript() []rebootExchange {
	return []rebootExchange{{path: descriptionPath, body: rebootDescriptionFixture}, {path: "/config", action: "Reboot", body: rebootResponseFixture}}
}

func assertRebootError(t *testing.T, client *Client, confirm bool, kind string, uncertain bool) *Error {
	t.Helper()
	result, err := client.Reboot(t.Context(), confirm)
	var protocolErr *Error
	if result != (RebootResult{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "reboot" || protocolErr.FaultCode != "" {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
	if uncertain != (protocolErr.Code == "reboot_uncertain") || (uncertain && !strings.Contains(protocolErr.Message, "do not automatically repeat")) {
		t.Fatalf("incorrect uncertainty: %#v", protocolErr)
	}
	for _, secret := range []string{"private", client.base.Host, "/config", "/device", descriptionPath} {
		if strings.Contains(fmt.Sprintf("%#v", protocolErr), secret) {
			t.Fatalf("unsanitized error: %#v", protocolErr)
		}
	}
	return protocolErr
}

func TestRebootPreviewSendsNoSOAP(t *testing.T) {
	for _, credentials := range []bool{false, true} {
		t.Run(fmt.Sprint(credentials), func(t *testing.T) {
			client, posts := rebootFixtureClient(t, rebootScript()[:1], credentials)
			endpoint := client.base.String()
			client.base.Path = "/"
			result, err := client.Reboot(t.Context(), false)
			if err != nil || result != (RebootResult{Endpoint: endpoint, Preview: true}) || posts.Load() != 0 {
				t.Fatalf("result=%#v posts=%d error=%v", result, posts.Load(), err)
			}
		})
	}
}

func TestRebootConfirmSendsExactlyOnePOST(t *testing.T) {
	for _, mode := range []string{"anonymous", "digest", "no-challenge"} {
		t.Run(mode, func(t *testing.T) {
			script := rebootScript()
			switch mode {
			case "digest":
				script[1].auth = true
				script = append(script[:1], append([]rebootExchange{
					{path: "/device", action: "GetInfo", status: http.StatusUnauthorized, challenge: rebootDigestChallenge},
					{path: "/device", action: "GetInfo", auth: true, body: deviceFixture},
				}, script[1:]...)...)
			case "no-challenge":
				script = append(script[:1], append([]rebootExchange{{path: "/device", action: "GetInfo", body: deviceFixture}}, script[1:]...)...)
			}
			client, posts := rebootFixtureClient(t, script, mode != "anonymous")
			result, err := client.Reboot(t.Context(), true)
			if err != nil || result != (RebootResult{Endpoint: client.base.String(), Accepted: true}) || posts.Load() != 1 {
				t.Fatalf("result=%#v posts=%d error=%v", result, posts.Load(), err)
			}
			if client.digestChallenge != nil || client.services != nil || client.http.CheckRedirect != nil {
				t.Fatal("reboot changed shared client state")
			}
		})
	}
}

func TestRebootRejectsMissingDuplicateAndUnsupportedServices(t *testing.T) {
	for _, replacement := range []string{"", rebootServiceFixture + rebootServiceFixture, strings.Replace(rebootServiceFixture, "DeviceConfig:1", "DeviceConfig:2", 1), rebootServiceFixture + strings.Replace(rebootServiceFixture, "DeviceConfig:1", "DeviceConfig:2", 1)} {
		for _, confirm := range []bool{false, true} {
			script := rebootScript()[:1]
			script[0].body = strings.Replace(script[0].body, rebootServiceFixture, replacement, 1)
			client, posts := rebootFixtureClient(t, script, false)
			_ = assertRebootError(t, client, confirm, "unsupported", false)
			if posts.Load() != 0 {
				t.Fatal("invalid services triggered reboot")
			}
		}
	}
}

func TestRebootRejectsUnsafeEndpoints(t *testing.T) {
	for _, suffix := range []string{"/private-path", "?private-query", "?", "#private-fragment", "/%2f"} {
		client, _ := rebootFixtureClient(t, nil, false)
		base, err := client.base.Parse(suffix)
		if err != nil {
			t.Fatal(err)
		}
		client.base = base
		_ = assertRebootError(t, client, true, "usage", false)
	}
	client, _ := rebootFixtureClient(t, nil, false)
	base, err := client.base.Parse("http://private-user:private-password@" + client.base.Host)
	if err != nil {
		t.Fatal(err)
	}
	client.base = base
	_ = assertRebootError(t, client, true, "usage", false)
}

func TestRebootRejectsUnsafeControlURLs(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { externalRequests.Add(1) }))
	defer external.Close()
	for _, path := range []string{"/config", "/device"} {
		for _, address := range []string{"", external.URL + "/private", "//" + strings.TrimPrefix(external.URL, "http://") + "/private", "__ORIGIN__/private?query", "__ORIGIN__/private?", "__ORIGIN__/private#fragment", "http://private-user:private-password@192.0.2.1/private", "%private"} {
			script := rebootScript()[:1]
			script[0].body = strings.Replace(script[0].body, ">"+path+"<", ">"+address+"<", 1)
			client, posts := rebootFixtureClient(t, script, true)
			_ = assertRebootError(t, client, true, "protocol", false)
			if posts.Load() != 0 {
				t.Fatal("unsafe URL triggered reboot")
			}
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("unsafe URL reached external server")
	}
}

func TestRebootAcceptsAbsoluteControlURL(t *testing.T) {
	script := rebootScript()
	script[0].body = strings.Replace(script[0].body, ">/config<", ">__ORIGIN__/config<", 1)
	client, posts := rebootFixtureClient(t, script, false)
	result, err := client.Reboot(t.Context(), true)
	if err != nil || !result.Accepted || posts.Load() != 1 {
		t.Fatalf("result=%#v posts=%d error=%v", result, posts.Load(), err)
	}
}

func TestRebootResponsesNeverRetry(t *testing.T) {
	fault := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>private-fault</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	for _, test := range []struct {
		name, body, kind string
		status           int
		uncertain        bool
	}{
		{"http401", "private-body", "auth", 401, false},
		{"http403", "private-body", "auth", 403, false},
		{"empty", "", "protocol", 200, true},
		{"malformed", "<private-body", "protocol", 200, true},
		{"wrong-action", deviceFixture, "protocol", 200, true},
		{"wrong-namespace", strings.Replace(rebootResponseFixture, "DeviceConfig:1", "DeviceConfig:2", 1), "protocol", 200, true},
		{"wrong-envelope", strings.ReplaceAll(rebootResponseFixture, "http://schemas.xmlsoap.org/soap/envelope/", "private-envelope"), "protocol", 200, true},
		{"bare-response", `<u:RebootResponse xmlns:u="urn:dslforum-org:service:DeviceConfig:1"/>`, "protocol", 200, true},
		{"empty-body", `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body/></s:Envelope>`, "protocol", 200, true},
		{"trailing-root", rebootResponseFixture + `<private/>`, "protocol", 200, true},
		{"trailing-text", rebootResponseFixture + "private", "protocol", 200, true},
		{"output-argument", strings.Replace(rebootResponseFixture, `DeviceConfig:1"/>`, `DeviceConfig:1"><private/></u:RebootResponse>`, 1), "protocol", 200, true},
		{"http500-success-body", rebootResponseFixture, "protocol", 500, true},
		{"invalid-action", fault, "unsupported", 500, false},
		{"invalid-action-http200", fault, "unsupported", 200, false},
		{"fault", strings.Replace(fault, ">401<", ">private-code<", 1), "router", 500, false},
		{"missing-fault-code", strings.Replace(fault, "<errorCode>401</errorCode>", "", 1), "router", 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := rebootScript()
			script[1].body, script[1].status, script[1].challenge = test.body, test.status, rebootDigestChallenge
			script = append(script[:1], append([]rebootExchange{{path: "/device", action: "GetInfo", body: deviceFixture}}, script[1:]...)...)
			client, posts := rebootFixtureClient(t, script, true)
			err := assertRebootError(t, client, true, test.kind, test.uncertain)
			if err.StatusCode != test.status || posts.Load() != 1 {
				t.Fatalf("error=%#v posts=%d", err, posts.Load())
			}
		})
	}
}

func TestRebootDroppedConnectionIsUncertain(t *testing.T) {
	script := rebootScript()
	script[1].drop = true
	client, posts := rebootFixtureClient(t, script, false)
	_ = assertRebootError(t, client, true, "network", true)
	if posts.Load() != 1 {
		t.Fatalf("reboot attempts=%d", posts.Load())
	}
}

func TestRebootRefusesRedirectsAtEveryStage(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { externalRequests.Add(1) }))
	defer external.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		for position := range 3 {
			script := []rebootExchange{{path: descriptionPath, body: rebootDescriptionFixture}, {path: "/device", action: "GetInfo", body: deviceFixture}, {path: "/config", action: "Reboot", body: rebootResponseFixture}}
			script = script[:position+1]
			script[position].status, script[position].location = status, external.URL+"/private"
			client, posts := rebootFixtureClient(t, script, true)
			kind := "network"
			if position == 2 && (status == 307 || status == 308) {
				kind = "protocol"
			}
			_ = assertRebootError(t, client, true, kind, position == 2)
			want := int64(0)
			if position == 2 {
				want = 1
			}
			if posts.Load() != want || client.http.CheckRedirect != nil {
				t.Fatal("redirect changed request count or shared redirect policy")
			}
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("redirect reached external server")
	}
}

func TestRebootPreflightErrorsAreSanitized(t *testing.T) {
	for position := range 2 {
		for _, test := range []struct {
			body, kind string
			status     int
		}{
			{"private", "auth", 401}, {"private", "auth", 403}, {"<private", "protocol", 200},
			{`<Fault><errorCode>private-code</errorCode><errorDescription>private-fault</errorDescription></Fault>`, "router", 500},
		} {
			script := []rebootExchange{{path: descriptionPath, body: rebootDescriptionFixture}, {path: "/device", action: "GetInfo", body: deviceFixture}}
			script = script[:position+1]
			script[position].body, script[position].status = test.body, test.status
			client, posts := rebootFixtureClient(t, script, false)
			if position == 1 {
				client.username, client.password = "private-user", "private-password"
			}
			_ = assertRebootError(t, client, true, test.kind, false)
			if posts.Load() != 0 {
				t.Fatal("failed preflight sent reboot")
			}
		}
	}
	client, _ := rebootFixtureClient(t, nil, false)
	client.http.Transport = wifiFailingTransport{}
	_ = assertRebootError(t, client, true, "network", false)
}

func TestRebootDigestRejectionNeverRetriesMutation(t *testing.T) {
	script := []rebootExchange{
		{path: descriptionPath, body: rebootDescriptionFixture},
		{path: "/device", action: "GetInfo", status: 401, challenge: rebootDigestChallenge},
		{path: "/device", action: "GetInfo", auth: true, body: deviceFixture},
		{path: "/config", action: "Reboot", auth: true, status: 401, challenge: rebootDigestChallenge},
	}
	client, posts := rebootFixtureClient(t, script, true)
	_ = assertRebootError(t, client, true, "auth", false)
	if posts.Load() != 1 {
		t.Fatalf("reboot attempts=%d", posts.Load())
	}
}

func TestDoctorAdvertisesRebootWithoutExtraReads(t *testing.T) {
	unsupported := DoctorCheck{State: "unsupported", Remediation: rebootRemediation}
	for _, tc := range []struct {
		description string
		want        DoctorCheck
	}{
		{rebootDescriptionFixture, DoctorCheck{State: "advertised"}},
		{strings.Replace(rebootDescriptionFixture, rebootServiceFixture, "", 1), unsupported},
		{strings.Replace(rebootDescriptionFixture, "DeviceConfig:1", "DeviceConfig:2", 1), unsupported},
		{strings.Replace(rebootDescriptionFixture, rebootServiceFixture, rebootServiceFixture+rebootServiceFixture, 1), unsupported},
	} {
		description, want := tc.description, tc.want
		client, posts := rebootFixtureClient(t, []rebootExchange{{path: descriptionPath, body: description}, {path: "/device", action: "GetInfo", body: deviceFixture}}, false)
		report, err := client.Doctor(t.Context())
		if err != nil || report.Capabilities.Reboot != want || posts.Load() != 0 {
			t.Fatal("doctor capability discovery violated its read-only contract")
		}
	}
}

func TestWiFiMutationPreviewSendsOnlyGetInfo(t *testing.T) {
	client, requests := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi1": {{action: "GetInfo", payload: enabledInfo("1")}},
	})
	result, err := client.WiFiMutation(t.Context(), 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preview || result.Instance != "urn:WLANConfiguration-com:serviceId:WLANConfiguration1" || !result.Current || result.Intended {
		t.Fatalf("preview = %#v", result)
	}
	if request := <-requests; request != "/wifi1#GetInfo" || mutationRequestCount(requests) != 0 {
		t.Fatal("preview invoked an action other than GetInfo")
	}
}

func TestWiFiMutationIsIdempotentWithoutSetEnable(t *testing.T) {
	client, requests := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi1": {{action: "GetInfo", payload: enabledInfo("1")}},
	})
	result, err := client.WiFiMutation(t.Context(), 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || !result.Previous || !result.Current || result.Instance == "" {
		t.Fatalf("no-change result = %#v", result)
	}
	if request := <-requests; request != "/wifi1#GetInfo" || mutationRequestCount(requests) != 0 {
		t.Fatal("idempotent change still sent SetEnable")
	}
}

func TestWiFiMutationSendsSetEnableAndConfirms(t *testing.T) {
	client, requests := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi1": {
			{action: "GetInfo", payload: enabledInfo("0")},
			{action: "SetEnable", body: "<NewEnable>1</NewEnable>", payload: `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:SetEnableResponse xmlns:u="urn:dslforum-org:service:WLANConfiguration:1"></u:SetEnableResponse></s:Body></s:Envelope>`},
			{action: "GetInfo", payload: enabledInfo("1")},
		},
	})
	result, err := client.WiFiMutation(t.Context(), 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Previous || !result.Current {
		t.Fatalf("result = %#v", result)
	}
	actions := []string{<-requests, <-requests, <-requests}
	if actions[0] != "/wifi1#GetInfo" || actions[1] != "/wifi1#SetEnable" || actions[2] != "/wifi1#GetInfo" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestWiFiMutationRefusesUnconfirmedState(t *testing.T) {
	client, _ := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi1": {
			{action: "GetInfo", payload: enabledInfo("1")},
			{action: "SetEnable", body: "<NewEnable>0</NewEnable>", payload: `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:SetEnableResponse xmlns:u="urn:dslforum-org:service:WLANConfiguration:1"></u:SetEnableResponse></s:Body></s:Envelope>`},
			{action: "GetInfo", payload: enabledInfo("1")},
		},
	})
	result, err := client.WiFiMutation(t.Context(), 1, false, true)
	var protocolErr *Error
	if result.Instance != "" || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || !strings.Contains(protocolErr.Message, "did not confirm") {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
}

func TestWiFiMutationSetEnableFaultIsUnsupported(t *testing.T) {
	client, _ := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi1": {
			{action: "GetInfo", payload: enabledInfo("1")},
			{action: "SetEnable", status: http.StatusInternalServerError, payload: `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><errorCode>401</errorCode><errorDescription>Invalid Action</errorDescription></s:Fault></s:Body></s:Envelope>`},
		},
	})
	result, err := client.WiFiMutation(t.Context(), 1, false, true)
	var protocolErr *Error
	if result.Instance != "" || !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || !strings.Contains(protocolErr.Message, "SetEnable") {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
}

func TestWiFiMutationAuthAndRouterFaultsArePreserved(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   string
	}{
		{http.StatusUnauthorized, "auth"},
		{http.StatusInternalServerError, "router"},
	} {
		client, _ := mutationFixtureClient(t, map[string][]mutationExchange{
			"/wifi1": {{action: "GetInfo", status: test.status, payload: `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><errorCode>501</errorCode><errorDescription>private-fault</errorDescription></s:Fault></s:Body></s:Envelope>`}},
		})
		result, err := client.WiFiMutation(t.Context(), 1, true, true)
		var protocolErr *Error
		if result.Instance != "" || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || strings.Contains(fmt.Sprintf("%#v", err), "private") {
			t.Fatalf("status=%d result=%#v error=%#v", test.status, result, err)
		}
	}
}

func TestWiFiMutationRequiresExplicitInstance(t *testing.T) {
	for _, test := range []struct {
		instance uint64
		code     string
	}{
		{0, "ambiguous_instance"},
		{7, "unknown_instance"},
	} {
		client, requests := mutationFixtureClient(t, map[string][]mutationExchange{})
		result, err := client.WiFiMutation(t.Context(), test.instance, true, false)
		var protocolErr *Error
		if result.Instance != "" || !errors.As(err, &protocolErr) || protocolErr.Kind != "usage" || protocolErr.Code != test.code || mutationRequestCount(requests) != 0 {
			t.Fatalf("instance=%d result=%#v error=%#v", test.instance, result, err)
		}
	}
}

func TestWiFiMutationSelectsExplicitInstance(t *testing.T) {
	client, requests := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi2": {{action: "GetInfo", payload: enabledInfo("0")}},
	})
	result, err := client.WiFiMutation(t.Context(), 2, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance != "urn:WLANConfiguration-com:serviceId:WLANConfiguration2" || result.Intended {
		t.Fatalf("result = %#v", result)
	}
	if request := <-requests; request != "/wifi2#GetInfo" {
		t.Fatalf("request = %s", request)
	}
}

// Regression for observed FRITZ!Box 6591 Cable / FRITZ!OS 8.25 guest-instance
// behavior: SetEnable is accepted without a fault, but the verification read
// still returns the old state. The command must refuse to report success.
func TestWiFiMutationReportsUnappliedSetEnable(t *testing.T) {
	client, _ := mutationFixtureClient(t, map[string][]mutationExchange{
		"/wifi2": {
			{action: "GetInfo", payload: enabledInfo("0")},
			{action: "SetEnable", body: "<NewEnable>1</NewEnable>", payload: `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:SetEnableResponse xmlns:u="urn:dslforum-org:service:WLANConfiguration:1"></u:SetEnableResponse></s:Body></s:Envelope>`},
			{action: "GetInfo", payload: enabledInfo("0")},
		},
	})
	result, err := client.WiFiMutation(t.Context(), 2, true, true)
	var protocolErr *Error
	if result.Instance != "" || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || !strings.Contains(protocolErr.Message, "did not confirm") {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
}

const backupServiceFixture = `<service><serviceType>urn:dslforum-org:service:DeviceConfig:1</serviceType><controlURL>/config</controlURL></service>`
const backupDescriptionFixture = `<root><device><serviceList>` + backupServiceFixture + `</serviceList></device></root>`
const backupDoctorDescriptionFixture = `<root><device><serviceList>` + backupServiceFixture + `<service><serviceType>urn:dslforum-org:service:DeviceInfo:1</serviceType><controlURL>/device</controlURL></service></serviceList></device></root>`
const backupExportPassphrase = "private-export-passphrase"
const backupGetConfigFileAction = `"urn:dslforum-org:service:DeviceConfig:1#X_AVM-DE_GetConfigFile"`
const backupGetInfoAction = `"urn:dslforum-org:service:DeviceInfo:1#GetInfo"`

// backupExchange scripts one expected request of the export flow. Exchanges
// are consumed in order; any unexpected request fails the test.
type backupExchange struct {
	method, path, soapAction, soapBody, body, challenge, location, nc string
	status                                                            int
	drop                                                              bool
}

// backupFixtureClient serves the documented export flow over TLS, because the
// configuration download must be HTTPS. The digest expectations mirror the
// production client: the SOAP action retry uses nonce count 1 and the one-time
// download uses nonce count 2.
func backupFixtureClient(t *testing.T, script []backupExchange, credentials bool) (*Client, *atomic.Int64) {
	t.Helper()
	pending := make(chan backupExchange, len(script))
	for _, exchange := range script {
		pending <- exchange
	}
	var downloads atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every response closes its connection, so a dropped exchange cannot be
		// transparently replayed by the HTTP transport and request counting
		// stays deterministic.
		w.Header().Set("Connection", "close")
		if strings.HasPrefix(r.URL.Path, "/TR064/") && r.Method == http.MethodGet {
			downloads.Add(1)
		}
		var exchange backupExchange
		select {
		case exchange = <-pending:
		default:
			t.Error("unexpected request during the configuration export")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if exchange.method == "" {
			exchange.method = http.MethodGet
		}
		if r.Method != exchange.method || r.URL.RequestURI() != exchange.path || r.Header.Get("SOAPAction") != exchange.soapAction {
			t.Errorf("request=%s %s %s, want=%s %s %s", r.Method, r.URL.RequestURI(), r.Header.Get("SOAPAction"), exchange.method, exchange.path, exchange.soapAction)
		}
		if exchange.soapBody != "" {
			body, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(body), exchange.soapBody) {
				t.Error("the SOAP request body did not carry the documented argument")
			}
		}
		auth := r.Header.Get("Authorization")
		if exchange.nc != "" {
			params := map[string]string{}
			for _, part := range strings.Split(strings.TrimPrefix(auth, "Digest "), ",") {
				key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
				params[key] = strings.Trim(value, `"`)
			}
			want := md5hex(md5hex("private-user:synthetic:private-password") + ":synthetic-nonce:" + exchange.nc + ":" + params["cnonce"] + ":auth:" + md5hex(exchange.method+":"+exchange.path))
			if !strings.HasPrefix(auth, "Digest ") || params["uri"] != exchange.path || params["nc"] != exchange.nc || params["response"] != want || params["cnonce"] == "" {
				t.Error("invalid digest authorization for the export request")
			}
		} else if auth != "" {
			t.Error("unexpected authorization")
		}
		if exchange.drop {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		if exchange.challenge != "" {
			w.Header().Set("WWW-Authenticate", exchange.challenge)
		}
		if exchange.location != "" {
			w.Header().Set("Location", exchange.location)
		}
		if exchange.status != 0 {
			w.WriteHeader(exchange.status)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(exchange.body, "__ORIGIN__", r.Host)))
	}))
	t.Cleanup(func() {
		server.Close()
		if len(pending) != 0 {
			t.Errorf("%d scripted requests were not made", len(pending))
		}
	})
	username, password := "", ""
	if credentials {
		username, password = "private-user", "private-password"
	}
	client, err := New(server.URL, username, password, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, &downloads
}

// backupScript returns the expected request sequence of one successful export.
func backupScript(credentials bool) []backupExchange {
	description := backupExchange{path: descriptionPath, body: backupDescriptionFixture}
	action := backupExchange{
		method: http.MethodPost, path: "/config", soapAction: backupGetConfigFileAction,
		soapBody: "<" + configExportPasswordArg + ">" + backupExportPassphrase + "</" + configExportPasswordArg + ">",
		body:     configFileURLFixture,
	}
	download := backupExchange{path: "/TR064/synthetic-export-token", body: configExportFixture}
	if !credentials {
		return []backupExchange{description, action, download}
	}
	challengeAction := action
	challengeAction.status, challengeAction.body, challengeAction.challenge = http.StatusUnauthorized, "private-challenge-body", rebootDigestChallenge
	challengeDownload := download
	challengeDownload.status, challengeDownload.body, challengeDownload.challenge = http.StatusUnauthorized, "private-challenge-body", rebootDigestChallenge
	action.nc, download.nc = "00000001", "00000002"
	return []backupExchange{description, challengeAction, action, challengeDownload, download}
}

func assertBackupError(t *testing.T, client *Client, kind, code string) *Error {
	t.Helper()
	export, err := client.ConfigExport(t.Context(), backupExportPassphrase)
	var protocolErr *Error
	if export != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "backup" || protocolErr.Code != code {
		t.Fatalf("export=%d error=%#v", len(export), err)
	}
	for _, secret := range []string{"private", backupExportPassphrase, "/TR064/", "/config", client.base.Host, descriptionPath} {
		if strings.Contains(fmt.Sprintf("%#v", protocolErr), secret) {
			t.Fatalf("unsanitized export error: %#v", protocolErr)
		}
	}
	return protocolErr
}

func TestConfigExportDownloadsDocumentedExport(t *testing.T) {
	for _, credentials := range []bool{false, true} {
		t.Run(fmt.Sprint(credentials), func(t *testing.T) {
			client, downloads := backupFixtureClient(t, backupScript(credentials), credentials)
			export, err := client.ConfigExport(t.Context(), backupExportPassphrase)
			wantDownloads := int64(1)
			if credentials {
				wantDownloads = 2
			}
			if err != nil || string(export) != configExportFixture || downloads.Load() != wantDownloads {
				t.Fatalf("err=%v downloads=%d", err, downloads.Load())
			}
			if client.services != nil || client.digestChallenge != nil || client.http.CheckRedirect != nil {
				t.Fatal("the export changed shared client state")
			}
		})
	}
}

func TestConfigExportRequiresExportPassphrase(t *testing.T) {
	client, downloads := backupFixtureClient(t, nil, false)
	_ = assertBackupPassphraseError(t, client)
	if downloads.Load() != 0 {
		t.Fatal("a missing passphrase contacted the router")
	}
}

func assertBackupPassphraseError(t *testing.T, client *Client) *Error {
	t.Helper()
	export, err := client.ConfigExport(t.Context(), "")
	var protocolErr *Error
	if export != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "usage" || protocolErr.Code != "backup_passphrase_missing" {
		t.Fatalf("export=%d error=%#v", len(export), err)
	}
	return protocolErr
}

func TestConfigExportRejectsMissingDuplicateAndUnsupportedServices(t *testing.T) {
	for _, replacement := range []string{"", backupServiceFixture + backupServiceFixture, strings.Replace(backupServiceFixture, "DeviceConfig:1", "DeviceConfig:2", 1), backupServiceFixture + strings.Replace(backupServiceFixture, "DeviceConfig:1", "DeviceConfig:2", 1)} {
		script := backupScript(false)[:1]
		script[0].body = strings.Replace(script[0].body, backupServiceFixture, replacement, 1)
		client, downloads := backupFixtureClient(t, script, false)
		_ = assertBackupError(t, client, "unsupported", "")
		if downloads.Load() != 0 {
			t.Fatal("invalid services triggered an export")
		}
	}
}

func TestConfigExportRefusesUnsafeEndpoints(t *testing.T) {
	for _, suffix := range []string{"/private-path", "?private-query", "?", "#private-fragment", "/%2f"} {
		client, downloads := backupFixtureClient(t, nil, false)
		base, err := client.base.Parse(suffix)
		if err != nil {
			t.Fatal(err)
		}
		client.base = base
		_ = assertBackupError(t, client, "usage", "invalid_configuration")
		if downloads.Load() != 0 {
			t.Fatal("an unsafe endpoint triggered an export")
		}
	}
	client, downloads := backupFixtureClient(t, nil, false)
	base, err := client.base.Parse("http://private-user:private-password@" + client.base.Host)
	if err != nil {
		t.Fatal(err)
	}
	client.base = base
	_ = assertBackupError(t, client, "usage", "invalid_configuration")
	if downloads.Load() != 0 {
		t.Fatal("an unsafe endpoint triggered an export")
	}
}

func TestConfigExportRefusesUnsafeControlURLs(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { externalRequests.Add(1) }))
	defer external.Close()
	for _, address := range []string{"", external.URL + "/private", "//" + strings.TrimPrefix(external.URL, "http://") + "/private", "__ORIGIN__/private?query", "__ORIGIN__/private?", "__ORIGIN__/private#fragment", "http://private-user:private-password@192.0.2.1/private", "%private"} {
		script := backupScript(false)[:1]
		script[0].body = strings.Replace(backupDescriptionFixture, ">/config<", ">"+address+"<", 1)
		client, downloads := backupFixtureClient(t, script, false)
		_ = assertBackupError(t, client, "protocol", "")
		if downloads.Load() != 0 {
			t.Fatal("an unsafe control URL triggered an export")
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("an unsafe control URL reached an external server")
	}
}

func TestConfigExportAcceptsAbsoluteControlURL(t *testing.T) {
	script := backupScript(false)
	script[0].body = strings.Replace(backupDescriptionFixture, ">/config<", ">https://__ORIGIN__/config<", 1)
	client, downloads := backupFixtureClient(t, script, false)
	export, err := client.ConfigExport(t.Context(), backupExportPassphrase)
	if err != nil || string(export) != configExportFixture || downloads.Load() != 1 {
		t.Fatalf("err=%v downloads=%d", err, downloads.Load())
	}
}

func TestConfigExportResponsesAreValidated(t *testing.T) {
	fault := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><errorCode>401</errorCode><errorDescription>private-fault</errorDescription></s:Fault></s:Body></s:Envelope>`
	for _, test := range []struct {
		name, body, kind, code string
		status, statusWant     int
	}{
		{"empty-url", strings.Replace(configFileURLFixture, "https://__ORIGIN__/TR064/synthetic-export-token", "", 1), "protocol", "", 200, 0},
		{"missing-url", `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:X_AVM-DE_GetConfigFileResponse xmlns:u="urn:dslforum-org:service:DeviceConfig:1"/></s:Body></s:Envelope>`, "protocol", "", 200, 200},
		{"malformed", "<private-body", "protocol", "", 200, 200},
		{"wrong-namespace", strings.Replace(configFileURLFixture, "DeviceConfig:1", "DeviceConfig:2", 1), "protocol", "", 200, 200},
		{"trailing-root", configFileURLFixture + `<private/>`, "protocol", "", 200, 200},
		{"http403", "private-body", "auth", "", 403, 403},
		{"invalid-action-fault", fault, "unsupported", "", 500, 500},
		{"invalid-action-fault-http200", fault, "unsupported", "", 200, 200},
		{"router-fault", strings.Replace(fault, ">401<", ">private-code<", 1), "router", "", 500, 500},
		{"missing-fault-code", strings.Replace(fault, "<errorCode>401</errorCode>", "", 1), "router", "", 200, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := backupScript(false)[:1]
			script = append(script, backupExchange{method: http.MethodPost, path: "/config", soapAction: backupGetConfigFileAction, status: test.status, body: test.body})
			client, downloads := backupFixtureClient(t, script, false)
			err := assertBackupError(t, client, test.kind, test.code)
			if err.StatusCode != test.statusWant {
				t.Fatalf("error=%#v", err)
			}
			if downloads.Load() != 0 {
				t.Fatal("an invalid action response still downloaded the export")
			}
		})
	}
}

func TestConfigExportRefusesUnsafeDownloadURLs(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { externalRequests.Add(1) }))
	defer external.Close()
	for _, test := range []struct {
		name, url, kind string
	}{
		{"plaintext", strings.Replace(configFileURLFixture, "https://__ORIGIN__", "http://__ORIGIN__", 1), "protocol"},
		{"off-host", strings.Replace(configFileURLFixture, "__ORIGIN__", "192.0.2.1", 1), "protocol"},
		{"userinfo", strings.Replace(configFileURLFixture, "__ORIGIN__", "private-user:private-password@192.0.2.1", 1), "protocol"},
		{"fragment", strings.Replace(configFileURLFixture, "synthetic-export-token", "synthetic-export-token#private", 1), "protocol"},
		{"credentials-query", strings.Replace(configFileURLFixture, "synthetic-export-token", "synthetic-export-token?private", 1), "protocol"},
		{"no-host", strings.Replace(configFileURLFixture, "https://__ORIGIN__/", "https://", 1), "protocol"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := backupScript(false)[:1]
			script = append(script, backupExchange{method: http.MethodPost, path: "/config", soapAction: backupGetConfigFileAction, body: test.url})
			client, downloads := backupFixtureClient(t, script, false)
			_ = assertBackupError(t, client, test.kind, "")
			if downloads.Load() != 0 {
				t.Fatal("an unsafe download URL was fetched")
			}
		})
	}
	if externalRequests.Load() != 0 {
		t.Fatal("an unsafe download URL reached an external server")
	}
}

func TestConfigExportAuthFailuresNeverDownload(t *testing.T) {
	script := backupScript(true)[:3]
	script[2].status, script[2].body, script[2].challenge = http.StatusUnauthorized, "private-body", rebootDigestChallenge
	client, downloads := backupFixtureClient(t, script, true)
	_ = assertBackupError(t, client, "auth", "")
	if downloads.Load() != 0 {
		t.Fatal("an unauthorized action still downloaded the export")
	}
}

func TestConfigExportDownloadFailuresAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		kind, code string
	}{
		{"auth", http.StatusUnauthorized, "auth", ""},
		{"forbidden", http.StatusForbidden, "auth", ""},
		{"router", http.StatusInternalServerError, "router", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := backupScript(true)
			script[4].status, script[4].body = test.status, "private-body"
			client, downloads := backupFixtureClient(t, script, true)
			err := assertBackupError(t, client, test.kind, test.code)
			if err.StatusCode != test.status {
				t.Fatalf("status=%#v", err)
			}
			if downloads.Load() != 2 {
				t.Fatal("the download did not use the documented one-time URL flow")
			}
		})
	}
}

func TestConfigExportDroppedDownloadNeverRetries(t *testing.T) {
	script := backupScript(false)
	script[2].drop = true
	client, downloads := backupFixtureClient(t, script, false)
	_ = assertBackupError(t, client, "network", "")
	if downloads.Load() != 1 {
		t.Fatalf("download attempts=%d", downloads.Load())
	}
}

func TestConfigExportRefusesRedirectsAtEveryStage(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { externalRequests.Add(1) }))
	defer external.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		for position := range 3 {
			script := backupScript(false)[:position+1]
			script[position].status, script[position].location = status, external.URL+"/private"
			client, downloads := backupFixtureClient(t, script, false)
			_ = assertBackupError(t, client, "network", "")
			want := int64(0)
			if position == 2 {
				want = 1
			}
			if downloads.Load() != want || client.http.CheckRedirect != nil {
				t.Fatal("a redirect changed the request count or the shared redirect policy")
			}
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("a redirect reached an external server")
	}
}

func TestConfigExportRejectsOversizedDownload(t *testing.T) {
	original := maxConfigExportBytes
	maxConfigExportBytes = 8
	t.Cleanup(func() { maxConfigExportBytes = original })
	client, downloads := backupFixtureClient(t, backupScript(false), false)
	_ = assertBackupError(t, client, "protocol", "")
	if downloads.Load() != 1 {
		t.Fatal("an oversized export was downloaded more than once")
	}
}

func TestConfigExportFailsClosedOnUntrustedCertificates(t *testing.T) {
	client, downloads := backupFixtureClient(t, nil, false)
	client.http = &http.Client{Timeout: 10 * time.Second}
	err := assertBackupError(t, client, "network", tlsUntrustedCode)
	if !strings.Contains(err.Message, "certificate") {
		t.Fatalf("error=%#v", err)
	}
	if downloads.Load() != 0 {
		t.Fatal("an untrusted certificate still downloaded the export")
	}
}

func TestDoctorAdvertisesBackupWithoutExporting(t *testing.T) {
	unsupported := DoctorCheck{State: "unsupported", Remediation: configExportRemediation}
	for _, tc := range []struct {
		description string
		want        DoctorCheck
	}{
		{backupDoctorDescriptionFixture, DoctorCheck{State: "advertised"}},
		{strings.Replace(backupDoctorDescriptionFixture, backupServiceFixture, "", 1), unsupported},
		{strings.Replace(backupDoctorDescriptionFixture, "DeviceConfig:1", "DeviceConfig:2", 1), unsupported},
		{strings.Replace(backupDoctorDescriptionFixture, backupServiceFixture, backupServiceFixture+backupServiceFixture, 1), unsupported},
	} {
		script := []backupExchange{
			{path: descriptionPath, body: tc.description},
			{method: http.MethodPost, path: "/device", soapAction: backupGetInfoAction, body: deviceFixture},
		}
		client, downloads := backupFixtureClient(t, script, false)
		report, err := client.Doctor(t.Context())
		if err != nil || report.Capabilities.Backup != tc.want || downloads.Load() != 0 {
			t.Fatalf("report=%#v error=%v", report, err)
		}
	}
}
