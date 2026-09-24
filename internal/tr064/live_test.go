package tr064

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const liveTestFlag = "ROUTER_AXI_LIVE_TEST"

// liveMutationTestFlag is an additional explicit opt-in for live mutation
// coverage. It is deliberately separate from liveTestFlag and is never
// honored on CI or in ordinary test runs.
const liveMutationTestFlag = "ROUTER_AXI_LIVE_MUTATION_TEST"
const liveMutationInstanceFlag = "ROUTER_AXI_WIFI_INSTANCE"

// liveBackupTestFlag is an additional explicit opt-in for live configuration
// export coverage. It is deliberately separate from liveTestFlag and is
// never honored on CI or in ordinary test runs. The live backup test never
// writes a backup file: it exercises the documented action and download and
// discards the payload.
const liveBackupTestFlag = "ROUTER_AXI_LIVE_BACKUP_TEST"
const backupPassphraseFlag = "ROUTER_AXI_BACKUP_PASSWORD"

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

func TestLiveWiFi(t *testing.T) {
	settings, enabled := liveTestConfig(os.Getenv)
	if !enabled {
		t.Skip("live router tests require explicit local opt-in and complete configuration")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}
	if err := client.discover(t.Context()); err != nil {
		t.Fatal("Wi-Fi capability discovery failed")
	}
	capability := client.advertisedCapability([]string{wlanServicePrefix}, wifiRemediation)
	radios, err := client.WiFi(t.Context())
	if capability.State == "unsupported" {
		var protocolErr *Error
		if radios != nil || !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" {
			t.Fatal("absent Wi-Fi capability was not reported as unsupported")
		}
		return
	}
	if err != nil || len(radios) == 0 {
		t.Fatal("advertised Wi-Fi capability could not be read")
	}
	bands := map[string]bool{"2400": true, "5000": true, "6000": true, "unknown": true}
	modes := map[string]bool{"None": true, "Basic": true, "WPA": true, "11i": true, "WPAand11i": true, "WPA3": true, "11iandWPA3": true, "OWE": true, "OWETrans": true, "unknown": true}
	standards := map[string]bool{"b": true, "g": true, "n": true, "ac": true, "ax": true, "be": true, "unknown": true}
	var previous uint64
	for i, radio := range radios {
		id, err := strconv.ParseUint(strings.TrimPrefix(radio.ServiceID, wlanIDPrefix), 10, 64)
		if !strings.HasPrefix(radio.ServiceID, wlanIDPrefix) || err != nil || (i > 0 && id <= previous) {
			t.Fatal("Wi-Fi live read returned invalid or unordered service IDs")
		}
		previous = id
		if !bands[radio.Band] || !modes[radio.SecurityMode] || !standards[radio.Standard] || radio.Channel > 255 || radio.AssociatedDevices > 65535 {
			t.Fatal("Wi-Fi live read returned invalid fields")
		}
	}
}

func TestLiveForwards(t *testing.T) {
	settings, enabled := liveTestConfig(os.Getenv)
	if !enabled {
		t.Skip("live router tests require explicit local opt-in and complete configuration")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}
	forwards, err := client.Forwards(t.Context())
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "unsupported" && forwards == nil && (strings.Contains(protocolErr.Message, forwardsRemediation) || strings.Contains(protocolErr.Message, "Layer3Forwarding")) {
			t.Skip("documented port-mapping enumeration is unsupported on this WAN connection; hardware mappings were not validated")
		}
		t.Fatal("port-forward live inspection failed")
	}
	if forwards == nil || len(forwards) > maxPortMappingEntries {
		t.Fatal("port-forward live read returned an invalid list")
	}
	for i, forward := range forwards {
		if (forward.Protocol != "TCP" && forward.Protocol != "UDP") || forward.ExternalPort == 0 || forward.ExternalPort > 65535 || forward.InternalPort == 0 || forward.InternalPort > 65535 || forward.InternalClient == "" || (forward.LeaseDuration != nil && *forward.LeaseDuration > 4294967295) {
			t.Fatal("port-forward live read returned invalid fields")
		}
		if i > 0 && compareForwards(forwards[i-1], forward) > 0 {
			t.Fatal("port-forward live read returned an unordered list")
		}
	}
}

func TestLiveLeases(t *testing.T) {
	settings, enabled := liveTestConfig(os.Getenv)
	if !enabled {
		t.Skip("live router tests require explicit local opt-in and complete configuration")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}
	leases, err := client.Leases(t.Context())
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "unsupported" && leases == nil {
			t.Skip("documented Hosts observation is unsupported; hardware lease metadata was not validated")
		}
		t.Fatal("lease observation live read failed")
	}
	if leases == nil || len(leases) > maxHostEntries {
		t.Fatal("lease observation returned an invalid list")
	}
	for _, lease := range leases {
		if lease.AddressSource != "DHCP" && lease.AddressSource != "Static" && lease.AddressSource != "unknown" {
			t.Fatal("lease observation returned an invalid address source")
		}
		if lease.LeaseTimeRemaining != nil && (*lease.LeaseTimeRemaining <= 0 || *lease.LeaseTimeRemaining >= 2147483647) {
			t.Fatal("lease observation returned a non-finite or invalid remaining time")
		}
	}
}

func TestLiveBackupSafetyGate(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   bool
	}{
		{name: "ordinary test", values: map[string]string{}},
		{name: "live gate only", values: map[string]string{liveTestFlag: "1", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password", backupPassphraseFlag: "passphrase"}},
		{name: "backup flag alone", values: map[string]string{liveBackupTestFlag: "1", backupPassphraseFlag: "passphrase"}},
		{name: "wrong backup flag", values: map[string]string{liveTestFlag: "1", liveBackupTestFlag: "true", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password", backupPassphraseFlag: "passphrase"}},
		{name: "missing passphrase", values: map[string]string{liveTestFlag: "1", liveBackupTestFlag: "1", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password"}},
		{name: "CI is always blocked", values: map[string]string{liveTestFlag: "1", liveBackupTestFlag: "1", "CI": "true", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password", backupPassphraseFlag: "passphrase"}},
		{name: "explicit local opt-in", values: map[string]string{liveTestFlag: "1", liveBackupTestFlag: "1", "ROUTER_AXI_HOST": "router.test", "ROUTER_AXI_USERNAME": "user", "ROUTER_AXI_PASSWORD": "password", backupPassphraseFlag: "passphrase"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, got := liveBackupConfig(func(key string) string { return test.values[key] })
			if got != test.want {
				t.Fatalf("enabled = %t, want %t", got, test.want)
			}
		})
	}
}

func liveBackupConfig(getenv func(string) string) (liveTestSettings, string, bool) {
	settings, enabled := liveTestConfig(getenv)
	if !enabled || getenv(liveBackupTestFlag) != "1" {
		return liveTestSettings{}, "", false
	}
	passphrase := getenv(backupPassphraseFlag)
	if passphrase == "" {
		return liveTestSettings{}, "", false
	}
	return settings, passphrase, true
}

func TestLiveBackup(t *testing.T) {
	settings, passphrase, enabled := liveBackupConfig(os.Getenv)
	if !enabled {
		t.Skip("live backup coverage requires ROUTER_AXI_LIVE_TEST=1 plus ROUTER_AXI_LIVE_BACKUP_TEST=1, complete configuration, and ROUTER_AXI_BACKUP_PASSWORD")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}
	// The export payload is deliberately neither written to disk nor logged:
	// this test reports only pass/fail at the command level.
	export, err := client.ConfigExport(t.Context(), passphrase)
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) {
			if protocolErr.Kind == "unsupported" {
				t.Skip("DeviceConfig:X_AVM-DE_GetConfigFile is unsupported; the hardware export was not validated")
			}
			if protocolErr.Code == tlsUntrustedCode {
				t.Skip("the router certificate is not trusted locally; the hardware export was not validated")
			}
		}
		t.Fatal("the live configuration export failed")
	}
	if len(export) == 0 || int64(len(export)) > maxConfigExportBytes {
		t.Fatal("the live configuration export returned an invalid payload")
	}
}

func TestLiveWiFiMutation(t *testing.T) {
	settings, enabled := liveTestConfig(os.Getenv)
	if !enabled || os.Getenv(liveMutationTestFlag) != "1" {
		t.Skip("live mutation coverage requires ROUTER_AXI_LIVE_TEST=1 plus an additional explicit ROUTER_AXI_LIVE_MUTATION_TEST=1 opt-in and complete configuration")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}
	ctx := t.Context()
	services, err := client.wlanServices(ctx)
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "unsupported" {
			t.Skip("WLANConfiguration is unsupported; hardware mutation was not validated")
		}
		t.Fatal("Wi-Fi capability discovery failed")
	}
	var instance uint64
	if explicit := strings.TrimSpace(os.Getenv(liveMutationInstanceFlag)); explicit != "" {
		instance, err = strconv.ParseUint(explicit, 10, 64)
		if err != nil || instance == 0 {
			t.Fatal("live mutation instance must be a WLANConfiguration number of 1 or greater")
		}
	} else if len(services) == 1 {
		// The single instance is unambiguous.
	} else {
		t.Skip("multiple WLAN instances require an explicit instance; hardware mutation was not validated")
	}
	// Never log SSIDs, radio data, or addresses: this test reports only
	// pass/fail at the command level.
	result, err := client.WiFiMutation(ctx, instance, true, false)
	if err != nil || !result.Preview {
		t.Fatal("live Wi-Fi state read failed")
	}
	original := result.Current
	// Restore the original state even when the toggle-verification itself
	// fails, so the live test can never leave the radio toggled. t.Context
	// is canceled before Cleanup runs, so the restore uses its own context.
	restored := false
	t.Cleanup(func() {
		if !restored {
			cleanup, err := client.WiFiMutation(context.Background(), instance, original, true)
			if err != nil || cleanup.Preview || cleanup.Current != original {
				t.Error("live Wi-Fi mutation cleanup could not restore the original radio state")
			}
		}
	})
	// Toggle, then restore, and require the router to confirm both changes.
	toggled, err := client.WiFiMutation(ctx, instance, !original, true)
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "unsupported" {
			t.Skip("WLANConfiguration:SetEnable is unsupported; hardware mutation was not validated")
		}
		t.Fatal("live Wi-Fi mutation was not confirmed by the router")
	}
	if toggled.Preview || !toggled.Changed || toggled.Current != !original {
		t.Fatal("live Wi-Fi mutation was not confirmed by the router")
	}
	cleanup, err := client.WiFiMutation(ctx, instance, original, true)
	restored = err == nil && !cleanup.Preview && cleanup.Current == original
	if !restored {
		t.Fatal("live Wi-Fi mutation did not restore the original radio state")
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

	doctor, err := client.Doctor(t.Context())
	if err != nil {
		t.Fatal("doctor live read failed")
	}
	if doctor.Reachability.State != "reachable" || doctor.Protocol.State != "available" || doctor.Authentication.State != "authenticated" || doctor.Model == "" || doctor.Firmware == "" {
		t.Fatal("doctor live read returned incomplete checks")
	}
	if doctor.Capabilities.Status.State != "advertised" || doctor.Capabilities.Overview.State != "advertised" || doctor.Capabilities.WAN.State != "advertised" || doctor.Capabilities.Traffic.State != "advertised" || doctor.Capabilities.Calls.State != "advertised" || doctor.Capabilities.Devices.State != "advertised" || doctor.Capabilities.Leases.State != "advertised" {
		t.Fatal("doctor live read returned an unexpected capability state")
	}
	if _, err := client.Status(t.Context()); err != nil {
		t.Fatal("status live read failed")
	}
	if _, err := client.Overview(t.Context()); err != nil {
		t.Fatal("overview live read failed")
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
	if _, err := client.Devices(t.Context()); err != nil {
		t.Fatal("devices live read failed")
	}
}
