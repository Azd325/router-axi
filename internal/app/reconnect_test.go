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

func (f fakeReader) WANReconnect(_ context.Context, confirm bool) (tr064.WANReconnectResult, error) {
	return tr064.WANReconnectResult{Endpoint: "http://router.test:49000", Preview: !confirm, Accepted: confirm}, f.err
}

func TestWANReconnectOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"wan", "reconnect"}, "wan_reconnect:\n  endpoint: \"http://router.test:49000\"\n  preview: true\n  effect: the internet connection will drop and a new external address may be assigned\n  execute: \"router-axi wan reconnect --host http://router.test:49000 --confirm\"\n"},
		{[]string{"wan", "reconnect", "--json"}, `{"wan_reconnect":{"endpoint":"http://router.test:49000","preview":true,"effect":"the internet connection will drop and a new external address may be assigned","execute":"router-axi wan reconnect --host http://router.test:49000 --confirm --json"}}` + "\n"},
		{[]string{"wan", "reconnect", "--confirm"}, "wan_reconnect:\n  endpoint: \"http://router.test:49000\"\n  accepted: true\n  recovery: wait for the connection to return, then run router-axi wan; do not automatically repeat wan reconnect\n"},
		{[]string{"--confirm", "--json", "wan", "reconnect"}, `{"wan_reconnect":{"endpoint":"http://router.test:49000","accepted":true,"recovery":"wait for the connection to return, then run router-axi wan; do not automatically repeat wan reconnect"}}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("wan reconnect output contract failed: code=%d stdout=%q", code, stdout)
		}
	}
}

func TestWANReconnectGrammarBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"wan", "--confirm"}, {"wan", "detail", "--confirm"}, {"wan", "reconnect", "--force"},
		{"wan", "reconnect", "--confrim"}, {"wan", "reconnect", "--all"}, {"wan", "reconnect", "--instance", "1"},
		{"wan", "reconnect", "now"}, {"wan", "reconnect", "detail"}, {"wan", "detail", "reconnect"},
		{"wan", "reconnect", "--confirm=false"}, {"wan", "reconnect", "--confirm", "false"},
		{"wan", "reconnect", "--host"}, {"wan", "reconnect", "--host", "--confirm"},
		{"wan", "reconnect", "--confirm", "--unknown"}, {"wan", "reconnect", "--help", "--unknown"},
		{"reconnect"}, {"reconnect", "--confirm"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(Config) (Reader, error) {
				t.Fatal("invalid grammar reached the client factory")
				return nil, nil
			}, func(string) string { return "" })
			input := append([]string(nil), args...)
			if jsonOutput {
				input = append(input, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), input, &stdout, &stderr)
			if code != ExitUsage || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || (jsonOutput && !json.Valid(stdout.Bytes())) {
				t.Fatalf("invalid grammar not rejected: %v", args)
			}
		}
	}
}

func TestWANReconnectHelpIsOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) {
		t.Fatal("help reached the client factory")
		return nil, nil
	}, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"wan", "reconnect", "--confirm", "--help"}, &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "wan reconnect [--confirm]") || !strings.Contains(stdout.String(), "not idempotent") {
		t.Fatal("wan reconnect help omitted its safety contract")
	}
}

func TestWANReconnectConfigurationErrorsArePrivate(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		application := New(func(Config) (Reader, error) {
			return nil, errors.New("synthetic-sensitive-configuration")
		}, func(string) string { return "" })
		args := []string{"wan", "reconnect"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitUsage || stderr.Len() != 0 || strings.Contains(stdout.String(), "synthetic-sensitive") || !strings.Contains(stdout.String(), "invalid_configuration") {
			t.Fatal("wan reconnect configuration error was not sanitized")
		}
	}
}

func TestWANReconnectRejectsSensitiveArguments(t *testing.T) {
	for _, args := range [][]string{
		{"wan", "reconnect", "--password=synthetic-sensitive"},
		{"wan", "reconnect", "--host", "http://synthetic-sensitive:synthetic-sensitive@router.test"},
		{"wan", "reconnect", "--host", "http://router.test/?synthetic-sensitive"},
		{"wan", "reconnect", "--host", "http://router.test/%synthetic-sensitive"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(config Config) (Reader, error) {
				return tr064.New(config.Host, "", "", nil)
			}, func(string) string { return "" })
			input := append([]string(nil), args...)
			if jsonOutput {
				input = append(input, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), input, &stdout, &stderr)
			if code != ExitUsage || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || strings.Contains(stdout.String(), "synthetic-sensitive") || strings.Contains(stdout.String(), "router.test") {
				t.Fatal("invalid wan reconnect input was not rejected privately")
			}
			if jsonOutput && !json.Valid(stdout.Bytes()) {
				t.Fatal("invalid wan reconnect input error was not JSON")
			}
		}
	}
}

const wanReconnectAppDescription = `<root><service><serviceType>urn:dslforum-org:service:Layer3Forwarding:1</serviceType><controlURL>/layer3</controlURL><SCPDURL>/layer3-scpd.xml</SCPDURL></service><service><serviceType>urn:dslforum-org:service:WANPPPConnection:1</serviceType><serviceId>urn:WANPPPConnection-com:serviceId:WANPPPConnection1</serviceId><controlURL>/wanppp</controlURL><SCPDURL>/wanppp-scpd.xml</SCPDURL></service></root>`

func wanReconnectAppServer(t *testing.T, posts, later *int, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *posts > 0 {
			*later++
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml":
			_, _ = io.WriteString(w, wanReconnectAppDescription)
		case r.Method == http.MethodGet && r.URL.Path == "/layer3-scpd.xml":
			_, _ = io.WriteString(w, `<scpd><actionList><action><name>GetDefaultConnectionService</name></action></actionList></scpd>`)
		case r.Method == http.MethodGet && r.URL.Path == "/wanppp-scpd.xml":
			_, _ = io.WriteString(w, `<scpd><actionList><action><name>ForceTermination</name></action></actionList></scpd>`)
		case r.Method == http.MethodPost && r.URL.Path == "/layer3":
			_, _ = io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetDefaultConnectionServiceResponse xmlns:u="urn:dslforum-org:service:Layer3Forwarding:1"><NewDefaultConnectionService>1.WANPPPConnection.1</NewDefaultConnectionService></u:GetDefaultConnectionServiceResponse></s:Body></s:Envelope>`)
		case r.Method == http.MethodPost && r.URL.Path == "/wanppp" && r.Header.Get("SOAPAction") == `"urn:dslforum-org:service:WANPPPConnection:1#ForceTermination"`:
			*posts++
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			t.Error("unexpected wan reconnect request")
		}
	}))
}

const wanReconnectAppResponse = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:ForceTerminationResponse xmlns:u="urn:dslforum-org:service:WANPPPConnection:1"/></s:Body></s:Envelope>`

func TestWANReconnectClientBoundary(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		for _, jsonOutput := range []bool{false, true} {
			for _, explicitHost := range []bool{false, true} {
				posts, later := 0, 0
				server := wanReconnectAppServer(t, &posts, &later, http.StatusOK, wanReconnectAppResponse)
				application := New(func(config Config) (Reader, error) {
					return tr064.New(config.Host, config.Username, config.Password, server.Client())
				}, func(key string) string {
					if key == "ROUTER_AXI_HOST" {
						if explicitHost {
							return "unused.test"
						}
						return server.URL
					}
					return ""
				})
				args := []string{"wan", "reconnect"}
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
				wantPosts := 0
				if confirm {
					wantPosts = 1
				}
				if code != ExitOK || stderr.Len() != 0 || posts != wantPosts || later != 0 {
					t.Fatalf("wan reconnect safety boundary failed: code=%d posts=%d subsequent=%d", code, posts, later)
				}
				if !strings.Contains(stdout.String(), server.URL) || (!confirm && !strings.Contains(stdout.String(), "router-axi wan reconnect --host "+server.URL+" --confirm")) {
					t.Fatal("preview/result did not preserve selected endpoint")
				}
			}
		}
	}
}

func TestWANReconnectErrorExitCodes(t *testing.T) {
	fault := func(code string) string {
		return `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>` + code + `</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	}
	for _, test := range []struct {
		status int
		body   string
		exit   int
	}{
		{http.StatusUnauthorized, "synthetic-sensitive-auth", ExitAuth},
		{http.StatusForbidden, "synthetic-sensitive-auth", ExitAuth},
		{http.StatusOK, "synthetic-sensitive-invalid", ExitRouter},
		{http.StatusInternalServerError, fault("401"), ExitUnsupported},
		{http.StatusInternalServerError, fault("707"), ExitRouter},
	} {
		for _, jsonOutput := range []bool{false, true} {
			posts, later := 0, 0
			server := wanReconnectAppServer(t, &posts, &later, test.status, test.body)
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"wan", "reconnect", "--confirm"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if code != test.exit || posts != 1 || later != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || (jsonOutput && !json.Valid(stdout.Bytes())) {
				t.Fatalf("wan reconnect failure contract: code=%d posts=%d subsequent=%d", code, posts, later)
			}
			if strings.Contains(stdout.String(), "synthetic-sensitive") || strings.Contains(stdout.String(), server.URL) || strings.Contains(stdout.String(), "Envelope") {
				t.Fatal("wan reconnect error leaked discarded response data")
			}
		}
	}
}

func TestWANReconnectOutputFailure(t *testing.T) {
	for _, args := range [][]string{{"wan", "reconnect"}, {"wan", "reconnect", "--json"}, {"wan", "reconnect", "--confirm"}, {"wan", "reconnect", "--confirm", "--json"}} {
		application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
		if code := application.Run(t.Context(), args, failingWriter{}, &bytes.Buffer{}); code != ExitInternal {
			t.Fatalf("output failure code=%d", code)
		}
	}
}

func TestWANReconnectPreviewQuotesEndpoint(t *testing.T) {
	endpoint := "http://[::1]:49000"
	var stdout bytes.Buffer
	code := writeWANReconnect(&stdout, tr064.WANReconnectResult{Endpoint: endpoint, Preview: true}, false)
	want := "  execute: " + strconv.Quote("router-axi wan reconnect --host '"+endpoint+"' --confirm") + "\n"
	if code != ExitOK || !strings.HasSuffix(stdout.String(), want) {
		t.Fatal("wan reconnect execute command is not shell-quoted")
	}
}
