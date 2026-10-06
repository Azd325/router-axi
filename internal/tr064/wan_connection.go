package tr064

import (
	"context"
	"strings"
)

type WANConnection struct {
	Type       string  `json:"type"`
	IPv6Status *string `json:"ipv6_status"`
	IPv6Uptime *uint64 `json:"ipv6_uptime"`
}

type WANNAT struct {
	Enabled       bool `json:"enabled"`
	RSIPAvailable bool `json:"rsip_available"`
}

type WANLinkLayerMaxBitRates struct {
	Upstream   uint64 `json:"upstream"`
	Downstream uint64 `json:"downstream"`
}

type WANDisconnectPrevention struct {
	Enabled bool   `json:"enabled"`
	Hour    uint64 `json:"hour"`
}

const (
	WANIPConnectionService  = "WANIPConnection"
	WANPPPConnectionService = "WANPPPConnection"

	wanPPPConnectionPrefix   = "urn:dslforum-org:service:" + WANPPPConnectionService + ":"
	wanDisconnectHourMaximum = 23
)

var (
	wanConnectionStatuses = []string{"Unconfigured", "Connecting", "Authenticating", "PendingDisconnect", "Disconnecting", "Disconnected", "Connected"}
	wanIPConnectionTypes  = []string{"Unconfigured", "IP_Routed", "IP_Bridged"}
	wanPPPConnectionTypes = []string{"Unconfigured", "IP_Routed", "IP_Bridged", "DHCP_Spoofed", "PPPoE_Bridged", "PPTP_Relay", "L2TP_Relay", "PPPoE_Relay"}
)

func (c *Client) wanConnectionReads(ctx context.Context, active service, actions map[string]bool, result *WANDetail) error {
	ppp := strings.HasPrefix(active.Type, wanPPPConnectionPrefix)
	result.ConnectionService = WANIPConnectionService
	connectionTypes := wanIPConnectionTypes
	if ppp {
		result.ConnectionService = WANPPPConnectionService
		connectionTypes = wanPPPConnectionTypes
	}
	read := func(action string) (soapValues, bool, error) {
		if !actions[action] {
			return soapValues{}, false, nil
		}
		values, err := c.actionOnService(ctx, active, action)
		if err == nil {
			return values, true, nil
		}
		if failure := wanDetailError(err); failure.Kind != "unsupported" {
			return soapValues{}, false, failure
		}
		return soapValues{}, false, nil
	}
	if values, supported, err := read("GetInfo"); err != nil {
		return err
	} else if supported {
		if result.Connection, err = parseWANConnection(values, connectionTypes); err != nil {
			return err
		}
	}
	if values, supported, err := read("GetNATRSIPStatus"); err != nil {
		return err
	} else if supported {
		var nat WANNAT
		if nat.Enabled, err = parseWANDetailBool(values.NATEnabled, "NAT enabled flag"); err != nil {
			return err
		}
		if nat.RSIPAvailable, err = parseWANDetailBool(values.RSIPAvailable, "RSIP available flag"); err != nil {
			return err
		}
		result.NAT = &nat
	}
	if !ppp {
		return nil
	}
	if values, supported, err := read("GetLinkLayerMaxBitRates"); err != nil {
		return err
	} else if supported {
		var rates WANLinkLayerMaxBitRates
		if rates.Upstream, err = parseWANDetailUint(values.LinkLayerUpstreamMaxBitRate, "link-layer maximum upstream bit rate"); err != nil {
			return err
		}
		if rates.Downstream, err = parseWANDetailUint(values.LinkLayerDownstreamMaxBitRate, "link-layer maximum downstream bit rate"); err != nil {
			return err
		}
		result.LinkLayerMaxBitRates = &rates
	}
	if values, supported, err := read("X_AVM_DE_GetAutoDisconnectTimeSpan"); err != nil {
		return err
	} else if supported {
		var prevention WANDisconnectPrevention
		if prevention.Enabled, err = parseWANDetailBool(values.DisconnectPreventionEnable, "disconnect prevention flag"); err != nil {
			return err
		}
		if prevention.Hour, err = parseWANDetailUint(values.DisconnectPreventionHour, "disconnect prevention hour"); err != nil {
			return err
		}
		if prevention.Hour > wanDisconnectHourMaximum {
			return &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid disconnect prevention hour"}
		}
		result.DisconnectPrevention = &prevention
	}
	return nil
}

func parseWANConnection(values soapValues, connectionTypes []string) (*WANConnection, error) {
	connectionType, err := normalizeWANDetailState(values.ConnectionType, connectionTypes, "connection type")
	if err != nil {
		return nil, err
	}
	connection := WANConnection{Type: connectionType}
	if strings.TrimSpace(values.IPv6ConnectionStatus) != "" {
		status := exposureState(values.IPv6ConnectionStatus, wanConnectionStatuses...)
		connection.IPv6Status = &status
	}
	if strings.TrimSpace(values.IPv6Uptime) != "" {
		uptime, err := parseWANDetailUint(values.IPv6Uptime, "IPv6 uptime")
		if err != nil {
			return nil, err
		}
		connection.IPv6Uptime = &uptime
	}
	return &connection, nil
}

func parseWANDetailBool(value, field string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	}
	return false, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid " + field}
}
