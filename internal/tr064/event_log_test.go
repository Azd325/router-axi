package tr064

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/event-log-description.xml
var eventLogDescriptionFixture string

//go:embed testdata/event-log-scpd.xml
var eventLogSCPDFixture string

//go:embed testdata/event-log-path.xml
var eventLogPathFixture string

//go:embed testdata/event-log.xml
var eventLogFixture string

type eventLogResponse struct {
	body     string
	status   int
	location string
	filter   string
}

func eventLogFixtureClient(t *testing.T, overrides map[string]eventLogResponse) (*Client, *[]string) {
	t.Helper()
	return eventLogFixtureClientOn(t, httptest.NewServer, overrides)
}

func eventLogFixtureClientOn(t *testing.T, start func(http.Handler) *httptest.Server, overrides map[string]eventLogResponse) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		requests = append(requests, key)
		var body string
		switch key {
		case "GET /tr64desc.xml":
			body = eventLogDescriptionFixture
		case "GET /device-info.xml":
			body = eventLogSCPDFixture
		case "POST /device-info":
			if r.Header.Get("SOAPAction") != `"`+eventLogServicePrefix+`1#`+eventLogAction+`"` {
				t.Errorf("unexpected SOAP action")
			}
			request, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(request), `></u:`+eventLogAction+`>`) {
				t.Errorf("unexpected SOAP arguments")
			}
			body = eventLogPathFixture
		case "GET /event-log.lua":
			if r.URL.Query().Get("token") != "synthetic-token" || r.URL.Query().Get("filter") == "" {
				t.Errorf("missing download query")
			}
			body = eventLogFixture
		default:
			t.Errorf("unexpected event-log request: %s", key)
		}
		response := overrides[key]
		if response.filter != "" && r.URL.Query().Get("filter") != response.filter {
			t.Errorf("unexpected download group filter: %q", r.URL.Query().Get("filter"))
		}
		if response.body != "" {
			body = response.body
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

func TestEventLogGroupsAndLimits(t *testing.T) {
	for _, test := range []struct {
		selection    string
		limit, total int
		groups       []string
	}{
		{"", 100, 4, []string{"sys", "net", "wlan", "usb"}},
		{"sys,net", 1, 2, []string{"sys", "net"}},
		{"fon", 1000, 1, []string{"fon"}},
		{"sys,net,fon,wlan,usb", 100, 5, []string{"sys", "net", "fon", "wlan", "usb"}},
	} {
		filter := "all"
		if len(test.groups) == 1 {
			filter = test.groups[0]
		}
		client, requests := eventLogFixtureClient(t, map[string]eventLogResponse{"GET /event-log.lua": {filter: filter}})
		result, err := client.EventLog(t.Context(), test.selection, test.limit)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Groups, test.groups) || result.Total != test.total || result.Omitted != test.total-len(result.Lines) || len(result.Lines) > test.limit {
			t.Fatalf("result=%+v", result)
		}
		if test.selection == "" {
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "phone") || strings.Contains(string(encoded), "token") || strings.Contains(string(encoded), "event-log.lua") {
				t.Fatalf("sensitive download or phone output")
			}
		}
		if result.More != "" {
			t.Fatalf("protocol layer set a next step: %+v", result)
		}
		if !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /device-info.xml", "POST /device-info", "GET /event-log.lua"}) {
			t.Fatalf("requests=%q", *requests)
		}
	}
}

func TestEventLogPreflight(t *testing.T) {
	for _, test := range []struct{ stage, body, kind string }{
		{"GET /tr64desc.xml", `<root><device/></root>`, "unsupported"},
		{"GET /tr64desc.xml", strings.Replace(eventLogDescriptionFixture, "</service>", "</service><service><serviceType>"+eventLogServicePrefix+"1</serviceType></service>", 1), "unsupported"},
		{"GET /device-info.xml", `<scpd><actionList><action><name>GetDeviceLog</name></action></actionList></scpd>`, "unsupported"},
		{"GET /device-info.xml", `<wrong/>`, "protocol"},
		{"GET /tr64desc.xml", strings.ReplaceAll(eventLogDescriptionFixture, "/device-info.xml", "https://other.example.test/description.xml"), "protocol"},
	} {
		client, requests := eventLogFixtureClient(t, map[string]eventLogResponse{test.stage: {body: test.body}})
		_, err := client.EventLog(t.Context(), "", 100)
		var failure *Error
		if !errors.As(err, &failure) || failure.Kind != test.kind {
			t.Fatalf("err=%v", err)
		}
		for _, request := range *requests {
			if strings.HasPrefix(request, "POST") {
				t.Fatal("SOAP before preflight")
			}
		}
	}
}

func TestEventLogInvalidInputBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		group string
		limit int
	}{{"unknown", 100}, {"sys,sys", 100}, {"all", 100}, {"", 0}, {"", 1001}} {
		client, requests := eventLogFixtureClient(t, nil)
		_, err := client.EventLog(t.Context(), test.group, test.limit)
		var failure *Error
		if !errors.As(err, &failure) || failure.Kind != "usage" || len(*requests) != 0 {
			t.Fatalf("err=%v requests=%q", err, *requests)
		}
	}
	var failure *Error
	for _, edit := range []func(*Client){
		func(client *Client) { client.base.RawQuery = "token=synthetic" },
		func(client *Client) { client.base.Path = "/private" },
		func(client *Client) { client.base.Fragment = "fragment" },
		func(client *Client) { client.base.User = url.User("sample") },
	} {
		client, err := New("https://router.example.test", "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		edit(client)
		_, err = client.EventLog(t.Context(), "", 100)
		if !errors.As(err, &failure) || failure.Code != "invalid_configuration" {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestEventLogAbsoluteURLAndFilterReplacement(t *testing.T) {
	overrides := map[string]eventLogResponse{}
	client, _ := eventLogFixtureClient(t, overrides)
	path := client.base.String() + "/event-log.lua?token=synthetic-token&amp;filter=fon"
	overrides["POST /device-info"] = eventLogResponse{body: strings.ReplaceAll(eventLogPathFixture, "/event-log.lua?token=synthetic-token", path)}
	overrides["GET /event-log.lua"] = eventLogResponse{filter: "sys"}
	result, err := client.EventLog(t.Context(), "sys", 100)
	if err != nil || result.Total != 1 || result.Lines[0].Group != "sys" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestEventLogSOAPFaults(t *testing.T) {
	for _, test := range []struct{ code, kind string }{{"401", "unsupported"}, {"606", "auth"}, {"501", "router"}} {
		body := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>` + test.code + `</errorCode><errorDescription>synthetic-secret</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
		client, requests := eventLogFixtureClient(t, map[string]eventLogResponse{"POST /device-info": {status: 500, body: body}})
		_, err := client.EventLog(t.Context(), "", 100)
		var failure *Error
		if !errors.As(err, &failure) || failure.Kind != test.kind || failure.FaultCode != test.code || strings.Contains(err.Error(), "secret") || len(*requests) != 3 {
			t.Fatalf("err=%v requests=%q", err, *requests)
		}
	}
}

func TestEventLogDownloadValidation(t *testing.T) {
	for _, start := range []func(http.Handler) *httptest.Server{httptest.NewServer, httptest.NewTLSServer} {
		for _, path := range []string{"http://router.example.test/event-log.lua", "https://other.example.test/event-log.lua", "https://user@router.example.test/event-log.lua", "/event-log.lua#synthetic-secret", "/event-log.lua?bad=%zz", "", "other-scheme", "other-port"} {
			overrides := map[string]eventLogResponse{}
			client, requests := eventLogFixtureClientOn(t, start, overrides)
			switch path {
			case "other-scheme":
				other := *client.base
				other.Scheme = map[string]string{"http": "https", "https": "http"}[other.Scheme]
				path = other.String() + "/event-log.lua"
			case "other-port":
				other := *client.base
				other.Host = other.Hostname() + ":1"
				path = other.String() + "/event-log.lua"
			}
			overrides["POST /device-info"] = eventLogResponse{body: strings.ReplaceAll(eventLogPathFixture, "/event-log.lua?token=synthetic-token", path)}
			_, err := client.EventLog(t.Context(), "", 100)
			var failure *Error
			if !errors.As(err, &failure) || failure.Kind != "protocol" || strings.Contains(err.Error(), "synthetic-secret") || len(*requests) != 3 {
				t.Fatalf("err=%v requests=%q", err, *requests)
			}
		}
	}
}

func TestEventLogOverHTTPS(t *testing.T) {
	client, requests := eventLogFixtureClientOn(t, httptest.NewTLSServer, nil)
	result, err := client.EventLog(t.Context(), "", 100)
	if err != nil || result.Total != 4 || len(*requests) != 4 {
		t.Fatalf("result=%+v err=%v requests=%q", result, err, *requests)
	}
}

func TestEventLogFailuresStopAndSanitize(t *testing.T) {
	for _, stage := range []string{"GET /tr64desc.xml", "GET /device-info.xml", "POST /device-info", "GET /event-log.lua"} {
		for _, test := range []struct {
			response eventLogResponse
			kind     string
		}{
			{eventLogResponse{status: 401, body: "synthetic-secret"}, "auth"},
			{eventLogResponse{status: 403, body: "synthetic-secret"}, "auth"},
			{eventLogResponse{status: 500, body: `<error>synthetic-secret</error>`}, "router"},
			{eventLogResponse{status: 302, location: "https://other.example.test/synthetic-secret"}, "network"},
		} {
			client, requests := eventLogFixtureClient(t, map[string]eventLogResponse{stage: test.response})
			_, err := client.EventLog(t.Context(), "", 100)
			var failure *Error
			if !errors.As(err, &failure) || failure.Kind != test.kind || failure.Operation != "event-log" || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "event-log.lua") {
				t.Fatalf("stage=%s err=%v", stage, err)
			}
			if (*requests)[len(*requests)-1] != stage {
				t.Fatalf("requests continued: %q", *requests)
			}
		}
	}
}

func TestEventLogUntrustedCertificate(t *testing.T) {
	client, requests := eventLogFixtureClientOn(t, httptest.NewTLSServer, nil)
	client.http = &http.Client{}
	_, err := client.EventLog(t.Context(), "", 100)
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != "network" || failure.Code != tlsUntrustedCode || len(*requests) != 0 {
		t.Fatalf("err=%v", err)
	}
}

func TestEventLogXMLVariants(t *testing.T) {
	for _, test := range []struct {
		name, body     string
		total, omitted int
		fails          bool
	}{
		{"empty", `<DeviceLog/>`, 0, 0, false},
		{"multiline", "<DeviceLog><Event><group>sys</group><msg>one\r\ntwo\nthree</msg></Event></DeviceLog>", 3, 2, false},
		{"escaped", `<DeviceLog><Event><group>sys</group><msg>text &amp; &lt;data&gt;</msg></Event></DeviceLog>`, 1, 0, false},
		{"CDATA", `<DeviceLog><Event><group>sys</group><msg><![CDATA[text <sample>]]></msg></Event></DeviceLog>`, 1, 0, false},
		{"missing group", `<DeviceLog><Event><msg>synthetic-secret</msg></Event></DeviceLog>`, 0, 0, true},
		{"unknown group", `<DeviceLog><Event><group>future</group><msg>synthetic-secret</msg></Event></DeviceLog>`, 0, 0, true},
		{"duplicate group", `<DeviceLog><Event><group>sys</group><group>fon</group><msg>synthetic-secret</msg></Event></DeviceLog>`, 0, 0, true},
		{"missing message", `<DeviceLog><Event><group>sys</group></Event></DeviceLog>`, 0, 0, true},
		{"wrong root", `<wrong/>`, 0, 0, true},
		{"malformed", `<DeviceLog>`, 0, 0, true},
		{"trailing root", `<DeviceLog/><DeviceLog/>`, 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := parseEventLog([]byte(test.body), []string{"sys"}, 1)
			if test.fails {
				if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || result.Total != test.total || result.Omitted != test.omitted {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestEventLogBodyBound(t *testing.T) {
	client, _ := eventLogFixtureClient(t, map[string]eventLogResponse{"GET /event-log.lua": {body: strings.Repeat("x", maxEventLogBytes+1)}})
	_, err := client.EventLog(t.Context(), "sys", 100)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "event_log_too_large" {
		t.Fatalf("err=%v", err)
	}
}
