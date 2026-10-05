package tr064

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/device-detail-description.xml
var deviceDetailDescriptionFixture string

//go:embed testdata/device-detail-scpd.xml
var deviceDetailSCPDFixture string

//go:embed testdata/device-detail-entry.xml
var deviceDetailEntryFixture string

//go:embed testdata/device-detail-entry-legacy.xml
var deviceDetailLegacyFixture string

const deviceDetailStage = "/hosts#X_AVM-DE_GetSpecificHostEntryByIP"

var deviceDetailAddress = netip.MustParseAddr("192.0.2.20")

type deviceDetailResponse struct {
	body     string
	status   int
	location string
}

func deviceDetailFixtureClient(t *testing.T, description, scpd string, responses map[string]deviceDetailResponse) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "GET " + r.URL.RequestURI()
		body := ""
		switch {
		case r.Method == http.MethodGet && r.URL.Path == descriptionPath:
			body = description
		case r.Method == http.MethodGet && r.URL.Path == "/hosts.xml":
			body = scpd
		case r.Method == http.MethodPost && r.URL.Path == "/hosts":
			header := strings.Trim(r.Header.Get("SOAPAction"), `"`)
			serviceType, action, ok := strings.Cut(header, "#")
			if !ok || serviceType != deviceDetailServicePrefix+"1" && serviceType != deviceDetailServicePrefix+"2" || action != deviceDetailAction {
				t.Errorf("forbidden device detail action: %q", header)
				http.Error(w, "forbidden", http.StatusBadRequest)
				return
			}
			key = r.URL.Path + "#" + action
			request, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + serviceType + `"><NewIPAddress>192.0.2.20</NewIPAddress></u:` + action + `></s:Body></s:Envelope>`
			if string(request) != want {
				t.Errorf("unexpected device detail SOAP request: %s", request)
			}
		default:
			t.Errorf("forbidden device detail request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "forbidden", http.StatusBadRequest)
			return
		}
		requests = append(requests, key)
		response, exists := responses[key]
		if response.body != "" {
			body = response.body
		}
		if r.Method == http.MethodPost && !exists {
			t.Errorf("missing device detail fixture: %s", key)
		}
		if response.location != "" {
			w.Header().Set("Location", response.location)
		}
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, &requests
}

func deviceDetailEntry(body string) map[string]deviceDetailResponse {
	return map[string]deviceDetailResponse{deviceDetailStage: {body: body}}
}

func deviceDetailFault(code string) deviceDetailResponse {
	return deviceDetailResponse{status: 500, body: `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>` + code + `</errorCode><errorDescription>synthetic-secret</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`}
}

func TestDeviceDetailDocumentedRead(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("service "+version, func(t *testing.T) {
			description := strings.ReplaceAll(deviceDetailDescriptionFixture, deviceDetailServicePrefix+"1", deviceDetailServicePrefix+version)
			client, requests := deviceDetailFixtureClient(t, description, deviceDetailSCPDFixture, deviceDetailEntry(deviceDetailEntryFixture))
			result, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result)
			want := `{"name":"synthetic-host","ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:20","interface_type":"Ethernet","active":true,"port":2,"speed_mbps":1000,"guest":false,"vpn":false,"wan_access":"granted","update_available":true,"update_successful":"succeeded"}`
			if err != nil || string(encoded) != want {
				t.Fatalf("result=%s err=%v", encoded, err)
			}
			if !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /hosts.xml", deviceDetailStage}) {
				t.Fatalf("requests=%q", *requests)
			}
		})
	}
}

func TestDeviceDetailLegacyEntryReportsUnknown(t *testing.T) {
	client, _ := deviceDetailFixtureClient(t, deviceDetailDescriptionFixture, deviceDetailSCPDFixture, deviceDetailEntry(deviceDetailLegacyFixture))
	result, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	want := `{"ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:30","interface_type":"802.11","active":false,"port":null,"speed_mbps":0,"guest":null,"vpn":null,"wan_access":null,"update_available":false,"update_successful":"unknown"}`
	if err != nil || string(encoded) != want {
		t.Fatalf("result=%s err=%v", encoded, err)
	}
}

func TestDeviceDetailOptionalFieldVariants(t *testing.T) {
	for _, test := range []struct{ old, value, want string }{
		{"<NewX_AVM-DE_Guest>0</NewX_AVM-DE_Guest>", "<NewX_AVM-DE_Guest>true</NewX_AVM-DE_Guest>", `"guest":true`},
		{"<NewX_AVM-DE_VPN>0</NewX_AVM-DE_VPN>", "<NewX_AVM-DE_VPN>1</NewX_AVM-DE_VPN>", `"vpn":true`},
		{"<NewX_AVM-DE_UpdateAvailable>1</NewX_AVM-DE_UpdateAvailable>", "<NewX_AVM-DE_UpdateAvailable>false</NewX_AVM-DE_UpdateAvailable>", `"update_available":false`},
		{"<NewX_AVM-DE_UpdateAvailable>1</NewX_AVM-DE_UpdateAvailable>", "", `"update_available":null`},
		{"<NewX_AVM-DE_Port>2</NewX_AVM-DE_Port>", "", `"port":null`},
		{"<NewX_AVM-DE_Speed>1000</NewX_AVM-DE_Speed>", "", `"speed_mbps":null`},
		{">granted<", ">denied<", `"wan_access":"denied"`},
		{">granted<", ">error<", `"wan_access":"error"`},
		{">granted<", ">unknown<", `"wan_access":"unknown"`},
		{">granted<", ">synthetic-future-state<", `"wan_access":"unknown"`},
		{">succeeded<", ">failed<", `"update_successful":"failed"`},
		{">succeeded<", ">synthetic-future-state<", `"update_successful":"unknown"`},
		{"<NewX_AVM-DE_UpdateSuccessful>succeeded</NewX_AVM-DE_UpdateSuccessful>", "", `"update_successful":null`},
	} {
		entry := strings.ReplaceAll(deviceDetailEntryFixture, test.old, test.value)
		if entry == deviceDetailEntryFixture {
			t.Fatalf("fixture does not contain %q", test.old)
		}
		client, _ := deviceDetailFixtureClient(t, deviceDetailDescriptionFixture, deviceDetailSCPDFixture, deviceDetailEntry(entry))
		result, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
		encoded, _ := json.Marshal(result)
		if err != nil || !strings.Contains(string(encoded), test.want) || strings.Contains(string(encoded), "synthetic-future-state") {
			t.Fatalf("value=%q result=%s err=%v", test.value, encoded, err)
		}
	}
}

func TestDeviceDetailRefusesNonIPv4Target(t *testing.T) {
	for _, address := range []netip.Addr{{}, netip.MustParseAddr("2001:db8::20"), netip.MustParseAddr("::ffff:192.0.2.20")} {
		client, requests := deviceDetailFixtureClient(t, deviceDetailDescriptionFixture, deviceDetailSCPDFixture, nil)
		_, err := client.DeviceDetail(t.Context(), address)
		var protocolErr *Error
		if !errors.As(err, &protocolErr) || protocolErr.Kind != "usage" || protocolErr.Code != "invalid_device_address" || len(*requests) != 0 {
			t.Fatalf("address=%v err=%v requests=%q", address, err, *requests)
		}
	}
}

func TestDeviceDetailPreflightBeforeSOAP(t *testing.T) {
	for _, test := range []struct{ name, description, scpd, kind string }{
		{"absent", `<root><device/></root>`, deviceDetailSCPDFixture, "unsupported"},
		{"ambiguous", strings.Replace(deviceDetailDescriptionFixture, "</service>", "</service><service><serviceType>"+deviceDetailServicePrefix+"2</serviceType><controlURL>/second</controlURL><SCPDURL>/second.xml</SCPDURL></service>", 1), deviceDetailSCPDFixture, "unsupported"},
		{"missing action", deviceDetailDescriptionFixture, strings.ReplaceAll(deviceDetailSCPDFixture, "<action><name>X_AVM-DE_GetSpecificHostEntryByIP</name></action>", ""), "unsupported"},
		{"invalid SCPD", deviceDetailDescriptionFixture, `<root/>`, "protocol"},
		{"malformed SCPD", deviceDetailDescriptionFixture, `<scpd>`, "protocol"},
		{"missing SCPD URL", strings.ReplaceAll(deviceDetailDescriptionFixture, "<SCPDURL>/hosts.xml</SCPDURL>", ""), deviceDetailSCPDFixture, "protocol"},
		{"external control", strings.ReplaceAll(deviceDetailDescriptionFixture, "/hosts</controlURL>", "https://other.example.test/control</controlURL>"), deviceDetailSCPDFixture, "protocol"},
		{"external SCPD", strings.ReplaceAll(deviceDetailDescriptionFixture, "/hosts.xml", "https://other.example.test/service.xml"), deviceDetailSCPDFixture, "protocol"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := deviceDetailFixtureClient(t, test.description, test.scpd, nil)
			_, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
			var protocolErr *Error
			if !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Operation != "devices detail" {
				t.Fatalf("err=%v", err)
			}
			for _, request := range *requests {
				if !strings.HasPrefix(request, "GET ") {
					t.Fatalf("SOAP before preflight: %q", *requests)
				}
			}
		})
	}
}

func TestDeviceDetailInvalidFieldsAreSanitized(t *testing.T) {
	for _, test := range []struct{ old, value string }{
		{"<NewActive>1</NewActive>", "<NewActive>synthetic-secret</NewActive>"},
		{"<NewActive>1</NewActive>", ""},
		{"synthetic-host", "synthetic\nsecret"},
		{"synthetic-host", strings.Repeat("x", 257)},
		{"<NewMACAddress>02:00:00:00:00:20</NewMACAddress>", "<NewMACAddress>synthetic\tsecret</NewMACAddress>"},
		{">Ethernet<", ">" + strings.Repeat("x", 257) + "<"},
		{"<NewX_AVM-DE_Port>2</NewX_AVM-DE_Port>", "<NewX_AVM-DE_Port>synthetic-secret</NewX_AVM-DE_Port>"},
		{"<NewX_AVM-DE_Port>2</NewX_AVM-DE_Port>", "<NewX_AVM-DE_Port>-1</NewX_AVM-DE_Port>"},
		{"<NewX_AVM-DE_Speed>1000</NewX_AVM-DE_Speed>", "<NewX_AVM-DE_Speed>4294967296</NewX_AVM-DE_Speed>"},
		{"<NewX_AVM-DE_Guest>0</NewX_AVM-DE_Guest>", "<NewX_AVM-DE_Guest>synthetic-secret</NewX_AVM-DE_Guest>"},
		{"<NewX_AVM-DE_VPN>0</NewX_AVM-DE_VPN>", "<NewX_AVM-DE_VPN>2</NewX_AVM-DE_VPN>"},
		{"<NewX_AVM-DE_UpdateAvailable>1</NewX_AVM-DE_UpdateAvailable>", "<NewX_AVM-DE_UpdateAvailable>synthetic-secret</NewX_AVM-DE_UpdateAvailable>"},
	} {
		entry := strings.ReplaceAll(deviceDetailEntryFixture, test.old, test.value)
		if entry == deviceDetailEntryFixture {
			t.Fatalf("fixture does not contain %q", test.old)
		}
		client, _ := deviceDetailFixtureClient(t, deviceDetailDescriptionFixture, deviceDetailSCPDFixture, deviceDetailEntry(entry))
		result, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
		var protocolErr *Error
		if !reflect.DeepEqual(result, DeviceDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "secret") {
			t.Fatalf("value=%q result=%+v err=%v", test.value, result, err)
		}
	}
}

func TestDeviceDetailFailures(t *testing.T) {
	for _, stage := range []string{"GET /tr64desc.xml", "GET /hosts.xml", deviceDetailStage} {
		for _, test := range []struct {
			response deviceDetailResponse
			kind     string
		}{
			{deviceDetailResponse{status: 401, body: "synthetic-secret"}, "auth"},
			{deviceDetailResponse{status: 403, body: "synthetic-secret"}, "auth"},
			{deviceDetailResponse{status: 500, body: "<error>synthetic-secret</error>"}, "router"},
			{deviceDetailResponse{body: `<broken>synthetic-secret`}, "protocol"},
			{deviceDetailResponse{status: 302, location: "https://other.example.test/private", body: "synthetic-secret"}, "network"},
			{deviceDetailResponse{status: 302, location: "/followed", body: "synthetic-secret"}, "network"},
		} {
			responses := deviceDetailEntry(deviceDetailEntryFixture)
			responses[stage] = test.response
			client, requests := deviceDetailFixtureClient(t, deviceDetailDescriptionFixture, deviceDetailSCPDFixture, responses)
			_, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
			var protocolErr *Error
			if !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Operation != "devices detail" || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.test") {
				t.Fatalf("stage=%s status=%d err=%v", stage, test.response.status, err)
			}
			if (*requests)[len(*requests)-1] != stage {
				t.Fatalf("requests continued after failure: %q", *requests)
			}
		}
	}
}

func TestDeviceDetailRouterFaults(t *testing.T) {
	for _, test := range []struct{ fault, kind, code, message string }{
		{"401", "unsupported", "", "router does not support the documented device detail read"},
		{"402", "router", "", "router rejected the requested IP address"},
		{"606", "router", "", "the logged-in account needs the App or Phone right"},
		{"714", "usage", "unknown_device", "router has no host entry for the requested IP address"},
		{"820", "router", "", "device detail inspection failed"},
	} {
		responses := map[string]deviceDetailResponse{deviceDetailStage: deviceDetailFault(test.fault)}
		client, _ := deviceDetailFixtureClient(t, deviceDetailDescriptionFixture, deviceDetailSCPDFixture, responses)
		result, err := client.DeviceDetail(t.Context(), deviceDetailAddress)
		var protocolErr *Error
		if !reflect.DeepEqual(result, DeviceDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Code != test.code || protocolErr.FaultCode != test.fault || !strings.Contains(protocolErr.Message, test.message) {
			t.Fatalf("fault=%s result=%+v err=%v", test.fault, result, err)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "192.0.2.20") {
			t.Fatalf("fault=%s leaked router text or the target address: %v", test.fault, err)
		}
	}
}
