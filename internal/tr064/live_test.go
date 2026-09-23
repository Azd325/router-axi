package tr064

import (
	"os"
	"testing"
	"time"
)

const liveTestFlag = "ROUTER_AXI_LIVE_TEST"

type liveTestSettings struct {
	host, username, password string
}

func liveTestConfig(getenv func(string) string) (liveTestSettings, bool) {
	if getenv(liveTestFlag) != "1" || getenv("CI") != "" {
		return liveTestSettings{}, false
	}
	settings := liveTestSettings{
		host:     getenv("ROUTER_AXI_HOST"),
		username: getenv("ROUTER_AXI_USERNAME"),
		password: getenv("ROUTER_AXI_PASSWORD"),
	}
	if settings.host == "" || settings.username == "" || settings.password == "" {
		return liveTestSettings{}, false
	}
	return settings, true
}

func TestLiveTestSafetyGate(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   bool
	}{
		{name: "ordinary test", values: map[string]string{}},
		{name: "flag alone", values: map[string]string{liveTestFlag: "1"}},
		{name: "wrong flag", values: map[string]string{liveTestFlag: "true", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password"}},
		{name: "CI is always blocked", values: map[string]string{liveTestFlag: "1", "CI": "true", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password"}},
		{name: "explicit local opt-in", values: map[string]string{liveTestFlag: "1", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := liveTestConfig(func(key string) string { return test.values[key] })
			if got != test.want {
				t.Fatalf("enabled = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLiveReadOnlyCommands(t *testing.T) {
	settings, enabled := liveTestConfig(os.Getenv)
	if !enabled {
		t.Skip("live router tests require explicit local opt-in and complete configuration")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}

	if _, err := client.Status(t.Context()); err != nil {
		t.Fatal("status live read failed")
	}
	wan, err := client.WAN(t.Context())
	if err != nil {
		t.Fatal("wan live read failed")
	}
	if wan.IPFamily != "ipv4" && wan.IPFamily != "ipv6" && wan.IPFamily != "unknown" {
		t.Fatal("wan live read returned an invalid IP family")
	}
	traffic, err := client.Traffic(t.Context())
	if err != nil {
		t.Fatal("traffic live read failed")
	}
	if _, err := time.Parse(time.RFC3339, traffic.ObservedAt); err != nil {
		t.Fatal("traffic live read returned an invalid observation time")
	}
	if _, err := client.Calls(t.Context()); err != nil {
		t.Fatal("calls live read failed")
	}
}
