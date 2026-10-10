package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

type firmwareStatusReader struct{ fakeReader }

func (firmwareStatusReader) Status(context.Context) (tr064.Status, error) {
	return tr064.Status{Manufacturer: "AVM", Model: "7590", Software: "8.20", Hardware: "true", Serial: "0123"}, nil
}

func runWithReader(t *testing.T, reader Reader, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	code := application.Run(t.Context(), args, &stdout, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	return code, stdout.String()
}

func TestAmbiguousScalarsAreQuoted(t *testing.T) {
	for value, want := range map[string]bool{
		"8.20": true, "8.02": true, "0": true, "-1": true, "1e3": true, "007": true, "true": true, "false": true, "null": true,
		"FRITZ!Box 7590 AX": false, "192.0.2.10": false, "02:00:00:00:00:10": false, "": false, "TRUE": false, "1.2.3": false, "7590AX": false,
	} {
		if got := ambiguousScalar(value); got != want {
			t.Errorf("ambiguousScalar(%q)=%t want %t", value, got, want)
		}
	}
}

func TestNumberLikeTextIsQuotedInCompactOutput(t *testing.T) {
	code, out := runWithReader(t, firmwareStatusReader{}, "status")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"  model: \"7590\"\n", "  software: \"8.20\"\n", "  hardware: \"true\"\n", "  serial: \"0123\"\n", "  manufacturer: AVM\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	code, out = runWithReader(t, fakeReader{}, "wifi")
	if code != ExitOK || !strings.Contains(out, ",true,\"2400\",6,2\n") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestSetupAllPrintsOneTable(t *testing.T) {
	application := setupApp(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"setup", "check", "--agent", "all"}, &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "setup[3]{agent,state,command,codex_hooks_feature}:" || strings.Count(stdout.String(), "setup") != 1 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	for i, agent := range []string{"claude", "codex", "opencode"} {
		if !strings.HasPrefix(lines[i+1], "  "+agent+",") {
			t.Errorf("row %d=%q", i, lines[i+1])
		}
	}
	stdout.Reset()
	if code := application.Run(t.Context(), []string{"setup", "check", "--agent", "claude"}, &stdout, &stderr); code != ExitOK || !strings.HasPrefix(stdout.String(), "setup:\n  agent: claude\n") {
		t.Fatalf("single agent stdout=%q", stdout.String())
	}
}

func TestFieldsSelectAndOrderColumns(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"leases", "--fields", "lease_time_remaining,name"}, "leases[2]{lease_time_remaining,name}:\n  unknown,sanitized-static-device\n  3600,\"\"\n"},
		{[]string{"--fields", "ssid,service_id", "wifi"}, "radios[1]{ssid,service_id}:\n  synthetic-ap,\"urn:WLANConfiguration-com:serviceId:WLANConfiguration1\"\nnext: router-axi wifi detail\n"},
		{[]string{"wifi", "--fields", "service_id,ssid"}, "radios[1]{service_id,ssid}:\n  \"urn:WLANConfiguration-com:serviceId:WLANConfiguration1\",synthetic-ap\n"},
		{[]string{"forwards", "--fields", "description"}, "forwards[1]{description}:\n  synthetic service\n"},
		{[]string{"calls", "--fields", "id,date"}, "calls[1]{id,date}:\n  \"12\",\"10.03.24 12:34\"\n"},
	} {
		code, out := runWithReader(t, fakeReader{}, test.args...)
		if code != ExitOK || !strings.HasPrefix(out, test.want) {
			t.Errorf("args=%v code=%d out=%q", test.args, code, out)
		}
	}
	_, withFields := runWithReader(t, fakeReader{}, "devices", "--json", "--fields", "name")
	_, without := runWithReader(t, fakeReader{}, "devices", "--json")
	if withFields != without || !strings.Contains(without, "interface_type") {
		t.Errorf("--json with --fields changed output: %q", withFields)
	}
}

func TestFieldsRejectUnknownDuplicateAndMisplacedNames(t *testing.T) {
	for _, test := range []struct {
		args   []string
		wanted []string
	}{
		{[]string{"leases", "--fields", "name,bogus"}, []string{"unknown field for leases: \"bogus\"", "valid fields: name, ip_address, mac_address, address_source, lease_time_remaining, interface_type, active"}},
		{[]string{"wifi", "--fields", "name"}, []string{"valid fields: service_id, ssid, enabled, channel, band, standard, associated_devices, security_mode"}},
		{[]string{"calls", "--fields", "id,id"}, []string{"--fields lists id more than once"}},
		{[]string{"devices", "--fields", "name,"}, []string{"unknown field for devices: \"\""}},
		{[]string{"status", "--fields", "name"}, []string{"--fields is valid only with"}},
		{[]string{"wifi", "detail", "--fields", "ssid"}, []string{"--fields is valid only with"}},
		{[]string{"leases", "--fields"}, []string{"--fields requires one comma-separated list"}},
	} {
		code, out := runWithReader(t, fakeReader{}, test.args...)
		if code != ExitUsage || !strings.HasPrefix(out, "error:\n  code: invalid_arguments\n") {
			t.Errorf("args=%v code=%d out=%q", test.args, code, out)
		}
		for _, want := range test.wanted {
			if !strings.Contains(out, want) {
				t.Errorf("args=%v missing %q in %q", test.args, want, out)
			}
		}
	}
	code, out := runWithReader(t, fakeReader{}, "leases", "--fields", "bogus")
	if code != ExitUsage || !strings.Contains(out, "  hint: router-axi leases --fields name,ip_address\n") {
		t.Errorf("hint missing: %q", out)
	}
	code, out = runWithReader(t, fakeReader{}, "leases", "--fields", "bogus", "--help")
	if code != ExitOK || !strings.HasPrefix(out, "usage: router-axi leases") {
		t.Errorf("help must win over fields validation: %q", out)
	}
}

func TestHintsCarryHostOnlyWhenPassed(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"status"}, "next[3]: router-axi wan,router-axi devices,router-axi doctor\n"},
		{[]string{"--host", "router.test"}, "next[3]: router-axi wan --host router.test,router-axi devices --host router.test,router-axi doctor --host router.test\n"},
		{[]string{"wan", "--host", "router.test"}, "next: router-axi traffic --host router.test\n"},
		{[]string{"devices"}, "next: router-axi devices detail --ip <ip>\n"},
		{[]string{"devices", "--host", "router.test"}, "next: router-axi devices detail --ip <ip> --host router.test\n"},
		{[]string{"wifi", "--host", "router.test"}, "next: router-axi wifi detail --host router.test\n"},
		{[]string{"wifi", "disable", "--instance", "1", "--confirm", "--host", "router.test"}, "next: router-axi wifi --host router.test\n"},
	} {
		code, out := runWithReader(t, &wifiMutationReader{result: tr064.WiFiMutation{Instance: "urn:WLANConfiguration-com:serviceId:WLANConfiguration1", Action: "disable", Previous: true}}, test.args...)
		if code != ExitOK || !strings.HasSuffix(out, test.want) {
			t.Errorf("args=%v code=%d out=%q", test.args, code, out)
		}
	}
	code, out := runWithReader(t, manyDevicesReader{}, "devices", "--host", "router.test")
	if code != ExitOK || !strings.HasSuffix(out, "omitted: 3\nnext[2]: router-axi devices --all --host router.test,router-axi devices detail --ip <ip> --host router.test\n") {
		t.Errorf("omitted hints: code=%d out=%q", code, out[max(0, len(out)-200):])
	}
}

func TestHintsNeverRepeatHostUserInformation(t *testing.T) {
	for host, want := range map[string]string{
		"https://user:secret@router.test:49443": " --host https://router.test:49443",
		"user:secret@router.test":               " --host router.test",
		"router.test":                           " --host router.test",
		"":                                      "",
	} {
		got := hostFlag(host)
		if got != want || strings.Contains(got, "secret") {
			t.Errorf("hostFlag(%q)=%q want %q", host, got, want)
		}
	}
	_, out := runWithReader(t, fakeReader{}, "--host", "https://user:secret@router.test", "devices")
	if strings.Contains(out, "secret") || strings.Contains(out, "user") {
		t.Errorf("hint exposed user information: %q", out)
	}
}

func TestUnsupportedCapabilityHintsDoctor(t *testing.T) {
	reader := fakeReader{err: &tr064.Error{Kind: "unsupported", Message: "synthetic"}}
	code, out := runWithReader(t, reader, "dhcp", "--host", "router.test")
	if code != ExitUnsupported || !strings.Contains(out, "  code: unsupported_capability\n") || !strings.HasSuffix(out, "  hint: router-axi doctor --host router.test\n") {
		t.Errorf("code=%d out=%q", code, out)
	}
	code, out = runWithReader(t, reader, "dhcp", "--json")
	if code != ExitUnsupported || !strings.Contains(out, `"hint":"router-axi doctor"`) {
		t.Errorf("json code=%d out=%q", code, out)
	}
}

func TestUsageErrorHintsAreCorrectingCommands(t *testing.T) {
	for _, operation := range []string{"reboot", "wan reconnect", "wake", "firmware check", "backup", "wifi detail", "devices detail", "wifi"} {
		for _, code := range []string{"invalid_configuration", "backup_passphrase_missing", "backup_requires_https", "invalid_wake_target", ""} {
			spec := protocolError(&tr064.Error{Kind: "usage", Code: code, Operation: operation, Message: "synthetic"}, "router.test")
			if !strings.HasPrefix(spec.detail.Hint, "router-axi ") && !strings.HasPrefix(spec.detail.Hint, "set ROUTER_AXI_BACKUP_PASSWORD") || strings.Contains(spec.detail.Hint, "--help") {
				t.Errorf("operation=%q code=%q hint=%q", operation, code, spec.detail.Hint)
			}
		}
	}
	if hint := protocolError(&tr064.Error{Kind: "usage", Operation: "wake", Message: "synthetic"}, "router.test").detail.Hint; hint != "router-axi wake <mac> --host router.test" {
		t.Errorf("hint=%q", hint)
	}
}

type colonRightsReader struct{ fakeReader }

func (colonRightsReader) Account(context.Context) (tr064.Account, error) {
	return tr064.Account{Username: "synthetic", Rights: []tr064.AccountRight{{Path: "App:Phone", Access: "readwrite"}}}, nil
}

func TestColonCellsAreQuotedInEveryTable(t *testing.T) {
	for _, test := range []struct {
		reader Reader
		args   []string
		want   string
	}{
		{fakeReader{}, []string{"leases"}, ",\"02:00:00:00:00:10\",true\n"},
		{fakeReader{}, []string{"guest"}, "  \"urn:WLANConfiguration-com:serviceId:WLANConfiguration2\",unknown,"},
		{colonRightsReader{}, []string{"account"}, "    \"App:Phone\",readwrite\n"},
	} {
		code, out := runWithReader(t, test.reader, test.args...)
		if code != ExitOK || !strings.Contains(out, test.want) {
			t.Errorf("args=%v code=%d out=%q", test.args, code, out)
		}
	}
}

func TestFieldsHintsCarryHost(t *testing.T) {
	for _, fields := range []string{"bogus", "name,name"} {
		code, out := runWithReader(t, fakeReader{}, "leases", "--host", "router.test", "--fields", fields)
		if code != ExitUsage || !strings.HasSuffix(out, "  hint: router-axi leases --fields name,ip_address --host router.test\n") {
			t.Errorf("fields=%q code=%d out=%q", fields, code, out)
		}
	}
}

func TestDoctorUnsupportedHasNoSelfHint(t *testing.T) {
	reader := fakeReader{err: &tr064.Error{Kind: "unsupported", Operation: "doctor", Message: "synthetic"}}
	code, out := runWithReader(t, reader, "doctor", "--host", "router.test", "--json")
	if code != ExitUnsupported || !strings.Contains(out, `"code":"unsupported_capability"`) || strings.Contains(out, "hint") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

func TestOmittedHintCarriesFields(t *testing.T) {
	forwards := make([]tr064.Forward, 103)
	for i := range forwards {
		forwards[i] = tr064.Forward{Protocol: "TCP", ExternalPort: uint64(8000 + i), InternalClient: "192.0.2.1", InternalPort: 80}
	}
	for _, test := range []struct {
		reader  Reader
		command string
		fields  string
	}{
		{manyLeasesReader{}, "leases", "name,lease_time_remaining"},
		{manyCallsReader{}, "calls", "date,duration"},
		{manyDevicesReader{}, "devices", "name,ip_address"},
		{forwardsReader{forwards: forwards}, "forwards", "external_port,description"},
	} {
		code, out := runWithReader(t, test.reader, test.command, "--fields", test.fields, "--host", "router.test")
		want := "router-axi " + test.command + " --all --fields " + test.fields + " --host router.test"
		if code != ExitOK || !strings.Contains(out, want) {
			t.Errorf("%s code=%d out=%q", test.command, code, out)
		}
	}
}

func TestHintToCommandWithoutFieldsDoesNotCarryThem(t *testing.T) {
	code, out := runWithReader(t, fakeReader{}, "wifi", "--fields", "ssid,channel")
	if code != ExitOK || !strings.Contains(out, "next: router-axi wifi detail\n") || strings.Contains(out, "next: router-axi wifi detail --fields") {
		t.Errorf("wifi code=%d out=%q", code, out)
	}
	code, out = runWithReader(t, manyDevicesReader{}, "devices", "--fields", "name")
	if code != ExitOK || strings.Contains(out, "detail --ip <ip> --fields") {
		t.Errorf("devices code=%d out=%q", code, out)
	}
	code, out = runWithReader(t, fakeReader{})
	if code != ExitOK || strings.Contains(out, "--fields") {
		t.Errorf("home code=%d out=%q", code, out)
	}
}

func TestUnparsableHostUserInfoNeverEchoed(t *testing.T) {
	if got := hostFlag("https://user:sec%ret@router.test"); got != " --host <address>" {
		t.Errorf("hostFlag=%q", got)
	}
	code, out := runWithReader(t, fakeReader{}, "leases", "--host", "https://user:sec%ret@router.test", "--fields", "bogus")
	if code != ExitUsage || strings.Contains(out, "sec%ret") || !strings.Contains(out, "--fields name,ip_address --host <address>") {
		t.Errorf("code=%d out=%q", code, out)
	}
}
