package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) Exposure(context.Context) (tr064.Exposure, error) {
	if f.err != nil {
		return tr064.Exposure{}, f.err
	}
	enabled, disabled, port := true, false, uint64(2121)
	return tr064.Exposure{
		Remote: tr064.RemoteExposure{
			RemoteAccess: &tr064.RemoteAccessExposure{Enabled: true, Port: 443, LetsEncryptEnabled: &enabled, LetsEncryptState: "valid"},
			DDNS:         &tr064.DDNSExposure{StatusIPv4: "offline", StatusIPv6: "new-address"},
			MyFRITZ:      &tr064.MyFRITZExposure{Enabled: true, Port: 8443, DeviceRegistered: true, State: "dyndns_verified"},
		},
		Local: tr064.LocalExposure{
			Storage:   &tr064.StorageExposure{FTPEnabled: true, FTPStatus: "Enable", FTPWANEnabled: &enabled, FTPWANSSLOnly: &disabled, FTPWANPort: &port},
			UPnP:      &tr064.UPnPExposure{Enabled: true},
			WebDAV:    &tr064.WebDAVExposure{},
			Speedtest: &tr064.SpeedtestExposure{TCPEnabled: true, WANUDPEnabled: true, TCPPort: 4711, UDPPort: 4712, UDPBidirectPort: 4713},
			TR069:     &tr064.TR069Exposure{PeriodicInformEnabled: true},
		},
	}, nil
}

type partialExposureReader struct {
	fakeReader
	exposure tr064.Exposure
}

func (r partialExposureReader) Exposure(context.Context) (tr064.Exposure, error) {
	return r.exposure, nil
}

func TestExposureOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"exposure"}, "exposure:\n  remote:\n    remote_access:\n      enabled: true\n      port: 443\n      letsencrypt_enabled: true\n      letsencrypt_state: valid\n    ddns:\n      enabled: false\n      status_ipv4: offline\n      status_ipv6: new-address\n    myfritz:\n      enabled: true\n      port: 8443\n      device_registered: true\n      state: dyndns_verified\n  local:\n    storage:\n      ftp_enabled: true\n      ftp_status: Enable\n      smb_enabled: false\n      ftp_wan_enabled: true\n      ftp_wan_ssl_only: false\n      ftp_wan_port: 2121\n    upnp:\n      enabled: true\n      media_server_enabled: false\n    webdav:\n      enabled: false\n    speedtest:\n      tcp_enabled: true\n      udp_enabled: false\n      udp_bidirect_enabled: false\n      wan_tcp_enabled: false\n      wan_udp_enabled: true\n      tcp_port: 4711\n      udp_port: 4712\n      udp_bidirect_port: 4713\n    tr069:\n      periodic_inform_enabled: true\n      upgrades_managed: false\n"},
		{[]string{"exposure", "--json"}, `{"remote":{"remote_access":{"enabled":true,"port":443,"letsencrypt_enabled":true,"letsencrypt_state":"valid"},"ddns":{"enabled":false,"status_ipv4":"offline","status_ipv6":"new-address"},"myfritz":{"enabled":true,"port":8443,"device_registered":true,"state":"dyndns_verified"}},"local":{"storage":{"ftp_enabled":true,"ftp_status":"Enable","smb_enabled":false,"ftp_wan_enabled":true,"ftp_wan_ssl_only":false,"ftp_wan_port":2121},"upnp":{"enabled":true,"media_server_enabled":false},"webdav":{"enabled":false},"speedtest":{"tcp_enabled":true,"udp_enabled":false,"udp_bidirect_enabled":false,"wan_tcp_enabled":false,"wan_udp_enabled":true,"tcp_port":4711,"udp_port":4712,"udp_bidirect_port":4713},"tr069":{"periodic_inform_enabled":true,"upgrades_managed":false}}}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestExposureUnsupportedAndUnknownOutput(t *testing.T) {
	partial := tr064.Exposure{
		Remote: tr064.RemoteExposure{RemoteAccess: &tr064.RemoteAccessExposure{Port: 443, LetsEncryptState: "unknown"}},
		Local:  tr064.LocalExposure{Storage: &tr064.StorageExposure{FTPStatus: "Disable"}},
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"exposure"}, "exposure:\n  remote:\n    remote_access:\n      enabled: false\n      port: 443\n      letsencrypt_enabled: unknown\n      letsencrypt_state: unknown\n    ddns: unsupported\n    myfritz: unsupported\n  local:\n    storage:\n      ftp_enabled: false\n      ftp_status: Disable\n      smb_enabled: false\n      ftp_wan_enabled: unknown\n      ftp_wan_ssl_only: unknown\n      ftp_wan_port: unknown\n    upnp: unsupported\n    webdav: unsupported\n    speedtest: unsupported\n    tr069: unsupported\n"},
		{[]string{"exposure", "--json"}, `{"remote":{"remote_access":{"enabled":false,"port":443,"letsencrypt_enabled":null,"letsencrypt_state":"unknown"},"ddns":null,"myfritz":null},"local":{"storage":{"ftp_enabled":false,"ftp_status":"Disable","smb_enabled":false,"ftp_wan_enabled":null,"ftp_wan_ssl_only":null,"ftp_wan_port":null},"upnp":null,"webdav":null,"speedtest":null,"tr069":null}}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return partialExposureReader{exposure: partial}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestExposureUsage(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("usage error contacted router"); return nil, nil }, func(string) string { return "" })
	for _, args := range [][]string{{"exposure", "remote"}, {"exposure", "--all"}, {"exposure", "--confirm"}, {"exposure", "--instance", "1"}} {
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), args, &stdout, &stderr); code != ExitUsage || !strings.Contains(stdout.String(), "invalid_arguments") {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"exposure", "--port"}, &stdout, &stderr); code != ExitUsage || !strings.Contains(stdout.String(), "valid flags for exposure: --host, --json, --help") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExposureHelp(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("help contacted router"); return nil, nil }, func(string) string { t.Fatal("help read credentials"); return "" })
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"exposure", "--help"}, &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 || !strings.HasPrefix(stdout.String(), "usage: router-axi exposure [--host ADDRESS] [--json] [--help]\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"X_AVM-DE_RemoteAccess:GetInfo and GetDDNSInfo", "X_AVM-DE_MyFritz:GetInfo", "X_AVM-DE_Storage", "X_AVM-DE_UPnP", "X_AVM-DE_WebDAVClient", "X_AVM-DE_Speedtest", "ManagementServer", "unsupported (null in JSON)", "never kept or printed", "examples: router-axi exposure; router-axi exposure --json\n"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help lacks %q: %q", want, stdout.String())
		}
	}
}

func TestExposureStructuredErrors(t *testing.T) {
	for _, test := range []struct {
		err  *tr064.Error
		code int
		want string
	}{
		{&tr064.Error{Kind: "unsupported", Operation: "exposure", Message: "router supports none of the documented exposure reads"}, ExitUnsupported, "unsupported_capability"},
		{&tr064.Error{Kind: "router", Operation: "exposure", FaultCode: "606", Message: "router denied X_AVM-DE_Storage:GetInfo; the logged-in account needs the App right"}, ExitRouter, "the logged-in account needs the App right"},
		{&tr064.Error{Kind: "auth", Operation: "exposure", Message: "exposure inspection failed"}, ExitAuth, "authentication_failed"},
	} {
		for _, args := range [][]string{{"exposure"}, {"exposure", "--json"}} {
			var stdout, stderr bytes.Buffer
			code := New(func(Config) (Reader, error) { return fakeReader{err: test.err}, nil }, func(string) string { return "" }).Run(t.Context(), args, &stdout, &stderr)
			if code != test.code || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.want) || strings.Contains(stdout.String(), "remote_access") {
				t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
			}
		}
	}
}
