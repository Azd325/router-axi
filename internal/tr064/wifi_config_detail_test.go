package tr064

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/wifi-detail-scpd.xml
var wifiDetailSCPDFixture string

//go:embed testdata/wifi-night.xml
var wifiNightFixture string

//go:embed testdata/wifi-wps.xml
var wifiWPSFixture string

//go:embed testdata/wifi-guest-config.xml
var wifiGuestConfigFixture string

func wifiConfigurationOverrides() map[string]wifiResponse {
	return map[string]wifiResponse{wifiSCPPath + "#SCPD": {body: wifiDetailSCPDFixture}}
}

func wifiFault(code string) string {
	return `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><errorCode>` + code + `</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription></s:Fault></s:Body></s:Envelope>`
}

func TestWiFiConfigurationDocumentedValuesAndSafeActions(t *testing.T) {
	client, requests := wifiFixtureClient(t, wifiDescriptionFixture, wifiConfigurationOverrides())
	detail, err := client.WiFiDetail(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if detail.NightControl == nil || *detail.NightControl.Schedule != "<night><enabled>1</enabled></night>" || !*detail.NightControl.NoForcedOff ||
		detail.WPS == nil || *detail.WPS.Mode != "pbc" || *detail.WPS.Status != "inactive" {
		t.Fatalf("incorrect configuration: %#v", detail)
	}
	for _, want := range []string{"/wlan.xml#SCPD", "/wifi2#GetInfo", "/wifi2#GetChannelInfo", "/wifi2#X_AVM-DE_GetNightControl", "/wifi2#X_AVM-DE_GetWPSInfo"} {
		if got := <-requests; got != want {
			t.Fatalf("request = %q, want %q", got, want)
		}
	}
	if len(requests) != 0 {
		t.Fatal("unexpected action requests")
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration2","enabled":true,"status":"Up","standard":"ax","max_bit_rate":"Auto","channel":36,"band":"5000","night_control":{"schedule":"\u003cnight\u003e\u003cenabled\u003e1\u003c/enabled\u003e\u003c/night\u003e","no_forced_off":true},"wps":{"mode":"pbc","status":"inactive"}}`
	if string(encoded) != want || strings.Contains(string(encoded), "synthetic-sensitive") {
		t.Fatalf("JSON = %s", encoded)
	}
}

func TestWiFiConfigurationUnavailablePartsDoNotHideOtherFields(t *testing.T) {
	for _, action := range []string{"X_AVM-DE_GetNightControl", "X_AVM-DE_GetWPSInfo"} {
		for _, variant := range []string{"not advertised", "401", "501", "606"} {
			t.Run(action+"/"+variant, func(t *testing.T) {
				overrides := wifiConfigurationOverrides()
				if variant == "not advertised" {
					overrides[wifiSCPPath+"#SCPD"] = wifiResponse{body: strings.Replace(wifiDetailSCPDFixture, "<action><name>"+action+"</name></action>", "", 1)}
				} else {
					overrides[action] = wifiResponse{body: wifiFault(variant), status: http.StatusInternalServerError}
				}
				client, requests := wifiFixtureClient(t, wifiDescriptionFixture, overrides)
				detail, err := client.WiFiDetail(t.Context(), 2)
				if err != nil || !detail.Enabled || detail.Channel == nil || *detail.Channel != 36 || detail.Band != "5000" {
					t.Fatalf("detail=%#v err=%v", detail, err)
				}
				parts := map[string]bool{
					"X_AVM-DE_GetNightControl": detail.NightControl != nil,
					"X_AVM-DE_GetWPSInfo":      detail.WPS != nil,
				}
				for name, available := range parts {
					if available != (name != action) {
						t.Fatalf("%s available = %t", name, available)
					}
				}
				for len(requests) > 0 {
					if got := <-requests; variant == "not advertised" && strings.HasSuffix(got, "#"+action) {
						t.Fatal("unadvertised action was called")
					}
				}
			})
		}
	}
}

func TestWiFiConfigurationOptionalAndFutureValues(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "future enum", true: "missing fields"}[missing], func(t *testing.T) {
			overrides := wifiConfigurationOverrides()
			if missing {
				for action, fixture := range map[string]string{"X_AVM-DE_GetNightControl": wifiNightFixture, "X_AVM-DE_GetWPSInfo": wifiWPSFixture} {
					start := strings.Index(fixture, "><New") + 1
					end := strings.Index(fixture, "</u:")
					overrides[action] = wifiResponse{body: fixture[:start] + fixture[end:]}
				}
			} else {
				overrides["X_AVM-DE_GetWPSInfo"] = wifiResponse{body: strings.ReplaceAll(strings.ReplaceAll(wifiWPSFixture, ">pbc<", ">synthetic-sensitive-mode<"), ">inactive<", ">synthetic-sensitive-status<")}
			}
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, overrides)
			detail, err := client.WiFiDetail(t.Context(), 2)
			if err != nil || detail.WPS == nil || detail.NightControl == nil {
				t.Fatalf("detail=%#v err=%v", detail, err)
			}
			if missing {
				if detail.WPS.Mode != nil || detail.WPS.Status != nil || detail.NightControl.Schedule != nil || detail.NightControl.NoForcedOff != nil {
					t.Fatal("missing fields must be unknown")
				}
			} else if *detail.WPS.Mode != "unknown" || *detail.WPS.Status != "unknown" {
				t.Fatal("future enum values must be unknown")
			}
		})
	}
}

func TestWiFiConfigurationFailuresAreStructuredAndRedacted(t *testing.T) {
	for _, test := range []struct {
		action, body, kind string
		status             int
	}{
		{"X_AVM-DE_GetNightControl", strings.Replace(wifiNightFixture, "<NewNightTimeControlNoForcedOff>1", "<NewNightTimeControlNoForcedOff>synthetic-sensitive", 1), "protocol", 0},
		{"X_AVM-DE_GetWPSInfo", "synthetic-sensitive", "auth", http.StatusUnauthorized},
	} {
		t.Run(test.action+"/"+test.kind, func(t *testing.T) {
			overrides := wifiConfigurationOverrides()
			overrides[test.action] = wifiResponse{body: test.body, status: test.status}
			client, _ := wifiFixtureClient(t, wifiDescriptionFixture, overrides)
			detail, err := client.WiFiDetail(t.Context(), 2)
			var failure *Error
			if detail.ServiceID != "" || !errors.As(err, &failure) || failure.Kind != test.kind || failure.Operation != "wifi detail" || strings.Contains(err.Error(), "synthetic-sensitive") {
				t.Fatalf("detail=%#v err=%#v", detail, err)
			}
		})
	}
}

func TestGuestConfigurationRawStringsFromClassification(t *testing.T) {
	for _, fixture := range []string{wifiGuestConfigFixture, strings.ReplaceAll(wifiGuestConfigFixture, ">90<", ">router-raw-value<")} {
		client, requests := wifiFixtureClient(t, wifiDescriptionFixture, map[string]wifiResponse{"/wifi2#X_AVM-DE_GetWLANExtInfo": {body: fixture}})
		guests, err := client.GuestWiFi(t.Context())
		if err != nil || len(guests) != 1 {
			t.Fatalf("guests=%#v err=%v", guests, err)
		}
		wantTimeout := "90"
		if strings.Contains(fixture, "router-raw-value") {
			wantTimeout = "router-raw-value"
		}
		active, remain, noForcedOff, isolation := "1", "42", "1", "1"
		want := GuestConfiguration{TimeoutActive: &active, Timeout: &wantTimeout, TimeRemain: &remain, NoForcedOff: &noForcedOff, UserIsolation: &isolation}
		if !reflect.DeepEqual(guests[0].Configuration, want) {
			t.Fatalf("configuration=%#v", guests[0].Configuration)
		}
		calls := 0
		for len(requests) > 0 {
			if strings.HasSuffix(<-requests, "#X_AVM-DE_GetWLANExtInfo") {
				calls++
			}
		}
		if calls != 3 {
			t.Fatal("guest configuration must reuse the classification response")
		}
		encoded, err := json.Marshal(guests)
		if err != nil || strings.Contains(string(encoded), "synthetic-sensitive") {
			t.Fatalf("unsafe JSON=%s err=%v", encoded, err)
		}
	}
}
