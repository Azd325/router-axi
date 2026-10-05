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

//go:embed testdata/firmware-check-description.xml
var firmwareCheckDescriptionFixture string

//go:embed testdata/firmware-check-response.xml
var firmwareCheckResponseFixture string

type firmwareCheckExchange struct {
	path, action, body, challenge, location string
	status                                  int
	auth, drop                              bool
}

func firmwareCheckScript(confirm bool) []firmwareCheckExchange {
	script := []firmwareCheckExchange{{path: descriptionPath, body: firmwareCheckDescriptionFixture}, {path: "/user-interface.xml", body: firmwareSCPDFixture}}
	if confirm {
		script = append(script, firmwareCheckExchange{path: "/user-interface", action: firmwareCheckAction, body: firmwareCheckResponseFixture})
	}
	return script
}

func firmwareCheckFixtureClient(t *testing.T, script []firmwareCheckExchange, credentials bool) (*Client, *atomic.Int64) {
	t.Helper()
	pending := make(chan firmwareCheckExchange, len(script))
	for _, exchange := range script {
		pending <- exchange
	}
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("SOAPAction"), "#"+firmwareCheckAction) {
			sends.Add(1)
		}
		var exchange firmwareCheckExchange
		select {
		case exchange = <-pending:
		default:
			t.Error("unexpected request, retry, or poll")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, soapAction := http.MethodGet, ""
		if exchange.action != "" {
			method = http.MethodPost
			family := firmwareServicePrefix
			if exchange.action == "GetInfo" {
				family = "urn:dslforum-org:service:DeviceInfo:"
			}
			soapAction = `"` + family + "1#" + exchange.action + `"`
			body, err := io.ReadAll(r.Body)
			want := `<?xml version="1.0"?><s:Envelope xmlns:s="` + soapNamespace + `" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + exchange.action + ` xmlns:u="` + family + `1"></u:` + exchange.action + `></s:Body></s:Envelope>`
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
			if exchange.action == firmwareCheckAction {
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

func assertFirmwareCheckError(t *testing.T, client *Client, confirm bool, kind string, uncertain bool) {
	t.Helper()
	result, err := client.FirmwareCheck(t.Context(), confirm)
	var protocolErr *Error
	if result != (FirmwareCheckResult{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "firmware check" || protocolErr.FaultCode != "" {
		t.Fatalf("result=%#v error=%#v", result, err)
	}
	if uncertain != (protocolErr.Code == "firmware_check_uncertain") {
		t.Fatalf("incorrect uncertainty: %#v", protocolErr)
	}
	for _, secret := range []string{"private-", client.base.Host, "__ORIGIN__"} {
		if strings.Contains(protocolErr.Error(), secret) {
			t.Fatal("error exposed discarded data")
		}
	}
}

func TestFirmwareCheckPreviewAndAccepted(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		client, sends := firmwareCheckFixtureClient(t, firmwareCheckScript(confirm), false)
		result, err := client.FirmwareCheck(t.Context(), confirm)
		want := FirmwareCheckResult{Endpoint: client.base.String(), Preview: !confirm, Accepted: confirm}
		count := int64(0)
		if confirm {
			count = 1
		}
		if err != nil || result != want || sends.Load() != count {
			t.Fatalf("result=%#v sends=%d error=%v", result, sends.Load(), err)
		}
		if client.services != nil || client.digestChallenge != nil || client.http.CheckRedirect != nil {
			t.Fatal("firmware check modified shared client")
		}
	}
}

func TestFirmwareCheckDigestPreflight(t *testing.T) {
	for _, stage := range []string{"soap", "no-challenge"} {
		t.Run(stage, func(t *testing.T) {
			script := firmwareCheckScript(true)
			read := firmwareCheckExchange{path: "/device", action: "GetInfo", body: deviceFixture}
			preflight := []firmwareCheckExchange{read}
			if stage == "soap" {
				challenge, authorized := read, read
				challenge.status, challenge.challenge, challenge.body = 401, rebootDigestChallenge, ""
				authorized.auth = true
				script[2].auth = true
				preflight = []firmwareCheckExchange{challenge, authorized}
			}
			script = append(script[:2], append(preflight, script[2])...)
			client, sends := firmwareCheckFixtureClient(t, script, true)
			result, err := client.FirmwareCheck(t.Context(), true)
			if err != nil || !result.Accepted || sends.Load() != 1 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestFirmwareCheckDigestPreflightFailureSendsNothing(t *testing.T) {
	script := append(firmwareCheckScript(false), firmwareCheckExchange{path: "/device", action: "GetInfo", status: 401, challenge: rebootDigestChallenge}, firmwareCheckExchange{path: "/device", action: "GetInfo", status: 401, auth: true})
	client, sends := firmwareCheckFixtureClient(t, script, true)
	assertFirmwareCheckError(t, client, true, "auth", false)
	if sends.Load() != 0 {
		t.Fatal("failed preflight sent the check")
	}
}

func TestFirmwareCheckRequiresUniqueSupportedService(t *testing.T) {
	for _, description := range []string{`<root/>`, strings.ReplaceAll(firmwareCheckDescriptionFixture, "UserInterface:1", "UserInterface:2"), strings.Replace(firmwareCheckDescriptionFixture, "</serviceList>", `<service><serviceType>urn:dslforum-org:service:UserInterface:1</serviceType><controlURL>/other</controlURL></service></serviceList>`, 1)} {
		for _, confirm := range []bool{false, true} {
			client, _ := firmwareCheckFixtureClient(t, []firmwareCheckExchange{{path: descriptionPath, body: description}}, false)
			assertFirmwareCheckError(t, client, confirm, "unsupported", false)
		}
	}
}

func TestFirmwareCheckRequiresAdvertisedAction(t *testing.T) {
	for _, scpd := range []string{`<scpd/>`, strings.ReplaceAll(firmwareSCPDFixture, "<action><name>"+firmwareCheckAction+"</name></action>", "")} {
		for _, confirm := range []bool{false, true} {
			script := firmwareCheckScript(false)
			script[1].body = scpd
			client, _ := firmwareCheckFixtureClient(t, script, false)
			assertFirmwareCheckError(t, client, confirm, "unsupported", false)
		}
	}
	for _, scpd := range []string{"<private-invalid", `<root/>`} {
		script := firmwareCheckScript(false)
		script[1].body = scpd
		client, _ := firmwareCheckFixtureClient(t, script, false)
		assertFirmwareCheckError(t, client, true, "protocol", false)
	}
}

func TestFirmwareCheckRejectsUnsafeOriginsAndServiceURLs(t *testing.T) {
	for _, suffix := range []string{"private-path", "?private-query", "#private-fragment"} {
		client, _ := firmwareCheckFixtureClient(t, nil, false)
		client.base, _ = client.base.Parse("/" + suffix)
		assertFirmwareCheckError(t, client, true, "usage", false)
	}
	client, _ := firmwareCheckFixtureClient(t, nil, false)
	client.base.ForceQuery = true
	assertFirmwareCheckError(t, client, true, "usage", false)
	for _, field := range []string{"controlURL", "SCPDURL"} {
		for _, raw := range []string{"", "https://other.test/private-path", "//other.test/private-path", "/private?query", "/private?", "/private#fragment", "http://private-user@other.test/", "%private-invalid"} {
			old := "/user-interface"
			if field == "SCPDURL" {
				old = "/user-interface.xml"
			}
			description := strings.Replace(firmwareCheckDescriptionFixture, "<"+field+">"+old+"</"+field+">", "<"+field+">"+raw+"</"+field+">", 1)
			client, _ := firmwareCheckFixtureClient(t, []firmwareCheckExchange{{path: descriptionPath, body: description}}, false)
			kind := "protocol"
			if raw == "" && field == "SCPDURL" {
				kind = "unsupported"
			}
			assertFirmwareCheckError(t, client, true, kind, false)
		}
	}
}

func TestFirmwareCheckAcceptsAbsoluteSameOriginURLs(t *testing.T) {
	script := firmwareCheckScript(true)
	script[0].body = strings.NewReplacer("/user-interface</controlURL>", "__ORIGIN__/user-interface</controlURL>", "/user-interface.xml</SCPDURL>", "__ORIGIN__/user-interface.xml</SCPDURL>").Replace(script[0].body)
	client, sends := firmwareCheckFixtureClient(t, script, false)
	result, err := client.FirmwareCheck(t.Context(), true)
	if err != nil || !result.Accepted || sends.Load() != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestFirmwareCheckResponsesNeverRetryOrPoll(t *testing.T) {
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
		{"wrong-action", wakeResponseFixture, "protocol", 200, true, false},
		{"wrong-version", strings.ReplaceAll(firmwareCheckResponseFixture, "UserInterface:1", "UserInterface:2"), "protocol", 200, true, false},
		{"extra-output", strings.Replace(firmwareCheckResponseFixture, `UserInterface:1"/>`, `UserInterface:1"><private-output/></u:`+firmwareCheckAction+`Response>`, 1), "protocol", 200, true, false},
		{"trailing-root", firmwareCheckResponseFixture + "<private-extra/>", "protocol", 200, true, false},
		{"fault", fault, "unsupported", 500, false, false},
		{"invalid-arguments", strings.ReplaceAll(fault, ">401<", ">402<"), "router", 500, false, false},
		{"denied", strings.ReplaceAll(fault, ">401<", ">606<"), "router", 500, false, false},
		{"server-error", firmwareCheckResponseFixture, "protocol", 500, true, false},
		{"dropped", "", "network", 0, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := firmwareCheckScript(true)
			script[2].status, script[2].body, script[2].drop, script[2].challenge = test.status, test.body, test.drop, rebootDigestChallenge
			client, sends := firmwareCheckFixtureClient(t, script, false)
			assertFirmwareCheckError(t, client, true, test.kind, test.uncertain)
			if sends.Load() != 1 {
				t.Fatal("check retried")
			}
		})
	}
}

func TestFirmwareCheckRefusesRedirectsAtEveryStage(t *testing.T) {
	for _, stage := range []int{0, 1, 2} {
		script := firmwareCheckScript(true)[:stage+1]
		script[stage].status, script[stage].location, script[stage].body = 307, "/private-path", ""
		client, sends := firmwareCheckFixtureClient(t, script, false)
		kind := "protocol"
		if stage < 2 {
			kind = "network"
		}
		assertFirmwareCheckError(t, client, true, kind, stage == 2)
		want := int64(0)
		if stage == 2 {
			want = 1
		}
		if sends.Load() != want {
			t.Fatal("redirect retried")
		}
	}
}

func TestFirmwareCheckCanceledPreflightSendsNothing(t *testing.T) {
	client, sends := firmwareCheckFixtureClient(t, nil, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := client.FirmwareCheck(ctx, true)
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Kind != "network" || protocolErr.Code == "firmware_check_uncertain" || sends.Load() != 0 {
		t.Fatal("canceled preflight initiated the check")
	}
}
