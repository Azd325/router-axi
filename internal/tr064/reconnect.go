package tr064

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type WANReconnectResult struct {
	Endpoint string
	Preview  bool
	Accepted bool
}

const (
	layer3ForwardingPrefix  = "urn:dslforum-org:service:Layer3Forwarding:"
	wanReconnectOperation   = "wan reconnect"
	wanReconnectAction      = "ForceTermination"
	wanReconnectRemediation = "enable Layer3Forwarding:GetDefaultConnectionService and a WANIPConnection or WANPPPConnection service with ForceTermination, or use supported firmware"
)

func (c *Client) WANReconnect(ctx context.Context, confirm bool) (WANReconnectResult, error) {
	if c.base.User != nil || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" || (c.base.EscapedPath() != "" && c.base.EscapedPath() != "/") {
		return WANReconnectResult{}, &Error{Kind: "usage", Code: "invalid_configuration", Operation: wanReconnectOperation, Message: "wan reconnect requires a router origin without user information, query, fragment, or non-root path"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("wan reconnect refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	var challenge string
	if client.username != "" {
		client.digestChallenge = &challenge
	}
	if err := client.discover(ctx); err != nil {
		return WANReconnectResult{}, wanReconnectPreflightError(err)
	}
	target, err := client.wanReconnectTarget(ctx)
	if err != nil {
		return WANReconnectResult{}, err
	}
	endpoint := *client.base
	endpoint.Path, endpoint.RawPath = "", ""
	result := WANReconnectResult{Endpoint: endpoint.String()}
	if !confirm {
		result.Preview = true
		return result, nil
	}
	body, status, sent, err := client.postOnce(ctx, target, wanReconnectAction, challenge)
	switch {
	case err != nil && !sent:
		return WANReconnectResult{}, wanReconnectPreflightError(err)
	case err != nil:
		return WANReconnectResult{}, wanReconnectUncertain("network", status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return WANReconnectResult{}, &Error{Kind: "auth", Operation: wanReconnectOperation, StatusCode: status, Message: "router rejected wan reconnect authentication; do not automatically repeat"}
	}
	fault, faultCode, valid := emptyActionResponse(body, xml.Name{Space: target.Type, Local: wanReconnectAction + "Response"})
	switch {
	case !valid:
		return WANReconnectResult{}, wanReconnectUncertain("protocol", status)
	case fault && faultCode == "401":
		return WANReconnectResult{}, &Error{Kind: "unsupported", Operation: wanReconnectOperation, StatusCode: status, Message: "router does not support " + wanReconnectAction + " on the active WAN connection service; " + wanReconnectRemediation}
	case fault:
		return WANReconnectResult{}, &Error{Kind: "router", Operation: wanReconnectOperation, StatusCode: status, Message: "router returned a fault for wan reconnect; the connection state is unknown; do not automatically repeat"}
	case status < 200 || status >= 300:
		return WANReconnectResult{}, wanReconnectUncertain("protocol", status)
	}
	result.Accepted = true
	return result, nil
}

func (c *Client) wanReconnectTarget(ctx context.Context) (service, error) {
	layer3, err := c.uniqueService(layer3ForwardingPrefix, wanReconnectOperation, wanReconnectRemediation)
	if err != nil {
		return service{}, err
	}
	if err := c.wanReconnectAdvertises(ctx, layer3, "GetDefaultConnectionService"); err != nil {
		return service{}, err
	}
	values, err := c.actionOnService(ctx, layer3, "GetDefaultConnectionService")
	if err != nil {
		return service{}, wanReconnectPreflightError(err)
	}
	defaultService := strings.TrimSpace(values.DefaultConnectionService)
	var matches []service
	for _, svc := range c.allServices {
		if wanReconnectFamily(svc.Type) != "" && (defaultService == svc.Type || defaultService == svc.ID || activeWANServiceID(svc.Type, svc.ID, defaultService)) {
			matches = append(matches, svc)
		}
	}
	if defaultService == "" || len(matches) != 1 || matches[0].Type != wanReconnectFamily(matches[0].Type)+"1" {
		return service{}, &Error{Kind: "unsupported", Operation: wanReconnectOperation, Message: "wan reconnect requires the default connection service to name exactly one advertised WANIPConnection:1 or WANPPPConnection:1 service; " + wanReconnectRemediation}
	}
	target := matches[0]
	control, safe := c.wanReconnectURL(target.ControlURL)
	if !safe {
		return service{}, &Error{Kind: "protocol", Operation: wanReconnectOperation, Message: "router advertised an unsafe wan reconnect control URL"}
	}
	if err := c.wanReconnectAdvertises(ctx, target, wanReconnectAction); err != nil {
		return service{}, err
	}
	target.ControlURL = control.Path
	return target, nil
}

func wanReconnectFamily(serviceType string) string {
	for _, prefix := range wanMappingPrefixes {
		if strings.HasPrefix(serviceType, prefix) {
			return prefix
		}
	}
	return ""
}

func (c *Client) wanReconnectURL(raw string) (*url.URL, bool) {
	parsed, err := c.base.Parse(raw)
	if err != nil || raw == "" || !sameOrigin(c.base, parsed) || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, false
	}
	return parsed, true
}

func (c *Client) wanReconnectAdvertises(ctx context.Context, svc service, action string) error {
	unsupported := &Error{Kind: "unsupported", Operation: wanReconnectOperation, Message: "router does not advertise " + action + " for wan reconnect; " + wanReconnectRemediation}
	if svc.SCPDURL == "" {
		return unsupported
	}
	scpdURL, safe := c.wanReconnectURL(svc.SCPDURL)
	if !safe {
		return &Error{Kind: "protocol", Operation: wanReconnectOperation, Message: "router advertised an unsafe wan reconnect service-description URL"}
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return wanReconnectPreflightError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return &Error{Kind: "protocol", Operation: wanReconnectOperation, Message: "router returned an invalid wan reconnect service description"}
	}
	for _, candidate := range scpd.Actions {
		if strings.TrimSpace(candidate.Name) == action {
			return nil
		}
	}
	return unsupported
}

func wanReconnectPreflightError(err error) *Error {
	return preflightError(wanReconnectOperation, "wan reconnect preflight failed; no reconnect was sent", err)
}

func wanReconnectUncertain(kind string, status int) *Error {
	return &Error{Kind: kind, Code: "wan_reconnect_uncertain", Operation: wanReconnectOperation, StatusCode: status, Message: "wan reconnect outcome is uncertain; the internet connection may be reconnecting; do not automatically repeat"}
}
