package tr064

import (
	"bytes"
	"cmp"
	"context"
	"crypto/md5"
	"crypto/rand"
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
	Calls    DoctorCheck `json:"calls"`
	Devices  DoctorCheck `json:"devices"`
	Leases   DoctorCheck `json:"leases"`
	WiFi     DoctorCheck `json:"wifi"`
	Forwards DoctorCheck `json:"forwards"`
	Reboot   DoctorCheck `json:"reboot"`
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
	Status, LastError, ExternalIP                                            string
	Manufacturer, Model, Serial, Software, Hardware                          string
	Uptime, DownloadRate, UploadRate, TotalDownload, TotalUpload             string
	CallListURL, HostNumberOfEntries, DefaultConnectionService               string
	MACAddress, IPAddress, InterfaceType, Active, HostName, AddressSource    string
	LeaseTimeRemaining                                                       *string
	FaultCode, FaultDescription                                              string
	Enable, SSID, Standard                                                   string
	Channel, FrequencyBand, TotalAssociations, BeaconType                    string
	PortMappingCount, ExternalPort, PortMappingProtocol                      string
	InternalPort, InternalClient, PortMappingEnabled, PortMappingDescription string
	RemoteHost, LeaseDuration                                                *string
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
		case "NewByteReceiveRate":
			target = &v.DownloadRate
		case "NewByteSendRate":
			target = &v.UploadRate
		case "NewTotalBytesReceived":
			target = &v.TotalDownload
		case "NewTotalBytesSent":
			target = &v.TotalUpload
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
		case "NewChannel":
			target = &v.Channel
		case "NewX_AVM-DE_FrequencyBand":
			target = &v.FrequencyBand
		case "NewTotalAssociations":
			target = &v.TotalAssociations
		case "NewBeaconType":
			target = &v.BeaconType
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
			target = &v.PortMappingEnabled
		case "NewPortMappingDescription":
			target = &v.PortMappingDescription
		case "NewLeaseDuration":
			v.LeaseDuration = new(string)
			target = v.LeaseDuration
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
			Status: unknown, Overview: unknown, WAN: unknown, Traffic: unknown, Calls: unknown, Devices: unknown, Leases: unknown, WiFi: unknown, Forwards: unknown, Reboot: unknown,
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
	report.Capabilities.Calls = c.advertisedCapability([]string{"urn:dslforum-org:service:X_AVM-DE_OnTel:"}, "enable telephony and its TR-064 service or use supported firmware")
	hostsCapability := c.advertisedCapability([]string{"urn:dslforum-org:service:Hosts:"}, "enable the Hosts TR-064 service or use supported firmware")
	report.Capabilities.Devices = hostsCapability
	report.Capabilities.Leases = hostsCapability
	report.Capabilities.WiFi = c.advertisedCapability([]string{wlanServicePrefix}, wifiRemediation)
	report.Capabilities.Forwards = c.advertisedCapability(wanMappingPrefixes, forwardsRemediation)
	report.Capabilities.Reboot = c.rebootCapability()
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

func (c *Client) Status(ctx context.Context) (Status, error) {
	v, err := c.action(ctx, "urn:dslforum-org:service:DeviceInfo:", "GetInfo")
	if err != nil {
		return Status{}, err
	}
	return Status{v.Manufacturer, v.Model, v.Serial, v.Software, v.Hardware, number(v.Uptime)}, nil
}

func (c *Client) WAN(ctx context.Context) (WAN, error) {
	serviceType, err := c.wanConnectionService(ctx)
	if err != nil {
		return WAN{}, err
	}
	v, err := c.action(ctx, serviceType, "GetStatusInfo")
	if err != nil {
		return WAN{}, err
	}
	ip, err := c.action(ctx, serviceType, "GetExternalIPAddress")
	if err != nil {
		return WAN{}, err
	}
	return WAN{v.Status, ip.ExternalIP, ipFamily(ip.ExternalIP), number(v.Uptime), v.LastError}, nil
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
		// GetInfo returns the SSID and the BSSID of the beacon. The SSID is
		// public beacon data and is reported; the BSSID and every other
		// returned field are discarded here and never leave the client.
		info, err := c.actionOnService(ctx, svc, "GetInfo")
		if err != nil {
			return nil, wifiError(err)
		}
		enabled, err := parseEnable(info.Enable)
		if err != nil {
			return nil, err
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
			return nil, wifiError(err)
		}
		number, err := strconv.ParseUint(strings.TrimSpace(channel.Channel), 10, 8)
		if err != nil {
			return nil, &Error{Kind: "protocol", Operation: "wifi", Message: "router returned an invalid Wi-Fi channel"}
		}
		associations, err := c.actionOnService(ctx, svc, "GetTotalAssociations")
		if err != nil {
			return nil, wifiError(err)
		}
		count, err := strconv.ParseUint(strings.TrimSpace(associations.TotalAssociations), 10, 16)
		if err != nil {
			return nil, &Error{Kind: "protocol", Operation: "wifi", Message: "router returned an invalid Wi-Fi association count"}
		}
		security, err := c.actionOnService(ctx, svc, "GetBeaconType")
		if err != nil {
			return nil, wifiError(err)
		}
		if security.BeaconType == "" {
			return nil, &Error{Kind: "protocol", Operation: "wifi", Message: "router omitted the Wi-Fi security mode"}
		}
		mode := "unknown"
		switch security.BeaconType {
		case "None", "Basic", "WPA", "11i", "WPAand11i", "WPA3", "11iandWPA3", "OWE", "OWETrans":
			mode = security.BeaconType
		}
		radios = append(radios, Radio{svc.ID, info.SSID, enabled, number, band, standard, count, mode})
	}
	return radios, nil
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
	var target service
	if instance == 0 {
		if len(services) != 1 {
			return WiFiMutation{}, &Error{Kind: "usage", Code: "ambiguous_instance", Operation: "wifi", Message: fmt.Sprintf("router advertises %d WLAN instances; specify the target with --instance", len(services))}
		}
		target = services[0]
	} else {
		wanted := wlanIDPrefix + strconv.FormatUint(instance, 10)
		for _, svc := range services {
			if svc.ID == wanted {
				target = svc
				break
			}
		}
		if target.ID == "" {
			return WiFiMutation{}, &Error{Kind: "usage", Code: "unknown_instance", Operation: "wifi", Message: "router does not advertise WLANConfiguration" + strconv.FormatUint(instance, 10)}
		}
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
	control := client.base.ResolveReference(&url.URL{Path: target.ControlURL})
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="` + soapNamespace + `" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:Reboot xmlns:u="` + target.Type + `"></u:Reboot></s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, control.String(), strings.NewReader(envelope))
	if err != nil {
		return RebootResult{}, rebootPreflightError(err)
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+target.Type+`#Reboot"`)
	if challenge != "" {
		auth, err := digestAuthorization(challenge, http.MethodPost, control.RequestURI(), client.username, client.password, 2)
		if err != nil {
			return RebootResult{}, rebootPreflightError(&Error{Kind: "auth"})
		}
		req.Header.Set("Authorization", auth)
	}
	resp, err := client.http.Do(req)
	if err != nil {
		return RebootResult{}, rebootUncertain("network", 0)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return RebootResult{}, &Error{Kind: "auth", Operation: "reboot", StatusCode: resp.StatusCode, Message: "router rejected reboot authentication; do not automatically repeat"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return RebootResult{}, rebootUncertain("network", resp.StatusCode)
	}
	if err := validateRebootResponse(body, resp.StatusCode); err != nil {
		return RebootResult{}, err
	}
	result.Accepted = true
	return result, nil
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
	var matches []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, prefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 || matches[0].Type != prefix+"1" {
		return service{}, &Error{Kind: "unsupported", Operation: "reboot", Message: "reboot requires exactly one supported service instance; " + rebootRemediation}
	}
	svc := matches[0]
	control, err := c.base.Parse(svc.ControlURL)
	if err != nil || svc.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" {
		return service{}, &Error{Kind: "protocol", Operation: "reboot", Message: "router advertised an unsafe reboot preflight control URL"}
	}
	svc.ControlURL = control.Path
	return svc, nil
}

func rebootPreflightError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "reboot", Message: "reboot preflight failed; no reboot was sent"}
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
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var stack []xml.Name
	var envelopeSeen, bodySeen, responseSeen, faultSeen bool
	invalid := func() error { return rebootUncertain("protocol", status) }
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return invalid()
		}
		switch token := token.(type) {
		case xml.StartElement:
			switch len(stack) {
			case 0:
				if envelopeSeen || token.Name != (xml.Name{Space: soapNamespace, Local: "Envelope"}) {
					return invalid()
				}
				envelopeSeen = true
			case 1:
				if token.Name != (xml.Name{Space: soapNamespace, Local: "Body"}) || bodySeen {
					return invalid()
				}
				bodySeen = true
			case 2:
				if responseSeen || faultSeen {
					return invalid()
				}
				switch token.Name {
				case xml.Name{Space: deviceConfigPrefix + "1", Local: "RebootResponse"}:
					responseSeen = true
				case xml.Name{Space: soapNamespace, Local: "Fault"}:
					faultSeen = true
				default:
					return invalid()
				}
			default:
				if !faultSeen {
					return invalid()
				}
			}
			stack = append(stack, token.Name)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if !faultSeen && strings.TrimSpace(string(token)) != "" {
				return invalid()
			}
		}
	}
	if !envelopeSeen || !bodySeen || len(stack) != 0 {
		return invalid()
	}
	if faultSeen {
		var values soapValues
		if err := xml.Unmarshal(body, &values); err != nil {
			return invalid()
		}
		if values.FaultCode == "401" {
			return &Error{Kind: "unsupported", Operation: "reboot", StatusCode: status, Message: "router does not support DeviceConfig:Reboot; " + rebootRemediation}
		}
		return &Error{Kind: "router", Operation: "reboot", StatusCode: status, Message: "router rejected reboot; do not automatically repeat"}
	}
	if !responseSeen || status < 200 || status >= 300 {
		return invalid()
	}
	return nil
}

const maxHostEntries = 4096

const (
	maxPortMappingEntries = 4096
	activeWANMessage      = "could not determine the active WAN service"
	activeWANRemediation  = "enable Layer3Forwarding:GetDefaultConnectionService or use supported firmware"
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
		return service{}, activeWANError(err)
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
	active := matches[0]
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
	switch strings.TrimSpace(values.PortMappingEnabled) {
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

func (c *Client) wanConnectionService(ctx context.Context) (string, error) {
	if err := c.discover(ctx); err != nil {
		return "", err
	}
	for _, prefix := range []string{"urn:dslforum-org:service:WANIPConnection:", "urn:dslforum-org:service:WANPPPConnection:"} {
		for serviceType := range c.services {
			if strings.HasPrefix(serviceType, prefix) {
				return serviceType, nil
			}
		}
	}
	return "", &Error{Kind: "unsupported", Operation: "wan", Message: "router does not advertise a WAN connection service"}
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
		return nil, 0, &Error{Kind: "network", Operation: method + " " + u.Path, Message: err.Error()}
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
			return nil, 0, &Error{Kind: "network", Operation: method + " " + u.Path, Message: err.Error()}
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
