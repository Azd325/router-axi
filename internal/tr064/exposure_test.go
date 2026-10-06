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
	"slices"
	"strings"
	"testing"
)

//go:embed testdata/exposure-description.xml
var exposureDescriptionFixture string

//go:embed testdata/exposure-remote-scpd.xml
var exposureRemoteSCPDFixture string

//go:embed testdata/exposure-info-scpd.xml
var exposureInfoSCPDFixture string

//go:embed testdata/exposure-remote-access.xml
var exposureRemoteAccessFixture string

//go:embed testdata/exposure-ddns.xml
var exposureDDNSFixture string

//go:embed testdata/exposure-myfritz.xml
var exposureMyFRITZFixture string

//go:embed testdata/exposure-storage.xml
var exposureStorageFixture string

//go:embed testdata/exposure-upnp.xml
var exposureUPnPFixture string

//go:embed testdata/exposure-webdav.xml
var exposureWebDAVFixture string

//go:embed testdata/exposure-speedtest.xml
var exposureSpeedtestFixture string

//go:embed testdata/exposure-management-server.xml
var exposureManagementServerFixture string

type exposureResponse struct {
	body, location string
	status         int
}

func exposureResponses() map[string]exposureResponse {
	return map[string]exposureResponse{
		"GET /remote.xml":     {body: exposureRemoteSCPDFixture},
		"/remote#GetInfo":     {body: exposureRemoteAccessFixture},
		"/remote#GetDDNSInfo": {body: exposureDDNSFixture},
		"/myfritz#GetInfo":    {body: exposureMyFRITZFixture},
		"/storage#GetInfo":    {body: exposureStorageFixture},
		"/upnp#GetInfo":       {body: exposureUPnPFixture},
		"/webdav#GetInfo":     {body: exposureWebDAVFixture},
		"/speedtest#GetInfo":  {body: exposureSpeedtestFixture},
		"/mgmsrv#GetInfo":     {body: exposureManagementServerFixture},
	}
}

var exposureAllRequests = []string{
	"GET /tr64desc.xml",
	"GET /remote.xml", "/remote#GetInfo", "/remote#GetDDNSInfo",
	"GET /myfritz.xml", "/myfritz#GetInfo",
	"GET /storage.xml", "/storage#GetInfo",
	"GET /upnp.xml", "/upnp#GetInfo",
	"GET /webdav.xml", "/webdav#GetInfo",
	"GET /speedtest.xml", "/speedtest#GetInfo",
	"GET /mgmsrv.xml", "/mgmsrv#GetInfo",
}

func wantExposure() Exposure {
	enabled, port := true, uint64(2121)
	return Exposure{
		Remote: RemoteExposure{
			RemoteAccess: &RemoteAccessExposure{Enabled: true, Port: 443, LetsEncryptEnabled: &enabled, LetsEncryptState: "valid"},
			DDNS:         &DDNSExposure{Enabled: true, StatusIPv4: "new address", StatusIPv6: "new-address"},
			MyFRITZ:      &MyFRITZExposure{Enabled: true, Port: 8443, DeviceRegistered: true, State: "dyndns_verified"},
		},
		Local: LocalExposure{
			Storage:   &StorageExposure{FTPEnabled: true, FTPStatus: "Enable", FTPWANEnabled: &enabled, FTPWANSSLOnly: &enabled, FTPWANPort: &port},
			UPnP:      &UPnPExposure{Enabled: true},
			WebDAV:    &WebDAVExposure{Enabled: true},
			Speedtest: &SpeedtestExposure{TCPEnabled: true, UDPBidirectEnabled: true, WANUDPEnabled: true, TCPPort: 4711, UDPPort: 4712, UDPBidirectPort: 4713},
			TR069:     &TR069Exposure{PeriodicInformEnabled: true},
		},
	}
}

func exposureFixtureClient(t *testing.T, description string, responses map[string]exposureResponse) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "GET " + r.URL.RequestURI()
		response := exposureResponse{body: exposureInfoSCPDFixture}
		switch {
		case r.URL.Path == descriptionPath:
			response.body = strings.ReplaceAll(description, "__ORIGIN__", "http://"+r.Host)
		case r.Method == http.MethodPost:
			serviceType, action, _ := strings.Cut(strings.Trim(r.Header.Get("SOAPAction"), `"`), "#")
			key = r.URL.Path + "#" + action
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + serviceType + `"></u:` + action + `></s:Body></s:Envelope>`
			if (action != "GetInfo" && key != "/remote#GetDDNSInfo") || string(body) != want {
				t.Errorf("forbidden exposure request: %s %q", key, body)
			}
			response = exposureResponse{}
		case r.Method != http.MethodGet:
			t.Errorf("forbidden exposure request: %s %s", r.Method, r.URL.Path)
		}
		requests = append(requests, key)
		if override, exists := responses[key]; exists {
			response = override
		} else if r.Method == http.MethodPost {
			t.Errorf("missing exposure fixture response for %s", key)
		}
		if response.location != "" {
			w.Header().Set("Location", strings.ReplaceAll(response.location, "__ORIGIN__", "http://"+r.Host))
		}
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		_, _ = io.WriteString(w, response.body)
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, &requests
}

func TestExposureReportsDocumentedFlags(t *testing.T) {
	client, requests := exposureFixtureClient(t, exposureDescriptionFixture, exposureResponses())
	result, err := client.Exposure(t.Context())
	if err != nil || !reflect.DeepEqual(result, wantExposure()) {
		encoded, _ := json.Marshal(result)
		t.Fatalf("result=%s error=%v", encoded, err)
	}
	if !reflect.DeepEqual(*requests, exposureAllRequests) {
		t.Fatalf("requests=%v", *requests)
	}
}

func TestExposureDropsReturnedIdentifiers(t *testing.T) {
	for name, fixture := range map[string]string{"remote access": exposureRemoteAccessFixture, "ddns": exposureDDNSFixture, "myfritz": exposureMyFRITZFixture, "webdav": exposureWebDAVFixture, "management server": exposureManagementServerFixture} {
		if !strings.Contains(fixture, "private-") {
			t.Fatalf("%s fixture carries no identifier", name)
		}
		var values soapValues
		if err := xml.Unmarshal([]byte(fixture), &values); err != nil {
			t.Fatal(err)
		}
		if decoded := fmt.Sprintf("%#v", values); strings.Contains(decoded, "private") {
			t.Fatalf("%s identifier was decoded: %s", name, decoded)
		}
	}
	client, _ := exposureFixtureClient(t, exposureDescriptionFixture, exposureResponses())
	result, err := client.Exposure(t.Context())
	encoded, marshalErr := json.Marshal(result)
	if err != nil || marshalErr != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "example.test") {
		t.Fatalf("result=%s error=%v", encoded, errors.Join(err, marshalErr))
	}
}

func TestExposureReportsUnsupportedPartsSeparately(t *testing.T) {
	invalidAction := exposureResponse{body: `<Fault><errorCode>401</errorCode><errorDescription>private-fault</errorDescription></Fault>`, status: http.StatusInternalServerError}
	without := func(requests []string, removed ...string) []string {
		return slices.DeleteFunc(slices.Clone(requests), func(request string) bool { return slices.Contains(removed, request) })
	}
	removeService := func(name string) string {
		begin := strings.Index(exposureDescriptionFixture, "<service><serviceType>urn:dslforum-org:service:"+name+":")
		end := begin + strings.Index(exposureDescriptionFixture[begin:], "</service>") + len("</service>")
		return exposureDescriptionFixture[:begin] + exposureDescriptionFixture[end:]
	}
	t.Run("service not advertised", func(t *testing.T) {
		client, requests := exposureFixtureClient(t, removeService("X_AVM-DE_Storage"), exposureResponses())
		result, err := client.Exposure(t.Context())
		want := wantExposure()
		want.Local.Storage = nil
		if err != nil || !reflect.DeepEqual(result, want) || !reflect.DeepEqual(*requests, without(exposureAllRequests, "GET /storage.xml", "/storage#GetInfo")) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("action not advertised", func(t *testing.T) {
		responses := exposureResponses()
		responses["GET /remote.xml"] = exposureResponse{body: exposureInfoSCPDFixture}
		responses["GET /mgmsrv.xml"] = exposureResponse{body: `<scpd><actionList><action><name>SetPeriodicInform</name></action></actionList></scpd>`}
		client, requests := exposureFixtureClient(t, exposureDescriptionFixture, responses)
		result, err := client.Exposure(t.Context())
		want := wantExposure()
		want.Remote.DDNS, want.Local.TR069 = nil, nil
		if err != nil || !reflect.DeepEqual(result, want) || !reflect.DeepEqual(*requests, without(exposureAllRequests, "/remote#GetDDNSInfo", "/mgmsrv#GetInfo")) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("invalid action fault", func(t *testing.T) {
		responses := exposureResponses()
		responses["/remote#GetInfo"], responses["/speedtest#GetInfo"] = invalidAction, invalidAction
		client, requests := exposureFixtureClient(t, exposureDescriptionFixture, responses)
		result, err := client.Exposure(t.Context())
		want := wantExposure()
		want.Remote.RemoteAccess, want.Local.Speedtest = nil, nil
		if err != nil || !reflect.DeepEqual(result, want) || !reflect.DeepEqual(*requests, exposureAllRequests) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("duplicate service", func(t *testing.T) {
		begin := strings.Index(exposureDescriptionFixture, "<service><serviceType>urn:dslforum-org:service:X_AVM-DE_UPnP:")
		duplicate := exposureDescriptionFixture[begin : begin+strings.Index(exposureDescriptionFixture[begin:], "</service>")+len("</service>")]
		client, requests := exposureFixtureClient(t, strings.Replace(exposureDescriptionFixture, duplicate, duplicate+duplicate, 1), exposureResponses())
		result, err := client.Exposure(t.Context())
		want := wantExposure()
		want.Local.UPnP = nil
		if err != nil || !reflect.DeepEqual(result, want) || !reflect.DeepEqual(*requests, without(exposureAllRequests, "GET /upnp.xml", "/upnp#GetInfo")) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("one part only", func(t *testing.T) {
		description := exposureDescriptionFixture
		for _, name := range []string{"X_AVM-DE_RemoteAccess", "X_AVM-DE_MyFritz", "X_AVM-DE_Storage", "X_AVM-DE_WebDAVClient", "X_AVM-DE_Speedtest", "ManagementServer"} {
			description = strings.Replace(description, "service:"+name+":1", "service:"+name+"Other:1", 1)
		}
		client, requests := exposureFixtureClient(t, description, exposureResponses())
		result, err := client.Exposure(t.Context())
		if err != nil || !reflect.DeepEqual(result, Exposure{Local: LocalExposure{UPnP: &UPnPExposure{Enabled: true}}}) || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /upnp.xml", "/upnp#GetInfo"}) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	for name, test := range map[string]struct {
		description string
		responses   map[string]exposureResponse
	}{
		"no service":   {description: `<root/>`},
		"no action":    {description: exposureDescriptionFixture, responses: map[string]exposureResponse{"GET /remote.xml": {body: `<scpd/>`}, "GET /myfritz.xml": {body: `<scpd/>`}, "GET /storage.xml": {body: `<scpd/>`}, "GET /upnp.xml": {body: `<scpd/>`}, "GET /webdav.xml": {body: `<scpd/>`}, "GET /speedtest.xml": {body: `<scpd/>`}, "GET /mgmsrv.xml": {body: `<scpd/>`}}},
		"only invalid": {description: exposureDescriptionFixture, responses: map[string]exposureResponse{"GET /remote.xml": {body: exposureRemoteSCPDFixture}, "/remote#GetInfo": invalidAction, "/remote#GetDDNSInfo": invalidAction, "/myfritz#GetInfo": invalidAction, "/storage#GetInfo": invalidAction, "/upnp#GetInfo": invalidAction, "/webdav#GetInfo": invalidAction, "/speedtest#GetInfo": invalidAction, "/mgmsrv#GetInfo": invalidAction}},
	} {
		t.Run("nothing available: "+name, func(t *testing.T) {
			client, _ := exposureFixtureClient(t, test.description, test.responses)
			result, err := client.Exposure(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, Exposure{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || protocolErr.Operation != "exposure" || strings.Contains(fmt.Sprintf("%#v", err), "private") {
				t.Fatalf("result=%#v error=%#v", result, err)
			}
		})
	}
}

func TestExposureLaterDocumentArgumentsAreOptional(t *testing.T) {
	strip := func(fixture string, names ...string) string {
		for _, name := range names {
			begin := strings.Index(fixture, "<New"+name+">")
			end := strings.Index(fixture, "</New"+name+">") + len("</New"+name+">")
			fixture = fixture[:begin] + fixture[end:]
		}
		return fixture
	}
	responses := exposureResponses()
	responses["/remote#GetInfo"] = exposureResponse{body: strip(exposureRemoteAccessFixture, "LetsEncryptEnabled", "LetsEncryptState")}
	responses["/myfritz#GetInfo"] = exposureResponse{body: strip(exposureMyFRITZFixture, "State")}
	responses["/storage#GetInfo"] = exposureResponse{body: strip(exposureStorageFixture, "FTPWANEnable", "FTPWANSSLOnly", "FTPWANPort")}
	client, _ := exposureFixtureClient(t, exposureDescriptionFixture, responses)
	result, err := client.Exposure(t.Context())
	want := wantExposure()
	want.Remote.RemoteAccess = &RemoteAccessExposure{Enabled: true, Port: 443, LetsEncryptState: "unknown"}
	want.Remote.MyFRITZ.State = "unknown"
	want.Local.Storage = &StorageExposure{FTPEnabled: true, FTPStatus: "Enable"}
	if err != nil || !reflect.DeepEqual(result, want) {
		encoded, _ := json.Marshal(result)
		t.Fatalf("result=%s error=%v", encoded, err)
	}
}

func TestExposureValidatesFields(t *testing.T) {
	values := soapValues{Enabled: "1", Enable: "0", Port: "443", DeviceRegistered: "0", FTPEnable: "0", SMBEnable: "1", UPnPMediaServer: "1", EnableTCP: "0", EnableUDP: "0", EnableUDPBidirect: "0", WANEnableTCP: "0", WANEnableUDP: "0", PortTCP: "1", PortUDP: "2", PortUDPBidirect: "3", PeriodicInformEnable: "0", UpgradesManaged: "1"}
	parse := func(v soapValues) error {
		var parser exposureParser
		remoteAccessExposure(v, &parser)
		ddnsExposure(v, &parser)
		myFRITZExposure(v, &parser)
		storageExposure(v, &parser)
		upnpExposure(v, &parser)
		webDAVExposure(v, &parser)
		speedtestExposure(v, &parser)
		tr069Exposure(v, &parser)
		return parser.err
	}
	if err := parse(values); err != nil {
		t.Fatal(err)
	}
	undocumented := values
	undocumented.LetsEncryptState, undocumented.StatusIPv4, undocumented.StatusIPv6, undocumented.MyFritzState, undocumented.FTPStatus = "private-value", "new-address", "new address", "private-value", "enable"
	var parser exposureParser
	if remote, ddns, myfritz, storage := remoteAccessExposure(undocumented, &parser), ddnsExposure(undocumented, &parser), myFRITZExposure(undocumented, &parser), storageExposure(undocumented, &parser); parser.err != nil || remote.LetsEncryptState != "unknown" || ddns.StatusIPv4 != "unknown" || ddns.StatusIPv6 != "unknown" || myfritz.State != "unknown" || storage.FTPStatus != "unknown" {
		t.Fatalf("remote=%#v ddns=%#v myfritz=%#v storage=%#v error=%v", remote, ddns, myfritz, storage, parser.err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*soapValues)
	}{
		{"missing enabled flag", func(v *soapValues) { v.Enabled = "" }},
		{"invalid enable flag", func(v *soapValues) { v.Enable = "2" }},
		{"missing port", func(v *soapValues) { v.Port = "" }},
		{"negative port", func(v *soapValues) { v.Port = "-1" }},
		{"port above ui4", func(v *soapValues) { v.PortUDP = "4294967296" }},
		{"invalid Let's Encrypt flag", func(v *soapValues) { v.LetsEncryptEnabled = "private-value" }},
		{"missing registration flag", func(v *soapValues) { v.DeviceRegistered = "" }},
		{"missing SMB flag", func(v *soapValues) { v.SMBEnable = "" }},
		{"invalid FTP WAN flag", func(v *soapValues) { v.FTPWANSSLOnly = "private-value" }},
		{"FTP WAN port above ui2", func(v *soapValues) { v.FTPWANPort = "65536" }},
		{"missing media server flag", func(v *soapValues) { v.UPnPMediaServer = "" }},
		{"invalid speedtest flag", func(v *soapValues) { v.WANEnableUDP = "private-value" }},
		{"missing speedtest port", func(v *soapValues) { v.PortUDPBidirect = "" }},
		{"missing periodic inform flag", func(v *soapValues) { v.PeriodicInformEnable = "" }},
		{"invalid managed upgrades flag", func(v *soapValues) { v.UpgradesManaged = "private-value" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := values
			test.mutate(&v)
			var protocolErr *Error
			if err := parse(v); !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || protocolErr.Operation != "exposure" || strings.Contains(fmt.Sprintf("%#v", protocolErr), "private-value") {
				t.Fatalf("error=%#v", err)
			}
		})
	}
	t.Run("remote access port above ui2", func(t *testing.T) {
		v := values
		v.Port = "65536"
		var remote, myfritz exposureParser
		remoteAccessExposure(v, &remote)
		if myFRITZExposure(v, &myfritz); remote.err == nil || myfritz.err != nil {
			t.Fatalf("remote=%v myfritz=%v", remote.err, myfritz.err)
		}
	})
}

func TestExposurePreflightFailsBeforeSOAP(t *testing.T) {
	for _, test := range []struct {
		name, description string
		responses         map[string]exposureResponse
	}{
		{name: "off-origin SCPD", description: strings.Replace(exposureDescriptionFixture, "/remote.xml</SCPDURL>", "http://outside.test/scpd.xml</SCPDURL>", 1)},
		{name: "off-origin control URL", description: strings.Replace(exposureDescriptionFixture, "<controlURL>/remote</controlURL>", "<controlURL>http://outside.test/remote</controlURL>", 1)},
		{name: "invalid SCPD", description: exposureDescriptionFixture, responses: map[string]exposureResponse{"GET /remote.xml": {body: `<private>value</private>`}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := exposureFixtureClient(t, test.description, test.responses)
			result, err := client.Exposure(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, Exposure{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || protocolErr.Operation != "exposure" || strings.Contains(fmt.Sprintf("%#v", err), "outside.test") || strings.Contains(fmt.Sprintf("%#v", err), "private") {
				t.Fatalf("result=%#v error=%#v", result, err)
			}
			for _, request := range *requests {
				if strings.Contains(request, "#") {
					t.Fatalf("SOAP request sent after failed preflight: %v", *requests)
				}
			}
		})
	}
}

func TestExposureFailuresAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name, request, body, kind, message string
		status                             int
	}{
		{name: "internal error", request: "/myfritz#GetInfo", body: `<Fault><errorCode>820</errorCode><errorDescription>private-fault</errorDescription></Fault>`, kind: "router", message: "exposure inspection failed", status: http.StatusInternalServerError},
		{name: "invalid XML", request: "/storage#GetInfo", body: `<private-value`, kind: "protocol", message: "exposure inspection failed", status: http.StatusOK},
		{name: "invalid field", request: "/upnp#GetInfo", body: strings.Replace(exposureUPnPFixture, "<NewEnable>1<", "<NewEnable>private-value<", 1), kind: "protocol", message: "router returned an invalid UPnP enabled flag", status: http.StatusOK},
		{name: "unauthorized", request: "/remote#GetDDNSInfo", kind: "auth", message: "exposure inspection failed", status: http.StatusUnauthorized},
		{name: "unauthorized SCPD", request: "GET /webdav.xml", kind: "auth", message: "exposure inspection failed", status: http.StatusUnauthorized},
		{name: "missing SCPD", request: "GET /speedtest.xml", kind: "router", message: "exposure inspection failed", status: http.StatusNotFound},
		{name: "any right denied", request: "/remote#GetInfo", body: `<Fault><errorCode>606</errorCode><errorDescription>private-fault</errorDescription></Fault>`, kind: "router", message: "router denied X_AVM-DE_RemoteAccess:GetInfo; the logged-in account needs the App, Phone, NAS, or Homeauto right", status: http.StatusInternalServerError},
		{name: "App right denied", request: "/storage#GetInfo", body: `<Fault><errorCode>606</errorCode><errorDescription>private-fault</errorDescription></Fault>`, kind: "router", message: "router denied X_AVM-DE_Storage:GetInfo; the logged-in account needs the App right", status: http.StatusInternalServerError},
		{name: "configuration right denied", request: "/mgmsrv#GetInfo", body: `<Fault><errorCode>606</errorCode><errorDescription>private-fault</errorDescription></Fault>`, kind: "router", message: "router denied ManagementServer:GetInfo; the logged-in account needs the configuration right", status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			responses := exposureResponses()
			responses[test.request] = exposureResponse{body: test.body, status: test.status}
			client, _ := exposureFixtureClient(t, exposureDescriptionFixture, responses)
			result, err := client.Exposure(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, Exposure{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Operation != "exposure" || protocolErr.Message != test.message || strings.Contains(fmt.Sprintf("%#v", err), "private") || strings.Contains(fmt.Sprintf("%#v", err), client.base.Host) {
				t.Fatalf("result=%#v error=%#v", result, err)
			}
		})
	}
}

func TestExposureRefusesSCPDRedirect(t *testing.T) {
	responses := exposureResponses()
	responses["GET /remote.xml"] = exposureResponse{status: http.StatusFound, location: "__ORIGIN__/myfritz.xml"}
	client, requests := exposureFixtureClient(t, exposureDescriptionFixture, responses)
	result, err := client.Exposure(t.Context())
	var protocolErr *Error
	if !reflect.DeepEqual(result, Exposure{}) || !errors.As(err, &protocolErr) || protocolErr.Operation != "exposure" || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /remote.xml"}) {
		t.Fatalf("result=%#v error=%#v requests=%v", result, err, *requests)
	}
}
