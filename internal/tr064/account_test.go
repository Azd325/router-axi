package tr064

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func accountFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("testdata/account-" + name + ".xml")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func accountFixtureClient(t *testing.T, fixtures map[string]string, status int, redirect string) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if r.Method == http.MethodPost {
			_, action, _ := strings.Cut(strings.Trim(r.Header.Get("SOAPAction"), `"`), "#")
			key = r.URL.Path + "#" + action
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			if strings.Contains(string(body), "<New") || strings.Contains(action, "Set") || action == "X_AVM-DE_GetUserList" {
				t.Errorf("unexpected account request: %s", body)
			}
		}
		requests = append(requests, key)
		if r.URL.Path == redirect {
			http.Redirect(w, r, "/unexpected", http.StatusFound)
			return
		}
		body, ok := fixtures[key]
		if !ok {
			t.Errorf("unexpected request %s", key)
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		if status != 0 && r.Method == http.MethodPost {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, strings.ReplaceAll(body, "__ORIGIN__", "http://"+r.Host))
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, &requests
}

func accountFixtures(t *testing.T) map[string]string {
	return map[string]string{
		"GET /tr64desc.xml":                    accountFixture(t, "description"),
		"GET /security.xml":                    accountFixture(t, "security-scpd"),
		"GET /auth.xml":                        accountFixture(t, "auth-scpd"),
		"/security#X_AVM-DE_GetCurrentUser":    accountFixture(t, "current-user"),
		"/security#GetInfo":                    accountFixture(t, "security-info"),
		"/security#X_AVM-DE_GetAnonymousLogin": accountFixture(t, "anonymous-login"),
		"/auth#GetInfo":                        accountFixture(t, "auth-info"),
		"/device#GetInfo":                      deviceFixture,
	}
}

func TestAccountDocumentedReads(t *testing.T) {
	for _, variant := range []string{"current", "legacy", "boolean-text", "absolute-urls", "service-version-2", "empty-rights"} {
		t.Run(variant, func(t *testing.T) {
			fixtures := accountFixtures(t)
			switch variant {
			case "legacy":
				fixtures["/security#X_AVM-DE_GetCurrentUser"] = accountFixture(t, "current-user-legacy")
				fixtures["/security#GetInfo"] = accountFixture(t, "security-info-legacy")
			case "boolean-text":
				fixtures["/auth#GetInfo"] = strings.ReplaceAll(fixtures["/auth#GetInfo"], ">1<", ">true<")
				fixtures["/security#GetInfo"] = strings.ReplaceAll(fixtures["/security#GetInfo"], ">0<", ">false<")
				fixtures["/security#X_AVM-DE_GetAnonymousLogin"] = strings.ReplaceAll(fixtures["/security#X_AVM-DE_GetAnonymousLogin"], ">0<", ">false<")
			case "absolute-urls":
				fixtures["GET /tr64desc.xml"] = strings.ReplaceAll(strings.ReplaceAll(fixtures["GET /tr64desc.xml"], "<controlURL>/", "<controlURL>__ORIGIN__/"), "<SCPDURL>/", "<SCPDURL>__ORIGIN__/")
			case "service-version-2":
				fixtures["GET /tr64desc.xml"] = strings.ReplaceAll(fixtures["GET /tr64desc.xml"], ":1</serviceType>", ":2</serviceType>")
			case "empty-rights":
				fixtures["/security#X_AVM-DE_GetCurrentUser"] = `<Envelope><NewX_AVM-DE_CurrentUsername/><NewX_AVM-DE_CurrentUserRights>&lt;rights/&gt;</NewX_AVM-DE_CurrentUserRights></Envelope>`
			}
			client, requests := accountFixtureClient(t, fixtures, 0, "")
			result, err := client.Account(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if result.AnonymousLoginEnabled || !result.SecondFactorEnabled {
				t.Fatalf("posture=%+v", result)
			}
			if variant == "legacy" {
				if result.Username != "" || result.Rights != nil || result.DefaultPasswordActive != nil {
					t.Fatalf("legacy=%+v", result)
				}
			} else if result.DefaultPasswordActive == nil || *result.DefaultPasswordActive {
				t.Fatalf("default password=%v", result.DefaultPasswordActive)
			}
			if variant == "empty-rights" {
				if result.Rights == nil || len(result.Rights) != 0 {
					t.Fatalf("empty=%+v", result)
				}
			} else if variant != "legacy" {
				encoded, err := json.Marshal(result)
				want := `{"username":"synthetic-account","rights":[{"path":"BoxAdmin","access":"none"},{"path":"Phone","access":"readwrite"},{"path":"Dial","access":"none"},{"path":"NAS","access":"readonly"},{"path":"HomeAuto","access":"readwrite"}],"anonymous_login_enabled":false,"default_password_active":false,"second_factor_enabled":true}`
				if err != nil || string(encoded) != want {
					t.Fatalf("JSON=%s error=%v", encoded, err)
				}
			}
			wantRequests := []string{"GET /tr64desc.xml", "GET /security.xml", "GET /auth.xml", "/security#X_AVM-DE_GetCurrentUser", "/security#GetInfo", "/security#X_AVM-DE_GetAnonymousLogin", "/auth#GetInfo"}
			if !reflect.DeepEqual(*requests, wantRequests) {
				t.Fatalf("requests=%v", *requests)
			}
		})
	}
}

func TestAccountPreflightBeforeSOAP(t *testing.T) {
	for _, variant := range []string{"missing-security", "missing-auth", "duplicate-security", "duplicate-auth", "missing-current-user", "missing-security-info", "missing-anonymous", "missing-auth-info", "invalid-scpd", "foreign-control", "foreign-scpd", "url-query", "url-fragment", "url-userinfo", "missing-url"} {
		t.Run(variant, func(t *testing.T) {
			fixtures := accountFixtures(t)
			kind := "unsupported"
			switch variant {
			case "missing-security":
				fixtures["GET /tr64desc.xml"] = strings.ReplaceAll(fixtures["GET /tr64desc.xml"], "LANConfigSecurity:", "OtherSecurity:")
			case "missing-auth":
				fixtures["GET /tr64desc.xml"] = strings.ReplaceAll(fixtures["GET /tr64desc.xml"], "X_AVM-DE_Auth:", "OtherAuth:")
			case "duplicate-security", "duplicate-auth":
				prefix := accountSecurityPrefix
				if variant == "duplicate-auth" {
					prefix = accountAuthPrefix
				}
				fixtures["GET /tr64desc.xml"] = strings.Replace(fixtures["GET /tr64desc.xml"], "</serviceList>", "<service><serviceType>"+prefix+"2</serviceType></service></serviceList>", 1)
			case "missing-current-user":
				fixtures["GET /security.xml"] = strings.ReplaceAll(fixtures["GET /security.xml"], "X_AVM-DE_GetCurrentUser", "Unavailable")
			case "missing-security-info":
				fixtures["GET /security.xml"] = strings.ReplaceAll(fixtures["GET /security.xml"], "GetInfo", "Unavailable")
			case "missing-anonymous":
				fixtures["GET /security.xml"] = strings.ReplaceAll(fixtures["GET /security.xml"], "X_AVM-DE_GetAnonymousLogin", "Unavailable")
			case "missing-auth-info":
				fixtures["GET /auth.xml"] = `<scpd/>`
			case "invalid-scpd":
				kind = "protocol"
				fixtures["GET /auth.xml"] = `<other/>`
			default:
				kind = "protocol"
				replacement := map[string]string{"foreign-control": "https://outside.test/private", "url-query": "/security?private=1", "url-fragment": "/security#private", "url-userinfo": "http://private@outside.test/security", "missing-url": ""}[variant]
				needle := "/security</controlURL>"
				if variant == "foreign-scpd" {
					needle = "/auth.xml</SCPDURL>"
					replacement = "https://outside.test/private"
				}
				ending := "</controlURL>"
				if variant == "foreign-scpd" {
					ending = "</SCPDURL>"
				}
				fixtures["GET /tr64desc.xml"] = strings.Replace(fixtures["GET /tr64desc.xml"], needle, replacement+ending, 1)
			}
			client, requests := accountFixtureClient(t, fixtures, 0, "")
			result, err := client.Account(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, Account{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "account" || strings.Contains(err.Error(), "private") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			for _, request := range *requests {
				if !strings.HasPrefix(request, "GET ") {
					t.Fatalf("SOAP before preflight: %v", *requests)
				}
			}
		})
	}
}

func TestAccountEnabledLoginPosture(t *testing.T) {
	fixtures := accountFixtures(t)
	fixtures["/security#GetInfo"] = strings.ReplaceAll(fixtures["/security#GetInfo"], ">0<", ">1<")
	fixtures["/security#X_AVM-DE_GetAnonymousLogin"] = strings.ReplaceAll(fixtures["/security#X_AVM-DE_GetAnonymousLogin"], ">0<", ">1<")
	fixtures["/auth#GetInfo"] = strings.ReplaceAll(fixtures["/auth#GetInfo"], ">1<", ">0<")
	client, _ := accountFixtureClient(t, fixtures, 0, "")
	result, err := client.Account(t.Context())
	if err != nil || !result.AnonymousLoginEnabled || result.DefaultPasswordActive == nil || !*result.DefaultPasswordActive || result.SecondFactorEnabled {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAccountMalformedResponses(t *testing.T) {
	for _, test := range []struct{ key, body string }{
		{"/security#X_AVM-DE_GetCurrentUser", `<Envelope/>`},
		{"/security#X_AVM-DE_GetCurrentUser", `<Envelope><NewX_AVM-DE_CurrentUsername>private&#10;value</NewX_AVM-DE_CurrentUsername></Envelope>`},
		{"/security#GetInfo", `<Envelope><NewX_AVM-DE_IsDefaultPasswordActive>private</NewX_AVM-DE_IsDefaultPasswordActive></Envelope>`},
		{"/security#X_AVM-DE_GetAnonymousLogin", `<Envelope/>`},
		{"/security#X_AVM-DE_GetAnonymousLogin", `<Envelope><NewX_AVM-DE_AnonymousLoginEnabled>private</NewX_AVM-DE_AnonymousLoginEnabled></Envelope>`},
		{"/auth#GetInfo", `<Envelope/>`},
		{"/auth#GetInfo", `<Envelope><NewEnabled>private</NewEnabled></Envelope>`},
		{"/auth#GetInfo", `<invalid`},
	} {
		t.Run(test.key+test.body, func(t *testing.T) {
			fixtures := accountFixtures(t)
			fixtures[test.key] = test.body
			client, _ := accountFixtureClient(t, fixtures, 0, "")
			result, err := client.Account(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, Account{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "private") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestAccountRightsValidation(t *testing.T) {
	for _, value := range []string{"", "<rights", "<rights>private</rights>", "<rights/>private", "<rights/><other/>", "<other/>", "<rights><path>NAS</path></rights>", "<rights><access>none</access><path>NAS</path></rights>", "<rights><path>private</path><access>none</access></rights>", "<rights><path>NAS</path><access>private</access></rights>", "<rights><path>NAS</path><access>none</access><path>NAS</path><access>readonly</access></rights>", "<rights><path><other>NAS</other></path><access>none</access></rights>", strings.Repeat("x", 65537)} {
		_, err := parseAccountRights(&value)
		var protocolErr *Error
		if !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || strings.Contains(err.Error(), "private") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestAccountFailuresAreSanitized(t *testing.T) {
	for _, test := range []struct {
		status      int
		fault, kind string
	}{{401, "", "auth"}, {403, "", "auth"}, {500, "401", "unsupported"}, {500, "606", "router"}, {500, "820", "router"}} {
		fixtures := accountFixtures(t)
		fixtures["/security#X_AVM-DE_GetCurrentUser"] = `<Envelope><errorCode>` + test.fault + `</errorCode><errorDescription>private-secret</errorDescription></Envelope>`
		client, requests := accountFixtureClient(t, fixtures, test.status, "")
		result, err := client.Account(t.Context())
		var protocolErr *Error
		if !reflect.DeepEqual(result, Account{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.StatusCode != test.status || strings.Contains(err.Error(), "private") || len(*requests) != 4 {
			t.Fatalf("result=%+v err=%v requests=%v", result, err, *requests)
		}
	}
}

func TestAccountRefusesRedirects(t *testing.T) {
	for _, path := range []string{"/tr64desc.xml", "/security.xml", "/auth.xml", "/security", "/auth"} {
		client, requests := accountFixtureClient(t, accountFixtures(t), 0, path)
		_, err := client.Account(t.Context())
		var protocolErr *Error
		if !errors.As(err, &protocolErr) || protocolErr.Kind != "network" {
			t.Fatalf("err=%v", err)
		}
		for _, request := range *requests {
			if strings.Contains(request, "unexpected") {
				t.Fatal("followed redirect")
			}
		}
	}
}

func TestDoctorAccountAdvertisementOnly(t *testing.T) {
	client, requests := accountFixtureClient(t, accountFixtures(t), 0, "")
	report, err := client.Doctor(t.Context())
	if err != nil || report.Capabilities.Account.State != "advertised" || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "/device#GetInfo"}) {
		t.Fatalf("report=%+v err=%v requests=%v", report, err, *requests)
	}
}
