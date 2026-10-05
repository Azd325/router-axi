package tr064

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
)

type FirmwareCheckResult struct {
	Endpoint string
	Preview  bool
	Accepted bool
}

const (
	firmwareCheckOperation   = "firmware check"
	firmwareCheckAction      = "X_AVM-DE_CheckUpdate"
	firmwareCheckRemediation = "use a UserInterface:1 service advertising X_AVM-DE_CheckUpdate"
)

func (c *Client) FirmwareCheck(ctx context.Context, confirm bool) (FirmwareCheckResult, error) {
	if c.base.User != nil || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" || (c.base.EscapedPath() != "" && c.base.EscapedPath() != "/") {
		return FirmwareCheckResult{}, &Error{Kind: "usage", Code: "invalid_configuration", Operation: firmwareCheckOperation, Message: "firmware check requires a router origin without user information, query, fragment, or non-root path"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("firmware check refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return FirmwareCheckResult{}, firmwareCheckPreflightError(err)
	}
	target, err := client.uniqueService(firmwareServicePrefix, firmwareCheckOperation, firmwareCheckRemediation)
	if err != nil {
		return FirmwareCheckResult{}, err
	}
	if err := client.advertisesAction(ctx, target, firmwareCheckOperation, firmwareCheckAction, firmwareCheckRemediation, firmwareCheckPreflightError); err != nil {
		return FirmwareCheckResult{}, err
	}
	endpoint := *client.base
	endpoint.Path, endpoint.RawPath = "", ""
	result := FirmwareCheckResult{Endpoint: endpoint.String()}
	if !confirm {
		result.Preview = true
		return result, nil
	}
	var challenge string
	if client.username != "" {
		info, err := client.uniqueService("urn:dslforum-org:service:DeviceInfo:", firmwareCheckOperation, "enable DeviceInfo:GetInfo for firmware check authentication preflight")
		if err != nil {
			return FirmwareCheckResult{}, err
		}
		client.digestChallenge = &challenge
		if _, err := client.actionOnService(ctx, info, "GetInfo"); err != nil {
			return FirmwareCheckResult{}, firmwareCheckPreflightError(err)
		}
	}
	body, status, sent, err := client.postOnce(ctx, target, firmwareCheckAction, challenge)
	switch {
	case err != nil && !sent:
		return FirmwareCheckResult{}, firmwareCheckPreflightError(err)
	case err != nil:
		return FirmwareCheckResult{}, firmwareCheckUncertain("network", status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return FirmwareCheckResult{}, &Error{Kind: "auth", Operation: firmwareCheckOperation, StatusCode: status, Message: "router rejected firmware check authentication; do not automatically repeat"}
	}
	fault, faultCode, valid := emptyActionResponse(body, xml.Name{Space: target.Type, Local: firmwareCheckAction + "Response"})
	switch {
	case !valid:
		return FirmwareCheckResult{}, firmwareCheckUncertain("protocol", status)
	case fault && faultCode == "401":
		return FirmwareCheckResult{}, &Error{Kind: "unsupported", Operation: firmwareCheckOperation, StatusCode: status, Message: "router does not support UserInterface:" + firmwareCheckAction + "; " + firmwareCheckRemediation}
	case fault:
		return FirmwareCheckResult{}, &Error{Kind: "router", Operation: firmwareCheckOperation, StatusCode: status, Message: "router returned a fault for firmware check; the update check state is unknown; do not automatically repeat"}
	case status < 200 || status >= 300:
		return FirmwareCheckResult{}, firmwareCheckUncertain("protocol", status)
	}
	result.Accepted = true
	return result, nil
}

func firmwareCheckPreflightError(err error) *Error {
	return preflightError(firmwareCheckOperation, "firmware check preflight failed; no update check was sent", err)
}

func firmwareCheckUncertain(kind string, status int) *Error {
	return &Error{Kind: kind, Code: "firmware_check_uncertain", Operation: firmwareCheckOperation, StatusCode: status, Message: "firmware check outcome is uncertain; the router may have started the update check; run router-axi firmware to read the reported state; do not automatically repeat"}
}
