package tr064

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/wan-connection-scpd.xml
var wanConnectionSCPDFixture string

//go:embed testdata/wan-connection-info-ip.xml
var wanConnectionInfoIPFixture string

//go:embed testdata/wan-connection-info-ppp.xml
var wanConnectionInfoPPPFixture string

//go:embed testdata/wan-connection-nat.xml
var wanConnectionNATFixture string

//go:embed testdata/wan-connection-link-rates.xml
var wanConnectionLinkRatesFixture string

//go:embed testdata/wan-connection-disconnect-prevention.xml
var wanConnectionDisconnectPreventionFixture string

const (
	wanIPConnectionType  = "urn:dslforum-org:service:WANIPConnection:1"
	wanPPPConnectionType = "urn:dslforum-org:service:WANPPPConnection:1"
)

var wanPPPConnectionActions = []string{"GetInfo", "GetNATRSIPStatus", "GetLinkLayerMaxBitRates", "X_AVM_DE_GetAutoDisconnectTimeSpan"}

func wanConnectionScript(activeType string) []forwardExchange {
	activePath, info := "/ip1", wanConnectionInfoIPFixture
	if activeType == wanPPPConnectionType {
		activePath, info = "/ppp1", wanConnectionInfoPPPFixture
	}
	script := append(wanDetailScript("1", activeType, activePath, wanConnectionSCPDFixture, false),
		forwardExchange{path: activePath, action: "GetInfo", body: info, serviceType: activeType},
		forwardExchange{path: activePath, action: "GetNATRSIPStatus", body: strings.ReplaceAll(wanConnectionNATFixture, "__SERVICE_TYPE__", activeType), serviceType: activeType},
	)
	if activeType == wanPPPConnectionType {
		script = append(script,
			forwardExchange{path: activePath, action: "GetLinkLayerMaxBitRates", body: wanConnectionLinkRatesFixture, serviceType: activeType},
			forwardExchange{path: activePath, action: "X_AVM_DE_GetAutoDisconnectTimeSpan", body: wanConnectionDisconnectPreventionFixture, serviceType: activeType},
		)
	}
	return script
}

func replaceWANConnectionBody(t *testing.T, script []forwardExchange, action, old, replacement string) {
	t.Helper()
	for index := range script {
		if script[index].action == action {
			if !strings.Contains(script[index].body, old) {
				t.Fatalf("%s fixture lacks %q", action, old)
			}
			script[index].body = strings.Replace(script[index].body, old, replacement, 1)
			return
		}
	}
	t.Fatalf("script lacks %s", action)
}

func wanConnectionParts(detail WANDetail) map[string]bool {
	return map[string]bool{
		"GetInfo":                            detail.Connection != nil,
		"GetNATRSIPStatus":                   detail.NAT != nil,
		"GetLinkLayerMaxBitRates":            detail.LinkLayerMaxBitRates != nil,
		"X_AVM_DE_GetAutoDisconnectTimeSpan": detail.DisconnectPrevention != nil,
	}
}

func TestWANDetailReportsPPPConnectionState(t *testing.T) {
	detail, err := forwardFixtureClient(t, wanConnectionScript(wanPPPConnectionType)).WANDetail(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	status := "Connected"
	want := WANDetail{
		AccessType: "Cable", PhysicalLinkStatus: "Up", MaxDownloadBitsPerSecond: 1100000000, MaxUploadBitsPerSecond: 55000000, DNSServers: []string{},
		ConnectionService:    "WANPPPConnection",
		Connection:           &WANConnection{Type: "IP_Routed", IPv6Status: &status},
		NAT:                  &WANNAT{Enabled: true, RSIPAvailable: false},
		LinkLayerMaxBitRates: &WANLinkLayerMaxBitRates{Upstream: 40000000, Downstream: 100000000},
		DisconnectPrevention: &WANDisconnectPrevention{Enabled: true, Hour: 3},
	}
	if !reflect.DeepEqual(detail, want) {
		t.Fatalf("detail=%#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := `"sync_groups":null,"connection_service":"WANPPPConnection","connection":{"type":"IP_Routed","ipv6_status":"Connected"},"nat":{"enabled":true,"rsip_available":false},"link_layer_max_bit_rates":{"upstream":40000000,"downstream":100000000},"disconnect_prevention":{"enabled":true,"hour":3}}`
	if !strings.HasSuffix(string(encoded), wantSuffix) || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "203.0.113.42") || strings.Contains(string(encoded), "192.0.2.53") {
		t.Fatalf("JSON=%s", encoded)
	}
}

func TestWANDetailNeverReadsPPPOnlyPartsOnIPConnection(t *testing.T) {
	detail, err := forwardFixtureClient(t, wanConnectionScript(wanIPConnectionType)).WANDetail(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if detail.ConnectionService != "WANIPConnection" || detail.Connection == nil || detail.Connection.Type != "IP_Routed" || detail.NAT == nil || !detail.NAT.Enabled || detail.NAT.RSIPAvailable || detail.LinkLayerMaxBitRates != nil || detail.DisconnectPrevention != nil {
		t.Fatalf("detail=%#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "203.0.113.42") {
		t.Fatalf("JSON=%s error=%v", encoded, err)
	}
}

func TestWANDetailConnectionPartsAreIndependent(t *testing.T) {
	for _, action := range wanPPPConnectionActions {
		for _, variant := range []string{"unadvertised", "invalid-action"} {
			t.Run(action+"/"+variant, func(t *testing.T) {
				var script []forwardExchange
				for _, exchange := range wanConnectionScript(wanPPPConnectionType) {
					switch {
					case exchange.path == "/ppp1.xml" && variant == "unadvertised":
						exchange.body = strings.Replace(exchange.body, "<action><name>"+action+"</name></action>", "", 1)
					case exchange.action == action && variant == "unadvertised":
						continue
					case exchange.action == action:
						exchange.status = http.StatusInternalServerError
						exchange.body = `<Envelope><errorCode>401</errorCode><errorDescription>private-canary</errorDescription></Envelope>`
					}
					script = append(script, exchange)
				}
				detail, err := forwardFixtureClient(t, script).WANDetail(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				for part, reported := range wanConnectionParts(detail) {
					if reported == (part == action) {
						t.Fatalf("part %s reported=%t with %s %s: %#v", part, reported, action, variant, detail)
					}
				}
			})
		}
	}
}

func TestWANDetailConnectionReadFailuresAreAtomicAndRedacted(t *testing.T) {
	for _, action := range wanPPPConnectionActions {
		for _, code := range []string{"606", "501"} {
			t.Run(action+code, func(t *testing.T) {
				script := wanConnectionScript(wanPPPConnectionType)
				for index := range script {
					if script[index].action == action {
						script[index].status = http.StatusInternalServerError
						script[index].body = `<Envelope><errorCode>` + code + `</errorCode><errorDescription>private-canary</errorDescription></Envelope>`
						script = script[:index+1]
						break
					}
				}
				detail, err := forwardFixtureClient(t, script).WANDetail(t.Context())
				var protocolErr *Error
				if !errors.As(err, &protocolErr) || protocolErr.Kind != "router" || protocolErr.Operation != "wan detail" || protocolErr.StatusCode != http.StatusInternalServerError || strings.Contains(fmt.Sprintf("%#v", err), "private") || !reflect.DeepEqual(detail, WANDetail{}) {
					t.Fatalf("detail=%#v error=%#v", detail, err)
				}
			})
		}
	}
}

func TestWANDetailLegacyConnectionInfoLeavesIPv6Unknown(t *testing.T) {
	script := wanConnectionScript(wanPPPConnectionType)
	replaceWANConnectionBody(t, script, "GetInfo", "<NewX_AVM-DE_IPv6ConnectionStatus>Connected</NewX_AVM-DE_IPv6ConnectionStatus>", "")
	detail, err := forwardFixtureClient(t, script).WANDetail(t.Context())
	if err != nil || detail.Connection == nil || detail.Connection.Type != "IP_Routed" || detail.Connection.IPv6Status != nil {
		t.Fatalf("detail=%#v error=%v", detail, err)
	}
	encoded, err := json.Marshal(detail.Connection)
	if err != nil || string(encoded) != `{"type":"IP_Routed","ipv6_status":null}` {
		t.Fatalf("JSON=%s error=%v", encoded, err)
	}
}

func TestWANDetailConnectionValueSets(t *testing.T) {
	for _, test := range []struct {
		activeType, field, value, want string
	}{
		{wanIPConnectionType, "ConnectionType", "Unconfigured", "Unconfigured"},
		{wanIPConnectionType, "ConnectionType", "IP_Bridged", "IP_Bridged"},
		{wanIPConnectionType, "ConnectionType", "PPPoE_Relay", "unknown"},
		{wanPPPConnectionType, "ConnectionType", "IP_Bridged", "IP_Bridged"},
		{wanPPPConnectionType, "ConnectionType", "DHCP_Spoofed", "DHCP_Spoofed"},
		{wanPPPConnectionType, "ConnectionType", "PPPoE_Bridged", "PPPoE_Bridged"},
		{wanPPPConnectionType, "ConnectionType", "PPTP_Relay", "PPTP_Relay"},
		{wanPPPConnectionType, "ConnectionType", "L2TP_Relay", "L2TP_Relay"},
		{wanPPPConnectionType, "ConnectionType", "PPPoE_Relay", "PPPoE_Relay"},
		{wanPPPConnectionType, "ConnectionType", "future-type", "unknown"},
		{wanPPPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "Unconfigured", "Unconfigured"},
		{wanPPPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "Connecting", "Connecting"},
		{wanPPPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "Authenticating", "Authenticating"},
		{wanPPPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "PendingDisconnect", "PendingDisconnect"},
		{wanPPPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "Disconnecting", "Disconnecting"},
		{wanPPPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "Disconnected", "Disconnected"},
		{wanIPConnectionType, "X_AVM-DE_IPv6ConnectionStatus", "future-status", "unknown"},
	} {
		t.Run(test.activeType+"/"+test.field+"/"+test.value, func(t *testing.T) {
			script := wanConnectionScript(test.activeType)
			current := "IP_Routed"
			if test.field != "ConnectionType" {
				current = "Connected"
			}
			replaceWANConnectionBody(t, script, "GetInfo", "<New"+test.field+">"+current+"<", "<New"+test.field+">"+test.value+"<")
			detail, err := forwardFixtureClient(t, script).WANDetail(t.Context())
			if err != nil || detail.Connection == nil || detail.Connection.IPv6Status == nil {
				t.Fatalf("detail=%#v error=%v", detail, err)
			}
			got := detail.Connection.Type
			if test.field != "ConnectionType" {
				got = *detail.Connection.IPv6Status
			}
			if got != test.want {
				t.Fatalf("%s=%q, want %q", test.field, got, test.want)
			}
		})
	}
}

func TestWANDetailRejectsMalformedConnectionFields(t *testing.T) {
	for _, test := range []struct{ name, action, old, replacement string }{
		{"missing-connection-type", "GetInfo", "<NewConnectionType>IP_Routed</NewConnectionType>", ""},
		{"nat-enabled", "GetNATRSIPStatus", "NATEnabled>1<", "NATEnabled>private-canary<"},
		{"missing-nat-enabled", "GetNATRSIPStatus", "<NewNATEnabled>1</NewNATEnabled>", ""},
		{"rsip-available", "GetNATRSIPStatus", "RSIPAvailable>0<", "RSIPAvailable>2<"},
		{"upstream-rate", "GetLinkLayerMaxBitRates", "UpstreamMaxBitRate>40000000<", "UpstreamMaxBitRate>-1<"},
		{"missing-downstream-rate", "GetLinkLayerMaxBitRates", "<NewDownstreamMaxBitRate>100000000</NewDownstreamMaxBitRate>", ""},
		{"prevention-flag", "X_AVM_DE_GetAutoDisconnectTimeSpan", "DisconnectPreventionEnable>1<", "DisconnectPreventionEnable>yes<"},
		{"prevention-hour", "X_AVM_DE_GetAutoDisconnectTimeSpan", "DisconnectPreventionHour>3<", "DisconnectPreventionHour>private-canary<"},
		{"prevention-hour-range", "X_AVM_DE_GetAutoDisconnectTimeSpan", "DisconnectPreventionHour>3<", "DisconnectPreventionHour>24<"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := wanConnectionScript(wanPPPConnectionType)
			replaceWANConnectionBody(t, script, test.action, test.old, test.replacement)
			for index := range script {
				if script[index].action == test.action {
					script = script[:index+1]
					break
				}
			}
			detail, err := forwardFixtureClient(t, script).WANDetail(t.Context())
			var protocolErr *Error
			if !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || protocolErr.Operation != "wan detail" || strings.Contains(fmt.Sprintf("%#v", err), "private") || !reflect.DeepEqual(detail, WANDetail{}) {
				t.Fatalf("detail=%#v error=%#v", detail, err)
			}
		})
	}
}

func TestWANDetailAcceptsDisconnectPreventionHourBounds(t *testing.T) {
	for _, hour := range []string{"0", "23"} {
		t.Run(hour, func(t *testing.T) {
			script := wanConnectionScript(wanPPPConnectionType)
			replaceWANConnectionBody(t, script, "X_AVM_DE_GetAutoDisconnectTimeSpan", "DisconnectPreventionHour>3<", "DisconnectPreventionHour>"+hour+"<")
			replaceWANConnectionBody(t, script, "X_AVM_DE_GetAutoDisconnectTimeSpan", "DisconnectPreventionEnable>1<", "DisconnectPreventionEnable>0<")
			detail, err := forwardFixtureClient(t, script).WANDetail(t.Context())
			if err != nil || detail.DisconnectPrevention == nil || detail.DisconnectPrevention.Enabled || fmt.Sprint(detail.DisconnectPrevention.Hour) != hour {
				t.Fatalf("detail=%#v error=%v", detail, err)
			}
		})
	}
}
