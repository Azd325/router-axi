package tr064

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Firmware struct {
	CurrentVersion   *string `json:"current_version"`
	UpdateAvailable  *bool   `json:"update_available"`
	OfferedVersion   *string `json:"offered_version"`
	UpdateState      *string `json:"update_state"`
	BuildType        *string `json:"build_type"`
	AutoUpdateMode   *string `json:"auto_update_mode"`
	UpdateTime       *string `json:"update_time"`
	LastVersion      *string `json:"last_version"`
	UpdateSuccessful *string `json:"update_successful"`
}

const (
	firmwareServicePrefix = "urn:dslforum-org:service:UserInterface:"
	firmwareRemediation   = "enable UserInterface with GetInfo and X_AVM-DE_GetInfo, or use supported firmware"
)

func (c *Client) Firmware(ctx context.Context) (Firmware, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("firmware inspection refuses redirects") }
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return Firmware{}, firmwareError(err)
	}
	target, err := client.firmwareService(ctx)
	if err != nil {
		return Firmware{}, err
	}
	info, err := client.actionOnService(ctx, target, "GetInfo")
	if err != nil {
		return Firmware{}, firmwareError(err)
	}
	if info.FirmwareResponseName != "GetInfoResponse" {
		return Firmware{}, firmwareFieldError("GetInfo response")
	}
	extended, err := client.actionOnService(ctx, target, "X_AVM-DE_GetInfo")
	if err != nil {
		return Firmware{}, firmwareError(err)
	}
	if extended.FirmwareResponseName != "X_AVM-DE_GetInfoResponse" {
		return Firmware{}, firmwareFieldError("X_AVM-DE_GetInfo response")
	}
	return parseFirmwareInfo(info, extended)
}

func (c *Client) firmwareService(ctx context.Context) (service, error) {
	var matches []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, firmwareServicePrefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 {
		return service{}, &Error{Kind: "unsupported", Operation: "firmware", Message: "router does not advertise exactly one UserInterface service; " + firmwareRemediation}
	}
	target := matches[0]
	control, controlErr := c.base.Parse(target.ControlURL)
	scpdURL, scpdErr := c.base.Parse(target.SCPDURL)
	if controlErr != nil || target.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" || scpdErr != nil || target.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return service{}, &Error{Kind: "protocol", Operation: "firmware", Message: "router advertised an invalid UserInterface service URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, firmwareError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return service{}, &Error{Kind: "protocol", Operation: "firmware", Message: "router returned an invalid UserInterface service description"}
	}
	var info, extended bool
	for _, action := range scpd.Actions {
		switch strings.TrimSpace(action.Name) {
		case "GetInfo":
			info = true
		case "X_AVM-DE_GetInfo":
			extended = true
		}
	}
	if !info || !extended {
		return service{}, &Error{Kind: "unsupported", Operation: "firmware", Message: "router does not advertise UserInterface:GetInfo and X_AVM-DE_GetInfo; " + firmwareRemediation}
	}
	target.ControlURL = control.Path
	return target, nil
}

func parseFirmwareInfo(info, extended soapValues) (Firmware, error) {
	var result Firmware
	available := strings.TrimSpace(info.UpgradeAvailable)
	switch available {
	case "":
	case "0", "1", "false", "true":
		value := available == "1" || available == "true"
		result.UpdateAvailable = &value
	default:
		return Firmware{}, firmwareFieldError("update availability")
	}
	for _, field := range []struct {
		value, name string
		target      **string
	}{
		{extended.CurrentFWVersion, "current version", &result.CurrentVersion},
		{info.OfferedVersion, "offered version", &result.OfferedVersion},
		{info.UpdateState, "update state", &result.UpdateState},
		{info.BuildType, "build type", &result.BuildType},
		{extended.AutoUpdateMode, "auto-update mode", &result.AutoUpdateMode},
		{extended.UpdateTime, "update time", &result.UpdateTime},
		{extended.LastFWVersion, "last version", &result.LastVersion},
		{extended.UpdateSuccessful, "update result", &result.UpdateSuccessful},
	} {
		value := strings.TrimSpace(field.value)
		if value == "" {
			continue
		}
		if len(value) > 256 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return Firmware{}, firmwareFieldError(field.name)
		}
		*field.target = &value
	}
	if result.UpdateTime != nil {
		if *result.UpdateTime == "0000-00-00T00:00:00" {
			result.UpdateTime = nil
		} else {
			_, zonedErr := time.Parse(time.RFC3339, *result.UpdateTime)
			_, localErr := time.Parse("2006-01-02T15:04:05", *result.UpdateTime)
			if zonedErr != nil && localErr != nil {
				return Firmware{}, firmwareFieldError("update time")
			}
		}
	}
	return result, nil
}

func firmwareFieldError(field string) *Error {
	return &Error{Kind: "protocol", Operation: "firmware", Message: "router returned an invalid firmware " + field}
}

func firmwareError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "firmware", Message: "firmware status inspection failed"}
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
		result.Message = "router does not support documented firmware status reads; " + firmwareRemediation
	}
	return result
}
