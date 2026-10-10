package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func TestWiFiConfigurationCompactOutput(t *testing.T) {
	no := false
	schedule, mode, status := "<night>synthetic</night>", "pbc", "inactive"
	value := tr064.RadioDetail{
		NightControl: &tr064.WiFiNightControl{Schedule: &schedule, NoForcedOff: &no},
		WPS:          &tr064.WiFiWPS{Mode: &mode, Status: &status},
	}
	var output bytes.Buffer
	if err := writeWiFiConfiguration(&output, value); err != nil {
		t.Fatal(err)
	}
	want := "  night_control:\n    schedule: <night>synthetic</night>\n    no_forced_off: false\n  wps:\n    mode: pbc\n    status: inactive\n"
	if output.String() != want {
		t.Fatalf("output = %q", output.String())
	}
	output.Reset()
	value.NightControl = &tr064.WiFiNightControl{}
	value.WPS = nil
	if err := writeWiFiConfiguration(&output, value); err != nil {
		t.Fatal(err)
	}
	want = "  night_control:\n    schedule: unknown\n    no_forced_off: unknown\n  wps: unavailable\n"
	if output.String() != want {
		t.Fatalf("partial output = %q", output.String())
	}
}

func TestGuestConfigurationOutputPreservesRawStringsAndFieldOrder(t *testing.T) {
	active, timeout, remain, off, isolation := "1", "90", "42", "router,raw", "1"
	guest := tr064.GuestNetwork{
		ServiceID: "urn:WLANConfiguration-com:serviceId:WLANConfiguration2", SSID: "synthetic-guest",
		Configuration: tr064.GuestConfiguration{TimeoutActive: &active, Timeout: &timeout, TimeRemain: &remain, NoForcedOff: &off, UserIsolation: &isolation},
	}
	reader := guestReader{guests: []tr064.GuestNetwork{guest}}
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	for _, jsonOutput := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"guest"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if code := application.Run(t.Context(), args, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		if jsonOutput {
			want := `{"guests":[{"service_id":"urn:WLANConfiguration-com:serviceId:WLANConfiguration2","ssid":"synthetic-guest","enabled":false,"channel":0,"band":"","standard":"","associated_clients":0,"security_mode":"","configuration":{"timeout_active":"1","timeout":"90","time_remain":"42","no_forced_off":"router,raw","user_isolation":"1"}}],"total":1}` + "\n"
			if stdout.String() != want {
				t.Fatalf("JSON = %s", stdout.String())
			}
		} else {
			want := "guest_configuration[1]{service_id,timeout_active,timeout,time_remain,no_forced_off,user_isolation}:\n  urn:WLANConfiguration-com:serviceId:WLANConfiguration2,\"1\",\"90\",\"42\",\"router,raw\",\"1\"\n"
			if !strings.HasSuffix(stdout.String(), want) {
				t.Fatalf("output = %q", stdout.String())
			}
		}
	}
}
