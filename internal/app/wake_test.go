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

const wakeAppMAC = "02:00:00:00:00:AB"

func (f fakeReader) Wake(_ context.Context, mac string, confirm bool) (tr064.WakeResult, error) {
	return tr064.WakeResult{Endpoint: "http://router.test:49000", MAC: mac, Preview: !confirm, Accepted: confirm}, f.err
}

func TestWakeOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"wake", wakeAppMAC}, "wake:\n  endpoint: \"http://router.test:49000\"\n  mac: \"02:00:00:00:00:AB\"\n  preview: true\n  effect: the router will send a Wake-on-LAN request to the selected MAC address\n  execute: \"router-axi wake 02:00:00:00:00:AB --host http://router.test:49000 --confirm\"\n"},
		{[]string{"wake", "02-00-00-00-00-ab", "--json"}, `{"wake":{"endpoint":"http://router.test:49000","mac":"02:00:00:00:00:AB","preview":true,"effect":"the router will send a Wake-on-LAN request to the selected MAC address","execute":"router-axi wake 02:00:00:00:00:AB --host http://router.test:49000 --confirm --json"}}` + "\n"},
		{[]string{"wake", wakeAppMAC, "--confirm"}, "wake:\n  endpoint: \"http://router.test:49000\"\n  mac: \"02:00:00:00:00:AB\"\n  accepted: true\n  recovery: acceptance does not mean the device woke; check the device manually; do not automatically repeat wake\n"},
		{[]string{"--confirm", "--json", "wake", wakeAppMAC}, `{"wake":{"endpoint":"http://router.test:49000","mac":"02:00:00:00:00:AB","accepted":true,"recovery":"acceptance does not mean the device woke; check the device manually; do not automatically repeat wake"}}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("code=%d stdout=%q", code, stdout)
		}
	}
}

func TestWakeGrammarBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"wake"}, {"wake", "--confirm"}, {"wake", "--mac", wakeAppMAC}, {"wake", wakeAppMAC, wakeAppMAC},
		{"wake", "private-target"}, {"wake", "02:00:00:00:00:GG"}, {"wake", "ff:ff:ff:ff:ff:ff"},
		{"wake", wakeAppMAC, "--force"}, {"wake", wakeAppMAC, "--all"}, {"wake", wakeAppMAC, "--instance", "1"},
		{"wake", wakeAppMAC, "--confrim"}, {"wake", wakeAppMAC, "--confirm=false"}, {"wake", wakeAppMAC, "--confirm", "false"},
		{"wake", wakeAppMAC, "--host"}, {"wake", wakeAppMAC, "--host", "--confirm"},
		{"wake", wakeAppMAC, "--help", "--unknown"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(Config) (Reader, error) { t.Fatal("invalid grammar reached factory"); return nil, nil }, func(string) string { return "" })
			input := append([]string(nil), args...)
			if jsonOutput {
				input = append(input, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), input, &stdout, &stderr)
			if code != ExitUsage || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || strings.Contains(stdout.String(), "private-target") || (jsonOutput && !json.Valid(stdout.Bytes())) {
				t.Fatalf("invalid grammar accepted: %v", args)
			}
		}
	}
}

func TestWakeHelpIsOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("help reached factory"); return nil, nil }, func(string) string { t.Fatal("help read environment"); return "" })
	for _, args := range [][]string{{"wake", "--help"}, {"wake", "--confirm", "--help"}, {"wake", wakeAppMAC, "--help"}} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "wake MAC") || !strings.Contains(stdout.String(), "No device lookup, retries, or polling") {
			t.Fatal("wake help omitted contract")
		}
	}
}

const wakeAppDescription = `<root><service><serviceType>urn:dslforum-org:service:Hosts:1</serviceType><controlURL>/hosts</controlURL><SCPDURL>/hosts-scpd.xml</SCPDURL></service></root>`
const wakeAppSCPD = `<scpd><actionList><action><name>X_AVM-DE_WakeOnLANByMACAddress</name><argumentList><argument><name>NewMACAddress</name><direction>in</direction><relatedStateVariable>MACAddress</relatedStateVariable></argument></argumentList></action></actionList></scpd>`
const wakeAppResponse = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:X_AVM-DE_WakeOnLANByMACAddressResponse xmlns:u="urn:dslforum-org:service:Hosts:1"/></s:Body></s:Envelope>`

func wakeAppServer(t *testing.T, posts, later *int, status int, body string, drop bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *posts > 0 {
			*later++
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml":
			_, _ = io.WriteString(w, wakeAppDescription)
		case r.Method == http.MethodGet && r.URL.Path == "/hosts-scpd.xml":
			_, _ = io.WriteString(w, wakeAppSCPD)
		case r.Method == http.MethodPost && r.URL.Path == "/hosts" && r.Header.Get("SOAPAction") == `"urn:dslforum-org:service:Hosts:1#X_AVM-DE_WakeOnLANByMACAddress"`:
			*posts++
			data, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(data), "<NewMACAddress>"+wakeAppMAC+"</NewMACAddress>") {
				t.Error("wrong wake target")
			}
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
			t.Error("unexpected lookup, retry, or polling")
		}
	}))
}

func TestWakeClientBoundary(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		for _, jsonOutput := range []bool{false, true} {
			for _, explicitHost := range []bool{false, true} {
				posts, later := 0, 0
				server := wakeAppServer(t, &posts, &later, 200, wakeAppResponse, false)
				application := New(func(config Config) (Reader, error) {
					return tr064.New(config.Host, config.Username, config.Password, server.Client())
				}, func(key string) string {
					if key == "ROUTER_AXI_HOST" {
						return server.URL
					}
					return ""
				})
				args := []string{"wake", "02-00-00-00-00-ab"}
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
				if !confirm && !strings.Contains(stdout.String(), "router-axi wake "+wakeAppMAC+" --host "+server.URL+" --confirm") {
					t.Fatal("preview lost target or endpoint")
				}
			}
		}
	}
}

func TestWakeErrorExitCodesAndPrivacy(t *testing.T) {
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
		{500, fault("606"), ExitRouter, false}, {500, fault("714"), ExitRouter, false}, {200, "", ExitNetwork, true},
	} {
		for _, jsonOutput := range []bool{false, true} {
			posts, later := 0, 0
			server := wakeAppServer(t, &posts, &later, test.status, test.body, test.drop)
			application := New(func(Config) (Reader, error) { return tr064.New(server.URL, "", "", server.Client()) }, func(string) string { return "" })
			args := []string{"wake", wakeAppMAC, "--confirm"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			server.Close()
			if code != test.exit || posts != 1 || later != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error") || (jsonOutput && !json.Valid(stdout.Bytes())) {
				t.Fatalf("code=%d posts=%d later=%d output=%q", code, posts, later, stdout.String())
			}
			for _, private := range []string{"private-", server.URL, wakeAppMAC, "Envelope"} {
				if strings.Contains(stdout.String(), private) {
					t.Fatal("error exposed discarded data")
				}
			}
			if (test.drop || test.body == "private-invalid") && !strings.Contains(stdout.String(), "wake_uncertain") {
				t.Fatal("lost response not uncertain")
			}
		}
	}
}

func TestWakeConfigurationErrorsArePrivate(t *testing.T) {
	application := New(func(Config) (Reader, error) { return nil, errors.New("private-configuration") }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"wake", wakeAppMAC, "--json"}, &stdout, &stderr)
	if code != ExitUsage || stderr.Len() != 0 || !json.Valid(stdout.Bytes()) || strings.Contains(stdout.String(), "private-") {
		t.Fatal("configuration error exposed data")
	}
}

func TestWakeOutputFailureAndEndpointQuoting(t *testing.T) {
	for _, args := range [][]string{{"wake", wakeAppMAC}, {"wake", wakeAppMAC, "--json"}, {"wake", wakeAppMAC, "--confirm"}, {"wake", wakeAppMAC, "--confirm", "--json"}} {
		application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" })
		if code := application.Run(t.Context(), args, failingWriter{}, &bytes.Buffer{}); code != ExitInternal {
			t.Fatalf("output failure code=%d", code)
		}
	}
	var stdout bytes.Buffer
	endpoint := "http://[::1]:49000"
	code := writeWake(&stdout, tr064.WakeResult{Endpoint: endpoint, MAC: wakeAppMAC, Preview: true}, false)
	want := "  execute: " + strconv.Quote("router-axi wake "+wakeAppMAC+" --host '"+endpoint+"' --confirm") + "\n"
	if code != ExitOK || !strings.HasSuffix(stdout.String(), want) {
		t.Fatal("preview endpoint not shell-quoted")
	}
}
