package tr064

import (
	"context"
	"errors"
	"strings"
)

type WiFiNightControl struct {
	Schedule    *string `json:"schedule"`
	NoForcedOff *bool   `json:"no_forced_off"`
}

type WiFiWPS struct {
	Mode   *string `json:"mode"`
	Status *string `json:"status"`
}

// AVM documents these guest configuration values as strings without units.
type GuestConfiguration struct {
	TimeoutActive *string `json:"timeout_active"`
	Timeout       *string `json:"timeout"`
	TimeRemain    *string `json:"time_remain"`
	NoForcedOff   *string `json:"no_forced_off"`
	UserIsolation *string `json:"user_isolation"`
}

func guestConfiguration(values soapValues) GuestConfiguration {
	return GuestConfiguration{
		TimeoutActive: optionalWifiDetailString(values.GuestTimeoutActive),
		Timeout:       optionalWifiDetailString(values.GuestTimeout),
		TimeRemain:    optionalWifiDetailString(values.GuestTimeRemain),
		NoForcedOff:   optionalWifiDetailString(values.GuestNoForcedOff),
		UserIsolation: optionalWifiDetailString(values.GuestUserIsolation),
	}
}

func optionalWiFiBool(value, field string) (*bool, error) {
	var enabled bool
	switch strings.TrimSpace(value) {
	case "":
		return nil, nil
	case "1", "true":
		enabled = true
	case "0", "false":
		enabled = false
	default:
		return nil, &Error{Kind: "protocol", Operation: "wifi detail", Message: "router returned an invalid Wi-Fi " + field}
	}
	return &enabled, nil
}

func optionalWiFiEnum(value string, allowed ...string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return &value
		}
	}
	unknown := "unknown"
	return &unknown
}

func (c *Client) readWiFiDetailPart(ctx context.Context, svc service, actions map[string]bool, action string) (soapValues, bool, error) {
	if !actions[action] {
		return soapValues{}, false, nil
	}
	values, err := c.actionOnService(ctx, svc, action)
	if err == nil {
		return values, true, nil
	}
	var failure *Error
	if errors.As(err, &failure) && failure.Kind == "router" {
		return soapValues{}, false, nil
	}
	return soapValues{}, false, wifiDetailError(err)
}

func (c *Client) readWiFiConfiguration(ctx context.Context, svc service, actions map[string]bool, result *RadioDetail) error {
	if v, available, err := c.readWiFiDetailPart(ctx, svc, actions, "X_AVM-DE_GetNightControl"); err != nil {
		return err
	} else if available {
		noForcedOff, err := optionalWiFiBool(v.NightTimeControlNoForcedOff, "night control flag")
		if err != nil {
			return err
		}
		result.NightControl = &WiFiNightControl{Schedule: optionalWifiDetailString(v.NightControl), NoForcedOff: noForcedOff}
	}
	if v, available, err := c.readWiFiDetailPart(ctx, svc, actions, "X_AVM-DE_GetWPSInfo"); err != nil {
		return err
	} else if available {
		result.WPS = &WiFiWPS{
			Mode:   optionalWiFiEnum(v.WPSMode, "pbc", "stop", "other"),
			Status: optionalWiFiEnum(v.WPSStatus, "off", "inactive", "active", "success", "err_common", "err_timeout", "err_reconfig", "err_internal", "err_abort"),
		}
	}
	return nil
}
