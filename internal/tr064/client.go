package tr064

import (
	"bytes"
	"cmp"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const descriptionPath = "/tr64desc.xml"

type Error struct {
	Kind       string
	Code       string
	Operation  string
	StatusCode int
	FaultCode  string
	Message    string
}

func (e *Error) Error() string {
	if e.Operation == "" {
		return e.Message
	}
	return e.Operation + ": " + e.Message
}

type Status struct {
	Manufacturer  string `json:"manufacturer"`
	Model         string `json:"model"`
	Serial        string `json:"serial"`
	Software      string `json:"software_version"`
	Hardware      string `json:"hardware_version"`
	UptimeSeconds uint64 `json:"uptime_seconds"`
}

type WAN struct {
	Status        string `json:"status"`
	ExternalIP    string `json:"external_ip"`
	IPFamily      string `json:"ip_family"`
	UptimeSeconds uint64 `json:"uptime_seconds"`
	LastError     string `json:"last_error"`
}

type Traffic struct {
	TotalDownloadBytes uint64 `json:"total_download_bytes"`
	TotalUploadBytes   uint64 `json:"total_upload_bytes"`
	ObservedAt         string `json:"observed_at"`
}

type WANDetail struct {
	AccessType                           string         `json:"access_type"`
	PhysicalLinkStatus                   string         `json:"physical_link_status"`
	MaxDownloadBitsPerSecond             uint64         `json:"max_download_bits_per_second"`
	MaxUploadBitsPerSecond               uint64         `json:"max_upload_bits_per_second"`
	RouterReportedDownloadBytesPerSecond *uint64        `json:"router_reported_download_bytes_per_second"`
	RouterReportedUploadBytesPerSecond   *uint64        `json:"router_reported_upload_bytes_per_second"`
	TotalDownloadBytes                   *uint64        `json:"total_download_bytes"`
	TotalUploadBytes                     *uint64        `json:"total_upload_bytes"`
	DNSServers                           []string       `json:"dns_servers"`
	TotalDownloadPackets                 *uint64        `json:"total_download_packets"`
	TotalUploadPackets                   *uint64        `json:"total_upload_packets"`
	SyncDownloadBitsPerSecond            *uint64        `json:"sync_download_bits_per_second"`
	SyncUploadBitsPerSecond              *uint64        `json:"sync_upload_bits_per_second"`
	TariffDownloadBitsPerSecond          *uint64        `json:"tariff_download_bits_per_second"`
	TariffUploadBitsPerSecond            *uint64        `json:"tariff_upload_bits_per_second"`
	Provider                             *string        `json:"provider"`
	SyncGroups                           []WANSyncGroup `json:"sync_groups"`
}

type WANSyncGroup struct {
	Index                        uint64   `json:"index"`
	MaxDownloadBytesPerSecond    uint64   `json:"max_download_bytes_per_second"`
	MaxUploadBytesPerSecond      uint64   `json:"max_upload_bytes_per_second"`
	DSCurrentBytesPerSecond      []uint64 `json:"ds_current_bytes_per_second"`
	MCCurrentBytesPerSecond      []uint64 `json:"mc_current_bytes_per_second"`
	UploadBytesPerSecond         []uint64 `json:"upload_bytes_per_second"`
	RealtimeUploadBytesPerSecond []uint64 `json:"realtime_upload_bytes_per_second"`
	HighUploadBytesPerSecond     []uint64 `json:"high_upload_bytes_per_second"`
	DefaultUploadBytesPerSecond  []uint64 `json:"default_upload_bytes_per_second"`
	LowUploadBytesPerSecond      []uint64 `json:"low_upload_bytes_per_second"`
}

type DHCP struct {
	ServerConfigurable *bool    `json:"server_configurable"`
	ServerEnabled      *bool    `json:"server_enabled"`
	RelayEnabled       *bool    `json:"relay_enabled"`
	AddressRangeStart  *string  `json:"address_range_start"`
	AddressRangeEnd    *string  `json:"address_range_end"`
	SubnetMask         *string  `json:"subnet_mask"`
	Routers            []string `json:"routers"`
	DNSServers         []string `json:"dns_servers"`
	DomainName         *string  `json:"domain_name"`
}

type DSL struct {
	LinkStatus                   string  `json:"link_status"`
	ModulationType               string  `json:"modulation_type"`
	CurrentProfile               string  `json:"current_profile"`
	UpstreamCurrentKbps          uint64  `json:"upstream_current_kbps"`
	DownstreamCurrentKbps        uint64  `json:"downstream_current_kbps"`
	UpstreamMaxKbps              uint64  `json:"upstream_max_kbps"`
	DownstreamMaxKbps            uint64  `json:"downstream_max_kbps"`
	UpstreamNoiseMarginTenthDB   uint32  `json:"upstream_noise_margin_tenth_db"`
	DownstreamNoiseMarginTenthDB uint32  `json:"downstream_noise_margin_tenth_db"`
	UpstreamAttenuationTenthDB   uint32  `json:"upstream_attenuation_tenth_db"`
	DownstreamAttenuationTenthDB uint32  `json:"downstream_attenuation_tenth_db"`
	FECErrors                    uint64  `json:"fec_errors"`
	CRCErrors                    uint64  `json:"crc_errors"`
	ATURVendor                   *string `json:"atur_vendor"`
	ATURCountry                  *string `json:"atur_country"`
	UpstreamPowerTenthDBm        *uint16 `json:"upstream_power_tenth_dbm"`
	DownstreamPowerTenthDBm      *uint16 `json:"downstream_power_tenth_dbm"`
}

type Overview struct {
	Router  Status  `json:"router"`
	WAN     WAN     `json:"wan"`
	Traffic Traffic `json:"traffic"`
}

type DoctorCheck struct {
	State       string `json:"state"`
	Remediation string `json:"remediation,omitempty"`
}

type DoctorCapabilities struct {
	Status   DoctorCheck `json:"status"`
	Overview DoctorCheck `json:"overview"`
	WAN      DoctorCheck `json:"wan"`
	Traffic  DoctorCheck `json:"traffic"`
	Watch    DoctorCheck `json:"watch"`
	Calls    DoctorCheck `json:"calls"`
	Devices  DoctorCheck `json:"devices"`
	Leases   DoctorCheck `json:"leases"`
	DHCP     DoctorCheck `json:"dhcp"`
	DSL      DoctorCheck `json:"dsl"`
	Firmware DoctorCheck `json:"firmware"`
	Account  DoctorCheck `json:"account"`
	WiFi     DoctorCheck `json:"wifi"`
	Forwards DoctorCheck `json:"forwards"`
	Reboot   DoctorCheck `json:"reboot"`
	Backup   DoctorCheck `json:"backup"`
	EventLog DoctorCheck `json:"event_log"`
}

type Doctor struct {
	Endpoint       string             `json:"endpoint"`
	Reachability   DoctorCheck        `json:"reachability"`
	Protocol       DoctorCheck        `json:"protocol"`
	Authentication DoctorCheck        `json:"authentication"`
	Model          string             `json:"model"`
	Firmware       string             `json:"firmware"`
	Capabilities   DoctorCapabilities `json:"capabilities"`
}

type Call struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	Remote    string `json:"remote"`
	Name      string `json:"name,omitempty"`
	Date      string `json:"date"`
	Duration  string `json:"duration"`
	Device    string `json:"device,omitempty"`
}

type Device struct {
	Name          string `json:"name,omitempty"`
	IPAddress     string `json:"ip_address"`
	MACAddress    string `json:"mac_address"`
	InterfaceType string `json:"interface_type"`
	Active        bool   `json:"active"`
}

type Lease struct {
	Name               string `json:"name,omitempty"`
	IPAddress          string `json:"ip_address"`
	MACAddress         string `json:"mac_address"`
	AddressSource      string `json:"address_source"`
	LeaseTimeRemaining *int64 `json:"lease_time_remaining,omitempty"`
	InterfaceType      string `json:"interface_type"`
	Active             bool   `json:"active"`
}

type Radio struct {
	ServiceID         string `json:"service_id"`
	SSID              string `json:"ssid"`
	Enabled           bool   `json:"enabled"`
	Channel           uint64 `json:"channel"`
	Band              string `json:"band"`
	Standard          string `json:"standard"`
	AssociatedDevices uint64 `json:"associated_devices"`
	SecurityMode      string `json:"security_mode"`
}

type GuestNetwork struct {
	ServiceID         string              `json:"service_id"`
	SSID              string              `json:"ssid"`
	Enabled           bool                `json:"enabled"`
	Channel           uint64              `json:"channel"`
	Band              string              `json:"band"`
	Standard          string              `json:"standard"`
	AssociatedClients uint64              `json:"associated_clients"`
	SecurityMode      string              `json:"security_mode"`
	Configuration     *GuestConfiguration `json:"configuration"`
}

// RadioDetail carries safe per-radio state and independently supported configuration reads.
// Absent optional fields are unknown; unsupported configuration parts are nil.
type RadioDetail struct {
	ServiceID            string                    `json:"service_id"`
	Enabled              bool                      `json:"enabled"`
	Status               *string                   `json:"status"`
	Standard             string                    `json:"standard"`
	MaxBitRate           *string                   `json:"max_bit_rate"`
	Channel              *uint64                   `json:"channel"`
	Band                 string                    `json:"band"`
	ChannelConfiguration *WiFiChannelConfiguration `json:"channel_configuration"`
	BeaconAdvertisement  *WiFiBeaconAdvertisement  `json:"beacon_advertisement"`
	NightControl         *WiFiNightControl         `json:"night_control"`
	WPS                  *WiFiWPS                  `json:"wps"`
	IPTVOptimization     *WiFiIPTVOptimization     `json:"iptv_optimization"`
}

type Forward struct {
	Enabled        bool    `json:"enabled"`
	Protocol       string  `json:"protocol"`
	ExternalPort   uint64  `json:"external_port"`
	InternalClient string  `json:"internal_client"`
	InternalPort   uint64  `json:"internal_port"`
	Description    string  `json:"description"`
	RemoteHost     string  `json:"remote_host"`
	LeaseDuration  *uint64 `json:"lease_duration,omitempty"`
}

type service struct {
	ID         string `xml:"serviceId"`
	Type       string `xml:"serviceType"`
	ControlURL string `xml:"controlURL"`
	SCPDURL    string `xml:"SCPDURL"`
}

type description struct{ Services []service }

func (d *description) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if end, ok := token.(xml.EndElement); ok && end.Name == start.Name {
			return nil
		}
		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "service" {
			continue
		}
		var svc service
		if err := decoder.DecodeElement(&svc, &element); err != nil {
			return err
		}
		d.Services = append(d.Services, svc)
	}
}

type soapValues struct {
	Status, LastError, ExternalIP                                                 string
	Manufacturer, Model, Serial, Software, Hardware                               string
	Uptime, TotalDownload, TotalUpload                                            string
	WANAccessType, PhysicalLinkStatus, DownstreamMaxBitRate, UpstreamMaxBitRate   string
	TotalPacketsSent, TotalPacketsReceived                                        string
	SyncDownstream, SyncUpstream, TariffDownstream, TariffUpstream                string
	Provider, TotalNumberSyncGroups, MonitorMaxDownstream, MonitorMaxUpstream     string
	DSCurrent, MCCurrent, USCurrent, PrioRealtime, PrioHigh, PrioDefault, PrioLow string
	CallListURL, HostNumberOfEntries, DefaultConnectionService                    string
	MACAddress, IPAddress, InterfaceType, Active, HostName, AddressSource         string
	LeaseTimeRemaining                                                            *string
	FaultCode, FaultDescription                                                   string
	Enable, SSID, Standard                                                        string
	WLANStatus, MaxBitRate                                                        string
	Channel, FrequencyBand, TotalAssociations, BeaconType, APType                 string
	PossibleChannels, AutoChannelEnabled, BeaconAdvertisementEnabled              string
	NightControl, NightTimeControlNoForcedOff, WPSMode, WPSStatus, IPTVOptimize   string
	GuestTimeoutActive, GuestTimeout, GuestTimeRemain                             string
	GuestNoForcedOff, GuestUserIsolation                                          string
	PortMappingCount, ExternalPort, PortMappingProtocol                           string
	InternalPort, InternalClient, Enabled, PortMappingDescription                 string
	RemoteHost, LeaseDuration                                                     *string
	DHCPServerConfigurable, DHCPServerEnable, DHCPRelay                           string
	MinAddress, MaxAddress, SubnetMask, DNSServers, DomainName, IPRouters         string
	LinkStatus, ModulationType, CurrentProfile                                    string
	UpstreamCurrRate, DownstreamCurrRate, UpstreamMaxRate, DownstreamMaxRate      string
	UpstreamNoiseMargin, DownstreamNoiseMargin                                    string
	UpstreamAttenuation, DownstreamAttenuation, FECErrors, CRCErrors              string
	ATURVendor, ATURCountry, UpstreamPower, DownstreamPower                       string
	ReceiveBlocks, TransmitBlocks, CellDelin, LinkRetrain, InitErrors             string
	InitTimeouts, LossOfFraming, ErroredSecs, SeverelyErroredSecs                 string
	ATUCFECErrors, HECErrors, ATUCHECErrors, ATUCCRCErrors                        string
	DSLDiagnoseState, CableNokDistance, DSLLastDiagnoseTime, DSLSignalLossTime    string
	DSLActive, DSLSync                                                            string
	CurrentUsername, CurrentUserRights                                            *string
	AnonymousLoginEnabled, DefaultPasswordActive                                  string
	HostPort, HostSpeed, HostGuest, HostVPN, HostWANAccess, HostUpdateAvailable   string

	UpgradeAvailable, OfferedVersion, UpdateState, BuildType string
	AutoUpdateMode, UpdateTime, LastFWVersion                string
	CurrentFWVersion, UpdateSuccessful                       string
	FirmwareResponseName                                     string
}

type soapArgument struct{ Name, Value string }

func (v *soapValues) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		e, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		var target *string
		switch e.Name.Local {
		case "NewConnectionStatus":
			target = &v.Status
		case "NewLastConnectionError":
			target = &v.LastError
		case "NewExternalIPAddress":
			target = &v.ExternalIP
		case "NewManufacturerName":
			target = &v.Manufacturer
		case "NewModelName":
			target = &v.Model
		case "NewSerialNumber":
			target = &v.Serial
		case "NewSoftwareVersion":
			target = &v.Software
		case "NewHardwareVersion":
			target = &v.Hardware
		case "NewUpTime", "NewUptime":
			target = &v.Uptime
		case "NewTotalBytesReceived":
			target = &v.TotalDownload
		case "NewTotalBytesSent":
			target = &v.TotalUpload
		case "NewWANAccessType":
			target = &v.WANAccessType
		case "NewPhysicalLinkStatus":
			target = &v.PhysicalLinkStatus
		case "NewLayer1DownstreamMaxBitRate":
			target = &v.DownstreamMaxBitRate
		case "NewLayer1UpstreamMaxBitRate":
			target = &v.UpstreamMaxBitRate
		case "NewTotalPacketsSent":
			target = &v.TotalPacketsSent
		case "NewTotalPacketsReceived":
			target = &v.TotalPacketsReceived
		case "NewX_AVM-DE_SyncDownstream":
			target = &v.SyncDownstream
		case "NewX_AVM-DE_SyncUpstream":
			target = &v.SyncUpstream
		case "NewX_AVM-DE_TariffDataRateDownstream":
			target = &v.TariffDownstream
		case "NewX_AVM-DE_TariffDataRateUpstream":
			target = &v.TariffUpstream
		case "NewX_AVM-DE_Provider":
			target = &v.Provider
		case "NewTotalNumberSyncGroups":
			target = &v.TotalNumberSyncGroups
		case "Newmax_ds":
			target = &v.MonitorMaxDownstream
		case "Newmax_us":
			target = &v.MonitorMaxUpstream
		case "Newds_current_bps":
			target = &v.DSCurrent
		case "Newmc_current_bps":
			target = &v.MCCurrent
		case "Newus_current_bps":
			target = &v.USCurrent
		case "Newprio_realtime_bps":
			target = &v.PrioRealtime
		case "Newprio_high_bps":
			target = &v.PrioHigh
		case "Newprio_default_bps":
			target = &v.PrioDefault
		case "Newprio_low_bps":
			target = &v.PrioLow
		case "NewCallListURL":
			target = &v.CallListURL
		case "NewDefaultConnectionService":
			target = &v.DefaultConnectionService
		case "NewHostNumberOfEntries":
			target = &v.HostNumberOfEntries
		case "NewMACAddress":
			target = &v.MACAddress
		case "NewIPAddress":
			target = &v.IPAddress
		case "NewInterfaceType":
			target = &v.InterfaceType
		case "NewActive":
			target = &v.Active
		case "NewHostName":
			target = &v.HostName
		case "NewAddressSource":
			target = &v.AddressSource
		case "NewLeaseTimeRemaining":
			v.LeaseTimeRemaining = new(string)
			target = v.LeaseTimeRemaining
		case "NewEnable":
			target = &v.Enable
		case "NewSSID":
			target = &v.SSID
		case "NewStandard":
			target = &v.Standard
		case "NewStatus":
			target = &v.WLANStatus
		case "NewMaxBitRate":
			target = &v.MaxBitRate
		case "NewChannel":
			target = &v.Channel
		case "NewX_AVM-DE_FrequencyBand":
			target = &v.FrequencyBand
		case "NewTotalAssociations":
			target = &v.TotalAssociations
		case "NewBeaconType":
			target = &v.BeaconType
		case "NewPossibleChannels":
			target = &v.PossibleChannels
		case "NewX_AVM-DEAuto_ChannelEnabled":
			target = &v.AutoChannelEnabled
		case "NewBeaconAdvertisementEnabled":
			target = &v.BeaconAdvertisementEnabled
		case "NewNightControl":
			target = &v.NightControl
		case "NewNightTimeControlNoForcedOff":
			target = &v.NightTimeControlNoForcedOff
		case "NewX_AVM-DE_WPSMode":
			target = &v.WPSMode
		case "NewX_AVM-DE_WPSStatus":
			target = &v.WPSStatus
		case "NewX_AVM-DE_IPTVoptimize":
			target = &v.IPTVOptimize
		case "NewX_AVM-DE_TimeoutActive":
			target = &v.GuestTimeoutActive
		case "NewX_AVM-DE_Timeout":
			target = &v.GuestTimeout
		case "NewX_AVM-DE_TimeRemain":
			target = &v.GuestTimeRemain
		case "NewX_AVM-DE_NoForcedOff":
			target = &v.GuestNoForcedOff
		case "NewX_AVM-DE_UserIsolation":
			target = &v.GuestUserIsolation
		case "NewX_AVM-DE_APType":
			target = &v.APType
		case "NewPortMappingNumberOfEntries":
			target = &v.PortMappingCount
		case "NewRemoteHost":
			v.RemoteHost = new(string)
			target = v.RemoteHost
		case "NewExternalPort":
			target = &v.ExternalPort
		case "NewProtocol":
			target = &v.PortMappingProtocol
		case "NewInternalPort":
			target = &v.InternalPort
		case "NewInternalClient":
			target = &v.InternalClient
		case "NewEnabled":
			target = &v.Enabled
		case "NewPortMappingDescription":
			target = &v.PortMappingDescription
		case "NewLeaseDuration":
			v.LeaseDuration = new(string)
			target = v.LeaseDuration
		case "NewDHCPServerConfigurable":
			target = &v.DHCPServerConfigurable
		case "NewDHCPServerEnable":
			target = &v.DHCPServerEnable
		case "NewDHCPRelay":
			target = &v.DHCPRelay
		case "NewMinAddress":
			target = &v.MinAddress
		case "NewMaxAddress":
			target = &v.MaxAddress
		case "NewSubnetMask":
			target = &v.SubnetMask
		case "NewDNSServers":
			target = &v.DNSServers
		case "NewDomainName":
			target = &v.DomainName
		case "NewIPRouters":
			target = &v.IPRouters
		case "NewLinkStatus":
			target = &v.LinkStatus
		case "NewModulationType":
			target = &v.ModulationType
		case "NewCurrentProfile":
			target = &v.CurrentProfile
		case "NewUpstreamCurrRate":
			target = &v.UpstreamCurrRate
		case "NewDownstreamCurrRate":
			target = &v.DownstreamCurrRate
		case "NewUpstreamMaxRate":
			target = &v.UpstreamMaxRate
		case "NewDownstreamMaxRate":
			target = &v.DownstreamMaxRate
		case "NewUpstreamNoiseMargin":
			target = &v.UpstreamNoiseMargin
		case "NewDownstreamNoiseMargin":
			target = &v.DownstreamNoiseMargin
		case "NewUpstreamAttenuation":
			target = &v.UpstreamAttenuation
		case "NewDownstreamAttenuation":
			target = &v.DownstreamAttenuation
		case "NewFECErrors":
			target = &v.FECErrors
		case "NewCRCErrors":
			target = &v.CRCErrors
		case "NewATURVendor":
			target = &v.ATURVendor
		case "NewATURCountry":
			target = &v.ATURCountry
		case "NewUpstreamPower":
			target = &v.UpstreamPower
		case "NewDownstreamPower":
			target = &v.DownstreamPower
		case "NewReceiveBlocks":
			target = &v.ReceiveBlocks
		case "NewTransmitBlocks":
			target = &v.TransmitBlocks
		case "NewCellDelin":
			target = &v.CellDelin
		case "NewLinkRetrain":
			target = &v.LinkRetrain
		case "NewInitErrors":
			target = &v.InitErrors
		case "NewInitTimeouts":
			target = &v.InitTimeouts
		case "NewLossOfFraming":
			target = &v.LossOfFraming
		case "NewErroredSecs":
			target = &v.ErroredSecs
		case "NewSeverelyErroredSecs":
			target = &v.SeverelyErroredSecs
		case "NewATUCFECErrors":
			target = &v.ATUCFECErrors
		case "NewHECErrors":
			target = &v.HECErrors
		case "NewATUCHECErrors":
			target = &v.ATUCHECErrors
		case "NewATUCCRCErrors":
			target = &v.ATUCCRCErrors
		// The vendor document (WANDSLInterfaceConfig v9, §3.3) spells this argument "Digagnose".
		case "NewX_AVM-DE_DSLDigagnoseState":
			target = &v.DSLDiagnoseState
		case "NewX_AVM-DE_CableNokDistance":
			target = &v.CableNokDistance
		case "NewX_AVM-DE_DSLLastDiagnoseTime":
			target = &v.DSLLastDiagnoseTime
		case "NewX_AVM-DE_DSLSignalLossTime":
			target = &v.DSLSignalLossTime
		case "NewX_AVM-DE_DSLActive":
			target = &v.DSLActive
		case "NewX_AVM-DE_DSLSync":
			target = &v.DSLSync
		case "GetInfoResponse", "X_AVM-DE_GetInfoResponse":
			v.FirmwareResponseName = e.Name.Local
		case "NewUpgradeAvailable":
			target = &v.UpgradeAvailable
		case "NewX_AVM-DE_Version":
			target = &v.OfferedVersion
		case "NewX_AVM-DE_UpdateState":
			target = &v.UpdateState
		case "NewX_AVM-DE_BuildType":
			target = &v.BuildType
		case "NewX_AVM-DE_AutoUpdateMode":
			target = &v.AutoUpdateMode
		case "NewX_AVM-DE_UpdateTime":
			target = &v.UpdateTime
		case "NewX_AVM-DE_LastFwVersion":
			target = &v.LastFWVersion
		case "NewX_AVM-DE_CurrentFwVersion":
			target = &v.CurrentFWVersion
		case "NewX_AVM-DE_UpdateSuccessful":
			target = &v.UpdateSuccessful
		case "NewX_AVM-DE_CurrentUsername":
			v.CurrentUsername = new(string)
			target = v.CurrentUsername
		case "NewX_AVM-DE_CurrentUserRights":
			v.CurrentUserRights = new(string)
			target = v.CurrentUserRights
		case "NewX_AVM-DE_AnonymousLoginEnabled":
			target = &v.AnonymousLoginEnabled
		case "NewX_AVM-DE_IsDefaultPasswordActive":
			target = &v.DefaultPasswordActive
		case "NewX_AVM-DE_Port":
			target = &v.HostPort
		case "NewX_AVM-DE_Speed":
			target = &v.HostSpeed
		case "NewX_AVM-DE_Guest":
			target = &v.HostGuest
		case "NewX_AVM-DE_VPN":
			target = &v.HostVPN
		case "NewX_AVM-DE_WANAccess":
			target = &v.HostWANAccess
		case "NewX_AVM-DE_UpdateAvailable":
			target = &v.HostUpdateAvailable
		case "errorCode":
			target = &v.FaultCode
		case "errorDescription":
			target = &v.FaultDescription
		}
		if target != nil {
			if err := d.DecodeElement(target, &e); err != nil {
				return err
			}
		}
	}
}

type Client struct {
	base               *url.URL
	username, password string
	http               *http.Client
	services           map[string]service
	allServices        []service
	now                func() time.Time
	digestChallenge    *string
}

func New(address, username, password string, httpClient *http.Client) (*Client, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	base, err := url.Parse(address)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("invalid router address %q", address)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("router address must use http or https")
	}
	if base.Port() == "" {
		port := "49000"
		if base.Scheme == "https" {
			port = "49443"
		}
		base.Host = net.JoinHostPort(base.Hostname(), port)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{base: base, username: username, password: password, http: httpClient, now: time.Now}, nil
}

func (c *Client) Doctor(ctx context.Context) (Doctor, error) {
	unknown := DoctorCheck{State: "unknown", Remediation: "restore TR-064 access, then run doctor again"}
	report := Doctor{
		Endpoint:       c.base.String(),
		Reachability:   unknown,
		Protocol:       unknown,
		Authentication: unknown,
		Capabilities: DoctorCapabilities{
			Status: unknown, Overview: unknown, WAN: unknown, Traffic: unknown, Watch: unknown, Calls: unknown, Devices: unknown, Leases: unknown, DHCP: unknown, DSL: unknown, Firmware: unknown, Account: unknown, WiFi: unknown, Forwards: unknown, Reboot: unknown, Backup: unknown, EventLog: unknown,
		},
	}
	if err := c.discover(ctx); err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "network" {
			report.Reachability = DoctorCheck{State: "unreachable", Remediation: "check --host, router power, and local network access"}
			return report, err
		}
		report.Reachability = DoctorCheck{State: "reachable"}
		if errors.As(err, &protocolErr) && protocolErr.StatusCode == http.StatusNotFound {
			report.Protocol = DoctorCheck{State: "disabled", Remediation: "enable TR-064 access on the router, then run doctor again"}
			return report, &Error{Kind: "unsupported", Operation: "doctor", Message: "TR-064 is disabled or unavailable"}
		}
		report.Protocol = DoctorCheck{State: "invalid", Remediation: "confirm the endpoint exposes a documented TR-064 device description"}
		return report, err
	}
	report.Reachability = DoctorCheck{State: "reachable"}
	report.Protocol = DoctorCheck{State: "available"}

	report.Capabilities.Status = c.advertisedCapability([]string{"urn:dslforum-org:service:DeviceInfo:"}, "enable the DeviceInfo TR-064 service or use supported firmware")
	report.Capabilities.WAN = c.advertisedCapability([]string{"urn:dslforum-org:service:WANIPConnection:", "urn:dslforum-org:service:WANPPPConnection:"}, "enable a WAN connection TR-064 service or use supported firmware")
	report.Capabilities.Traffic = c.advertisedCapability([]string{"urn:dslforum-org:service:WANCommonInterfaceConfig:"}, "enable the WAN common-interface TR-064 service or use supported firmware")
	report.Capabilities.Watch = c.watchCapability()
	report.Capabilities.Calls = c.advertisedCapability([]string{"urn:dslforum-org:service:X_AVM-DE_OnTel:"}, "enable telephony and its TR-064 service or use supported firmware")
	hostsCapability := c.advertisedCapability([]string{"urn:dslforum-org:service:Hosts:"}, "enable the Hosts TR-064 service or use supported firmware")
	report.Capabilities.Devices = hostsCapability
	report.Capabilities.Leases = hostsCapability
	report.Capabilities.DHCP = c.advertisedCapability([]string{dhcpServicePrefix}, dhcpRemediation)
	report.Capabilities.DSL = c.advertisedCapability([]string{dslServicePrefix}, dslRemediation)
	report.Capabilities.Firmware = c.advertisedCapability([]string{firmwareServicePrefix}, firmwareRemediation)
	report.Capabilities.Account = c.accountCapability()
	report.Capabilities.WiFi = c.advertisedCapability([]string{wlanServicePrefix}, wifiRemediation)
	report.Capabilities.Forwards = c.advertisedCapability(wanMappingPrefixes, forwardsRemediation)
	report.Capabilities.Reboot = c.rebootCapability()
	report.Capabilities.Backup = c.backupCapability()
	report.Capabilities.EventLog = c.advertisedCapability([]string{eventLogServicePrefix}, eventLogRemediation)
	if report.Capabilities.Status.State == "advertised" && report.Capabilities.WAN.State == "advertised" && report.Capabilities.Traffic.State == "advertised" {
		report.Capabilities.Overview = DoctorCheck{State: "advertised"}
	} else {
		report.Capabilities.Overview = DoctorCheck{State: "unsupported", Remediation: "resolve unsupported status, wan, or traffic capability"}
	}
	if report.Capabilities.Status.State != "advertised" {
		report.Authentication = DoctorCheck{State: "not_checked", Remediation: "enable the DeviceInfo TR-064 service, then run doctor again"}
		return report, &Error{Kind: "unsupported", Operation: "doctor", Message: "router does not advertise DeviceInfo; authentication could not be verified"}
	}
	status, err := c.Status(ctx)
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "auth" {
			report.Authentication = DoctorCheck{State: "unauthenticated", Remediation: "set valid ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD credentials"}
		} else {
			report.Authentication = DoctorCheck{State: "unverified", Remediation: "resolve the reported DeviceInfo error, then run doctor again"}
		}
		return report, err
	}
	report.Authentication = DoctorCheck{State: "authenticated"}
	report.Model = status.Model
	report.Firmware = status.Software
	return report, nil
}

func (c *Client) advertisedCapability(prefixes []string, remediation string) DoctorCheck {
	for serviceType := range c.services {
		for _, prefix := range prefixes {
			if strings.HasPrefix(serviceType, prefix) {
				return DoctorCheck{State: "advertised"}
			}
		}
	}
	return DoctorCheck{State: "unsupported", Remediation: remediation}
}

func (c *Client) watchCapability() DoctorCheck {
	for _, prefixes := range [][]string{
		{"urn:dslforum-org:service:Layer3Forwarding:"},
		{"urn:dslforum-org:service:WANCommonInterfaceConfig:"},
		wanMappingPrefixes,
	} {
		if c.advertisedCapability(prefixes, watchRemediation).State != "advertised" {
			return DoctorCheck{State: "unsupported", Remediation: watchRemediation}
		}
	}
	return DoctorCheck{State: "advertised"}
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	v, err := c.action(ctx, "urn:dslforum-org:service:DeviceInfo:", "GetInfo")
	if err != nil {
		return Status{}, err
	}
	return Status{v.Manufacturer, v.Model, v.Serial, v.Software, v.Hardware, number(v.Uptime)}, nil
}

func (c *Client) WAN(ctx context.Context) (WAN, error) {
	if err := c.discover(ctx); err != nil {
		return WAN{}, err
	}
	active, err := c.activeWANService(ctx)
	if err != nil {
		return WAN{}, wanActiveWANError(err)
	}
	v, err := c.actionOnService(ctx, active, "GetStatusInfo")
	if err != nil {
		return WAN{}, err
	}
	ip, err := c.actionOnService(ctx, active, "GetExternalIPAddress")
	if err != nil {
		return WAN{}, err
	}
	return WAN{v.Status, ip.ExternalIP, ipFamily(ip.ExternalIP), number(v.Uptime), v.LastError}, nil
}

const wanDetailRemediation = "use firmware that advertises WANCommonInterfaceConfig:GetCommonLinkProperties"

func (c *Client) WANDetail(ctx context.Context) (WANDetail, error) {
	if err := c.discover(ctx); err != nil {
		return WANDetail{}, wanDetailError(err)
	}
	common, commonActions, err := c.wanDetailService(ctx)
	if err != nil {
		return WANDetail{}, err
	}
	link, err := c.actionOnService(ctx, common, "GetCommonLinkProperties")
	if err != nil {
		return WANDetail{}, wanDetailError(err)
	}
	downstream, err := parseWANDetailUint(link.DownstreamMaxBitRate, "maximum download bit rate")
	if err != nil {
		return WANDetail{}, err
	}
	upstream, err := parseWANDetailUint(link.UpstreamMaxBitRate, "maximum upload bit rate")
	if err != nil {
		return WANDetail{}, err
	}
	accessType, err := normalizeWANDetailState(link.WANAccessType, []string{"DSL", "Ethernet", "X_AVM-DE_Fiber", "X_AVM-DE_UMTS", "X_AVM-DE_Cable", "X_AVM-DE_LTE", "unknown", "POTS", "Cable", "Other"}, "access type")
	if err != nil {
		return WANDetail{}, err
	}
	physicalStatus, err := normalizeWANDetailState(link.PhysicalLinkStatus, []string{"Up", "Down", "Initializing", "Unavailable"}, "physical-link status")
	if err != nil {
		return WANDetail{}, err
	}
	result := WANDetail{AccessType: accessType, PhysicalLinkStatus: physicalStatus, MaxDownloadBitsPerSecond: downstream, MaxUploadBitsPerSecond: upstream, DNSServers: []string{}}
	if err := c.advertisesLayer3Action(ctx); err != nil {
		return WANDetail{}, wanDetailError(err)
	}
	active, err := c.activeWANService(ctx)
	if err != nil {
		return WANDetail{}, wanDetailError(err)
	}
	active, activeActions, err := c.wanDetailActiveActions(ctx, active)
	if err != nil {
		return WANDetail{}, err
	}
	if activeActions["X_GetDNSServers"] {
		dns, err := c.actionOnService(ctx, active, "X_GetDNSServers")
		if err != nil {
			return WANDetail{}, wanDetailError(err)
		}
		if strings.TrimSpace(dns.DNSServers) != "" {
			for _, value := range strings.Split(dns.DNSServers, ",") {
				address, err := netip.ParseAddr(strings.TrimSpace(value))
				if err != nil {
					return WANDetail{}, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid DNS server address"}
				}
				result.DNSServers = append(result.DNSServers, address.String())
			}
		}
	}
	if err := c.wanDetailCommonReads(ctx, common, commonActions, &result); err != nil {
		return WANDetail{}, err
	}
	return result, nil
}

func (c *Client) wanDetailCommonReads(ctx context.Context, common service, actions map[string]bool, result *WANDetail) error {
	for _, counter := range []struct {
		action string
		value  func(soapValues) string
		target **uint64
	}{
		{"GetTotalBytesReceived", func(v soapValues) string { return v.TotalDownload }, &result.TotalDownloadBytes},
		{"GetTotalBytesSent", func(v soapValues) string { return v.TotalUpload }, &result.TotalUploadBytes},
		{"GetTotalPacketsReceived", func(v soapValues) string { return v.TotalPacketsReceived }, &result.TotalDownloadPackets},
		{"GetTotalPacketsSent", func(v soapValues) string { return v.TotalPacketsSent }, &result.TotalUploadPackets},
	} {
		if !actions[counter.action] {
			continue
		}
		values, err := c.actionOnService(ctx, common, counter.action)
		if err != nil {
			return wanDetailError(err)
		}
		n, err := parseWANDetailUint(counter.value(values), "WAN counter")
		if err != nil {
			return err
		}
		*counter.target = &n
	}
	addon, addonAuthorized, err := c.wanDetailRightsRestrictedRead(ctx, common, actions, "X_AVM-DE_GetAddonInfos")
	if err != nil {
		return err
	}
	if addonAuthorized {
		for _, field := range []struct {
			value        string
			target       **uint64
			zeroIsAbsent bool
		}{
			{addon.SyncDownstream, &result.SyncDownloadBitsPerSecond, false},
			{addon.SyncUpstream, &result.SyncUploadBitsPerSecond, false},
			{addon.TariffDownstream, &result.TariffDownloadBitsPerSecond, true},
			{addon.TariffUpstream, &result.TariffUploadBitsPerSecond, true},
		} {
			n, err := parseWANDetailUint(field.value, "sync or tariff bit rate")
			if err != nil {
				return err
			}
			if n == 0 && field.zeroIsAbsent {
				continue
			}
			*field.target = &n
		}
	}
	active, providerAuthorized, err := c.wanDetailRightsRestrictedRead(ctx, common, actions, "X_AVM-DE_GetActiveProvider")
	if err != nil {
		return err
	}
	if providerAuthorized {
		provider := strings.TrimSpace(active.Provider)
		if len(provider) > 128 || strings.IndexFunc(provider, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid provider name"}
		}
		if provider != "" {
			result.Provider = &provider
		}
	}
	if actions["X_AVM-DE_GetOnlineMonitor"] {
		groups, err := c.wanDetailSyncGroups(ctx, common)
		if err != nil {
			return err
		}
		result.SyncGroups = groups
	}
	return nil
}

func (c *Client) wanDetailRightsRestrictedRead(ctx context.Context, common service, actions map[string]bool, action string) (soapValues, bool, error) {
	if !actions[action] {
		return soapValues{}, false, nil
	}
	values, err := c.actionOnService(ctx, common, action)
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Kind == "router" && protocolErr.FaultCode == "606" {
			return soapValues{}, false, nil
		}
		return soapValues{}, false, wanDetailError(err)
	}
	return values, true, nil
}

const maxWANSyncGroups = 16
const maxWANRateSamples = 256

func (c *Client) wanDetailSyncGroups(ctx context.Context, common service) ([]WANSyncGroup, error) {
	groups := []WANSyncGroup{}
	var count uint64
	for index := uint64(0); index == 0 || index < count; index++ {
		values, err := c.actionOnService(ctx, common, "X_AVM-DE_GetOnlineMonitor", soapArgument{"NewSyncGroupIndex", strconv.FormatUint(index, 10)})
		if err != nil {
			return nil, wanDetailError(err)
		}
		n, err := parseWANDetailUint(values.TotalNumberSyncGroups, "sync group count")
		if err != nil {
			return nil, err
		}
		if n > maxWANSyncGroups || index > 0 && n != count {
			return nil, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an excessive or changing sync group count"}
		}
		count = n
		if count == 0 {
			break
		}
		group, err := parseWANSyncGroup(values, index)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func parseWANSyncGroup(values soapValues, index uint64) (WANSyncGroup, error) {
	group := WANSyncGroup{Index: index}
	var err error
	group.MaxDownloadBytesPerSecond, err = parseWANDetailUint(values.MonitorMaxDownstream, "sync group maximum download byte rate")
	if err != nil {
		return WANSyncGroup{}, err
	}
	group.MaxUploadBytesPerSecond, err = parseWANDetailUint(values.MonitorMaxUpstream, "sync group maximum upload byte rate")
	if err != nil {
		return WANSyncGroup{}, err
	}
	for _, field := range []struct {
		value  string
		target *[]uint64
	}{
		{values.DSCurrent, &group.DSCurrentBytesPerSecond},
		{values.MCCurrent, &group.MCCurrentBytesPerSecond},
		{values.USCurrent, &group.UploadBytesPerSecond},
		{values.PrioRealtime, &group.RealtimeUploadBytesPerSecond},
		{values.PrioHigh, &group.HighUploadBytesPerSecond},
		{values.PrioDefault, &group.DefaultUploadBytesPerSecond},
		{values.PrioLow, &group.LowUploadBytesPerSecond},
	} {
		*field.target = []uint64{}
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		parts := strings.Split(field.value, ",")
		if len(parts) > maxWANRateSamples {
			return WANSyncGroup{}, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an excessive sync group rate series"}
		}
		for _, part := range parts {
			n, err := parseWANDetailUint(part, "sync group byte rate")
			if err != nil {
				return WANSyncGroup{}, err
			}
			*field.target = append(*field.target, n)
		}
	}
	return group, nil
}

func (c *Client) wanDetailService(ctx context.Context) (service, map[string]bool, error) {
	const prefix = "urn:dslforum-org:service:WANCommonInterfaceConfig:"
	var services []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, prefix) {
			services = append(services, svc)
		}
	}
	if len(services) != 1 {
		return service{}, nil, &Error{Kind: "unsupported", Operation: "wan detail", Message: "router does not advertise exactly one WANCommonInterfaceConfig service; " + wanDetailRemediation}
	}
	svc := services[0]
	control, controlErr := c.base.Parse(svc.ControlURL)
	scpdURL, scpdErr := c.base.Parse(svc.SCPDURL)
	if controlErr != nil || svc.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.Fragment != "" || scpdErr != nil || svc.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.Fragment != "" {
		return service{}, nil, &Error{Kind: "protocol", Operation: "wan detail", Message: "router advertised an invalid WAN common-interface URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, nil, wanDetailError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return service{}, nil, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid WAN common-interface service description"}
	}
	actions := map[string]bool{}
	for _, action := range scpd.Actions {
		actions[strings.TrimSpace(action.Name)] = true
	}
	if !actions["GetCommonLinkProperties"] {
		return service{}, nil, &Error{Kind: "unsupported", Operation: "wan detail", Message: "router does not advertise WANCommonInterfaceConfig:GetCommonLinkProperties; " + wanDetailRemediation}
	}
	svc.ControlURL = control.Path
	return svc, actions, nil
}

func (c *Client) wanDetailActiveActions(ctx context.Context, svc service) (service, map[string]bool, error) {
	control, controlErr := c.base.Parse(svc.ControlURL)
	scpdURL, scpdErr := c.base.Parse(svc.SCPDURL)
	if controlErr != nil || svc.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.Fragment != "" || scpdErr != nil || svc.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.Fragment != "" {
		return service{}, nil, &Error{Kind: "protocol", Operation: "wan detail", Message: "router advertised an invalid active WAN service URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, nil, wanDetailError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return service{}, nil, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid active WAN service description"}
	}
	actions := map[string]bool{}
	for _, action := range scpd.Actions {
		actions[strings.TrimSpace(action.Name)] = true
	}
	svc.ControlURL = control.Path
	return svc, actions, nil
}

func normalizeWANDetailState(value string, allowed []string, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &Error{Kind: "protocol", Operation: "wan detail", Message: "router omitted the WAN " + field}
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "unknown", nil
}

func parseWANDetailUint(value, field string) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, &Error{Kind: "protocol", Operation: "wan detail", Message: "router returned an invalid " + field}
	}
	return n, nil
}

func wanDetailError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "wan detail", Message: "WAN detail inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode = protocolErr.Kind, protocolErr.StatusCode
		if protocolErr.Kind == "router" && protocolErr.FaultCode == "401" {
			result.Kind = "unsupported"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support the documented WAN detail action; " + wanDetailRemediation
	}
	return result
}

const (
	dhcpServicePrefix = "urn:dslforum-org:service:LANHostConfigManagement:"
	dhcpRemediation   = "enable LANHostConfigManagement with documented DHCP configuration reads, or use supported firmware"
)

func (c *Client) DHCP(ctx context.Context) (DHCP, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("DHCP inspection refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return DHCP{}, dhcpError(err)
	}
	target, err := client.dhcpService(ctx)
	if err != nil {
		return DHCP{}, err
	}
	values, err := client.actionOnService(ctx, target, "GetInfo")
	if err != nil {
		return DHCP{}, dhcpError(err)
	}
	return parseDHCPInfo(values)
}

func (c *Client) dhcpService(ctx context.Context) (service, error) {
	var matches []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, dhcpServicePrefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 {
		return service{}, &Error{Kind: "unsupported", Operation: "dhcp", Message: "router does not advertise exactly one LANHostConfigManagement service; " + dhcpRemediation}
	}
	target := matches[0]
	control, controlErr := c.base.Parse(target.ControlURL)
	scpdURL, scpdErr := c.base.Parse(target.SCPDURL)
	if controlErr != nil || target.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" || scpdErr != nil || target.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return service{}, &Error{Kind: "protocol", Operation: "dhcp", Message: "router advertised an invalid LANHostConfigManagement service URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, dhcpError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return service{}, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid LANHostConfigManagement service description"}
	}
	actions := make(map[string]bool, len(scpd.Actions))
	for _, action := range scpd.Actions {
		actions[strings.TrimSpace(action.Name)] = true
	}
	if !actions["GetInfo"] {
		return service{}, &Error{Kind: "unsupported", Operation: "dhcp", Message: "router does not advertise LANHostConfigManagement:GetInfo; " + dhcpRemediation}
	}
	target.ControlURL = control.Path
	return target, nil
}

func parseDHCPInfo(values soapValues) (DHCP, error) {
	result := DHCP{Routers: []string{}, DNSServers: []string{}}
	configurable, err := parseDHCPBool(values.DHCPServerConfigurable, "server configurable state")
	if err != nil {
		return DHCP{}, err
	}
	result.ServerConfigurable = configurable
	result.ServerEnabled, err = parseDHCPBool(values.DHCPServerEnable, "server enabled state")
	if err != nil {
		return DHCP{}, err
	}
	result.RelayEnabled, err = parseDHCPBool(values.DHCPRelay, "relay state")
	if err != nil {
		return DHCP{}, err
	}
	result.AddressRangeStart, result.AddressRangeEnd, err = parseDHCPRange(values.MinAddress, values.MaxAddress)
	if err != nil {
		return DHCP{}, err
	}
	result.SubnetMask, err = parseDHCPSubnetMask(values.SubnetMask)
	if err != nil {
		return DHCP{}, err
	}
	result.Routers, err = parseDHCPAddressList(values.IPRouters, "router list")
	if err != nil {
		return DHCP{}, err
	}
	result.DNSServers, err = parseDHCPAddressList(values.DNSServers, "DNS server list")
	if err != nil {
		return DHCP{}, err
	}
	result.DomainName = optionalDHCPText(values.DomainName)
	return result, nil
}

func parseDHCPBool(value, field string) (*bool, error) {
	switch strings.TrimSpace(value) {
	case "0", "false":
		result := false
		return &result, nil
	case "1", "true":
		result := true
		return &result, nil
	case "":
		return nil, nil
	}
	return nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP " + field}
}

func parseDHCPRange(start, end string) (*string, *string, error) {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	var startAddress, endAddress netip.Addr
	var startResult, endResult *string
	if start != "" {
		address, err := netip.ParseAddr(start)
		if err != nil || !address.Is4() {
			return nil, nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP address range"}
		}
		startAddress = address
		normalized := address.String()
		startResult = &normalized
	}
	if end != "" {
		address, err := netip.ParseAddr(end)
		if err != nil || !address.Is4() {
			return nil, nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP address range"}
		}
		endAddress = address
		normalized := address.String()
		endResult = &normalized
	}
	if startResult != nil && endResult != nil && startAddress.Compare(endAddress) > 0 {
		return nil, nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP address range"}
	}
	return startResult, endResult, nil
}

func parseDHCPSubnetMask(value string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() {
		return nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP subnet mask"}
	}
	octets := address.As4()
	mask := net.IPMask(octets[:])
	if _, bits := mask.Size(); bits != 32 {
		return nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP subnet mask"}
	}
	normalized := address.String()
	return &normalized, nil
}

func parseDHCPAddressList(value, field string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || !address.Is4() {
			return nil, &Error{Kind: "protocol", Operation: "dhcp", Message: "router returned an invalid DHCP " + field}
		}
		result = append(result, address.String())
	}
	return result, nil
}

func optionalDHCPText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func dhcpError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "dhcp", Message: "DHCP server configuration inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
		if result.Kind == "router" && result.FaultCode == "401" {
			result.Kind = "unsupported"
		} else if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Kind = "auth"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support documented DHCP configuration reads; " + dhcpRemediation
	}
	return result
}

const (
	dslServicePrefix = "urn:dslforum-org:service:WANDSLInterfaceConfig:"
	dslRemediation   = "enable WANDSLInterfaceConfig with X_AVM-DE_GetDSLInfo, or use supported firmware"
)

func (c *Client) DSL(ctx context.Context) (DSL, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("DSL inspection refuses redirects") }
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return DSL{}, dslError(err)
	}
	target, err := client.dslService(ctx)
	if err != nil {
		return DSL{}, err
	}
	values, err := client.actionOnService(ctx, target, "X_AVM-DE_GetDSLInfo")
	if err != nil {
		return DSL{}, dslError(err)
	}
	return parseDSLInfo(values)
}

func (c *Client) dslService(ctx context.Context) (service, error) {
	target, actions, err := c.dslServiceActions(ctx, "dsl", dslRemediation, dslError)
	if err != nil {
		return service{}, err
	}
	if !actions["X_AVM-DE_GetDSLInfo"] {
		return service{}, &Error{Kind: "unsupported", Operation: "dsl", Message: "router does not advertise WANDSLInterfaceConfig:X_AVM-DE_GetDSLInfo; " + dslRemediation}
	}
	return target, nil
}

func (c *Client) dslServiceActions(ctx context.Context, operation, remediation string, wrap func(error) *Error) (service, map[string]bool, error) {
	matches := make([]service, 0, 1)
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, dslServicePrefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 {
		return service{}, nil, &Error{Kind: "unsupported", Operation: operation, Message: "router does not advertise exactly one WANDSLInterfaceConfig service; " + remediation}
	}
	target := matches[0]
	control, controlErr := c.base.Parse(target.ControlURL)
	scpdURL, scpdErr := c.base.Parse(target.SCPDURL)
	if controlErr != nil || target.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" || scpdErr != nil || target.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return service{}, nil, &Error{Kind: "protocol", Operation: operation, Message: "router advertised an invalid WANDSLInterfaceConfig service URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, nil, wrap(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return service{}, nil, &Error{Kind: "protocol", Operation: operation, Message: "router returned an invalid WANDSLInterfaceConfig service description"}
	}
	actions := make(map[string]bool, len(scpd.Actions))
	for _, action := range scpd.Actions {
		actions[strings.TrimSpace(action.Name)] = true
	}
	target.ControlURL = control.Path
	return target, actions, nil
}

func parseDSLInfo(values soapValues) (DSL, error) {
	linkStatus, err := normalizeDSLState(values.LinkStatus, []string{"Up", "Down", "Initializing", "Unavailable"}, "link status")
	if err != nil {
		return DSL{}, err
	}
	modulation, err := normalizeDSLState(values.ModulationType, []string{"ADSL", "G.lite", "G.shdsl", "IDSL", "HDSL", "SDSL", "VDSL"}, "modulation type")
	if err != nil {
		return DSL{}, err
	}
	profile, err := requiredDSLText(values.CurrentProfile, "current profile")
	if err != nil {
		return DSL{}, err
	}
	rates := []struct {
		value, field string
		target       *uint64
	}{
		{values.UpstreamCurrRate, "upstream current rate", new(uint64)}, {values.DownstreamCurrRate, "downstream current rate", new(uint64)}, {values.UpstreamMaxRate, "upstream maximum rate", new(uint64)}, {values.DownstreamMaxRate, "downstream maximum rate", new(uint64)}, {values.FECErrors, "FEC errors", new(uint64)}, {values.CRCErrors, "CRC errors", new(uint64)},
	}
	for _, field := range rates {
		n, parseErr := parseDSLUint(field.value, field.field, 32)
		if parseErr != nil {
			return DSL{}, parseErr
		}
		*field.target = n
	}
	unsigned := []struct {
		value, field string
		target       *uint32
	}{
		{values.UpstreamNoiseMargin, "upstream noise margin", new(uint32)}, {values.DownstreamNoiseMargin, "downstream noise margin", new(uint32)}, {values.UpstreamAttenuation, "upstream attenuation", new(uint32)}, {values.DownstreamAttenuation, "downstream attenuation", new(uint32)},
	}
	for _, field := range unsigned {
		n, parseErr := parseDSLUint(field.value, field.field, 32)
		if parseErr != nil {
			return DSL{}, parseErr
		}
		*field.target = uint32(n)
	}
	result := DSL{LinkStatus: linkStatus, ModulationType: modulation, CurrentProfile: profile, UpstreamCurrentKbps: *rates[0].target, DownstreamCurrentKbps: *rates[1].target, UpstreamMaxKbps: *rates[2].target, DownstreamMaxKbps: *rates[3].target, UpstreamNoiseMarginTenthDB: *unsigned[0].target, DownstreamNoiseMarginTenthDB: *unsigned[1].target, UpstreamAttenuationTenthDB: *unsigned[2].target, DownstreamAttenuationTenthDB: *unsigned[3].target, FECErrors: *rates[4].target, CRCErrors: *rates[5].target}
	result.ATURVendor, err = optionalDSLText(values.ATURVendor, "ATUR vendor")
	if err != nil {
		return DSL{}, err
	}
	result.ATURCountry, err = optionalDSLText(values.ATURCountry, "ATUR country")
	if err != nil {
		return DSL{}, err
	}
	result.UpstreamPowerTenthDBm, err = optionalDSLUint16(values.UpstreamPower, "upstream power")
	if err != nil {
		return DSL{}, err
	}
	result.DownstreamPowerTenthDBm, err = optionalDSLUint16(values.DownstreamPower, "downstream power")
	if err != nil {
		return DSL{}, err
	}
	return result, nil
}

func normalizeDSLState(value string, allowed []string, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &Error{Kind: "protocol", Operation: "dsl", Message: "router omitted the DSL " + field}
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "unknown", nil
}

func parseDSLUint(value, field string, bitSize int) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, bitSize)
	if err != nil {
		return 0, &Error{Kind: "protocol", Operation: "dsl", Message: "router returned an invalid DSL " + field}
	}
	return n, nil
}

func optionalDSLUint16(value, field string) (*uint16, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	n, err := parseDSLUint(value, field, 16)
	if err != nil {
		return nil, err
	}
	result := uint16(n)
	return &result, nil
}

func requiredDSLText(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &Error{Kind: "protocol", Operation: "dsl", Message: "router omitted the DSL " + field}
	}
	if len(value) > 128 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", &Error{Kind: "protocol", Operation: "dsl", Message: "router returned an invalid DSL " + field}
	}
	return value, nil
}

func optionalDSLText(value, field string) (*string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	text, err := requiredDSLText(value, field)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func dslError(err error) *Error {
	return dslOperationError("dsl", "DSL diagnostic inspection failed", "router does not support documented DSL diagnostics; "+dslRemediation, err)
}

func dslOperationError(operation, message, unsupported string, err error) *Error {
	result := &Error{Kind: "protocol", Operation: operation, Message: message}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
		if result.Kind == "router" && result.FaultCode == "401" {
			result.Kind = "unsupported"
		} else if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Kind = "auth"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = unsupported
	}
	return result
}

func (c *Client) Overview(ctx context.Context) (Overview, error) {
	var overview Overview
	var err error
	overview.Router, err = c.Status(ctx)
	if err != nil {
		return Overview{}, err
	}
	overview.WAN, err = c.WAN(ctx)
	if err != nil {
		return Overview{}, err
	}
	overview.Traffic, err = c.Traffic(ctx)
	if err != nil {
		return Overview{}, err
	}
	return overview, nil
}

func (c *Client) Traffic(ctx context.Context) (Traffic, error) {
	received, err := c.action(ctx, "urn:dslforum-org:service:WANCommonInterfaceConfig:", "GetTotalBytesReceived")
	if err != nil {
		return Traffic{}, err
	}
	sent, err := c.action(ctx, "urn:dslforum-org:service:WANCommonInterfaceConfig:", "GetTotalBytesSent")
	if err != nil {
		return Traffic{}, err
	}
	return Traffic{
		TotalDownloadBytes: number(received.TotalDownload),
		TotalUploadBytes:   number(sent.TotalUpload),
		ObservedAt:         c.now().UTC().Format(time.RFC3339),
	}, nil
}

const (
	wlanServicePrefix = "urn:dslforum-org:service:WLANConfiguration:"
	wlanIDPrefix      = "urn:WLANConfiguration-com:serviceId:WLANConfiguration"
	wifiRemediation   = "enable the WLANConfiguration TR-064 service or use supported firmware"
	guestRemediation  = "use firmware that implements documented guest identification and status actions for every WLANConfiguration instance"
)

// wlanServices returns every advertised WLANConfiguration instance, sorted by
// the validated numeric suffix of its service identifier. Identifiers are
// validated before any action is invoked so a radio can never be misidentified.
func (c *Client) wlanServices(ctx context.Context) ([]service, error) {
	if err := c.discover(ctx); err != nil {
		return nil, wifiError(err)
	}
	services := make([]service, 0)
	ids := make(map[uint64]bool)
	for _, svc := range c.allServices {
		if !strings.HasPrefix(svc.Type, wlanServicePrefix) {
			continue
		}
		suffix := strings.TrimPrefix(svc.ID, wlanIDPrefix)
		id, err := strconv.ParseUint(suffix, 10, 64)
		if !strings.HasPrefix(svc.ID, wlanIDPrefix) || err != nil || strconv.FormatUint(id, 10) != suffix || ids[id] {
			return nil, &Error{Kind: "protocol", Operation: "wifi", Message: "router advertised an invalid or duplicate WLAN service identifier"}
		}
		ids[id] = true
		services = append(services, svc)
	}
	if len(services) == 0 {
		return nil, &Error{Kind: "unsupported", Operation: "wifi", Message: "router does not advertise WLANConfiguration; " + wifiRemediation}
	}
	sort.Slice(services, func(i, j int) bool {
		// The identifier suffix is a validated canonical number, so its numeric
		// value can be recovered here.
		left, _ := strconv.ParseUint(strings.TrimPrefix(services[i].ID, wlanIDPrefix), 10, 64)
		right, _ := strconv.ParseUint(strings.TrimPrefix(services[j].ID, wlanIDPrefix), 10, 64)
		return left < right
	})
	return services, nil
}

func (c *Client) WiFi(ctx context.Context) ([]Radio, error) {
	services, err := c.wlanServices(ctx)
	if err != nil {
		return nil, err
	}
	radios := make([]Radio, 0, len(services))
	for _, svc := range services {
		radio, err := c.readRadio(ctx, svc, "wifi", wifiError)
		if err != nil {
			return nil, err
		}
		radios = append(radios, radio)
	}
	return radios, nil
}

func (c *Client) GuestWiFi(ctx context.Context) ([]GuestNetwork, error) {
	services, err := c.wlanServices(ctx)
	if err != nil {
		return nil, guestError(err)
	}
	guestServices := make([]service, 0, 1)
	configurations := map[string]*GuestConfiguration{}
	for _, svc := range services {
		actions, err := c.wlanReadActions(ctx, svc, "guest", guestError)
		if err != nil {
			return nil, err
		}
		if !actions["X_AVM-DE_GetWLANExtInfo"] {
			return nil, guestError(&Error{Kind: "unsupported"})
		}
		info, err := c.actionOnService(ctx, svc, "X_AVM-DE_GetWLANExtInfo")
		if err != nil {
			return nil, guestError(err)
		}
		switch strings.TrimSpace(info.APType) {
		case "normal":
		case "guest":
			guestServices = append(guestServices, svc)
			configurations[svc.ID] = guestConfiguration(info)
		default:
			return nil, &Error{Kind: "protocol", Operation: "guest", Message: "router returned an invalid Wi-Fi access-point type"}
		}
	}
	guests := make([]GuestNetwork, 0, len(guestServices))
	for _, svc := range guestServices {
		radio, err := c.readRadio(ctx, svc, "guest", guestError)
		if err != nil {
			return nil, err
		}
		guests = append(guests, GuestNetwork{
			ServiceID: radio.ServiceID, SSID: radio.SSID, Enabled: radio.Enabled,
			Channel: radio.Channel, Band: radio.Band, Standard: radio.Standard,
			AssociatedClients: radio.AssociatedDevices, SecurityMode: radio.SecurityMode,
			Configuration: configurations[svc.ID],
		})
	}
	return guests, nil
}

func (c *Client) readRadio(ctx context.Context, svc service, operation string, actionError func(error) *Error) (Radio, error) {
	info, err := c.actionOnService(ctx, svc, "GetInfo")
	if err != nil {
		return Radio{}, actionError(err)
	}
	enabled, err := parseEnable(info.Enable)
	if err != nil {
		return Radio{}, &Error{Kind: "protocol", Operation: operation, Message: "router returned an invalid Wi-Fi enable state"}
	}
	band := "unknown"
	switch info.FrequencyBand {
	case "2400", "5000", "6000":
		band = info.FrequencyBand
	}
	standard := "unknown"
	switch info.Standard {
	case "b", "g", "n", "ac", "ax", "be":
		standard = info.Standard
	}
	channel, err := c.actionOnService(ctx, svc, "GetChannelInfo")
	if err != nil {
		return Radio{}, actionError(err)
	}
	number, err := strconv.ParseUint(strings.TrimSpace(channel.Channel), 10, 8)
	if err != nil {
		return Radio{}, &Error{Kind: "protocol", Operation: operation, Message: "router returned an invalid Wi-Fi channel"}
	}
	associations, err := c.actionOnService(ctx, svc, "GetTotalAssociations")
	if err != nil {
		return Radio{}, actionError(err)
	}
	count, err := strconv.ParseUint(strings.TrimSpace(associations.TotalAssociations), 10, 16)
	if err != nil {
		return Radio{}, &Error{Kind: "protocol", Operation: operation, Message: "router returned an invalid Wi-Fi association count"}
	}
	security, err := c.actionOnService(ctx, svc, "GetBeaconType")
	if err != nil {
		return Radio{}, actionError(err)
	}
	if security.BeaconType == "" {
		return Radio{}, &Error{Kind: "protocol", Operation: operation, Message: "router omitted the Wi-Fi security mode"}
	}
	mode := "unknown"
	switch security.BeaconType {
	case "None", "Basic", "WPA", "11i", "WPAand11i", "WPA3", "11iandWPA3", "OWE", "OWETrans":
		mode = security.BeaconType
	}
	return Radio{svc.ID, info.SSID, enabled, number, band, standard, count, mode}, nil
}

func guestError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "guest", Message: "guest Wi-Fi inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind = protocolErr.Kind
		result.StatusCode = protocolErr.StatusCode
		if protocolErr.Kind == "router" && protocolErr.FaultCode == "401" {
			result.Kind = "unsupported"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router cannot completely identify and inspect guest Wi-Fi through documented actions; " + guestRemediation
	}
	return result
}

func wifiError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "wifi", Message: "Wi-Fi inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind = protocolErr.Kind
		result.StatusCode = protocolErr.StatusCode
	}
	return result
}

// parseEnable strictly converts the documented 0/1 enable state.
func parseEnable(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, &Error{Kind: "protocol", Operation: "wifi", Message: "router returned an invalid Wi-Fi enable state"}
	}
}

// WiFiMutation describes one WLANConfiguration radio state change or its
// pre-confirmation preview. Only the documented GetInfo and SetEnable actions
// are used; the router's current state is always read first, and a mutation is
// only sent when the state actually differs from the requested state.
type WiFiMutation struct {
	Instance string
	Action   string
	Current  bool
	Intended bool
	Preview  bool
	Previous bool
	Changed  bool
}

const wifiMutationRemediation = "use firmware that implements WLANConfiguration:SetEnable, or use supported firmware"

// WiFiMutation reads the current enable state of the identified radio. Without
// confirmation it returns a preview without any state change. With
// confirmation it sends SetEnable only when the state differs, then re-reads
// GetInfo and refuses to report success unless the router confirmed the new
// state. Already-enabled and already-disabled targets are successful no-change
// results.
func (c *Client) WiFiMutation(ctx context.Context, instance uint64, enable, confirm bool) (WiFiMutation, error) {
	services, err := c.wlanServices(ctx)
	if err != nil {
		return WiFiMutation{}, err
	}
	target, err := selectWLANService(services, instance, "wifi")
	if err != nil {
		return WiFiMutation{}, err
	}
	action := "disable"
	if enable {
		action = "enable"
	}
	info, err := c.actionOnService(ctx, target, "GetInfo")
	if err != nil {
		return WiFiMutation{}, wifiError(err)
	}
	current, err := parseEnable(info.Enable)
	if err != nil {
		return WiFiMutation{}, err
	}
	result := WiFiMutation{Instance: target.ID, Action: action, Current: current, Intended: enable}
	if !confirm {
		result.Preview = true
		return result, nil
	}
	if current == enable {
		result.Previous, result.Changed = current, false
		return result, nil
	}
	result.Previous = current
	state := "0"
	if enable {
		state = "1"
	}
	if _, err := c.actionOnService(ctx, target, "SetEnable", soapArgument{Name: "NewEnable", Value: state}); err != nil {
		return WiFiMutation{}, wifiMutationError(err)
	}
	verified, err := c.actionOnService(ctx, target, "GetInfo")
	if err != nil {
		return WiFiMutation{}, wifiVerifyError(err)
	}
	confirmed, err := parseEnable(verified.Enable)
	if err != nil {
		return WiFiMutation{}, err
	}
	if confirmed != enable {
		return WiFiMutation{}, &Error{Kind: "protocol", Operation: "wifi", Message: "router did not confirm the requested Wi-Fi radio state"}
	}
	result.Current, result.Changed = confirmed, true
	return result, nil
}

// selectWLANService resolves the target radio for instance-scoped Wi-Fi
// commands. Instance 0 requires exactly one advertised radio; any other
// value must match the validated numeric service identifier suffix. Both
// failures are usage errors so the caller can suggest --instance.
func selectWLANService(services []service, instance uint64, operation string) (service, error) {
	if instance == 0 {
		if len(services) != 1 {
			return service{}, &Error{Kind: "usage", Code: "ambiguous_instance", Operation: operation, Message: fmt.Sprintf("router advertises %d WLAN instances; specify the target with --instance", len(services))}
		}
		return services[0], nil
	}
	wanted := wlanIDPrefix + strconv.FormatUint(instance, 10)
	for _, svc := range services {
		if svc.ID == wanted {
			return svc, nil
		}
	}
	return service{}, &Error{Kind: "usage", Code: "unknown_instance", Operation: operation, Message: "router does not advertise WLANConfiguration" + strconv.FormatUint(instance, 10)}
}

const wifiDetailRemediation = "enable the WLANConfiguration TR-064 service with GetInfo, or use supported firmware"

// WiFiDetail checks advertised actions before reading one identified radio.
func (c *Client) WiFiDetail(ctx context.Context, instance uint64) (RadioDetail, error) {
	services, err := c.wlanServices(ctx)
	if err != nil {
		return RadioDetail{}, wifiDetailError(err)
	}
	target, err := selectWLANService(services, instance, "wifi detail")
	if err != nil {
		return RadioDetail{}, err
	}
	actions, err := c.wlanReadActions(ctx, target, "wifi detail", wifiDetailError)
	if err != nil {
		return RadioDetail{}, err
	}
	if !actions["GetInfo"] {
		return RadioDetail{}, wifiDetailError(&Error{Kind: "unsupported"})
	}
	info, err := c.actionOnService(ctx, target, "GetInfo")
	if err != nil {
		return RadioDetail{}, wifiDetailError(err)
	}
	enabled, err := parseEnable(info.Enable)
	if err != nil {
		return RadioDetail{}, err
	}
	standard := "unknown"
	switch info.Standard {
	case "b", "g", "n", "ac", "ax", "be":
		standard = info.Standard
	}
	channel, channelSupported, err := c.readWiFiDetailPart(ctx, target, actions, "GetChannelInfo")
	if err != nil {
		return RadioDetail{}, wifiDetailError(err)
	}
	band := "unknown"
	switch channel.FrequencyBand {
	case "2400", "5000", "6000":
		band = channel.FrequencyBand
	}
	number, err := parseOptionalWifiDetailUint(channel.Channel, "Wi-Fi channel")
	if err != nil {
		return RadioDetail{}, err
	}
	result := RadioDetail{
		ServiceID: target.ID, Enabled: enabled, Status: optionalWifiDetailString(info.WLANStatus),
		Standard: standard, MaxBitRate: optionalWifiDetailString(info.MaxBitRate),
		Channel: number, Band: band,
	}
	if channelSupported {
		auto, err := optionalWiFiBool(channel.AutoChannelEnabled, "auto channel flag")
		if err != nil {
			return RadioDetail{}, err
		}
		result.ChannelConfiguration = &WiFiChannelConfiguration{PossibleChannels: optionalWifiDetailString(channel.PossibleChannels), AutoChannelEnabled: auto}
	}
	if err := c.readWiFiConfiguration(ctx, target, actions, &result); err != nil {
		return RadioDetail{}, err
	}
	return result, nil
}

// wlanReadActions uses the same URL and action-name checks as other detail reads.
func (c *Client) wlanReadActions(ctx context.Context, svc service, operation string, actionError func(error) *Error) (map[string]bool, error) {
	control, controlErr := c.base.Parse(svc.ControlURL)
	scpdURL, scpdErr := c.base.Parse(svc.SCPDURL)
	if controlErr != nil || svc.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" || scpdErr != nil || svc.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return nil, &Error{Kind: "protocol", Operation: operation, Message: "router advertised an invalid WLAN service URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return nil, actionError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return nil, &Error{Kind: "protocol", Operation: operation, Message: "router returned an invalid WLAN service description"}
	}
	actions := map[string]bool{}
	for _, action := range scpd.Actions {
		actions[strings.TrimSpace(action.Name)] = true
	}
	return actions, nil
}

func optionalWifiDetailString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// parseOptionalWifiDetailUint accepts an absent optional field as unknown but
// rejects a present value that is not a canonical small unsigned number.
func parseOptionalWifiDetailUint(value, field string) (*uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return nil, &Error{Kind: "protocol", Operation: "wifi detail", Message: "router returned an invalid " + field}
	}
	return &n, nil
}

func wifiDetailError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "wifi detail", Message: "Wi-Fi detail inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
		if protocolErr.Kind == "router" && protocolErr.FaultCode == "401" {
			result.Kind = "unsupported"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support the documented Wi-Fi detail actions; " + wifiDetailRemediation
	}
	return result
}

// wifiVerifyError reports a failure of the state-verification read after a
// SetEnable was already sent, so the caller knows a change may have been
// applied and that re-running the idempotent command is safe.
func wifiVerifyError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "wifi", Message: "Wi-Fi radio change sent but the new state could not be verified"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
	}
	return result
}

func wifiMutationError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "SetEnable", Message: "Wi-Fi radio change failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
		if result.Kind == "network" {
			result.Message = "router did not respond to the Wi-Fi radio change; the change may have been applied, and re-running the idempotent command is safe"
		}
		if result.Kind == "router" && result.FaultCode == "401" {
			result.Kind = "unsupported"
		}
		if result.Kind == "unsupported" {
			result.Message = "router does not support WLANConfiguration:SetEnable; " + wifiMutationRemediation
		}
	}
	return result
}

type RebootResult struct {
	Endpoint string
	Preview  bool
	Accepted bool
}

const (
	deviceConfigPrefix = "urn:dslforum-org:service:DeviceConfig:"
	rebootRemediation  = "enable the DeviceConfig TR-064 service with Reboot, or use supported firmware"
	soapNamespace      = "http://schemas.xmlsoap.org/soap/envelope/"
)

func (c *Client) Reboot(ctx context.Context, confirm bool) (RebootResult, error) {
	if c.base.User != nil || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" || (c.base.EscapedPath() != "" && c.base.EscapedPath() != "/") {
		return RebootResult{}, &Error{Kind: "usage", Code: "invalid_configuration", Operation: "reboot", Message: "reboot requires a router origin without user information, query, fragment, or non-root path"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("reboot refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return RebootResult{}, rebootPreflightError(err)
	}
	target, err := client.rebootService(deviceConfigPrefix)
	if err != nil {
		return RebootResult{}, err
	}
	endpoint := *client.base
	endpoint.Path, endpoint.RawPath = "", ""
	result := RebootResult{Endpoint: endpoint.String()}
	if !confirm {
		result.Preview = true
		return result, nil
	}
	var challenge string
	if client.username != "" {
		info, err := client.rebootService("urn:dslforum-org:service:DeviceInfo:")
		if err != nil {
			return RebootResult{}, err
		}
		client.digestChallenge = &challenge
		if _, err := client.actionOnService(ctx, info, "GetInfo"); err != nil {
			return RebootResult{}, rebootPreflightError(err)
		}
	}
	body, status, sent, err := client.postOnce(ctx, target, "Reboot", challenge)
	switch {
	case err != nil && !sent:
		return RebootResult{}, rebootPreflightError(err)
	case err != nil:
		return RebootResult{}, rebootUncertain("network", status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return RebootResult{}, &Error{Kind: "auth", Operation: "reboot", StatusCode: status, Message: "router rejected reboot authentication; do not automatically repeat"}
	}
	if err := validateRebootResponse(body, status); err != nil {
		return RebootResult{}, err
	}
	result.Accepted = true
	return result, nil
}

// postOnce sends one SOAP action outside the retrying request
// path. sent is false when the request never left this process.
func (c *Client) postOnce(ctx context.Context, target service, action, challenge string, arguments ...soapArgument) (body []byte, status int, sent bool, err error) {
	control := c.base.ResolveReference(&url.URL{Path: target.ControlURL})
	var argumentXML strings.Builder
	for _, argument := range arguments {
		argumentXML.WriteString("<" + argument.Name + ">")
		if err := xml.EscapeText(&argumentXML, []byte(argument.Value)); err != nil {
			return nil, 0, false, err
		}
		argumentXML.WriteString("</" + argument.Name + ">")
	}
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="` + soapNamespace + `" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + target.Type + `">` + argumentXML.String() + `</u:` + action + `></s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, control.String(), strings.NewReader(envelope))
	if err != nil {
		return nil, 0, false, err
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+target.Type+`#`+action+`"`)
	if challenge != "" {
		auth, err := digestAuthorization(challenge, http.MethodPost, control.RequestURI(), c.username, c.password, 2)
		if err != nil {
			return nil, 0, false, &Error{Kind: "auth"}
		}
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, resp.StatusCode, true, nil
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return nil, resp.StatusCode, true, errors.New("unreadable response")
	}
	return body, resp.StatusCode, true, nil
}

func (c *Client) rebootCapability() DoctorCheck {
	if _, err := c.rebootService(deviceConfigPrefix); err != nil {
		return DoctorCheck{State: "unsupported", Remediation: rebootRemediation}
	}
	if c.username != "" {
		if _, err := c.rebootService("urn:dslforum-org:service:DeviceInfo:"); err != nil {
			return DoctorCheck{State: "unsupported", Remediation: rebootRemediation}
		}
	}
	return DoctorCheck{State: "advertised"}
}

func (c *Client) rebootService(prefix string) (service, error) {
	return c.uniqueService(prefix, "reboot", rebootRemediation)
}

// uniqueService requires exactly one advertised service instance of the given
// type prefix and validates that its control URL stays on the router origin
// without user information, query, or fragment. Operations that must not touch
// an ambiguous target use it to fail before any request is sent.
func (c *Client) uniqueService(prefix, operation, remediation string) (service, error) {
	var matches []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, prefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 || matches[0].Type != prefix+"1" {
		return service{}, &Error{Kind: "unsupported", Operation: operation, Message: operation + " requires exactly one supported service instance; " + remediation}
	}
	svc := matches[0]
	control, err := c.base.Parse(svc.ControlURL)
	if err != nil || svc.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" {
		return service{}, &Error{Kind: "protocol", Operation: operation, Message: "router advertised an unsafe " + operation + " preflight control URL"}
	}
	svc.ControlURL = control.Path
	return svc, nil
}

// advertisesAction requires the service description of svc to list action by
// name. Send-once operations use it to fail before any action is sent.
func (c *Client) advertisesAction(ctx context.Context, svc service, operation, action, remediation string, preflight func(error) *Error) error {
	unsupported := &Error{Kind: "unsupported", Operation: operation, Message: "router does not advertise " + action + " for " + operation + "; " + remediation}
	if svc.SCPDURL == "" {
		return unsupported
	}
	scpdURL, err := c.base.Parse(svc.SCPDURL)
	if err != nil || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return &Error{Kind: "protocol", Operation: operation, Message: "router advertised an unsafe " + operation + " service-description URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return preflight(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return &Error{Kind: "protocol", Operation: operation, Message: "router returned an invalid " + operation + " service description"}
	}
	for _, candidate := range scpd.Actions {
		if strings.TrimSpace(candidate.Name) == action {
			return nil
		}
	}
	return unsupported
}

func rebootPreflightError(err error) *Error {
	return preflightError("reboot", "reboot preflight failed; no reboot was sent", err)
}

func preflightError(operation, message string, err error) *Error {
	result := &Error{Kind: "protocol", Operation: operation, Message: message}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode = protocolErr.Kind, protocolErr.StatusCode
		if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Kind = "auth"
		}
	}
	return result
}

func rebootUncertain(kind string, status int) *Error {
	return &Error{Kind: kind, Code: "reboot_uncertain", Operation: "reboot", StatusCode: status, Message: "reboot outcome is uncertain; the router may be restarting; do not automatically repeat"}
}

func validateRebootResponse(body []byte, status int) error {
	fault, faultCode, valid := emptyActionResponse(body, xml.Name{Space: deviceConfigPrefix + "1", Local: "RebootResponse"})
	switch {
	case !valid:
		return rebootUncertain("protocol", status)
	case fault && faultCode == "401":
		return &Error{Kind: "unsupported", Operation: "reboot", StatusCode: status, Message: "router does not support DeviceConfig:Reboot; " + rebootRemediation}
	case fault:
		return &Error{Kind: "router", Operation: "reboot", StatusCode: status, Message: "router rejected reboot; do not automatically repeat"}
	case status < 200 || status >= 300:
		return rebootUncertain("protocol", status)
	}
	return nil
}

// emptyActionResponse classifies the reply to an action without output
// arguments. valid is false unless the body is one SOAP envelope whose body
// holds either the empty response element or a fault.
func emptyActionResponse(body []byte, response xml.Name) (fault bool, faultCode string, valid bool) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var stack []xml.Name
	var envelopeSeen, bodySeen, responseSeen, faultSeen bool
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, "", false
		}
		switch token := token.(type) {
		case xml.StartElement:
			switch len(stack) {
			case 0:
				if envelopeSeen || token.Name != (xml.Name{Space: soapNamespace, Local: "Envelope"}) {
					return false, "", false
				}
				envelopeSeen = true
			case 1:
				if token.Name != (xml.Name{Space: soapNamespace, Local: "Body"}) || bodySeen {
					return false, "", false
				}
				bodySeen = true
			case 2:
				if responseSeen || faultSeen {
					return false, "", false
				}
				switch token.Name {
				case response:
					responseSeen = true
				case xml.Name{Space: soapNamespace, Local: "Fault"}:
					faultSeen = true
				default:
					return false, "", false
				}
			default:
				if !faultSeen {
					return false, "", false
				}
			}
			stack = append(stack, token.Name)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if !faultSeen && strings.TrimSpace(string(token)) != "" {
				return false, "", false
			}
		}
	}
	if !envelopeSeen || !bodySeen || len(stack) != 0 {
		return false, "", false
	}
	if faultSeen {
		var values soapValues
		if err := xml.Unmarshal(body, &values); err != nil {
			return false, "", false
		}
		return true, values.FaultCode, true
	}
	return false, "", responseSeen
}

const (
	configExportRemediation     = "enable the DeviceConfig TR-064 service with X_AVM-DE_GetConfigFile, or use supported firmware"
	configExportAction          = "X_AVM-DE_GetConfigFile"
	configExportPasswordArg     = "NewX_AVM-DE_Password"
	backupActionRejectedCode    = "backup_action_rejected"
	backupInvalidResponseCode   = "backup_invalid_response"
	backupUnsafeDownloadURLCode = "backup_unsafe_download_url"
	backupDownloadRejectedCode  = "backup_download_rejected"
	backupExportTooLargeCode    = "backup_export_too_large"
	tlsUntrustedCode            = "tls_untrusted"
	tlsRemediation              = "the router's HTTPS certificate is not trusted by this system; install the router certificate locally or use supported firmware"
)

// maxConfigExportBytes bounds the accepted configuration export payload.
var maxConfigExportBytes int64 = 64 << 20

// ConfigExport downloads the documented encrypted FRITZ!Box configuration
// export. It refuses a non-HTTPS router origin before any request, so the
// passphrase never travels in plaintext. It invokes only the DeviceConfig:X_AVM-DE_GetConfigFile SOAP action
// carrying only the export passphrase — with the standard Digest handshake,
// like every other documented read — then downloads the returned one-time
// HTTPS URL with the same credentials. The download URL is never
// returned to the caller, redirects and plaintext HTTP downloads are refused,
// and the router configuration is never modified: no factory reset, no
// X_AVM-DE_SetConfigFile, and no browser or undocumented endpoint is used.
func (c *Client) ConfigExport(ctx context.Context, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, &Error{Kind: "usage", Code: "backup_passphrase_missing", Operation: "backup", Message: "configuration export requires an export passphrase"}
	}
	if c.base.User != nil || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" || (c.base.EscapedPath() != "" && c.base.EscapedPath() != "/") {
		return nil, &Error{Kind: "usage", Code: "invalid_configuration", Operation: "backup", Message: "backup requires a router origin without user information, query, fragment, or non-root path"}
	}
	if c.base.Scheme != "https" {
		return nil, &Error{Kind: "usage", Code: "backup_requires_https", Operation: "backup", Message: "backup sends the export passphrase only over HTTPS; use an https router origin (for example --host https://fritz.box:49443) with a router certificate trusted by this system"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("backup refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return nil, configExportError(err)
	}
	target, err := client.uniqueService(deviceConfigPrefix, "backup", configExportRemediation)
	if err != nil {
		return nil, err
	}
	control := client.base.ResolveReference(&url.URL{Path: target.ControlURL})
	var argument strings.Builder
	argument.WriteString("<" + configExportPasswordArg + ">")
	if err := xml.EscapeText(&argument, []byte(passphrase)); err != nil {
		return nil, &Error{Kind: "protocol", Operation: "backup", Message: "could not encode the configuration export request"}
	}
	argument.WriteString("</" + configExportPasswordArg + ">")
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="` + soapNamespace + `" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + configExportAction + ` xmlns:u="` + target.Type + `">` + argument.String() + `</u:` + configExportAction + `></s:Body></s:Envelope>`
	headers := http.Header{"Content-Type": {`text/xml; charset="utf-8"`}, "SOAPAction": {`"` + target.Type + `#` + configExportAction + `"`}}
	body, status, err := client.request(ctx, http.MethodPost, control, []byte(envelope), headers)
	if err != nil {
		return nil, configExportError(err)
	}
	if status == http.StatusForbidden {
		return nil, &Error{Kind: "auth", Operation: "backup", StatusCode: status, Message: "router rejected the export credentials"}
	}
	configFileURL, err := validateConfigExportResponse(body, status)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(configFileURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, &Error{Kind: "protocol", Code: backupUnsafeDownloadURLCode, Operation: "backup", Message: "router did not return a documented HTTPS configuration download URL"}
	}
	if !strings.EqualFold(u.Hostname(), client.base.Hostname()) {
		return nil, &Error{Kind: "protocol", Code: backupUnsafeDownloadURLCode, Operation: "backup", Message: "configuration download URL is outside the router host"}
	}
	return client.downloadConfig(ctx, u)
}

// validateConfigExportResponse accepts only a well-formed SOAP envelope whose
// body is either the documented X_AVM-DE_GetConfigFileResponse carrying
// exactly one download-URL argument or a fault. Anything else, including
// additional output arguments or trailing content, fails closed. The fault
// texts and the returned URL never reach the caller's error output.
func validateConfigExportResponse(body []byte, status int) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var stack []xml.Name
	var envelopeSeen, bodySeen, responseSeen, faultSeen, urlSeen bool
	var configFileURL string
	invalid := func() error {
		return &Error{Kind: "protocol", Code: backupInvalidResponseCode, Operation: "backup", StatusCode: status, Message: "router returned an invalid configuration export response"}
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", invalid()
		}
		switch token := token.(type) {
		case xml.StartElement:
			switch len(stack) {
			case 0:
				if envelopeSeen || token.Name != (xml.Name{Space: soapNamespace, Local: "Envelope"}) {
					return "", invalid()
				}
				envelopeSeen = true
			case 1:
				if token.Name != (xml.Name{Space: soapNamespace, Local: "Body"}) || bodySeen {
					return "", invalid()
				}
				bodySeen = true
			case 2:
				if responseSeen || faultSeen {
					return "", invalid()
				}
				switch token.Name {
				case xml.Name{Space: deviceConfigPrefix + "1", Local: "X_AVM-DE_GetConfigFileResponse"}:
					responseSeen = true
				case xml.Name{Space: soapNamespace, Local: "Fault"}:
					faultSeen = true
				default:
					return "", invalid()
				}
			default:
				if !faultSeen {
					if !responseSeen || urlSeen || token.Name.Local != "NewX_AVM-DE_ConfigFileUrl" {
						return "", invalid()
					}
					urlSeen = true
				}
			}
			stack = append(stack, token.Name)
		case xml.EndElement:
			if len(stack) == 0 {
				return "", invalid()
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			switch {
			case urlSeen && len(stack) == 4:
				configFileURL += string(token)
			case faultSeen:
			case strings.TrimSpace(string(token)) != "":
				return "", invalid()
			}
		}
	}
	if !envelopeSeen || !bodySeen || len(stack) != 0 {
		return "", invalid()
	}
	if faultSeen {
		var values soapValues
		if err := xml.Unmarshal(body, &values); err != nil {
			return "", invalid()
		}
		if values.FaultCode == "401" {
			return "", &Error{Kind: "unsupported", Operation: "backup", StatusCode: status, Message: "router does not support DeviceConfig:X_AVM-DE_GetConfigFile; " + configExportRemediation}
		}
		return "", &Error{Kind: "router", Code: backupActionRejectedCode, Operation: "backup", StatusCode: status, Message: "router rejected the configuration export"}
	}
	if !responseSeen || !urlSeen || status < 200 || status >= 300 {
		return "", invalid()
	}
	return configFileURL, nil
}

// downloadConfig performs the one-time export download. The documented URL is
// HTTPS only; AVM's DeviceConfig reference requires SSL while its Remote
// Access reference shows an HTTP example URL, so plaintext downloads fail
// closed instead of downgrading. Certificate verification is never skipped.
func (c *Client) downloadConfig(ctx context.Context, u *url.URL) ([]byte, error) {
	do := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return c.http.Do(req)
	}
	resp, err := do("")
	if err != nil {
		return nil, downloadNetworkError(err)
	}
	if resp.StatusCode == http.StatusUnauthorized && c.username != "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		auth, authErr := digestAuthorization(challenge, http.MethodGet, u.RequestURI(), c.username, c.password, 2)
		if authErr != nil {
			return nil, &Error{Kind: "auth", Operation: "backup", Message: "router did not offer HTTP Digest authentication for the configuration download"}
		}
		resp, err = do(auth)
		if err != nil {
			return nil, downloadNetworkError(err)
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &Error{Kind: "auth", Operation: "backup", StatusCode: resp.StatusCode, Message: "router rejected the configuration download credentials"}
	}
	if resp.StatusCode >= 400 {
		return nil, &Error{Kind: "router", Code: backupDownloadRejectedCode, Operation: "backup", StatusCode: resp.StatusCode, Message: "router refused the configuration download"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigExportBytes+1))
	if err != nil {
		return nil, &Error{Kind: "network", Operation: "backup", Message: "configuration download could not be read"}
	}
	if int64(len(body)) > maxConfigExportBytes {
		return nil, &Error{Kind: "protocol", Code: backupExportTooLargeCode, Operation: "backup", Message: "router returned an implausibly large configuration export"}
	}
	return body, nil
}

// configExportError converts every export failure into a fixed message that
// carries no router data: fault text, status lines, URLs, one-time download
// tokens, and the passphrase are all discarded.
func configExportError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "backup", Message: "configuration export failed"}
	var protocolErr *Error
	if !errors.As(err, &protocolErr) {
		return result
	}
	result.Kind, result.StatusCode, result.FaultCode, result.Code = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode, protocolErr.Code
	switch {
	case result.Code == tlsUntrustedCode:
		result.Message = tlsRemediation
	case result.Kind == "network":
		result.Message = "router could not be reached during the configuration export"
	case result.Kind == "auth":
		result.Message = "router rejected the export credentials"
	case result.Kind == "router":
		result.Message = "router rejected the configuration export"
	}
	if result.Kind == "router" && result.FaultCode == "401" {
		result.Kind = "unsupported"
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support DeviceConfig:X_AVM-DE_GetConfigFile; " + configExportRemediation
	}
	return result
}

// downloadNetworkError reports a failed configuration-download transport
// without echoing the failed URL, which embeds a one-time router token. Trust
// failures are called out explicitly so the operator can act on them;
// certificate verification is never skipped.
func downloadNetworkError(err error) *Error {
	result := &Error{Kind: "network", Operation: "backup", Message: "the configuration download could not be completed"}
	if tlsUntrusted(err) {
		result.Code = tlsUntrustedCode
		result.Message = tlsRemediation
	}
	return result
}

// tlsUntrusted reports whether err is a TLS certificate verification failure.
func tlsUntrusted(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknownAuthority) || errors.As(err, &invalidCertificate) || errors.As(err, &hostname) || errors.As(err, &verification)
}

// requestNetworkError preserves a transport failure verbatim, as other
// commands do, and only marks TLS trust failures so they can be reported with
// their remediation.
func requestNetworkError(operation string, err error) *Error {
	result := &Error{Kind: "network", Operation: operation, Message: err.Error()}
	if tlsUntrusted(err) {
		result.Code = tlsUntrustedCode
	}
	return result
}

// backupCapability reports DeviceConfig advertisement only. It never invokes
// X_AVM-DE_GetConfigFile and proves no export permission.
func (c *Client) backupCapability() DoctorCheck {
	if _, err := c.uniqueService(deviceConfigPrefix, "backup", configExportRemediation); err != nil {
		return DoctorCheck{State: "unsupported", Remediation: configExportRemediation}
	}
	return DoctorCheck{State: "advertised"}
}

const (
	maxHostEntries = 4096

	maxPortMappingEntries = 4096
	activeWANMessage      = "could not determine the active WAN service"
	activeWANRemediation  = "enable Layer3Forwarding:GetDefaultConnectionService or use supported firmware"
	wanRemediation        = "enable a WANIPConnection or WANPPPConnection service and Layer3Forwarding:GetDefaultConnectionService, or use supported firmware"
	forwardsRemediation   = "enable a WANIPConnection or WANPPPConnection service with documented port-mapping enumeration actions, or use supported firmware"
)

var wanMappingPrefixes = []string{"urn:dslforum-org:service:WANIPConnection:", "urn:dslforum-org:service:WANPPPConnection:"}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	entries, err := c.hostEntries(ctx)
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(entries))
	for _, entry := range entries {
		devices = append(devices, Device{
			Name: entry.HostName, IPAddress: entry.IPAddress, MACAddress: entry.MACAddress,
			InterfaceType: entry.InterfaceType, Active: entry.Active,
		})
	}
	sortDeviceEntries(devices)
	return devices, nil
}

type hostEntry struct {
	HostName, IPAddress, MACAddress, InterfaceType, AddressSource string
	LeaseTimeRemaining                                            *string
	Active                                                        bool
}

func (c *Client) hostEntries(ctx context.Context) ([]hostEntry, error) {
	countValues, err := c.action(ctx, "urn:dslforum-org:service:Hosts:", "GetHostNumberOfEntries")
	if err != nil {
		return nil, err
	}
	count, err := strconv.ParseUint(strings.TrimSpace(countValues.HostNumberOfEntries), 10, 32)
	if err != nil || count > maxHostEntries {
		return nil, &Error{Kind: "protocol", Operation: "GetHostNumberOfEntries", Message: "router returned an invalid host count"}
	}
	entries := make([]hostEntry, 0, count)
	for index := uint64(0); index < count; index++ {
		values, err := c.action(ctx, "urn:dslforum-org:service:Hosts:", "GetGenericHostEntry", soapArgument{Name: "NewIndex", Value: strconv.FormatUint(index, 10)})
		if err != nil {
			return nil, err
		}
		active, err := strconv.ParseBool(strings.TrimSpace(values.Active))
		if err != nil {
			return nil, &Error{Kind: "protocol", Operation: "GetGenericHostEntry", Message: "router returned an invalid active state"}
		}
		entries = append(entries, hostEntry{values.HostName, values.IPAddress, values.MACAddress, values.InterfaceType, values.AddressSource, values.LeaseTimeRemaining, active})
	}
	return entries, nil
}

const leasesRemediation = "enable the Hosts TR-064 service with GetHostNumberOfEntries and GetGenericHostEntry, or use supported firmware"

func leasesError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "leases", Message: "host table inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode = protocolErr.Kind, protocolErr.StatusCode
		if protocolErr.Kind == "router" && protocolErr.FaultCode == "401" {
			result.Kind = "unsupported"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support host table enumeration; " + leasesRemediation
	}
	return result
}

func addressSource(value string) string {
	switch strings.TrimSpace(value) {
	case "DHCP":
		return "DHCP"
	case "Static":
		return "Static"
	default:
		return "unknown"
	}
}

func leaseTimeRemaining(raw *string) (*int64, error) {
	if raw == nil {
		return nil, nil
	}
	text := strings.TrimSpace(*raw)
	if text == "" || text == "4294967295" {
		return nil, nil
	}
	value, err := strconv.ParseInt(text, 10, 32)
	if err != nil || value < -1 {
		return nil, &Error{Kind: "protocol", Operation: "leases", Message: "router returned an invalid lease time remaining"}
	}
	if value <= 0 || value == 2147483647 {
		return nil, nil
	}
	return &value, nil
}

func (c *Client) Leases(ctx context.Context) ([]Lease, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("lease inspection refuses redirects")
	}
	client.http = &httpClient
	entries, err := client.hostEntries(ctx)
	if err != nil {
		return nil, leasesError(err)
	}
	leases := make([]Lease, 0, len(entries))
	for _, entry := range entries {
		remaining, err := leaseTimeRemaining(entry.LeaseTimeRemaining)
		if err != nil {
			return nil, err
		}
		leases = append(leases, Lease{
			Name: entry.HostName, IPAddress: entry.IPAddress, MACAddress: entry.MACAddress,
			AddressSource: addressSource(entry.AddressSource), LeaseTimeRemaining: remaining,
			InterfaceType: entry.InterfaceType, Active: entry.Active,
		})
	}
	sort.Slice(leases, func(i, j int) bool { return compareLeases(leases[i], leases[j]) < 0 })
	return leases, nil
}

func compareLeases(left, right Lease) int {
	order := cmp.Or(cmp.Compare(strings.ToLower(left.MACAddress), strings.ToLower(right.MACAddress)),
		cmp.Compare(left.IPAddress, right.IPAddress), cmp.Compare(left.Name, right.Name),
		cmp.Compare(left.MACAddress, right.MACAddress), cmp.Compare(left.AddressSource, right.AddressSource),
		cmp.Compare(left.InterfaceType, right.InterfaceType), cmp.Compare(strconv.FormatBool(left.Active), strconv.FormatBool(right.Active)))
	if order != 0 {
		return order
	}
	if left.LeaseTimeRemaining == nil && right.LeaseTimeRemaining != nil {
		return -1
	}
	if left.LeaseTimeRemaining != nil && right.LeaseTimeRemaining == nil {
		return 1
	}
	if left.LeaseTimeRemaining == nil {
		return 0
	}
	return cmp.Compare(*left.LeaseTimeRemaining, *right.LeaseTimeRemaining)
}

func sortDeviceEntries(devices []Device) {
	sort.SliceStable(devices, func(i, j int) bool {
		left := strings.ToLower(devices[i].MACAddress) + "\x00" + devices[i].IPAddress + "\x00" + devices[i].Name
		right := strings.ToLower(devices[j].MACAddress) + "\x00" + devices[j].IPAddress + "\x00" + devices[j].Name
		return left < right
	})
}

func (c *Client) Forwards(ctx context.Context) ([]Forward, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("port-forward inspection refuses redirects")
	}
	client.http = &httpClient
	c = &client
	if err := c.discover(ctx); err != nil {
		return nil, forwardsError(err)
	}
	activeService, err := c.activePortMappingService(ctx)
	if err != nil {
		return nil, err
	}
	countValues, err := c.actionOnService(ctx, activeService, "GetPortMappingNumberOfEntries")
	if err != nil {
		return nil, forwardsError(err)
	}
	count, err := portMappingCount(countValues.PortMappingCount)
	if err != nil {
		return nil, err
	}
	forwards := make([]Forward, 0, count)
	for index := uint64(0); index < count; index++ {
		values, err := c.actionOnService(ctx, activeService, "GetGenericPortMappingEntry", soapArgument{Name: "NewPortMappingIndex", Value: strconv.FormatUint(index, 10)})
		if err != nil {
			return nil, forwardsError(err)
		}
		forward, err := parseForward(values)
		if err != nil {
			return nil, err
		}
		forwards = append(forwards, forward)
	}
	finalCount, err := c.actionOnService(ctx, activeService, "GetPortMappingNumberOfEntries")
	if err != nil {
		return nil, forwardsError(err)
	}
	final, err := portMappingCount(finalCount.PortMappingCount)
	if err != nil {
		return nil, err
	}
	if final != count {
		return nil, &Error{Kind: "protocol", Operation: "forwards", Message: "port mappings changed during enumeration; retry the read"}
	}
	sort.SliceStable(forwards, func(i, j int) bool { return compareForwards(forwards[i], forwards[j]) < 0 })
	return forwards, nil
}

func compareForwards(left, right Forward) int {
	order := cmp.Or(cmp.Compare(left.Protocol, right.Protocol), cmp.Compare(left.ExternalPort, right.ExternalPort),
		cmp.Compare(left.RemoteHost, right.RemoteHost), cmp.Compare(left.InternalClient, right.InternalClient),
		cmp.Compare(left.InternalPort, right.InternalPort), cmp.Compare(strconv.FormatBool(left.Enabled), strconv.FormatBool(right.Enabled)),
		cmp.Compare(left.Description, right.Description))
	if order != 0 {
		return order
	}
	if left.LeaseDuration == nil && right.LeaseDuration != nil {
		return -1
	}
	if left.LeaseDuration != nil && right.LeaseDuration == nil {
		return 1
	}
	if left.LeaseDuration == nil {
		return 0
	}
	return cmp.Compare(*left.LeaseDuration, *right.LeaseDuration)
}

func (c *Client) activePortMappingService(ctx context.Context) (service, error) {
	active, err := c.activeWANService(ctx)
	if err != nil {
		var protocolErr *Error
		if errors.As(err, &protocolErr) && protocolErr.Operation == "forwards" {
			return service{}, err
		}
		return service{}, activeWANError(err)
	}
	control, err := c.base.Parse(active.ControlURL)
	if err != nil || active.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.Fragment != "" {
		return service{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router advertised an invalid WAN control URL"}
	}
	if err := c.advertisesPortMappingActions(ctx, active); err != nil {
		return service{}, err
	}
	active.ControlURL = control.Path
	return active, nil
}

func (c *Client) activeWANService(ctx context.Context) (service, error) {
	services := make([]service, 0)
	for _, svc := range c.allServices {
		for _, prefix := range wanMappingPrefixes {
			if strings.HasPrefix(svc.Type, prefix) {
				services = append(services, svc)
				break
			}
		}
	}
	if len(services) == 0 {
		return service{}, &Error{Kind: "unsupported", Operation: "forwards", Message: "router does not advertise a WANIPConnection or WANPPPConnection service; " + forwardsRemediation}
	}
	defaultValues, err := c.action(ctx, "urn:dslforum-org:service:Layer3Forwarding:", "GetDefaultConnectionService")
	if err != nil {
		return service{}, err
	}
	defaultService := strings.TrimSpace(defaultValues.DefaultConnectionService)
	if defaultService == "" {
		return service{}, &Error{Kind: "unsupported", Operation: "forwards", Message: activeWANMessage + "; " + activeWANRemediation}
	}
	var matches []service
	for _, svc := range services {
		if defaultService == svc.Type || defaultService == svc.ID || activeWANServiceID(svc.Type, svc.ID, defaultService) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 {
		return service{}, &Error{Kind: "unsupported", Operation: "forwards", Message: "router default WAN service does not name exactly one advertised WAN service; " + forwardsRemediation}
	}
	return matches[0], nil
}

func (c *Client) advertisesLayer3Action(ctx context.Context) error {
	var matches []service
	for _, candidate := range c.allServices {
		if strings.HasPrefix(candidate.Type, "urn:dslforum-org:service:Layer3Forwarding:") {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return &Error{Kind: "unsupported", Operation: "forwards", Message: "router does not advertise Layer3Forwarding:GetDefaultConnectionService; " + forwardsRemediation}
	}
	layer3 := matches[0]
	controlURL, controlErr := c.base.Parse(layer3.ControlURL)
	scpdURL, err := c.base.Parse(layer3.SCPDURL)
	if controlErr != nil || layer3.ControlURL == "" || !sameOrigin(c.base, controlURL) || controlURL.User != nil || controlURL.RawQuery != "" || controlURL.Fragment != "" || err != nil || layer3.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.Fragment != "" {
		return &Error{Kind: "protocol", Operation: "forwards", Message: "router advertised an invalid Layer3Forwarding service-description URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return forwardsError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid Layer3Forwarding service description"}
	}
	for _, candidate := range scpd.Actions {
		if strings.TrimSpace(candidate.Name) == "GetDefaultConnectionService" {
			layer3.ControlURL = controlURL.Path
			c.services[layer3.Type] = layer3
			return nil
		}
	}
	return &Error{Kind: "unsupported", Operation: "forwards", Message: "router does not advertise Layer3Forwarding:GetDefaultConnectionService; " + forwardsRemediation}
}

func activeWANError(err error) *Error {
	result := forwardsError(err)
	result.Operation = "forwards"
	result.Message = activeWANMessage
	if result.Kind == "router" && result.StatusCode == http.StatusInternalServerError {
		result.Kind = "unsupported"
	}
	if result.Kind == "unsupported" {
		result.Message += "; " + activeWANRemediation
	}
	return result
}

func wanActiveWANError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "wan", Message: activeWANMessage}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode = protocolErr.Kind, protocolErr.StatusCode
	}
	if result.Kind == "router" && result.StatusCode == http.StatusInternalServerError {
		result.Kind = "unsupported"
	}
	if result.Kind == "unsupported" {
		result.Message += "; " + wanRemediation
	}
	return result
}

// activeWANServiceID matches a Layer3Forwarding default connection service
// identifier shaped urn:upnp-org:serviceId:WANIPConnectionN,
// WANPPPConnectionN, uuid:...:WANIPConnection.N, or the dot-separated
// N.WANIPConnection.N that FRITZ!OS returns, against the advertised WAN
// service of that same family and instance.
func activeWANServiceID(advertisedType, advertisedID, defaultService string) bool {
	var family, instance string
	for _, candidate := range []string{"WANIPConnection", "WANPPPConnection"} {
		index := strings.LastIndex(defaultService, candidate)
		if index < 0 {
			continue
		}
		head, tail := defaultService[:index], defaultService[index+len(candidate):]
		if strings.Contains(tail, ":") {
			return false
		}
		if !strings.HasSuffix(head, ":") && !numericDeviceIndex(head) {
			return false
		}
		family, instance = candidate, strings.TrimPrefix(tail, ".")
		break
	}
	if family == "" || instance == "" || !strings.Contains(advertisedType, family) {
		return false
	}
	index := strings.LastIndex(advertisedID, family)
	if index < 0 {
		return false
	}
	return strings.TrimPrefix(advertisedID[index+len(family):], ":") == instance
}

func numericDeviceIndex(head string) bool {
	index, found := strings.CutSuffix(head, ".")
	return found && index != "" && strings.Trim(index, "0123456789") == ""
}

func (c *Client) advertisesPortMappingActions(ctx context.Context, svc service) error {
	if svc.SCPDURL == "" {
		return &Error{Kind: "unsupported", Operation: "forwards", Message: "router does not advertise port-mapping enumeration actions; " + forwardsRemediation}
	}
	scpdURL, err := c.base.Parse(svc.SCPDURL)
	if err != nil || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.Fragment != "" {
		return &Error{Kind: "protocol", Operation: "forwards", Message: "router advertised an invalid WAN service-description URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return forwardsError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil {
		return &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid WAN service description"}
	}
	advertised := map[string]bool{}
	for _, action := range scpd.Actions {
		advertised[action.Name] = true
	}
	if !advertised["GetPortMappingNumberOfEntries"] || !advertised["GetGenericPortMappingEntry"] {
		return &Error{Kind: "unsupported", Operation: "forwards", Message: "router does not advertise port-mapping enumeration actions; " + forwardsRemediation}
	}
	return nil
}

func portMappingCount(value string) (uint64, error) {
	count, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
	if err != nil || count > maxPortMappingEntries {
		return 0, &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid port-mapping count"}
	}
	return count, nil
}

func parseForward(values soapValues) (Forward, error) {
	enabled := false
	switch strings.TrimSpace(values.Enabled) {
	case "0", "false":
	case "1", "true":
		enabled = true
	default:
		return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid port-mapping enabled state"}
	}
	if values.RemoteHost == nil {
		return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router omitted the port-mapping remote-host restriction"}
	}
	if strings.TrimSpace(values.InternalClient) == "" {
		return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router omitted the port-mapping internal target"}
	}
	externalPort, err := strconv.ParseUint(strings.TrimSpace(values.ExternalPort), 10, 16)
	if err != nil || externalPort == 0 {
		return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid external port"}
	}
	internalPort, err := strconv.ParseUint(strings.TrimSpace(values.InternalPort), 10, 16)
	if err != nil || internalPort == 0 {
		return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid internal port"}
	}
	protocol := strings.TrimSpace(values.PortMappingProtocol)
	if protocol != "TCP" && protocol != "UDP" {
		return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid port-mapping protocol"}
	}
	var lease *uint64
	if values.LeaseDuration != nil {
		value, err := strconv.ParseUint(strings.TrimSpace(*values.LeaseDuration), 10, 32)
		if err != nil {
			return Forward{}, &Error{Kind: "protocol", Operation: "forwards", Message: "router returned an invalid port-mapping lease duration"}
		}
		lease = &value
	}
	return Forward{enabled, protocol, externalPort, values.InternalClient, internalPort, values.PortMappingDescription, *values.RemoteHost, lease}, nil
}

func forwardsError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "forwards", Message: "port-forward inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode = protocolErr.Kind, protocolErr.StatusCode
		if protocolErr.Kind == "router" && protocolErr.FaultCode == "401" {
			result.Kind = "unsupported"
			result.Message = "router rejected a documented port-mapping enumeration action; " + forwardsRemediation
		}
	}
	return result
}

func (c *Client) Calls(ctx context.Context) ([]Call, error) {
	v, err := c.action(ctx, "urn:dslforum-org:service:X_AVM-DE_OnTel:", "GetCallList")
	if err != nil {
		return nil, err
	}
	callURL, err := c.base.Parse(v.CallListURL)
	if err != nil {
		return nil, &Error{Kind: "protocol", Operation: "GetCallList", Message: "router returned an invalid call-list URL"}
	}
	if !sameOrigin(c.base, callURL) {
		return nil, &Error{Kind: "protocol", Operation: "GetCallList", Message: "call-list URL is outside the router origin"}
	}
	body, err := c.get(ctx, callURL)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Calls []struct{ ID, Type, Caller, Called, Name, Date, Duration, Device string } `xml:"Call"`
	}
	if err := xml.Unmarshal(body, &raw); err != nil {
		return nil, &Error{Kind: "protocol", Operation: "GetCallList", Message: "invalid call-list XML"}
	}
	calls := make([]Call, 0, len(raw.Calls))
	for _, item := range raw.Calls {
		remote := item.Caller
		if item.Type == "3" || item.Type == "4" {
			remote = item.Called
		}
		calls = append(calls, Call{item.ID, callDirection(item.Type), remote, item.Name, item.Date, item.Duration, item.Device})
	}
	return calls, nil
}

func (c *Client) discover(ctx context.Context) error {
	if c.services != nil {
		return nil
	}
	u := c.base.ResolveReference(&url.URL{Path: descriptionPath})
	body, err := c.get(ctx, u)
	if err != nil {
		return err
	}
	var desc description
	if err := xml.Unmarshal(body, &desc); err != nil {
		return &Error{Kind: "protocol", Operation: "discover", Message: "invalid TR-064 device description"}
	}
	c.allServices = desc.Services
	c.services = make(map[string]service, len(desc.Services))
	for _, svc := range desc.Services {
		c.services[svc.Type] = svc
	}
	return nil
}

func (c *Client) action(ctx context.Context, prefix, action string, arguments ...soapArgument) (soapValues, error) {
	if err := c.discover(ctx); err != nil {
		return soapValues{}, err
	}
	var svc service
	for serviceType, candidate := range c.services {
		if serviceType == prefix || strings.HasPrefix(serviceType, prefix) {
			svc = candidate
			break
		}
	}
	if svc.Type == "" {
		return soapValues{}, &Error{Kind: "unsupported", Operation: action, Message: "router does not advertise the required TR-064 service"}
	}
	return c.actionOnService(ctx, svc, action, arguments...)
}

func (c *Client) actionOnService(ctx context.Context, svc service, action string, arguments ...soapArgument) (soapValues, error) {
	var argumentXML strings.Builder
	for _, argument := range arguments {
		argumentXML.WriteString("<" + argument.Name + ">")
		if err := xml.EscapeText(&argumentXML, []byte(argument.Value)); err != nil {
			return soapValues{}, &Error{Kind: "protocol", Operation: action, Message: "could not encode SOAP request"}
		}
		argumentXML.WriteString("</" + argument.Name + ">")
	}
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + svc.Type + `">` + argumentXML.String() + `</u:` + action + `></s:Body></s:Envelope>`
	u := c.base.ResolveReference(&url.URL{Path: svc.ControlURL})
	headers := http.Header{"Content-Type": {`text/xml; charset="utf-8"`}, "SOAPAction": {`"` + svc.Type + `#` + action + `"`}}
	body, status, err := c.request(ctx, http.MethodPost, u, []byte(envelope), headers)
	if err != nil {
		return soapValues{}, err
	}
	var values soapValues
	if err := xml.Unmarshal(body, &values); err != nil {
		return values, &Error{Kind: "protocol", Operation: action, StatusCode: status, Message: "invalid SOAP response"}
	}
	if status >= 400 || values.FaultCode != "" {
		message := values.FaultDescription
		if message == "" {
			message = http.StatusText(status)
		}
		return values, &Error{Kind: "router", Operation: action, StatusCode: status, FaultCode: values.FaultCode, Message: message}
	}
	return values, nil
}

func (c *Client) get(ctx context.Context, u *url.URL) ([]byte, error) {
	body, status, err := c.request(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, &Error{Kind: "router", Operation: "GET " + u.Path, StatusCode: status, Message: http.StatusText(status)}
	}
	return body, nil
}

func (c *Client) request(ctx context.Context, method string, u *url.URL, body []byte, headers http.Header) ([]byte, int, error) {
	do := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for key, values := range headers {
			req.Header[key] = values
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return c.http.Do(req)
	}
	resp, err := do("")
	if err != nil {
		return nil, 0, requestNetworkError(method+" "+u.Path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized && c.username != "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		auth, authErr := digestAuthorization(challenge, method, u.RequestURI(), c.username, c.password, 1)
		if authErr != nil {
			return nil, 0, &Error{Kind: "auth", Operation: method + " " + u.Path, Message: authErr.Error()}
		}
		if c.digestChallenge != nil {
			*c.digestChallenge = challenge
		}
		resp, err = do(auth)
		if err != nil {
			return nil, 0, requestNetworkError(method+" "+u.Path, err)
		}
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, 0, &Error{Kind: "network", Operation: method + " " + u.Path, Message: "could not read router response"}
	}
	if closeErr != nil {
		return nil, 0, &Error{Kind: "network", Operation: method + " " + u.Path, Message: "could not close router response"}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, resp.StatusCode, &Error{Kind: "auth", Operation: method + " " + u.Path, StatusCode: resp.StatusCode, Message: "router rejected credentials"}
	}
	return responseBody, resp.StatusCode, nil
}

func number(value string) uint64 { n, _ := strconv.ParseUint(value, 10, 64); return n }
func ipFamily(value string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "unknown"
	}
	if address.Unmap().Is4() {
		return "ipv4"
	}
	return "ipv6"
}
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
func callDirection(kind string) string {
	switch kind {
	case "1":
		return "incoming"
	case "2":
		return "missed"
	case "3":
		return "outgoing"
	case "4":
		return "rejected"
	default:
		return "unknown"
	}
}

func digestAuthorization(challenge, method, uri, username, password string, nonceCount int) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return "", errors.New("router did not offer HTTP Digest authentication")
	}
	params := map[string]string{}
	for _, part := range strings.Split(challenge[len("Digest "):], ",") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) == 2 {
			params[strings.ToLower(pair[0])] = strings.Trim(pair[1], `"`)
		}
	}
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" {
		return "", errors.New("incomplete HTTP Digest challenge")
	}
	cnonceBytes := make([]byte, 8)
	if _, err := rand.Read(cnonceBytes); err != nil {
		return "", errors.New("could not create authentication nonce")
	}
	cnonce := hex.EncodeToString(cnonceBytes)
	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + uri)
	nc := fmt.Sprintf("%08x", nonceCount)
	response := md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	return fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q, algorithm=MD5, qop=auth, nc=%s, cnonce=%q`, username, realm, nonce, uri, response, nc, cnonce), nil
}

func md5hex(value string) string { sum := md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }
