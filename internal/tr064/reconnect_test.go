package tr064

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

//go:embed testdata/reconnect-description.xml
var reconnectDescriptionFixture string

//go:embed testdata/reconnect-wan-scpd.xml
var reconnectWANSCPDFixture string

//go:embed testdata/reconnect-default-ip.xml
var reconnectDefaultIPFixture string

//go:embed testdata/reconnect-default-ppp.xml
var reconnectDefaultPPPFixture string

//go:embed testdata/reconnect-response-ip.xml
var reconnectResponseIPFixture string

//go:embed testdata/reconnect-response-ppp.xml
var reconnectResponsePPPFixture string

const (
	reconnectLayer3Type    = "urn:dslforum-org:service:Layer3Forwarding:1"
	reconnectLayer3Control = "/upnp/control/layer3forwarding"
	reconnectLayer3SCPD    = "/layer3forwardingSCPD.xml"
)

type reconnectVariant struct {
	name, serviceType, control, scpd, defaultService, response string
}

var reconnectVariants = []reconnectVariant{
	{"ip", "urn:dslforum-org:service:WANIPConnection:1", "/upnp/control/wanipconnection1", "/wanipconnSCPD.xml", reconnectDefaultIPFixture, reconnectResponseIPFixture},
	{"ppp", "urn:dslforum-org:service:WANPPPConnection:1", "/upnp/control/wanpppconn1", "/wanpppconnSCPD.xml", reconnectDefaultPPPFixture, reconnectResponsePPPFixture},
}

type reconnectExchange struct {
	path, serviceType, action, body, challenge, location string
	status                                               int
	auth, drop                                           bool
}

const (
	reconnectDescriptionStep = iota
	reconnectLayer3SCPDStep
	reconnectDefaultStep
	reconnectWANSCPDStep
	reconnectSendStep
)

func reconnectScript(variant reconnectVariant) []reconnectExchange {
	return []reconnectExchange{
		{path: descriptionPath, body: reconnectDescriptionFixture},
		{path: reconnectLayer3SCPD, body: layer3SCPDFixture},
		{path: reconnectLayer3Control, serviceType: reconnectLayer3Type, action: "GetDefaultConnectionService", body: variant.defaultService},
		{path: variant.scpd, body: reconnectWANSCPDFixture},
		{path: variant.control, serviceType: variant.serviceType, action: "ForceTermination", body: variant.response},
	}
}

func reconnectFixtureClient(t *testing.T, script []reconnectExchange, credentials bool) (*Client, *atomic.Int64) {
	t.Helper()
	pending := make(chan reconnectExchange, len(script))
	for _, exchange := range script {
		pending <- exchange
	}
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction := r.Header.Get("SOAPAction")
		if strings.Contains(soapAction, "#ForceTermination") {
			sends.Add(1)
		}
		if strings.Contains(soapAction, "RequestConnection") {
			t.Error("wan reconnect sent RequestConnection")
		}
		var exchange reconnectExchange
		select {
		case exchange = <-pending:
		default:
			t.Errorf("unexpected request after the scripted exchanges: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, action := http.MethodGet, ""
		if exchange.action != "" {
			method = http.MethodPost
			action = `"` + exchange.serviceType + "#" + exchange.action + `"`
			body, err := io.ReadAll(r.Body)
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + exchange.action + ` xmlns:u="` + exchange.serviceType + `"></u:` + exchange.action + `></s:Body></s:Envelope>`
			if err != nil || string(body) != want {
				t.Error("unexpected SOAP request body")
			}
		}
		if r.Method != method || r.URL.RequestURI() != exchange.path || soapAction != action {
			t.Errorf("request=%s %s %s, want=%s %s %s", r.Method, r.URL.RequestURI(), soapAction, method, exchange.path, action)
		}
		auth := r.Header.Get("Authorization")
		if exchange.auth {
			params := map[string]string{}
			for _, part := range strings.Split(strings.TrimPrefix(auth, "Digest "), ",") {
				key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
				params[key] = strings.Trim(value, `"`)
			}
			nc := "00000001"
			if exchange.action == "ForceTermination" {
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
		_, _ = w.Write([]byte(strings.NewReplacer("__ORIGIN__", "http://"+r.Host, "__HOST__", r.Host).Replace(exchange.body)))
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
	return client, &sends
}

func assertReconnectError(t *testing.T, client *Client, confirm bool, kind string, uncertain bool) *Error {
	t.Helper()
	result, err := client.WANReconnect(t.Context(), confirm)
	var protocolErr *Error
	if result != (WANReconnectResult{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "wan reconnect" || protocolErr.FaultCode != "" {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
	if uncertain != (protocolErr.Code == "wan_reconnect_uncertain") || (uncertain && !strings.Contains(protocolErr.Message, "do not automatically repeat")) {
		t.Fatalf("incorrect uncertainty: %#v", protocolErr)
	}
	for _, secret := range []string{"private", client.base.Host, "/upnp", "SCPD.xml", descriptionPath} {
		if strings.Contains(fmt.Sprintf("%#v", protocolErr), secret) {
			t.Fatalf("unsanitized error: %#v", protocolErr)
		}
	}
	return protocolErr
}

func TestWANReconnectPreviewSendsNoMutation(t *testing.T) {
	for _, variant := range reconnectVariants {
		for _, credentials := range []bool{false, true} {
			t.Run(fmt.Sprint(variant.name, credentials), func(t *testing.T) {
				client, sends := reconnectFixtureClient(t, reconnectScript(variant)[:reconnectSendStep], credentials)
				endpoint := client.base.String()
				client.base.Path = "/"
				result, err := client.WANReconnect(t.Context(), false)
				if err != nil || result != (WANReconnectResult{Endpoint: endpoint, Preview: true}) || sends.Load() != 0 {
					t.Fatalf("result=%#v sends=%d error=%v", result, sends.Load(), err)
				}
			})
		}
	}
}

func TestWANReconnectConfirmSendsExactlyOneForceTermination(t *testing.T) {
	for _, variant := range reconnectVariants {
		for _, mode := range []string{"anonymous", "digest", "no-challenge"} {
			t.Run(variant.name+"-"+mode, func(t *testing.T) {
				script := reconnectScript(variant)
				if mode == "digest" {
					read := script[reconnectDefaultStep]
					challenge, authorized := read, read
					challenge.status, challenge.challenge, challenge.body = 401, rebootDigestChallenge, ""
					authorized.auth = true
					script[reconnectSendStep].auth = true
					script = append(script[:reconnectDefaultStep], append([]reconnectExchange{challenge, authorized}, script[reconnectDefaultStep+1:]...)...)
				}
				client, sends := reconnectFixtureClient(t, script, mode != "anonymous")
				endpoint := client.base.String()
				result, err := client.WANReconnect(t.Context(), true)
				if err != nil || result != (WANReconnectResult{Endpoint: endpoint, Accepted: true}) || sends.Load() != 1 {
					t.Fatalf("result=%#v sends=%d error=%v", result, sends.Load(), err)
				}
				if client.services != nil || client.digestChallenge != nil || client.http.CheckRedirect != nil {
					t.Fatal("wan reconnect changed the shared client")
				}
			})
		}
	}
}

func TestWANReconnectDiscardsCachedDiscovery(t *testing.T) {
	client, sends := reconnectFixtureClient(t, reconnectScript(reconnectVariants[1]), false)
	client.services, client.allServices = map[string]service{}, []service{}
	result, err := client.WANReconnect(t.Context(), true)
	if err != nil || !result.Accepted || sends.Load() != 1 {
		t.Fatalf("result=%#v sends=%d error=%v", result, sends.Load(), err)
	}
}

func TestWANReconnectRefusesAmbiguousAndUnsupportedTargets(t *testing.T) {
	ppp := reconnectVariants[1]
	layer3Service := `<service><serviceType>urn:dslforum-org:service:Layer3Forwarding:1</serviceType><serviceId>urn:Layer3Forwarding-com:serviceId:Layer3Forwarding1</serviceId><controlURL>/upnp/control/layer3forwarding</controlURL><SCPDURL>/layer3forwardingSCPD.xml</SCPDURL></service>`
	pppService := `<service><serviceType>urn:dslforum-org:service:WANPPPConnection:1</serviceType><serviceId>urn:WANPPPConnection-com:serviceId:WANPPPConnection1</serviceId><controlURL>/upnp/control/wanpppconn1</controlURL><SCPDURL>/wanpppconnSCPD.xml</SCPDURL></service>`
	withDefault := func(name string) string {
		return strings.Replace(reconnectDefaultPPPFixture, "1.WANPPPConnection.1", name, 1)
	}
	for _, test := range []struct {
		name  string
		steps int
		edit  func(script []reconnectExchange)
	}{
		{"no-layer3", 1, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, layer3Service, "", 1)
		}},
		{"duplicate-layer3", 1, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, layer3Service, layer3Service+layer3Service, 1)
		}},
		{"layer3-version-2", 1, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, "Layer3Forwarding:1", "Layer3Forwarding:2", 1)
		}},
		{"layer3-without-scpd", 1, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, "<SCPDURL>/layer3forwardingSCPD.xml</SCPDURL>", "", 1)
		}},
		{"layer3-action-not-advertised", 2, func(script []reconnectExchange) {
			script[1].body = strings.Replace(layer3SCPDFixture, "GetDefaultConnectionService", "GetDefaultConnectionServices", 1)
		}},
		{"empty-default", 3, func(script []reconnectExchange) { script[2].body = withDefault(" ") }},
		{"empty-default-without-service-id", 3, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, "<serviceId>urn:WANPPPConnection-com:serviceId:WANPPPConnection1</serviceId>", "", 1)
			script[2].body = withDefault("")
		}},
		{"default-names-no-advertised-service", 3, func(script []reconnectExchange) { script[2].body = withDefault("1.WANPPPConnection.2") }},
		{"default-names-other-service", 3, func(script []reconnectExchange) { script[2].body = withDefault("1.WANDSLLinkConfig.1") }},
		{"no-wan-service", 3, func(script []reconnectExchange) {
			script[0].body = `<root><device><serviceList>` + layer3Service + `</serviceList></device></root>`
		}},
		{"duplicate-wan-service", 3, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, pppService, pppService+pppService, 1)
		}},
		{"wan-version-2", 3, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, "WANPPPConnection:1", "WANPPPConnection:2", 1)
		}},
		{"wan-without-scpd", 3, func(script []reconnectExchange) {
			script[0].body = strings.Replace(reconnectDescriptionFixture, "<SCPDURL>/wanpppconnSCPD.xml</SCPDURL>", "", 1)
		}},
		{"force-termination-not-advertised", 4, func(script []reconnectExchange) {
			script[3].body = strings.Replace(reconnectWANSCPDFixture, "<action><name>ForceTermination</name></action>", "", 1)
		}},
	} {
		for _, confirm := range []bool{false, true} {
			t.Run(fmt.Sprint(test.name, confirm), func(t *testing.T) {
				script := reconnectScript(ppp)[:test.steps]
				test.edit(script)
				client, sends := reconnectFixtureClient(t, script, false)
				_ = assertReconnectError(t, client, confirm, "unsupported", false)
				if sends.Load() != 0 {
					t.Fatal("refused target received ForceTermination")
				}
			})
		}
	}
}

func TestWANReconnectRejectsUnsafeEndpoints(t *testing.T) {
	for _, edit := range []func(*Client){
		func(client *Client) { client.base.User = nil; client.base.RawQuery = "private" },
		func(client *Client) { client.base.Fragment = "private" },
		func(client *Client) { client.base.Path = "/private" },
		func(client *Client) { client.base.ForceQuery = true },
	} {
		for _, confirm := range []bool{false, true} {
			client, sends := reconnectFixtureClient(t, nil, false)
			edit(client)
			result, err := client.WANReconnect(t.Context(), confirm)
			var protocolErr *Error
			if result != (WANReconnectResult{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "usage" || protocolErr.Code != "invalid_configuration" || strings.Contains(protocolErr.Message, "private") || sends.Load() != 0 {
				t.Fatalf("result=%#v error=%#v", result, err)
			}
		}
	}
}

func TestWANReconnectRejectsUnsafeServiceURLs(t *testing.T) {
	for _, test := range []struct {
		name, old, replacement string
		steps                  int
	}{
		{"layer3-control-external", "<controlURL>/upnp/control/layer3forwarding</controlURL>", "<controlURL>http://private.example/control</controlURL>", 1},
		{"layer3-control-query", "<controlURL>/upnp/control/layer3forwarding</controlURL>", "<controlURL>/upnp/control/layer3forwarding?private</controlURL>", 1},
		{"layer3-scpd-external", "<SCPDURL>/layer3forwardingSCPD.xml</SCPDURL>", "<SCPDURL>http://private.example/scpd.xml</SCPDURL>", 1},
		{"wan-control-external", "<controlURL>/upnp/control/wanpppconn1</controlURL>", "<controlURL>http://private.example/control</controlURL>", 3},
		{"wan-control-userinfo", "<controlURL>/upnp/control/wanpppconn1</controlURL>", "<controlURL>http://private:private@__HOST__/upnp/control/wanpppconn1</controlURL>", 3},
		{"wan-control-fragment", "<controlURL>/upnp/control/wanpppconn1</controlURL>", "<controlURL>/upnp/control/wanpppconn1#private</controlURL>", 3},
		{"wan-control-empty", "<controlURL>/upnp/control/wanpppconn1</controlURL>", "", 3},
		{"wan-scpd-external", "<SCPDURL>/wanpppconnSCPD.xml</SCPDURL>", "<SCPDURL>http://private.example/scpd.xml</SCPDURL>", 3},
		{"wan-scpd-query", "<SCPDURL>/wanpppconnSCPD.xml</SCPDURL>", "<SCPDURL>/wanpppconnSCPD.xml?private</SCPDURL>", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(reconnectDescriptionFixture, test.old) {
				t.Fatal("fixture does not contain the replaced element")
			}
			script := reconnectScript(reconnectVariants[1])[:test.steps]
			script[0].body = strings.Replace(reconnectDescriptionFixture, test.old, test.replacement, 1)
			client, sends := reconnectFixtureClient(t, script, false)
			_ = assertReconnectError(t, client, true, "protocol", false)
			if sends.Load() != 0 {
				t.Fatal("unsafe service URL received ForceTermination")
			}
		})
	}
}

func TestWANReconnectAcceptsAbsoluteSameOriginURLs(t *testing.T) {
	script := reconnectScript(reconnectVariants[0])
	script[0].body = strings.NewReplacer(
		"<controlURL>/upnp/control/wanipconnection1</controlURL>", "<controlURL>__ORIGIN__/upnp/control/wanipconnection1</controlURL>",
		"<SCPDURL>/wanipconnSCPD.xml</SCPDURL>", "<SCPDURL>__ORIGIN__/wanipconnSCPD.xml</SCPDURL>",
	).Replace(reconnectDescriptionFixture)
	client, sends := reconnectFixtureClient(t, script, false)
	result, err := client.WANReconnect(t.Context(), true)
	if err != nil || !result.Accepted || sends.Load() != 1 {
		t.Fatalf("result=%#v sends=%d error=%v", result, sends.Load(), err)
	}
}

func TestWANReconnectResponsesNeverRetry(t *testing.T) {
	fault := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>private-fault</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	response := reconnectResponsePPPFixture
	for _, test := range []struct {
		name, body, kind string
		status           int
		uncertain        bool
	}{
		{"http401", "private-body", "auth", 401, false},
		{"http403", "private-body", "auth", 403, false},
		{"empty", "", "protocol", 200, true},
		{"malformed", "<private-body", "protocol", 200, true},
		{"wrong-action", reconnectDefaultPPPFixture, "protocol", 200, true},
		{"other-family", reconnectResponseIPFixture, "protocol", 200, true},
		{"wrong-version", strings.Replace(response, "WANPPPConnection:1", "WANPPPConnection:2", 1), "protocol", 200, true},
		{"wrong-envelope", strings.ReplaceAll(response, "http://schemas.xmlsoap.org/soap/envelope/", "private-envelope"), "protocol", 200, true},
		{"bare-response", `<u:ForceTerminationResponse xmlns:u="urn:dslforum-org:service:WANPPPConnection:1"/>`, "protocol", 200, true},
		{"empty-body", `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body/></s:Envelope>`, "protocol", 200, true},
		{"trailing-root", response + `<private/>`, "protocol", 200, true},
		{"trailing-text", response + "private", "protocol", 200, true},
		{"output-argument", strings.Replace(response, `</u:ForceTerminationResponse>`, `<private/></u:ForceTerminationResponse>`, 1), "protocol", 200, true},
		{"http500-success-body", response, "protocol", 500, true},
		{"invalid-action", fault, "unsupported", 500, false},
		{"invalid-action-http200", fault, "unsupported", 200, false},
		{"disconnect-in-progress", strings.Replace(fault, ">401<", ">707<", 1), "router", 500, false},
		{"fault", strings.Replace(fault, ">401<", ">private-code<", 1), "router", 500, false},
		{"missing-fault-code", strings.Replace(fault, "<errorCode>401</errorCode>", "", 1), "router", 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := reconnectScript(reconnectVariants[1])
			script[reconnectSendStep].body, script[reconnectSendStep].status, script[reconnectSendStep].challenge = test.body, test.status, rebootDigestChallenge
			client, sends := reconnectFixtureClient(t, script, true)
			err := assertReconnectError(t, client, true, test.kind, test.uncertain)
			if err.StatusCode != test.status || sends.Load() != 1 {
				t.Fatalf("error=%#v sends=%d", err, sends.Load())
			}
		})
	}
}

func TestWANReconnectDroppedConnectionIsUncertain(t *testing.T) {
	for _, variant := range reconnectVariants {
		script := reconnectScript(variant)
		script[reconnectSendStep].drop = true
		client, sends := reconnectFixtureClient(t, script, false)
		_ = assertReconnectError(t, client, true, "network", true)
		if sends.Load() != 1 {
			t.Fatalf("ForceTermination attempts=%d", sends.Load())
		}
	}
}

func TestWANReconnectRefusesRedirectsAtEveryStage(t *testing.T) {
	var externalRequests atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { externalRequests.Add(1) }))
	defer external.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		for position := range reconnectSendStep + 1 {
			script := reconnectScript(reconnectVariants[1])[:position+1]
			script[position].status, script[position].location = status, external.URL+"/private"
			client, sends := reconnectFixtureClient(t, script, true)
			sent := position == reconnectSendStep
			kind := "network"
			if sent && (status == 307 || status == 308) {
				kind = "protocol"
			}
			_ = assertReconnectError(t, client, true, kind, sent)
			want := int64(0)
			if sent {
				want = 1
			}
			if sends.Load() != want || client.http.CheckRedirect != nil {
				t.Fatal("redirect changed request count or shared redirect policy")
			}
		}
	}
	if externalRequests.Load() != 0 {
		t.Fatal("redirect reached external server")
	}
}

func TestWANReconnectPreflightErrorsAreSanitized(t *testing.T) {
	for position := range reconnectSendStep {
		for _, test := range []struct {
			body, kind string
			status     int
		}{
			{"private", "auth", 401}, {"private", "auth", 403}, {"<private", "protocol", 200},
			{`<Fault><errorCode>private-code</errorCode><errorDescription>private-fault</errorDescription></Fault>`, "router", 500},
		} {
			script := reconnectScript(reconnectVariants[1])[:position+1]
			script[position].body, script[position].status = test.body, test.status
			client, sends := reconnectFixtureClient(t, script, false)
			_ = assertReconnectError(t, client, true, test.kind, false)
			if sends.Load() != 0 {
				t.Fatal("failed preflight sent ForceTermination")
			}
		}
	}
	client, _ := reconnectFixtureClient(t, nil, false)
	client.http.Transport = wifiFailingTransport{}
	_ = assertReconnectError(t, client, true, "network", false)
}

func TestWANReconnectDigestRejectionNeverRetriesMutation(t *testing.T) {
	ppp := reconnectVariants[1]
	script := reconnectScript(ppp)
	read := script[reconnectDefaultStep]
	challenge, authorized := read, read
	challenge.status, challenge.challenge, challenge.body = 401, rebootDigestChallenge, ""
	authorized.auth = true
	script = []reconnectExchange{
		script[reconnectDescriptionStep], script[reconnectLayer3SCPDStep], challenge, authorized, script[reconnectWANSCPDStep],
		{path: ppp.control, serviceType: ppp.serviceType, action: "ForceTermination", auth: true, status: 401, challenge: rebootDigestChallenge},
	}
	client, sends := reconnectFixtureClient(t, script, true)
	_ = assertReconnectError(t, client, true, "auth", false)
	if sends.Load() != 1 {
		t.Fatalf("ForceTermination attempts=%d", sends.Load())
	}
}
