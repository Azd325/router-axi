package app

import (
	"bytes"
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

func TestRebootOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"reboot"}, "reboot:\n  endpoint: \"http://router.test:49000\"\n  preview: true\n  effect: the router will restart and temporarily interrupt all local services\n  execute: \"router-axi reboot --host http://router.test:49000 --confirm\"\n"},
		{[]string{"reboot", "--json"}, `{"reboot":{"endpoint":"http://router.test:49000","preview":true,"effect":"the router will restart and temporarily interrupt all local services","execute":"router-axi reboot --host http://router.test:49000 --confirm --json"}}` + "\n"},
		{[]string{"reboot", "--confirm"}, "reboot:\n  endpoint: \"http://router.test:49000\"\n  accepted: true\n  recovery: wait for the router to recover, then run router-axi doctor; do not automatically repeat reboot\n"},
		{[]string{"--confirm", "--json", "reboot"}, `{"reboot":{"endpoint":"http://router.test:49000","accepted":true,"recovery":"wait for the router to recover, then run router-axi doctor; do not automatically repeat reboot"}}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("reboot output contract failed: code=%d", code)
		}
	}
}

func TestRebootGrammarBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"reboot", "--force"}, {"reboot", "--confrim"}, {"reboot", "--all"},
		{"reboot", "--instance", "1"}, {"reboot", "now"}, {"reboot", "enable"},
		{"reboot", "--confirm=false"}, {"reboot", "--confirm", "false"},
		{"reboot", "--host"}, {"reboot", "--host", "--confirm"},
		{"reboot", "--confirm", "--unknown"}, {"reboot", "--help", "--unknown"},
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
			if code != ExitUsage || stdout.Len() != 0 || stderr.Len() == 0 || (jsonOutput && !json.Valid(stderr.Bytes())) {
				t.Fatalf("invalid grammar not rejected: %v", args)
			}
		}
	}
}

func TestRebootHelpIsOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) {
		t.Fatal("help reached the client factory")
		return nil, nil
	}, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"reboot", "--confirm", "--help"}, &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "[--confirm]") || !strings.Contains(stdout.String(), "not idempotent") {
		t.Fatal("reboot help omitted its safety contract")
	}
}

func TestRebootConfigurationErrorsArePrivate(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		application := New(func(Config) (Reader, error) {
			return nil, errors.New("synthetic-sensitive-configuration")
		}, func(string) string { return "" })
		args := []string{"reboot"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitUsage || stdout.Len() != 0 || strings.Contains(stderr.String(), "synthetic-sensitive") || !strings.Contains(stderr.String(), "invalid_configuration") {
			t.Fatal("reboot configuration error was not sanitized")
		}
	}
}

func TestRebootRejectsSensitiveArguments(t *testing.T) {
	for _, args := range [][]string{
		{"reboot", "--password=synthetic-sensitive"},
		{"reboot", "--host", "http://synthetic-sensitive:synthetic-sensitive@router.test"},
		{"reboot", "--host", "http://router.test/?synthetic-sensitive"},
		{"reboot", "--host", "http://router.test/%synthetic-sensitive"},
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
			if code != ExitUsage || stdout.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "synthetic-sensitive") || strings.Contains(stderr.String(), "router.test") {
				t.Fatal("invalid reboot input was not rejected privately")
			}
			if jsonOutput && !json.Valid(stderr.Bytes()) {
				t.Fatal("invalid reboot input error was not JSON")
			}
		}
	}
}

func TestRebootClientBoundary(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		for _, jsonOutput := range []bool{false, true} {
			for _, explicitHost := range []bool{false, true} {
				posts, readsAfterPost := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if posts > 0 {
						readsAfterPost++
					}
					if r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml" {
						_, _ = io.WriteString(w, `<root><service><serviceType>urn:dslforum-org:service:DeviceConfig:1</serviceType><controlURL>/config</controlURL></service></root>`)
						return
					}
					if r.Method != http.MethodPost || r.URL.Path != "/config" || r.Header.Get("SOAPAction") != `"urn:dslforum-org:service:DeviceConfig:1#Reboot"` {
						t.Error("unexpected reboot request")
					}
					posts++
					_, _ = io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:RebootResponse xmlns:u="urn:dslforum-org:service:DeviceConfig:1"/></s:Body></s:Envelope>`)
				}))
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
				args := []string{"reboot"}
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
				if code != ExitOK || stderr.Len() != 0 || posts != wantPosts || readsAfterPost != 0 {
					t.Fatalf("reboot safety boundary failed: code=%d posts=%d subsequent=%d", code, posts, readsAfterPost)
				}
				if !strings.Contains(stdout.String(), server.URL) || (!confirm && !strings.Contains(stdout.String(), "router-axi reboot --host "+server.URL+" --confirm")) {
					t.Fatal("preview/result did not preserve selected endpoint")
				}
			}
		}
	}
}

func TestRebootErrorExitCodes(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		exit   int
	}{
		{http.StatusUnauthorized, "synthetic-sensitive-auth", ExitAuth},
		{http.StatusForbidden, "synthetic-sensitive-auth", ExitAuth},
		{http.StatusOK, "synthetic-sensitive-invalid", ExitRouter},
		{http.StatusInternalServerError, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, ExitUnsupported},
	} {
		for _, jsonOutput := range []bool{false, true} {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `<root><service><serviceType>urn:dslforum-org:service:DeviceConfig:1</serviceType><controlURL>/config</controlURL></service></root>`)
					return
				}
				posts++
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"reboot", "--confirm"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if code != test.exit || posts != 1 || stdout.Len() != 0 || stderr.Len() == 0 || (jsonOutput && !json.Valid(stderr.Bytes())) {
				t.Fatalf("reboot failure contract: code=%d posts=%d", code, posts)
			}
			if strings.Contains(stderr.String(), "synthetic-sensitive") || strings.Contains(stderr.String(), server.URL) || strings.Contains(stderr.String(), "Envelope") {
				t.Fatal("reboot error leaked discarded response data")
			}
		}
	}
}

func TestRebootOutputFailure(t *testing.T) {
	for _, args := range [][]string{{"reboot"}, {"reboot", "--json"}, {"reboot", "--confirm"}, {"reboot", "--confirm", "--json"}} {
		application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
		if code := application.Run(t.Context(), args, failingWriter{}, &bytes.Buffer{}); code != ExitInternal {
			t.Fatalf("output failure code=%d", code)
		}
	}
}

func TestRebootPreviewQuotesEndpoint(t *testing.T) {
	endpoint := "http://[::1]:49000"
	var stdout bytes.Buffer
	code := writeReboot(&stdout, tr064.RebootResult{Endpoint: endpoint, Preview: true}, false)
	want := "  execute: " + strconv.Quote("router-axi reboot --host '"+endpoint+"' --confirm") + "\n"
	if code != ExitOK || !strings.HasSuffix(stdout.String(), want) {
		t.Fatal("reboot execute command is not shell-quoted")
	}
}
