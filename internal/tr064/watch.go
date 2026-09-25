package tr064

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type WatchSnapshot struct {
	ObservedAt         time.Time
	WANStatus          string
	WANUptimeSeconds   *uint64
	TotalDownloadBytes *uint64
	TotalUploadBytes   *uint64
	DownloadAt         time.Time
	UploadAt           time.Time
	Source             string `json:"-"`
}

const WANStatusConnected = "connected"

const watchRemediation = "enable the Layer3Forwarding and WANCommonInterfaceConfig TR-064 services or use supported firmware"

func (c *Client) WatchSnapshot(ctx context.Context) (WatchSnapshot, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("watch refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	c = &client
	if c.base.User != nil || c.base.Opaque != "" || c.base.Host == "" ||
		(c.base.Scheme != "http" && c.base.Scheme != "https") ||
		(c.base.Path != "" && c.base.Path != "/") || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" {
		return WatchSnapshot{}, watchError(&Error{Kind: "protocol"})
	}
	if err := ctx.Err(); err != nil {
		return WatchSnapshot{}, watchError(err)
	}
	if err := c.discover(ctx); err != nil {
		return WatchSnapshot{}, watchError(err)
	}
	layer3, err := c.uniqueService("urn:dslforum-org:service:Layer3Forwarding:", "watch", watchRemediation)
	if err != nil {
		return WatchSnapshot{}, watchError(err)
	}
	common, err := c.uniqueService("urn:dslforum-org:service:WANCommonInterfaceConfig:", "watch", watchRemediation)
	if err != nil {
		return WatchSnapshot{}, watchError(err)
	}
	c.services = map[string]service{layer3.Type: layer3}
	if err := ctx.Err(); err != nil {
		return WatchSnapshot{}, watchError(err)
	}
	wan, err := c.activeWANService(ctx)
	if err != nil {
		return WatchSnapshot{}, watchError(err)
	}
	control, err := c.base.Parse(wan.ControlURL)
	if err != nil || wan.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" {
		return WatchSnapshot{}, watchError(&Error{Kind: "protocol"})
	}
	wan.ControlURL = control.Path
	status, err := c.watchAction(ctx, wan, "GetStatusInfo")
	if err != nil {
		return WatchSnapshot{}, err
	}
	uptime, err := watchNumber(status.Uptime)
	if err != nil {
		return WatchSnapshot{}, err
	}
	received, err := c.watchAction(ctx, common, "GetTotalBytesReceived")
	if err != nil {
		return WatchSnapshot{}, err
	}
	downloadAt := c.now()
	download, err := watchNumber(received.TotalDownload)
	if err != nil {
		return WatchSnapshot{}, err
	}
	sent, err := c.watchAction(ctx, common, "GetTotalBytesSent")
	if err != nil {
		return WatchSnapshot{}, err
	}
	uploadAt := c.now()
	upload, err := watchNumber(sent.TotalUpload)
	if err != nil {
		return WatchSnapshot{}, err
	}
	state := "unknown"
	switch strings.TrimSpace(status.Status) {
	case "Unconfigured", "Connecting", "Authenticating", "Connected", "PendingDisconnect", "Disconnecting", "Disconnected":
		state = strings.ToLower(strings.TrimSpace(status.Status))
	}
	source := sha256.Sum256([]byte(strings.Join([]string{c.base.String(), wan.Type, wan.ID, wan.ControlURL, common.Type, common.ID, common.ControlURL}, "\x00")))
	return WatchSnapshot{
		ObservedAt: uploadAt, WANStatus: state, WANUptimeSeconds: uptime,
		TotalDownloadBytes: download, TotalUploadBytes: upload,
		DownloadAt: downloadAt, UploadAt: uploadAt, Source: hex.EncodeToString(source[:]),
	}, nil
}

func (c *Client) watchAction(ctx context.Context, svc service, action string) (soapValues, error) {
	if err := ctx.Err(); err != nil {
		return soapValues{}, watchError(err)
	}
	values, err := c.actionOnService(ctx, svc, action)
	if err != nil {
		return soapValues{}, watchError(err)
	}
	return values, nil
}

func watchNumber(value string) (*uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil, watchError(&Error{Kind: "protocol"})
	}
	return &number, nil
}

func watchError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "watch", Message: "router watch snapshot failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode = protocolErr.Kind, protocolErr.StatusCode
		if protocolErr.Code == tlsUntrustedCode {
			result.Code = tlsUntrustedCode
			result.Message = tlsRemediation
		}
		if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Kind = "auth"
		} else if result.Kind == "router" && protocolErr.FaultCode == "401" {
			result.Kind = "unsupported"
		}
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		result.Kind = "network"
	}
	return result
}
