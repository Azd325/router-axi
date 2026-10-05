package tr064

import (
	"context"
	"strings"
)

type WiFiChannelConfiguration struct {
	PossibleChannels   *string `json:"possible_channels"`
	AutoChannelEnabled *bool   `json:"auto_channel_enabled"`
}

type WiFiBeaconAdvertisement struct {
	Enabled *bool `json:"enabled"`
}

type WiFiNightControl struct {
	Schedule    *string `json:"schedule"`
	NoForcedOff *bool   `json:"no_forced_off"`
}

type WiFiWPS struct {
	Mode   *string `json:"mode"`
	Status *string `json:"status"`
}

type WiFiIPTVOptimization struct {
	Enabled *bool `json:"enabled"`
}

// AVM documents these guest configuration values as strings without units.
type GuestConfiguration struct {
	TimeoutActive *string `json:"timeout_active"`
	Timeout       *string `json:"timeout"`
	TimeRemain    *string `json:"time_remain"`
	NoForcedOff   *string `json:"no_forced_off"`
	UserIsolation *string `json:"user_isolation"`
}

func guestConfiguration(values soapValues) *GuestConfiguration {
	return &GuestConfiguration{
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
	failure := wifiDetailError(err)
	if failure.Kind == "unsupported" {
		return soapValues{}, false, nil
	}
	return soapValues{}, false, failure
}

func (c *Client) readWiFiConfiguration(ctx context.Context, svc service, actions map[string]bool, result *RadioDetail) error {
	if v, supported, err := c.readWiFiDetailPart(ctx, svc, actions, "GetBeaconAdvertisement"); err != nil {
		return err
	} else if supported {
		enabled, err := optionalWiFiBool(v.BeaconAdvertisementEnabled, "beacon advertisement flag")
		if err != nil {
			return err
		}
		result.BeaconAdvertisement = &WiFiBeaconAdvertisement{Enabled: enabled}
	}
	if v, supported, err := c.readWiFiDetailPart(ctx, svc, actions, "X_AVM-DE_GetNightControl"); err != nil {
		return err
	} else if supported {
		noForcedOff, err := optionalWiFiBool(v.NightTimeControlNoForcedOff, "night control flag")
		if err != nil {
			return err
		}
		result.NightControl = &WiFiNightControl{Schedule: optionalWifiDetailString(v.NightControl), NoForcedOff: noForcedOff}
	}
	if v, supported, err := c.readWiFiDetailPart(ctx, svc, actions, "X_AVM-DE_GetWPSInfo"); err != nil {
		return err
	} else if supported {
		result.WPS = &WiFiWPS{
			Mode:   optionalWiFiEnum(v.WPSMode, "pbc", "stop", "other"),
			Status: optionalWiFiEnum(v.WPSStatus, "off", "inactive", "active", "success", "err_common", "err_timeout", "err_reconfig", "err_internal", "err_abort"),
		}
	}
	if v, supported, err := c.readWiFiDetailPart(ctx, svc, actions, "X_AVM-DE_GetIPTVOptimized"); err != nil {
		return err
	} else if supported {
		enabled, err := optionalWiFiBool(v.IPTVOptimize, "IPTV optimization flag")
		if err != nil {
			return err
		}
		result.IPTVOptimization = &WiFiIPTVOptimization{Enabled: enabled}
	}
	return nil
}
