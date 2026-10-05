package tr064

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/firmware-description.xml
var firmwareDescriptionFixture string

//go:embed testdata/firmware-scpd.xml
var firmwareSCPDFixture string

//go:embed testdata/firmware-info.xml
var firmwareInfoFixture string

//go:embed testdata/firmware-ext-info.xml
var firmwareExtInfoFixture string

//go:embed testdata/firmware-info-no-update.xml
var firmwareNoUpdateFixture string

//go:embed testdata/firmware-ext-info-unset.xml
var firmwareUnsetFixture string

type firmwareResponse struct {
	body     string
	status   int
	location string
}

func firmwareFixtureClient(t *testing.T, description, scpd string, responses map[string]firmwareResponse) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "GET " + r.URL.RequestURI()
		body := ""
		switch {
		case r.Method == http.MethodGet && r.URL.Path == descriptionPath:
			body = description
		case r.Method == http.MethodGet && r.URL.Path == "/user-interface.xml":
			body = scpd
		case r.Method == http.MethodPost && r.URL.Path == "/user-interface":
			header := strings.Trim(r.Header.Get("SOAPAction"), `"`)
			serviceType, action, ok := strings.Cut(header, "#")
			if !ok || serviceType != firmwareServicePrefix+"1" && serviceType != firmwareServicePrefix+"2" || action != "GetInfo" && action != "X_AVM-DE_GetInfo" {
				t.Errorf("forbidden firmware action: %q", header)
				http.Error(w, "forbidden", http.StatusBadRequest)
				return
			}
			key = r.URL.Path + "#" + action
			request, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + serviceType + `"></u:` + action + `></s:Body></s:Envelope>`
			if string(request) != want {
				t.Errorf("unexpected firmware SOAP request: %s", request)
			}
		default:
			t.Errorf("forbidden firmware request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "forbidden", http.StatusBadRequest)
			return
		}
		requests = append(requests, key)
		response, exists := responses[key]
		if response.body != "" {
			body = response.body
		}
		if r.Method == http.MethodPost && !exists {
			t.Errorf("missing firmware fixture: %s", key)
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

func firmwareResponses(info, extended string) map[string]firmwareResponse {
	return map[string]firmwareResponse{
		"/user-interface#GetInfo":          {body: info},
		"/user-interface#X_AVM-DE_GetInfo": {body: extended},
	}
}

func TestFirmwareDocumentedReads(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("service "+version, func(t *testing.T) {
			description := strings.ReplaceAll(firmwareDescriptionFixture, firmwareServicePrefix+"1", firmwareServicePrefix+version)
			client, requests := firmwareFixtureClient(t, description, firmwareSCPDFixture, firmwareResponses(firmwareInfoFixture, firmwareExtInfoFixture))
			result, err := client.Firmware(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result)
			want := `{"current_version":"8.00","update_available":true,"offered_version":"8.10","update_state":"UpdateAvailable","build_type":"Release","auto_update_mode":"important","update_time":"2026-01-30T03:00:00+01:00","last_version":"7.90","update_successful":"succeeded"}`
			if err != nil || string(encoded) != want {
				t.Fatalf("result=%s err=%v", encoded, err)
			}
			if !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /user-interface.xml", "/user-interface#GetInfo", "/user-interface#X_AVM-DE_GetInfo"}) {
				t.Fatalf("requests=%q", *requests)
			}
		})
	}
}

func TestFirmwareNoUpdateAndUnsetHistory(t *testing.T) {
	client, _ := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, firmwareResponses(firmwareNoUpdateFixture, firmwareUnsetFixture))
	result, err := client.Firmware(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	want := `{"current_version":"8.00","update_available":false,"offered_version":null,"update_state":"NoUpdate","build_type":null,"auto_update_mode":"off","update_time":null,"last_version":null,"update_successful":"unknown"}`
	if err != nil || string(encoded) != want {
		t.Fatalf("result=%s err=%v", encoded, err)
	}
}

func TestFirmwareMissingFieldsAreUnknown(t *testing.T) {
	info := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><GetInfoResponse><NewX_AVM-DE_UpdateState>UpdateAvailable</NewX_AVM-DE_UpdateState><NewX_AVM-DE_Version>8.10</NewX_AVM-DE_Version></GetInfoResponse></s:Body></s:Envelope>`
	client, _ := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, firmwareResponses(info, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><X_AVM-DE_GetInfoResponse/></s:Body></s:Envelope>`))
	result, err := client.Firmware(t.Context())
	if err != nil || result.UpdateAvailable != nil || result.CurrentVersion != nil || result.AutoUpdateMode != nil || result.OfferedVersion == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestFirmwarePreflightBeforeSOAP(t *testing.T) {
	for _, test := range []struct{ name, description, scpd, kind string }{
		{"absent", `<root><device/></root>`, firmwareSCPDFixture, "unsupported"},
		{"ambiguous", strings.Replace(firmwareDescriptionFixture, "</service>", "</service><service><serviceType>"+firmwareServicePrefix+"2</serviceType><controlURL>/second</controlURL><SCPDURL>/second.xml</SCPDURL></service>", 1), firmwareSCPDFixture, "unsupported"},
		{"missing GetInfo", firmwareDescriptionFixture, strings.ReplaceAll(firmwareSCPDFixture, "<action><name>GetInfo</name></action>", ""), "unsupported"},
		{"missing extended", firmwareDescriptionFixture, strings.ReplaceAll(firmwareSCPDFixture, "<action><name>X_AVM-DE_GetInfo</name></action>", ""), "unsupported"},
		{"invalid SCPD", firmwareDescriptionFixture, `<root/>`, "protocol"},
		{"malformed SCPD", firmwareDescriptionFixture, `<scpd>`, "protocol"},
		{"external control", strings.ReplaceAll(firmwareDescriptionFixture, "/user-interface</controlURL>", "https://other.example.test/control</controlURL>"), firmwareSCPDFixture, "protocol"},
		{"external SCPD", strings.ReplaceAll(firmwareDescriptionFixture, "/user-interface.xml", "https://other.example.test/service.xml"), firmwareSCPDFixture, "protocol"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := firmwareFixtureClient(t, test.description, test.scpd, nil)
			_, err := client.Firmware(t.Context())
			var protocolErr *Error
			if !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Operation != "firmware" {
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

func TestFirmwareInvalidFieldsAreSanitized(t *testing.T) {
	for _, test := range []struct {
		old, value string
		extended   bool
	}{
		{"<NewUpgradeAvailable>1</NewUpgradeAvailable>", "<NewUpgradeAvailable>synthetic-secret</NewUpgradeAvailable>", false},
		{"8.10", strings.Repeat("x", 257), false},
		{"8.00", "synthetic\nsecret", true},
		{"2026-01-30T03:00:00+01:00", "synthetic-secret", true},
	} {
		info, extended := firmwareInfoFixture, firmwareExtInfoFixture
		if test.extended {
			extended = strings.ReplaceAll(extended, test.old, test.value)
		} else {
			info = strings.ReplaceAll(info, test.old, test.value)
		}
		client, _ := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, firmwareResponses(info, extended))
		_, err := client.Firmware(t.Context())
		var protocolErr *Error
		if !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "secret") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestFirmwareFailures(t *testing.T) {
	for _, stage := range []string{"GET /user-interface.xml", "/user-interface#GetInfo", "/user-interface#X_AVM-DE_GetInfo"} {
		for _, test := range []struct {
			response firmwareResponse
			kind     string
		}{
			{firmwareResponse{status: 401, body: "synthetic-secret"}, "auth"},
			{firmwareResponse{status: 403, body: "synthetic-secret"}, "auth"},
			{firmwareResponse{status: 500, body: "<error>synthetic-secret</error>"}, "router"},
			{firmwareResponse{body: `<broken>synthetic-secret`}, "protocol"},
			{firmwareResponse{body: `<root>synthetic-secret</root>`}, "protocol"},
			{firmwareResponse{status: 302, location: "https://other.example.test/private", body: "synthetic-secret"}, "network"},
		} {
			responses := firmwareResponses(firmwareInfoFixture, firmwareExtInfoFixture)
			responses[stage] = test.response
			client, requests := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, responses)
			_, err := client.Firmware(t.Context())
			var protocolErr *Error
			if !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.test") {
				t.Fatalf("stage=%s err=%v", stage, err)
			}
			if (*requests)[len(*requests)-1] != stage {
				t.Fatalf("requests continued after failure: %q", *requests)
			}
		}
	}
}

func TestFirmwareInvalidActionIsUnsupported(t *testing.T) {
	for _, stage := range []string{"/user-interface#GetInfo", "/user-interface#X_AVM-DE_GetInfo"} {
		responses := firmwareResponses(firmwareInfoFixture, firmwareExtInfoFixture)
		responses[stage] = firmwareResponse{status: 500, body: `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>synthetic-secret</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`}
		client, _ := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, responses)
		_, err := client.Firmware(t.Context())
		var protocolErr *Error
		if !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || protocolErr.FaultCode != "401" || strings.Contains(err.Error(), "secret") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestFirmwareFutureValuesAndLocalTime(t *testing.T) {
	info := strings.ReplaceAll(strings.ReplaceAll(firmwareInfoFixture, "UpdateAvailable", "FutureState"), "Release", "FutureBuild")
	extended := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(firmwareExtInfoFixture, "important", "future-mode"), "succeeded", "future-result"), "2026-01-30T03:00:00+01:00", "2026-01-30T03:00:00")
	client, _ := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, firmwareResponses(info, extended))
	result, err := client.Firmware(t.Context())
	if err != nil || result.UpdateAvailable == nil || !*result.UpdateAvailable || *result.UpdateState != "FutureState" || *result.BuildType != "FutureBuild" || *result.AutoUpdateMode != "future-mode" || *result.UpdateSuccessful != "future-result" || *result.UpdateTime != "2026-01-30T03:00:00" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoctorFirmwareAdvertisementOnly(t *testing.T) {
	for _, test := range []struct{ description, state string }{{firmwareDescriptionFixture, "advertised"}, {`<root><device><serviceList><service><serviceType>urn:dslforum-org:service:Hosts:1</serviceType><controlURL>/hosts</controlURL></service></serviceList></device></root>`, "unsupported"}} {
		client, requests := firmwareFixtureClient(t, test.description, "invalid SCPD must never be read", nil)
		report, err := client.Doctor(t.Context())
		var protocolErr *Error
		if !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || report.Capabilities.Firmware.State != test.state || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml"}) {
			t.Fatalf("report=%+v err=%v requests=%q", report, err, *requests)
		}
	}
}

func TestFirmwareBooleanVariants(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{{"0", false}, {"false", false}, {"1", true}, {"true", true}} {
		t.Run(test.value, func(t *testing.T) {
			info := strings.ReplaceAll(firmwareInfoFixture, "<NewUpgradeAvailable>1</NewUpgradeAvailable>", "<NewUpgradeAvailable>"+test.value+"</NewUpgradeAvailable>")
			client, _ := firmwareFixtureClient(t, firmwareDescriptionFixture, firmwareSCPDFixture, firmwareResponses(info, firmwareExtInfoFixture))
			result, err := client.Firmware(t.Context())
			if err != nil || result.UpdateAvailable == nil || *result.UpdateAvailable != test.want || *result.OfferedVersion != "8.10" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
