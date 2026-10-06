package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

type fakeWANConnectionReader struct {
	fakeReader
	detail tr064.WANDetail
}

func (f fakeWANConnectionReader) WANDetail(context.Context) (tr064.WANDetail, error) {
	return f.detail, nil
}

func runWANConnectionDetail(t *testing.T, detail tr064.WANDetail, args ...string) string {
	t.Helper()
	detail.AccessType, detail.PhysicalLinkStatus, detail.DNSServers = "DSL", "Up", []string{}
	application := New(func(Config) (Reader, error) { return fakeWANConnectionReader{detail: detail}, nil }, func(string) string { return "" })
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), append([]string{"wan", "detail"}, args...), &stdout, &stderr); code != ExitOK || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func TestWANDetailConnectionStateOutputContract(t *testing.T) {
	status, uptime := "Connected", uint64(3600)
	ppp := tr064.WANDetail{
		ConnectionService:    tr064.WANPPPConnectionService,
		Connection:           &tr064.WANConnection{Type: "IP_Routed", IPv6Status: &status, IPv6Uptime: &uptime},
		NAT:                  &tr064.WANNAT{Enabled: true},
		LinkLayerMaxBitRates: &tr064.WANLinkLayerMaxBitRates{Upstream: 40000000, Downstream: 100000000},
		DisconnectPrevention: &tr064.WANDisconnectPrevention{Enabled: true, Hour: 3},
	}
	for _, test := range []struct {
		name         string
		detail       tr064.WANDetail
		compact, raw string
	}{
		{
			"ppp", ppp,
			"  sync_groups: unknown\n  connection_service: WANPPPConnection\n  connection:\n    type: IP_Routed\n    ipv6_status: Connected\n    ipv6_uptime: 3600\n  nat:\n    enabled: true\n    rsip_available: false\n  link_layer_max_bit_rates:\n    upstream: 40000000\n    downstream: 100000000\n  disconnect_prevention:\n    enabled: true\n    hour: 3\n",
			`"sync_groups":null,"connection_service":"WANPPPConnection","connection":{"type":"IP_Routed","ipv6_status":"Connected","ipv6_uptime":3600},"nat":{"enabled":true,"rsip_available":false},"link_layer_max_bit_rates":{"upstream":40000000,"downstream":100000000},"disconnect_prevention":{"enabled":true,"hour":3}}` + "\n",
		},
		{
			"ppp-unsupported", tr064.WANDetail{ConnectionService: tr064.WANPPPConnectionService},
			"  sync_groups: unknown\n  connection_service: WANPPPConnection\n  connection: unsupported\n  nat: unsupported\n  link_layer_max_bit_rates: unsupported\n  disconnect_prevention: unsupported\n",
			`"sync_groups":null,"connection_service":"WANPPPConnection","connection":null,"nat":null,"link_layer_max_bit_rates":null,"disconnect_prevention":null}` + "\n",
		},
		{
			"ip-unsupported", tr064.WANDetail{ConnectionService: tr064.WANIPConnectionService},
			"  sync_groups: unknown\n  connection_service: WANIPConnection\n  connection: unsupported\n  nat: unsupported\n  link_layer_max_bit_rates: not_applicable\n  disconnect_prevention: not_applicable\n",
			`"sync_groups":null,"connection_service":"WANIPConnection","connection":null,"nat":null,"link_layer_max_bit_rates":null,"disconnect_prevention":null}` + "\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := runWANConnectionDetail(t, test.detail); !strings.HasSuffix(got, test.compact) {
				t.Fatalf("compact=%q", got)
			}
			if got := runWANConnectionDetail(t, test.detail, "--json"); !strings.HasSuffix(got, test.raw) {
				t.Fatalf("JSON=%q", got)
			}
		})
	}
}

func TestWANDetailHelpDocumentsConnectionState(t *testing.T) {
	code, stdout, stderr := runTest(t, "wan", "detail", "--help")
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{"GetInfo", "GetNATRSIPStatus", "GetLinkLayerMaxBitRates", "X_AVM_DE_GetAutoDisconnectTimeSpan", "unsupported", "not_applicable", "connection_service"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("wan detail help lacks %q: %q", want, stdout)
		}
	}
}
