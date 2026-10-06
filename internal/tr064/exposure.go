package tr064

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type Exposure struct {
	Remote RemoteExposure `json:"remote"`
	Local  LocalExposure  `json:"local"`
}

type RemoteExposure struct {
	RemoteAccess *RemoteAccessExposure `json:"remote_access"`
	DDNS         *DDNSExposure         `json:"ddns"`
	MyFRITZ      *MyFRITZExposure      `json:"myfritz"`
}

type LocalExposure struct {
	Storage   *StorageExposure   `json:"storage"`
	UPnP      *UPnPExposure      `json:"upnp"`
	WebDAV    *WebDAVExposure    `json:"webdav"`
	Speedtest *SpeedtestExposure `json:"speedtest"`
	TR069     *TR069Exposure     `json:"tr069"`
}

type RemoteAccessExposure struct {
	Enabled            bool   `json:"enabled"`
	Port               uint64 `json:"port"`
	LetsEncryptEnabled *bool  `json:"letsencrypt_enabled"`
	LetsEncryptState   string `json:"letsencrypt_state"`
}

type DDNSExposure struct {
	Enabled    bool   `json:"enabled"`
	StatusIPv4 string `json:"status_ipv4"`
	StatusIPv6 string `json:"status_ipv6"`
}

type MyFRITZExposure struct {
	Enabled          bool   `json:"enabled"`
	Port             uint64 `json:"port"`
	DeviceRegistered bool   `json:"device_registered"`
	State            string `json:"state"`
}

type StorageExposure struct {
	FTPEnabled    bool    `json:"ftp_enabled"`
	FTPStatus     string  `json:"ftp_status"`
	SMBEnabled    bool    `json:"smb_enabled"`
	FTPWANEnabled *bool   `json:"ftp_wan_enabled"`
	FTPWANSSLOnly *bool   `json:"ftp_wan_ssl_only"`
	FTPWANPort    *uint64 `json:"ftp_wan_port"`
}

type UPnPExposure struct {
	Enabled            bool `json:"enabled"`
	MediaServerEnabled bool `json:"media_server_enabled"`
}

type WebDAVExposure struct {
	Enabled bool `json:"enabled"`
}

type SpeedtestExposure struct {
	TCPEnabled         bool   `json:"tcp_enabled"`
	UDPEnabled         bool   `json:"udp_enabled"`
	UDPBidirectEnabled bool   `json:"udp_bidirect_enabled"`
	WANTCPEnabled      bool   `json:"wan_tcp_enabled"`
	WANUDPEnabled      bool   `json:"wan_udp_enabled"`
	TCPPort            uint64 `json:"tcp_port"`
	UDPPort            uint64 `json:"udp_port"`
	UDPBidirectPort    uint64 `json:"udp_bidirect_port"`
}

type TR069Exposure struct {
	PeriodicInformEnabled bool `json:"periodic_inform_enabled"`
	UpgradesManaged       bool `json:"upgrades_managed"`
}

const (
	exposureOperation   = "exposure"
	exposureRemediation = "use firmware that advertises a documented GetInfo read of X_AVM-DE_RemoteAccess, X_AVM-DE_MyFritz, X_AVM-DE_Storage, X_AVM-DE_UPnP, X_AVM-DE_WebDAVClient, X_AVM-DE_Speedtest, or ManagementServer"
	exposureAnyRight    = "App, Phone, NAS, or Homeauto"
	exposureConfigRight = "configuration"
)

func (c *Client) Exposure(ctx context.Context) (Exposure, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("exposure inspection refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return Exposure{}, exposureError(err)
	}
	var result Exposure
	type target struct {
		service service
		actions map[string]bool
	}
	targets := map[string]target{}
	supported := false
	for _, part := range []struct {
		service, action, right string
		store                  func(soapValues, *exposureParser)
	}{
		{"X_AVM-DE_RemoteAccess", "GetInfo", exposureAnyRight, func(v soapValues, p *exposureParser) { result.Remote.RemoteAccess = remoteAccessExposure(v, p) }},
		{"X_AVM-DE_RemoteAccess", "GetDDNSInfo", exposureAnyRight, func(v soapValues, p *exposureParser) { result.Remote.DDNS = ddnsExposure(v, p) }},
		{"X_AVM-DE_MyFritz", "GetInfo", exposureAnyRight, func(v soapValues, p *exposureParser) { result.Remote.MyFRITZ = myFRITZExposure(v, p) }},
		{"X_AVM-DE_Storage", "GetInfo", "App", func(v soapValues, p *exposureParser) { result.Local.Storage = storageExposure(v, p) }},
		{"X_AVM-DE_UPnP", "GetInfo", exposureConfigRight, func(v soapValues, p *exposureParser) { result.Local.UPnP = upnpExposure(v, p) }},
		{"X_AVM-DE_WebDAVClient", "GetInfo", exposureConfigRight, func(v soapValues, p *exposureParser) { result.Local.WebDAV = webDAVExposure(v, p) }},
		{"X_AVM-DE_Speedtest", "GetInfo", exposureConfigRight, func(v soapValues, p *exposureParser) { result.Local.Speedtest = speedtestExposure(v, p) }},
		{"ManagementServer", "GetInfo", exposureConfigRight, func(v soapValues, p *exposureParser) { result.Local.TR069 = tr069Exposure(v, p) }},
	} {
		selected, known := targets[part.service]
		if !known {
			svc, actions, err := client.serviceActions(ctx, "urn:dslforum-org:service:"+part.service+":", part.service, exposureOperation, exposureRemediation, exposureError)
			var failure *Error
			if err != nil && (!errors.As(err, &failure) || failure.Kind != "unsupported") {
				return Exposure{}, err
			}
			selected = target{svc, actions}
			targets[part.service] = selected
		}
		if !selected.actions[part.action] {
			continue
		}
		values, err := client.actionOnService(ctx, selected.service, part.action)
		if err != nil {
			failure := exposureError(err)
			if failure.Kind == "unsupported" {
				continue
			}
			if failure.Kind == "router" && failure.FaultCode == "606" {
				failure.Message = "router denied " + part.service + ":" + part.action + "; the logged-in account needs the " + part.right + " right"
			}
			return Exposure{}, failure
		}
		var parser exposureParser
		part.store(values, &parser)
		if parser.err != nil {
			return Exposure{}, parser.err
		}
		supported = true
	}
	if !supported {
		return Exposure{}, &Error{Kind: "unsupported", Operation: exposureOperation, Message: "router supports none of the documented exposure reads; " + exposureRemediation}
	}
	return result, nil
}

func remoteAccessExposure(v soapValues, p *exposureParser) *RemoteAccessExposure {
	return &RemoteAccessExposure{
		Enabled:            p.flag(v.Enabled, "remote access enabled flag"),
		Port:               p.port(v.Port, "remote access port", 16),
		LetsEncryptEnabled: p.optionalFlag(v.LetsEncryptEnabled, "Let's Encrypt enabled flag"),
		LetsEncryptState:   exposureState(v.LetsEncryptState, "not_used", "get", "valid", "invalid"),
	}
}

func ddnsExposure(v soapValues, p *exposureParser) *DDNSExposure {
	return &DDNSExposure{
		Enabled: p.flag(v.Enabled, "DDNS enabled flag"),
		// The vendor document (Remote Access v13, §2) spells this value "new address" for IPv4 and "new-address" for IPv6.
		StatusIPv4: ddnsStatus(v.StatusIPv4, "new address"),
		StatusIPv6: ddnsStatus(v.StatusIPv6, "new-address"),
	}
}

func ddnsStatus(value, newAddress string) string {
	return exposureState(value, "offline", "checking", "updating", "updated", "verifying", "complete", newAddress, "account-disabled", "internet-not-connected", "undefined")
}

func myFRITZExposure(v soapValues, p *exposureParser) *MyFRITZExposure {
	return &MyFRITZExposure{
		Enabled:          p.flag(v.Enabled, "MyFRITZ enabled flag"),
		Port:             p.port(v.Port, "MyFRITZ port", 32),
		DeviceRegistered: p.flag(v.DeviceRegistered, "MyFRITZ registration flag"),
		State:            exposureState(v.MyFritzState, "myfritz_disabled", "register_failed", "unregister", "dyndns_unknown", "dyndns_active", "dyndns_update_failed", "dyndns_auth_error", "dyndns_server_unreachable", "dyndns_server_error", "dyndns_server_update", "dyndns_not_verified", "dyndns_verified", "reserved"),
	}
}

func storageExposure(v soapValues, p *exposureParser) *StorageExposure {
	return &StorageExposure{
		FTPEnabled:    p.flag(v.FTPEnable, "FTP enabled flag"),
		FTPStatus:     exposureState(v.FTPStatus, "Enable", "Disable", "Error"),
		SMBEnabled:    p.flag(v.SMBEnable, "SMB enabled flag"),
		FTPWANEnabled: p.optionalFlag(v.FTPWANEnable, "FTP WAN enabled flag"),
		FTPWANSSLOnly: p.optionalFlag(v.FTPWANSSLOnly, "FTP WAN SSL-only flag"),
		FTPWANPort:    p.optionalPort(v.FTPWANPort, "FTP WAN port", 16),
	}
}

func upnpExposure(v soapValues, p *exposureParser) *UPnPExposure {
	return &UPnPExposure{
		Enabled:            p.flag(v.Enable, "UPnP enabled flag"),
		MediaServerEnabled: p.flag(v.UPnPMediaServer, "UPnP media server flag"),
	}
}

func webDAVExposure(v soapValues, p *exposureParser) *WebDAVExposure {
	return &WebDAVExposure{Enabled: p.flag(v.Enable, "WebDAV enabled flag")}
}

func speedtestExposure(v soapValues, p *exposureParser) *SpeedtestExposure {
	return &SpeedtestExposure{
		TCPEnabled:         p.flag(v.EnableTCP, "speedtest TCP enabled flag"),
		UDPEnabled:         p.flag(v.EnableUDP, "speedtest UDP enabled flag"),
		UDPBidirectEnabled: p.flag(v.EnableUDPBidirect, "speedtest bidirectional UDP enabled flag"),
		WANTCPEnabled:      p.flag(v.WANEnableTCP, "speedtest WAN TCP enabled flag"),
		WANUDPEnabled:      p.flag(v.WANEnableUDP, "speedtest WAN UDP enabled flag"),
		TCPPort:            p.port(v.PortTCP, "speedtest TCP port", 32),
		UDPPort:            p.port(v.PortUDP, "speedtest UDP port", 32),
		UDPBidirectPort:    p.port(v.PortUDPBidirect, "speedtest bidirectional UDP port", 32),
	}
}

func tr069Exposure(v soapValues, p *exposureParser) *TR069Exposure {
	return &TR069Exposure{
		PeriodicInformEnabled: p.flag(v.PeriodicInformEnable, "TR-069 periodic inform flag"),
		UpgradesManaged:       p.flag(v.UpgradesManaged, "TR-069 managed upgrades flag"),
	}
}

type exposureParser struct{ err error }

func (p *exposureParser) flag(value, field string) bool {
	switch strings.TrimSpace(value) {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	p.fail(field)
	return false
}

func (p *exposureParser) optionalFlag(value, field string) *bool {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	flag := p.flag(value, field)
	return &flag
}

func (p *exposureParser) port(value, field string, bitSize int) uint64 {
	port, err := strconv.ParseUint(strings.TrimSpace(value), 10, bitSize)
	if err != nil {
		p.fail(field)
	}
	return port
}

func (p *exposureParser) optionalPort(value, field string, bitSize int) *uint64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	port := p.port(value, field, bitSize)
	return &port
}

func (p *exposureParser) fail(field string) {
	if p.err == nil {
		p.err = &Error{Kind: "protocol", Operation: exposureOperation, Message: "router returned an invalid " + field}
	}
}

func exposureState(value string, documented ...string) string {
	value = strings.TrimSpace(value)
	if slices.Contains(documented, value) {
		return value
	}
	return "unknown"
}

func exposureError(err error) *Error {
	return readOperationError(exposureOperation, "exposure inspection failed", "router does not support the documented exposure reads; "+exposureRemediation, err)
}
