package tr064

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
)

type DeviceDetail struct {
	Name             string  `json:"name,omitempty"`
	IPAddress        string  `json:"ip_address"`
	MACAddress       string  `json:"mac_address"`
	InterfaceType    string  `json:"interface_type"`
	Active           bool    `json:"active"`
	Port             *uint64 `json:"port"`
	SpeedMbps        *uint64 `json:"speed_mbps"`
	Guest            *bool   `json:"guest"`
	VPN              *bool   `json:"vpn"`
	WANAccess        *string `json:"wan_access"`
	UpdateAvailable  *bool   `json:"update_available"`
	UpdateSuccessful *string `json:"update_successful"`
}

const (
	deviceDetailServicePrefix = "urn:dslforum-org:service:Hosts:"
	deviceDetailAction        = "X_AVM-DE_GetSpecificHostEntryByIP"
	deviceDetailRemediation   = "use firmware that advertises Hosts:X_AVM-DE_GetSpecificHostEntryByIP"
)

func (c *Client) DeviceDetail(ctx context.Context, address netip.Addr) (DeviceDetail, error) {
	if !address.Is4() {
		return DeviceDetail{}, &Error{Kind: "usage", Code: "invalid_device_address", Operation: "devices detail", Message: "devices detail requires exactly one IPv4 address"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("device detail inspection refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return DeviceDetail{}, deviceDetailError(err)
	}
	target, err := client.deviceDetailService(ctx)
	if err != nil {
		return DeviceDetail{}, err
	}
	values, err := client.actionOnService(ctx, target, deviceDetailAction, soapArgument{Name: "NewIPAddress", Value: address.String()})
	if err != nil {
		return DeviceDetail{}, deviceDetailError(err)
	}
	return parseDeviceDetail(address, values)
}

func (c *Client) deviceDetailService(ctx context.Context) (service, error) {
	var matches []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, deviceDetailServicePrefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 {
		return service{}, &Error{Kind: "unsupported", Operation: "devices detail", Message: "router does not advertise exactly one Hosts service; " + deviceDetailRemediation}
	}
	target := matches[0]
	control, controlErr := c.base.Parse(target.ControlURL)
	scpdURL, scpdErr := c.base.Parse(target.SCPDURL)
	if controlErr != nil || target.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" || scpdErr != nil || target.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return service{}, &Error{Kind: "protocol", Operation: "devices detail", Message: "router advertised an invalid Hosts service URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, deviceDetailError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return service{}, &Error{Kind: "protocol", Operation: "devices detail", Message: "router returned an invalid Hosts service description"}
	}
	advertised := false
	for _, action := range scpd.Actions {
		if strings.TrimSpace(action.Name) == deviceDetailAction {
			advertised = true
		}
	}
	if !advertised {
		return service{}, &Error{Kind: "unsupported", Operation: "devices detail", Message: "router does not advertise Hosts:" + deviceDetailAction + "; " + deviceDetailRemediation}
	}
	target.ControlURL = control.Path
	return target, nil
}

func parseDeviceDetail(address netip.Addr, values soapValues) (DeviceDetail, error) {
	result := DeviceDetail{IPAddress: address.String(), Name: values.HostName, MACAddress: values.MACAddress, InterfaceType: values.InterfaceType}
	for _, field := range []struct{ value, name string }{
		{result.Name, "name"}, {result.MACAddress, "MAC address"}, {result.InterfaceType, "interface type"},
	} {
		if len(field.value) > 256 || strings.ContainsFunc(field.value, unicode.IsControl) {
			return DeviceDetail{}, deviceDetailFieldError(field.name)
		}
	}
	active, err := deviceDetailBool(values.Active, "active state")
	if err != nil || active == nil {
		return DeviceDetail{}, deviceDetailFieldError("active state")
	}
	result.Active = *active
	if result.Port, err = deviceDetailUint(values.HostPort, "port"); err != nil {
		return DeviceDetail{}, err
	}
	if result.Port != nil && *result.Port == 0 {
		result.Port = nil
	}
	if result.SpeedMbps, err = deviceDetailUint(values.HostSpeed, "speed"); err != nil {
		return DeviceDetail{}, err
	}
	if result.Guest, err = deviceDetailBool(values.HostGuest, "guest flag"); err != nil {
		return DeviceDetail{}, err
	}
	if result.VPN, err = deviceDetailBool(values.HostVPN, "VPN flag"); err != nil {
		return DeviceDetail{}, err
	}
	if result.UpdateAvailable, err = deviceDetailBool(values.HostUpdateAvailable, "update availability"); err != nil {
		return DeviceDetail{}, err
	}
	result.WANAccess = deviceDetailState(values.HostWANAccess, "granted", "denied", "error")
	result.UpdateSuccessful = deviceDetailState(values.UpdateSuccessful, "succeeded", "failed")
	return result, nil
}

func deviceDetailBool(value, field string) (*bool, error) {
	switch strings.TrimSpace(value) {
	case "":
		return nil, nil
	case "1", "true":
		result := true
		return &result, nil
	case "0", "false":
		result := false
		return &result, nil
	}
	return nil, deviceDetailFieldError(field)
}

func deviceDetailUint(value, field string) (*uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return nil, deviceDetailFieldError(field)
	}
	return &n, nil
}

func deviceDetailState(value string, known ...string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, candidate := range known {
		if value == candidate {
			return &value
		}
	}
	unknown := "unknown"
	return &unknown
}

func deviceDetailFieldError(field string) *Error {
	return &Error{Kind: "protocol", Operation: "devices detail", Message: "router returned an invalid device " + field}
}

func deviceDetailError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "devices detail", Message: "device detail inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
		if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Kind = "auth"
		} else if result.Kind == "router" {
			switch result.FaultCode {
			case "401":
				result.Kind = "unsupported"
			case "402":
				result.Message = "router rejected the requested IP address"
			case "606":
				result.Message = "router denied Hosts:" + deviceDetailAction + "; the logged-in account needs the App or Phone right"
			case "714":
				result.Kind, result.Code, result.Message = "usage", "unknown_device", "router has no host entry for the requested IP address"
			}
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support the documented device detail read; " + deviceDetailRemediation
	}
	return result
}
