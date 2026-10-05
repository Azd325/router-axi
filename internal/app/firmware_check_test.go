package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) FirmwareCheck(_ context.Context, confirm bool) (tr064.FirmwareCheckResult, error) {
	return tr064.FirmwareCheckResult{Endpoint: "http://router.test:49000", Preview: !confirm, Accepted: confirm}, f.err
}

func TestFirmwareCheckOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"firmware", "check"}, "firmware_check:\n  endpoint: \"http://router.test:49000\"\n  preview: true\n  effect: the router will check for a firmware update; no update will be installed\n  execute: \"router-axi firmware check --host http://router.test:49000 --confirm\"\n"},
		{[]string{"firmware", "check", "--json"}, `{"firmware_check":{"endpoint":"http://router.test:49000","preview":true,"effect":"the router will check for a firmware update; no update will be installed","execute":"router-axi firmware check --host http://router.test:49000 --confirm --json"}}` + "\n"},
		{[]string{"firmware", "check", "--confirm"}, "firmware_check:\n  endpoint: \"http://router.test:49000\"\n  accepted: true\n  recovery: acceptance does not mean an update exists; run router-axi firmware to read the reported state; do not automatically repeat firmware check\n"},
		{[]string{"--confirm", "--json", "firmware", "check"}, `{"firmware_check":{"endpoint":"http://router.test:49000","accepted":true,"recovery":"acceptance does not mean an update exists; run router-axi firmware to read the reported state; do not automatically repeat firmware check"}}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("code=%d stdout=%q", code, stdout)
		}
	}
}

func TestFirmwareCheckGrammarBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"firmware", "--confirm"}, {"firmware", "update"}, {"firmware", "update", "--confirm"}, {"firmware", "check", "now"}, {"firmware", "check", "check"},
		{"firmware", "check", "--force"}, {"firmware", "check", "--all"}, {"firmware", "check", "--instance", "1"},
		{"firmware", "check", "--confrim"}, {"firmware", "check", "--confirm=false"}, {"firmware", "check", "--confirm", "false"},
		{"firmware", "check", "--host"}, {"firmware", "check", "--host", "--confirm"},
		{"firmware", "check", "--help", "--unknown"}, {"check"}, {"check", "--confirm"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(Config) (Reader, error) { t.Fatal("invalid grammar reached factory"); return nil, nil }, func(string) string { return "" })
			input := append([]string(nil), args...)
			if jsonOutput {
				input = append(input, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), input, &stdout, &stderr)
			if code != ExitUsage || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || (jsonOutput && !json.Valid(stdout.Bytes())) {
				t.Fatalf("invalid grammar accepted: %v", args)
			}
		}
	}
}

func TestFirmwareCheckUsageMessages(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"firmware", "check", "now"}, "firmware accepts one action: check"},
		{[]string{"firmware", "--confirm"}, "--confirm is valid only with firmware check, reboot, wake, wan reconnect, wifi enable, or wifi disable"},
	} {
		application := New(func(Config) (Reader, error) { t.Fatal("usage error reached factory"); return nil, nil }, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), append(test.args, "--json"), &stdout, &stderr)
		var output struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		if code != ExitUsage || output.Error.Message != test.want {
			t.Fatalf("code=%d message=%q", code, output.Error.Message)
		}
	}
}

func TestFirmwareCheckHelpIsOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("help reached factory"); return nil, nil }, func(string) string { t.Fatal("help read environment"); return "" })
	for _, args := range [][]string{{"firmware", "check", "--help"}, {"firmware", "check", "--confirm", "--help"}} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "firmware check [--confirm]") || !strings.Contains(stdout.String(), "No retries or polling") || !strings.Contains(stdout.String(), "never installs one") {
			t.Fatal("firmware check help omitted contract")
		}
	}
}

const firmwareCheckAppDescription = `<root><service><serviceType>urn:dslforum-org:service:UserInterface:1</serviceType><controlURL>/user-interface</controlURL><SCPDURL>/user-interface.xml</SCPDURL></service></root>`
const firmwareCheckAppSCPD = `<scpd><actionList><action><name>GetInfo</name></action><action><name>X_AVM-DE_CheckUpdate</name></action><action><name>X_AVM-DE_DoUpdate</name></action></actionList></scpd>`
const firmwareCheckAppResponse = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:X_AVM-DE_CheckUpdateResponse xmlns:u="urn:dslforum-org:service:UserInterface:1"/></s:Body></s:Envelope>`

func firmwareCheckAppServer(t *testing.T, posts, later *int, status int, body string, drop bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *posts > 0 {
			*later++
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml":
			_, _ = io.WriteString(w, firmwareCheckAppDescription)
		case r.Method == http.MethodGet && r.URL.Path == "/user-interface.xml":
			_, _ = io.WriteString(w, firmwareCheckAppSCPD)
		case r.Method == http.MethodPost && r.URL.Path == "/user-interface" && r.Header.Get("SOAPAction") == `"urn:dslforum-org:service:UserInterface:1#X_AVM-DE_CheckUpdate"`:
			*posts++
			if drop {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
				return
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			t.Error("unexpected action, retry, or polling")
		}
	}))
}

func TestFirmwareCheckClientBoundary(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		for _, jsonOutput := range []bool{false, true} {
			for _, explicitHost := range []bool{false, true} {
				posts, later := 0, 0
				server := firmwareCheckAppServer(t, &posts, &later, 200, firmwareCheckAppResponse, false)
				application := New(func(config Config) (Reader, error) {
					return tr064.New(config.Host, config.Username, config.Password, server.Client())
				}, func(key string) string {
					if key == "ROUTER_AXI_HOST" {
						return server.URL
					}
					return ""
				})
				args := []string{"firmware", "check"}
				if explicitHost {
					args = append(args, "--host", server.URL)
				}
				if confirm {
					args = append(args, "--confirm")
				}
				if jsonOutput {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				code := application.Run(t.Context(), args, &stdout, &stderr)
				server.Close()
				want := 0
				if confirm {
					want = 1
				}
				if code != ExitOK || stderr.Len() != 0 || posts != want || later != 0 {
					t.Fatalf("code=%d sends=%d later=%d", code, posts, later)
				}
				if !confirm && !strings.Contains(stdout.String(), "router-axi firmware check --host "+server.URL+" --confirm") {
					t.Fatal("preview lost endpoint")
				}
			}
		}
	}
}

func TestFirmwareCheckErrorExitCodesAndPrivacy(t *testing.T) {
	fault := func(code string) string {
		return `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>` + code + `</errorCode><errorDescription>private-fault</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	}
	for _, test := range []struct {
		status int
		body   string
		exit   int
		drop   bool
	}{
		{401, "private-auth", ExitAuth, false}, {403, "private-auth", ExitAuth, false},
		{200, "private-invalid", ExitRouter, false}, {500, fault("401"), ExitUnsupported, false},
		{500, fault("402"), ExitRouter, false}, {500, fault("606"), ExitRouter, false}, {200, "", ExitNetwork, true},
	} {
		for _, jsonOutput := range []bool{false, true} {
			posts, later := 0, 0
			server := firmwareCheckAppServer(t, &posts, &later, test.status, test.body, test.drop)
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"firmware", "check", "--confirm"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if code != test.exit || posts != 1 || later != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || (jsonOutput && !json.Valid(stdout.Bytes())) {
				t.Fatalf("code=%d posts=%d later=%d output=%q", code, posts, later, stdout.String())
			}
			for _, private := range []string{"private-", server.URL, "Envelope"} {
				if strings.Contains(stdout.String(), private) {
					t.Fatal("error exposed discarded data")
				}
			}
			wantCode := "router_protocol_error"
			if test.drop {
				wantCode = "router_unreachable"
			}
			if (test.drop || test.body == "private-invalid") && (!strings.Contains(stdout.String(), wantCode) || !strings.Contains(stdout.String(), "firmware check outcome is uncertain") || strings.Contains(stdout.String(), "firmware_check_uncertain")) {
				t.Fatal("lost response not uncertain")
			}
		}
	}
}

func TestFirmwareCheckConfigurationErrorsArePrivate(t *testing.T) {
	application := New(func(Config) (Reader, error) { return nil, errors.New("private-configuration") }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"firmware", "check", "--json"}, &stdout, &stderr)
	if code != ExitUsage || stderr.Len() != 0 || !json.Valid(stdout.Bytes()) || strings.Contains(stdout.String(), "private-") {
		t.Fatal("configuration error exposed data")
	}
	spec := protocolError(&tr064.Error{Kind: "usage", Code: "invalid_configuration", Operation: "firmware check", Message: "synthetic"})
	if spec.exit != ExitUsage || spec.detail.Hint != "router-axi firmware check --help" {
		t.Fatalf("spec=%#v", spec)
	}
}

func TestFirmwareCheckOutputFailureAndEndpointQuoting(t *testing.T) {
	for _, args := range [][]string{{"firmware", "check"}, {"firmware", "check", "--json"}, {"firmware", "check", "--confirm"}, {"firmware", "check", "--confirm", "--json"}} {
		application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
		if code := application.Run(t.Context(), args, failingWriter{}, &bytes.Buffer{}); code != ExitInternal {
			t.Fatalf("output failure code=%d", code)
		}
	}
	var stdout bytes.Buffer
	endpoint := "http://[::1]:49000"
	code := writeFirmwareCheck(&stdout, tr064.FirmwareCheckResult{Endpoint: endpoint, Preview: true}, false)
	want := "  execute: " + strconv.Quote("router-axi firmware check --host '"+endpoint+"' --confirm") + "\n"
	if code != ExitOK || !strings.HasSuffix(stdout.String(), want) {
		t.Fatal("preview endpoint not shell-quoted")
	}
}
