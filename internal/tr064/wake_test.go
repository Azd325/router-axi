package tr064

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

//go:embed testdata/wake-description.xml
var wakeDescriptionFixture string

//go:embed testdata/wake-scpd.xml
var wakeSCPDFixture string

//go:embed testdata/wake-response.xml
var wakeResponseFixture string

const wakeTestMAC = "02:00:00:00:00:AB"

type wakeExchange struct {
	path, action, body, challenge, location string
	status                                  int
	auth, drop                              bool
}

func wakeScript(confirm bool) []wakeExchange {
	script := []wakeExchange{{path: descriptionPath, body: wakeDescriptionFixture}, {path: "/hosts-scpd.xml", body: wakeSCPDFixture}}
	if confirm {
		script = append(script, wakeExchange{path: "/hosts", action: wakeAction, body: wakeResponseFixture})
	}
	return script
}

func wakeFixtureClient(t *testing.T, script []wakeExchange, credentials bool) (*Client, *atomic.Int64) {
	t.Helper()
	pending := make(chan wakeExchange, len(script))
	for _, exchange := range script {
		pending <- exchange
	}
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("SOAPAction"), "#"+wakeAction) {
			sends.Add(1)
		}
		var exchange wakeExchange
		select {
		case exchange = <-pending:
		default:
			t.Error("unexpected request, retry, lookup, or poll")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, soapAction := http.MethodGet, ""
		if exchange.action != "" {
			method = http.MethodPost
			family := hostsPrefix
			arguments := "<NewMACAddress>" + wakeTestMAC + "</NewMACAddress>"
			if exchange.action == "GetInfo" {
				family, arguments = "urn:dslforum-org:service:DeviceInfo:", ""
			}
			soapAction = `"` + family + "1#" + exchange.action + `"`
			body, err := io.ReadAll(r.Body)
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="` + soapNamespace + `" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + exchange.action + ` xmlns:u="` + family + `1">` + arguments + `</u:` + exchange.action + `></s:Body></s:Envelope>`
			if err != nil || string(body) != want {
				t.Error("incorrect documented SOAP arguments")
			}
		}
		if r.Method != method || r.URL.RequestURI() != exchange.path || r.Header.Get("SOAPAction") != soapAction {
			t.Error("incorrect request target or action")
		}
		auth := r.Header.Get("Authorization")
		if exchange.auth {
			params := map[string]string{}
			for _, part := range strings.Split(strings.TrimPrefix(auth, "Digest "), ",") {
				key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
				params[key] = strings.Trim(value, `"`)
			}
			nc := "00000001"
			if exchange.action == wakeAction {
				nc = "00000002"
			}
			want := md5hex(md5hex("private-user:synthetic:private-password") + ":synthetic-nonce:" + nc + ":" + params["cnonce"] + ":auth:" + md5hex(method+":"+exchange.path))
			if !strings.HasPrefix(auth, "Digest ") || params["uri"] != exchange.path || params["nc"] != nc || params["response"] != want || params["cnonce"] == "" {
				t.Error("incorrect Digest authorization")
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
		_, _ = io.WriteString(w, strings.ReplaceAll(exchange.body, "__ORIGIN__", "http://"+r.Host))
	}))
	t.Cleanup(func() {
		server.Close()
		if len(pending) != 0 {
			t.Errorf("%d expected requests missing", len(pending))
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

func assertWakeError(t *testing.T, client *Client, confirm bool, kind string, uncertain bool) {
	t.Helper()
	result, err := client.Wake(t.Context(), wakeTestMAC, confirm)
	var protocolErr *Error
	if result != (WakeResult{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "wake" || protocolErr.FaultCode != "" {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
	if uncertain != (protocolErr.Code == "wake_uncertain") {
		t.Fatalf("incorrect uncertainty: %#v", protocolErr)
	}
	for _, secret := range []string{"private-", client.base.Host, wakeTestMAC, "__ORIGIN__"} {
		if strings.Contains(protocolErr.Error(), secret) {
			t.Fatal("error exposed discarded data")
		}
	}
}

func TestWakeTargetValidationBeforeRequests(t *testing.T) {
	for _, raw := range []string{"", "device-name", "02:00:00:00:00", "02:00:00:00:00:00:00:AB", "0200.0000.00ab", "02:00-00:00:00:AB", "02:00:00:00:00:GG", " 02:00:00:00:00:AB", "02:00:00:00:00:AB ", "02:00:00:00:00:AB,02:00:00:00:00:AC", "ff:ff:ff:ff:ff:ff", "01:00:00:00:00:01", "00:00:00:00:00:00", "<private-input>"} {
		for _, confirm := range []bool{false, true} {
			client, sends := wakeFixtureClient(t, nil, false)
			result, err := client.Wake(t.Context(), raw, confirm)
			var protocolErr *Error
			if result != (WakeResult{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "usage" || sends.Load() != 0 || strings.Contains(protocolErr.Message, "private-input") {
				t.Fatal("invalid MAC was not rejected before request")
			}
		}
	}
}

func TestWakePreviewAndAccepted(t *testing.T) {
	for _, raw := range []string{wakeTestMAC, "02:00:00:00:00:ab", "02-00-00-00-00-ab"} {
		for _, confirm := range []bool{false, true} {
			client, sends := wakeFixtureClient(t, wakeScript(confirm), false)
			result, err := client.Wake(t.Context(), raw, confirm)
			want := WakeResult{Endpoint: client.base.String(), MAC: wakeTestMAC, Preview: !confirm, Accepted: confirm}
			count := int64(0)
			if confirm {
				count = 1
			}
			if err != nil || result != want || sends.Load() != count {
				t.Fatalf("result=%#v sends=%d error=%v", result, sends.Load(), err)
			}
			if client.services != nil || client.digestChallenge != nil || client.http.CheckRedirect != nil {
				t.Fatal("wake modified shared client")
			}
		}
	}
}

func TestWakeDigestPreflight(t *testing.T) {
	for _, stage := range []string{"description", "scpd", "soap", "no-challenge"} {
		t.Run(stage, func(t *testing.T) {
			script := wakeScript(true)
			readIndex := 0
			if stage == "scpd" {
				readIndex = 1
			}
			if stage == "soap" || stage == "no-challenge" {
				script = append(script[:2], wakeExchange{path: "/device-scpd.xml", body: `<scpd><actionList><action><name>GetInfo</name></action></actionList></scpd>`}, wakeExchange{path: "/device", action: "GetInfo", body: deviceFixture}, script[2])
				readIndex = 3
			}
			if stage != "no-challenge" {
				read := script[readIndex]
				challenge, authorized := read, read
				challenge.status, challenge.challenge, challenge.body = 401, rebootDigestChallenge, ""
				authorized.auth = true
				script[len(script)-1].auth = true
				script = append(script[:readIndex], append([]wakeExchange{challenge, authorized}, script[readIndex+1:]...)...)
			}
			client, sends := wakeFixtureClient(t, script, true)
			result, err := client.Wake(t.Context(), wakeTestMAC, true)
			if err != nil || !result.Accepted || sends.Load() != 1 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestWakeRequiresUniqueSupportedService(t *testing.T) {
	for _, description := range []string{`<root/>`, strings.ReplaceAll(wakeDescriptionFixture, "Hosts:1", "Hosts:2"), strings.Replace(wakeDescriptionFixture, "</serviceList>", `<service><serviceType>urn:dslforum-org:service:Hosts:1</serviceType><controlURL>/other</controlURL></service></serviceList>`, 1)} {
		for _, confirm := range []bool{false, true} {
			client, _ := wakeFixtureClient(t, []wakeExchange{{path: descriptionPath, body: description}}, false)
			assertWakeError(t, client, confirm, "unsupported", false)
		}
	}
}

func TestWakeRequiresDocumentedSCPDSignature(t *testing.T) {
	for _, scpd := range []string{`<scpd/>`, strings.ReplaceAll(wakeSCPDFixture, wakeAction, "X_AVM-DE_SetAutoWakeOnLANByMACAddress"), strings.ReplaceAll(wakeSCPDFixture, "NewMACAddress", "NewIPAddress"), strings.ReplaceAll(wakeSCPDFixture, ">in<", ">out<"), strings.ReplaceAll(wakeSCPDFixture, ">MACAddress<", ">IPAddress<"), strings.Replace(wakeSCPDFixture, "</argumentList>", `<argument><name>Other</name><direction>in</direction></argument></argumentList>`, 1), strings.Replace(wakeSCPDFixture, "</actionList>", `<action><name>`+wakeAction+`</name></action></actionList>`, 1)} {
		for _, confirm := range []bool{false, true} {
			script := wakeScript(false)
			script[1].body = scpd
			client, _ := wakeFixtureClient(t, script, false)
			assertWakeError(t, client, confirm, "unsupported", false)
		}
	}
	script := wakeScript(false)
	script[1].body = "<private-invalid"
	client, _ := wakeFixtureClient(t, script, false)
	assertWakeError(t, client, true, "protocol", false)
}

func TestWakeRejectsUnsafeOriginsAndServiceURLs(t *testing.T) {
	for _, suffix := range []string{"private-path", "?private-query", "#private-fragment"} {
		client, _ := wakeFixtureClient(t, nil, false)
		client.base, _ = client.base.Parse("/" + suffix)
		assertWakeError(t, client, true, "usage", false)
	}
	client, _ := wakeFixtureClient(t, nil, false)
	client.base.User = nil
	client.base.ForceQuery = true
	assertWakeError(t, client, true, "usage", false)
	for _, field := range []string{"controlURL", "SCPDURL"} {
		for _, raw := range []string{"", "https://other.test/private-path", "//other.test/private-path", "/private?query", "/private?", "/private#fragment", "http://private-user@other.test/", "%private-invalid"} {
			old := "/hosts"
			if field == "SCPDURL" {
				old = "/hosts-scpd.xml"
			}
			description := strings.Replace(wakeDescriptionFixture, "<"+field+">"+old+"</"+field+">", "<"+field+">"+raw+"</"+field+">", 1)
			client, _ := wakeFixtureClient(t, []wakeExchange{{path: descriptionPath, body: description}}, false)
			kind := "protocol"
			if raw == "" && field == "SCPDURL" {
				kind = "unsupported"
			}
			assertWakeError(t, client, true, kind, false)
		}
	}
}

func TestWakeAcceptsAbsoluteSameOriginURLs(t *testing.T) {
	script := wakeScript(true)
	script[0].body = strings.NewReplacer("/hosts</controlURL>", "__ORIGIN__/hosts</controlURL>", "/hosts-scpd.xml</SCPDURL>", "__ORIGIN__/hosts-scpd.xml</SCPDURL>").Replace(script[0].body)
	client, sends := wakeFixtureClient(t, script, false)
	result, err := client.Wake(t.Context(), wakeTestMAC, true)
	if err != nil || !result.Accepted || sends.Load() != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestWakeResponsesNeverRetryOrPoll(t *testing.T) {
	fault := `<s:Envelope xmlns:s="` + soapNamespace + `"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>private-fault</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	for _, test := range []struct {
		name, body, kind string
		status           int
		uncertain, drop  bool
	}{
		{"http401", "private-auth", "auth", 401, false, false},
		{"http403", "private-auth", "auth", 403, false, false},
		{"empty", "", "protocol", 200, true, false},
		{"malformed", "<private-invalid", "protocol", 200, true, false},
		{"wrong-action", reconnectResponseIPFixture, "protocol", 200, true, false},
		{"wrong-version", strings.ReplaceAll(wakeResponseFixture, "Hosts:1", "Hosts:2"), "protocol", 200, true, false},
		{"extra-output", strings.Replace(wakeResponseFixture, `Hosts:1"/>`, `Hosts:1"><private-output/></u:`+wakeAction+`Response>`, 1), "protocol", 200, true, false},
		{"trailing-root", wakeResponseFixture + "<private-extra/>", "protocol", 200, true, false},
		{"fault", fault, "unsupported", 500, false, false},
		{"denied", strings.ReplaceAll(fault, ">401<", ">606<"), "router", 500, false, false},
		{"no-entry", strings.ReplaceAll(fault, ">401<", ">714<"), "router", 500, false, false},
		{"server-error", wakeResponseFixture, "protocol", 500, true, false},
		{"dropped", "", "network", 0, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := wakeScript(true)
			script[2].status, script[2].body, script[2].drop, script[2].challenge = test.status, test.body, test.drop, rebootDigestChallenge
			client, sends := wakeFixtureClient(t, script, false)
			assertWakeError(t, client, true, test.kind, test.uncertain)
			if sends.Load() != 1 {
				t.Fatal("mutation retried")
			}
		})
	}
}

func TestWakeRefusesRedirectsAtEveryStage(t *testing.T) {
	for _, stage := range []int{0, 1, 2} {
		script := wakeScript(true)[:stage+1]
		script[stage].status, script[stage].location, script[stage].body = 307, "http://other.test/private-path", ""
		client, sends := wakeFixtureClient(t, script, false)
		kind := "protocol"
		if stage < 2 {
			kind = "network"
		}
		assertWakeError(t, client, true, kind, stage == 2)
		want := int64(0)
		if stage == 2 {
			want = 1
		}
		if sends.Load() != want {
			t.Fatal("redirect retried")
		}
	}
}

func TestWakeCanceledPreflightSendsNothing(t *testing.T) {
	client, sends := wakeFixtureClient(t, nil, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := client.Wake(ctx, wakeTestMAC, true)
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Kind != "network" || protocolErr.Code == "wake_uncertain" || sends.Load() != 0 {
		t.Fatal("canceled preflight initiated wake")
	}
}
