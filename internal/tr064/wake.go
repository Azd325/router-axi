package tr064

import (
	"context"
	"encoding/xml"
	"errors"
	"net"
	"net/http"
	"strings"
)

const (
	hostsPrefix     = "urn:dslforum-org:service:Hosts:"
	wakeAction      = "X_AVM-DE_WakeOnLANByMACAddress"
	wakeRemediation = "use a Hosts:1 service advertising X_AVM-DE_WakeOnLANByMACAddress with NewMACAddress"
)

type WakeResult struct {
	Endpoint string
	MAC      string
	Preview  bool
	Accepted bool
}

// WakeMAC accepts one six-octet unicast target and returns its canonical form.
func WakeMAC(raw string) (string, error) {
	invalid := &Error{Kind: "usage", Code: "invalid_wake_target", Operation: "wake", Message: "wake requires exactly one nonzero unicast MAC address of six colon-separated hexadecimal octets"}
	if len(raw) != 17 {
		return "", invalid
	}
	for _, index := range []int{2, 5, 8, 11, 14} {
		if raw[index] != ':' {
			return "", invalid
		}
	}
	mac, err := net.ParseMAC(raw)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" {
		return "", invalid
	}
	return strings.ToUpper(mac.String()), nil
}

func (c *Client) Wake(ctx context.Context, rawMAC string, confirm bool) (WakeResult, error) {
	mac, err := WakeMAC(rawMAC)
	if err != nil {
		return WakeResult{}, err
	}
	if c.base.User != nil || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" || (c.base.EscapedPath() != "" && c.base.EscapedPath() != "/") {
		return WakeResult{}, &Error{Kind: "usage", Code: "invalid_configuration", Operation: "wake", Message: "wake requires a router origin without user information, query, fragment, or non-root path"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("wake refuses redirects") }
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	var challenge string
	if client.username != "" {
		client.digestChallenge = &challenge
	}
	if err := client.discover(ctx); err != nil {
		return WakeResult{}, wakePreflightError(err)
	}
	target, err := client.uniqueService(hostsPrefix, "wake", wakeRemediation)
	if err != nil {
		return WakeResult{}, err
	}
	if err := client.wakeAdvertises(ctx, target); err != nil {
		return WakeResult{}, err
	}
	endpoint := *client.base
	endpoint.Path, endpoint.RawPath = "", ""
	result := WakeResult{Endpoint: endpoint.String(), MAC: mac}
	if !confirm {
		result.Preview = true
		return result, nil
	}
	if client.username != "" && challenge == "" {
		info, err := client.uniqueService("urn:dslforum-org:service:DeviceInfo:", "wake", "enable DeviceInfo:GetInfo for wake authentication preflight")
		if err != nil {
			return WakeResult{}, err
		}
		if _, err := client.actionOnService(ctx, info, "GetInfo"); err != nil {
			return WakeResult{}, wakePreflightError(err)
		}
	}
	body, status, sent, err := client.postOnce(ctx, target, wakeAction, challenge, soapArgument{Name: "NewMACAddress", Value: mac})
	switch {
	case err != nil && !sent:
		return WakeResult{}, wakePreflightError(err)
	case err != nil:
		return WakeResult{}, wakeUncertain("network", status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return WakeResult{}, &Error{Kind: "auth", Operation: "wake", StatusCode: status, Message: "router rejected wake authentication; do not automatically repeat"}
	}
	fault, faultCode, valid := emptyActionResponse(body, xml.Name{Space: target.Type, Local: wakeAction + "Response"})
	switch {
	case !valid:
		return WakeResult{}, wakeUncertain("protocol", status)
	case fault && faultCode == "401":
		return WakeResult{}, &Error{Kind: "unsupported", Operation: "wake", StatusCode: status, Message: "router does not support Hosts:" + wakeAction + "; " + wakeRemediation}
	case fault:
		return WakeResult{}, &Error{Kind: "router", Operation: "wake", StatusCode: status, Message: "router returned a fault for wake; the device state is unknown; do not automatically repeat"}
	case status < 200 || status >= 300:
		return WakeResult{}, wakeUncertain("protocol", status)
	}
	result.Accepted = true
	return result, nil
}

func (c *Client) wakeAdvertises(ctx context.Context, svc service) error {
	unsupported := &Error{Kind: "unsupported", Operation: "wake", Message: "router does not advertise the documented " + wakeAction + " signature for wake; " + wakeRemediation}
	if svc.SCPDURL == "" {
		return unsupported
	}
	scpdURL, err := c.base.Parse(svc.SCPDURL)
	if err != nil || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return &Error{Kind: "protocol", Operation: "wake", Message: "router advertised an unsafe wake service-description URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return wakePreflightError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name      string `xml:"name"`
			Arguments []struct {
				Name      string `xml:"name"`
				Direction string `xml:"direction"`
			} `xml:"argumentList>argument"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil {
		return &Error{Kind: "protocol", Operation: "wake", Message: "router returned an invalid wake service description"}
	}
	matches := 0
	for _, candidate := range scpd.Actions {
		if strings.TrimSpace(candidate.Name) != wakeAction {
			continue
		}
		matches++
		if len(candidate.Arguments) != 1 || strings.TrimSpace(candidate.Arguments[0].Name) != "NewMACAddress" || strings.TrimSpace(candidate.Arguments[0].Direction) != "in" {
			return unsupported
		}
	}
	if matches != 1 {
		return unsupported
	}
	return nil
}

func wakePreflightError(err error) *Error {
	return preflightError("wake", "wake preflight failed; no wake request was sent", err)
}

func wakeUncertain(kind string, status int) *Error {
	return &Error{Kind: kind, Code: "wake_uncertain", Operation: "wake", StatusCode: status, Message: "wake outcome is uncertain; the router may have sent the wake request; check the device manually; do not automatically repeat"}
}
