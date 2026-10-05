package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func TestWiFiConfigurationCompactOutput(t *testing.T) {
	yes, no := true, false
	channels, schedule, mode, status := "36,40", "<night>synthetic</night>", "pbc", "inactive"
	value := tr064.RadioDetail{
		ChannelConfiguration: &tr064.WiFiChannelConfiguration{PossibleChannels: &channels, AutoChannelEnabled: &yes},
		BeaconAdvertisement:  &tr064.WiFiBeaconAdvertisement{Enabled: &yes},
		NightControl:         &tr064.WiFiNightControl{Schedule: &schedule, NoForcedOff: &no},
		WPS:                  &tr064.WiFiWPS{Mode: &mode, Status: &status},
		IPTVOptimization:     &tr064.WiFiIPTVOptimization{Enabled: &no},
	}
	var output bytes.Buffer
	if err := writeWiFiConfiguration(&output, value); err != nil {
		t.Fatal(err)
	}
	want := "  channel_configuration:\n    possible_channels: 36,40\n    auto_channel_enabled: true\n  beacon_advertisement:\n    enabled: true\n  night_control:\n    schedule: <night>synthetic</night>\n    no_forced_off: false\n  wps:\n    mode: pbc\n    status: inactive\n  iptv_optimization:\n    enabled: false\n"
	if output.String() != want {
		t.Fatalf("output = %q", output.String())
	}
	output.Reset()
	value.NightControl = &tr064.WiFiNightControl{}
	value.ChannelConfiguration, value.BeaconAdvertisement, value.WPS, value.IPTVOptimization = nil, nil, nil, nil
	if err := writeWiFiConfiguration(&output, value); err != nil {
		t.Fatal(err)
	}
	want = "  channel_configuration: unsupported\n  beacon_advertisement: unsupported\n  night_control:\n    schedule: unknown\n    no_forced_off: unknown\n  wps: unsupported\n  iptv_optimization: unsupported\n"
	if output.String() != want {
		t.Fatalf("partial output = %q", output.String())
	}
}

func TestGuestConfigurationOutputPreservesRawStringsAndFieldOrder(t *testing.T) {
	active, timeout, remain, off, isolation := "1", "90", "42", "router,raw", "1"
	guest := tr064.GuestNetwork{
		ServiceID: "urn:WLANConfiguration-com:serviceId:WLANConfiguration2", SSID: "synthetic-guest",
		Configuration: &tr064.GuestConfiguration{TimeoutActive: &active, Timeout: &timeout, TimeRemain: &remain, NoForcedOff: &off, UserIsolation: &isolation},
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
			want := "guest_configuration[1]{service_id,timeout_active,timeout,time_remain,no_forced_off,user_isolation}:\n  urn:WLANConfiguration-com:serviceId:WLANConfiguration2,1,90,42,\"router,raw\",1\n"
			if !strings.HasSuffix(stdout.String(), want) {
				t.Fatalf("output = %q", stdout.String())
			}
		}
	}
}
