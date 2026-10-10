package app

import (
	"bytes"
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) DeviceDetail(_ context.Context, address netip.Addr) (tr064.DeviceDetail, error) {
	port, speed := uint64(2), uint64(1000)
	guest, vpn, available := false, false, true
	access, result := "granted", "succeeded"
	return tr064.DeviceDetail{Name: "synthetic-host", IPAddress: address.String(), MACAddress: "02:00:00:00:00:20", InterfaceType: "Ethernet", Active: true, Port: &port, SpeedMbps: &speed, Guest: &guest, VPN: &vpn, WANAccess: &access, UpdateAvailable: &available, UpdateSuccessful: &result}, f.err
}

type unknownDeviceDetailReader struct{ fakeReader }

func (unknownDeviceDetailReader) DeviceDetail(_ context.Context, address netip.Addr) (tr064.DeviceDetail, error) {
	return tr064.DeviceDetail{IPAddress: address.String()}, nil
}

func TestDeviceDetailOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"devices", "detail", "--ip", "192.0.2.20"}, "device_detail:\n  name: synthetic-host\n  ip_address: 192.0.2.20\n  mac_address: 02:00:00:00:00:20\n  interface_type: Ethernet\n  active: true\n  port: 2\n  speed_mbps: 1000\n  guest: false\n  vpn: false\n  wan_access: granted\n  update_available: true\n  update_successful: succeeded\n"},
		{[]string{"devices", "--ip", "192.0.2.20", "detail", "--json"}, `{"name":"synthetic-host","ip_address":"192.0.2.20","mac_address":"02:00:00:00:00:20","interface_type":"Ethernet","active":true,"port":2,"speed_mbps":1000,"guest":false,"vpn":false,"wan_access":"granted","update_available":true,"update_successful":"succeeded"}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout, stderr)
		}
	}
}

func TestDeviceDetailUnknownOutput(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"devices", "detail", "--ip", "192.0.2.20"}, "device_detail:\n  name: \"\"\n  ip_address: 192.0.2.20\n  mac_address: \"\"\n  interface_type: \"\"\n  active: false\n  port: unknown\n  speed_mbps: unknown\n  guest: unknown\n  vpn: unknown\n  wan_access: unknown\n  update_available: unknown\n  update_successful: unknown\n"},
		{[]string{"devices", "detail", "--ip", "192.0.2.20", "--json"}, `{"ip_address":"192.0.2.20","mac_address":"","interface_type":"","active":false,"port":null,"speed_mbps":null,"guest":null,"vpn":null,"wan_access":null,"update_available":null,"update_successful":null}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return unknownDeviceDetailReader{}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestDevicesListIsUnchangedByDetail(t *testing.T) {
	code, stdout, stderr := runTest(t, "devices")
	if code != ExitOK || stderr != "" || !strings.HasPrefix(stdout, "devices[") || !strings.Contains(stdout, "{name,ip_address,mac_address,interface_type,active}:") || strings.Contains(stdout, "device_detail") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestDeviceDetailTargetIsExactlyOneAddress(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("invalid input contacted router"); return nil, nil }, func(string) string { t.Fatal("invalid input read credentials"); return "" })
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"devices", "detail"}, "devices detail requires --ip ADDRESS"},
		{[]string{"devices", "detail", "--ip"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "--json"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "192.0.2.20", "--ip", "192.0.2.21"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "192.0.2.20", "--ip", "192.0.2.20"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "192.0.2.20,192.0.2.21"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "192.0.2.0/24"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "synthetic-host"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "2001:db8::20"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "::ffff:192.0.2.20"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "--ip", "192.0.2.020"}, "--ip requires exactly one IPv4 address"},
		{[]string{"devices", "detail", "192.0.2.20"}, "devices accepts one action: detail; select the device with --ip ADDRESS"},
		{[]string{"devices", "detail", "detail", "--ip", "192.0.2.20"}, "devices accepts one action: detail; select the device with --ip ADDRESS"},
		{[]string{"devices", "detail", "--ip", "192.0.2.20", "--all"}, "--all is not valid with devices detail; it reports exactly one device"},
		{[]string{"devices", "--ip", "192.0.2.20"}, "--ip is valid only with devices detail"},
		{[]string{"leases", "--ip", "192.0.2.20"}, "--ip is valid only with devices detail"},
		{[]string{"devices", "192.0.2.20"}, "exactly one command is required"},
		{[]string{"devices", "detail", "--ip", "192.0.2.20", "--confirm"}, "--confirm is valid only with firmware check, reboot, wake, wan reconnect, wifi enable, or wifi disable"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			args := test.args
			if jsonOutput {
				args = append(append([]string{}, args...), "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != ExitUsage || !strings.Contains(stdout.String(), test.want) || strings.Contains(stdout.String(), "192.0.2.2") {
				t.Fatalf("args=%q code=%d stdout=%q", args, code, stdout.String())
			}
			assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "invalid_arguments")
		}
	}
}

func TestDeviceDetailHelp(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("help contacted router"); return nil, nil }, func(string) string { t.Fatal("help read credentials"); return "" })
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"devices", "detail", "--help"}, []string{"usage: router-axi devices detail --ip ADDRESS", "Hosts:X_AVM-DE_GetSpecificHostEntryByIP", "App or Phone right", "router-axi devices detail --ip 192.0.2.20 --json"}},
		{[]string{"devices", "--help"}, []string{"usage: router-axi devices [--all] [--fields NAMES] [detail --ip ADDRESS]", "router-axi devices detail --ip 192.0.2.20"}},
		{[]string{"--help"}, []string{"devices detail adds one device's link and access state"}},
	} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
		for _, want := range test.want {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("args=%q stdout=%q missing=%q", test.args, stdout.String(), want)
			}
		}
	}
}

func TestDeviceDetailStructuredErrors(t *testing.T) {
	for _, test := range []struct {
		err  tr064.Error
		exit int
		code string
		hint string
	}{
		{tr064.Error{Kind: "auth"}, ExitAuth, "authentication_failed", ""},
		{tr064.Error{Kind: "network"}, ExitNetwork, "router_unreachable", ""},
		{tr064.Error{Kind: "unsupported"}, ExitUnsupported, "unsupported_capability", ""},
		{tr064.Error{Kind: "protocol"}, ExitRouter, "router_protocol_error", ""},
		{tr064.Error{Kind: "router"}, ExitRouter, "router_protocol_error", ""},
		{tr064.Error{Kind: "usage", Code: "unknown_device"}, ExitUsage, "unknown_device", "router-axi devices"},
		{tr064.Error{Kind: "usage", Code: "invalid_device_address"}, ExitUsage, "invalid_device_address", "router-axi devices"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			failure := test.err
			failure.Operation, failure.Message = "devices detail", "device detail inspection failed"
			application := New(func(Config) (Reader, error) { return fakeReader{err: &failure}, nil }, func(string) string { return "" })
			args := []string{"devices", "detail", "--ip", "192.0.2.20"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != test.exit || !strings.Contains(stdout.String(), test.hint) || strings.Contains(stdout.String(), "192.0.2.20") || strings.Contains(stdout.String(), "synthetic-host") {
				t.Fatalf("kind=%s json=%t code=%d stdout=%q", test.err.Kind, jsonOutput, code, stdout.String())
			}
			assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, test.code)
		}
	}
}
