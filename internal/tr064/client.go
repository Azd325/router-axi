package tr064

import (
	"bytes"
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
	"strconv"
	"strings"
	"time"
)

const descriptionPath = "/tr64desc.xml"

type Error struct {
	Kind       string
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

type Call struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	Remote    string `json:"remote"`
	Name      string `json:"name,omitempty"`
	Date      string `json:"date"`
	Duration  string `json:"duration"`
	Device    string `json:"device,omitempty"`
}

type service struct {
	Type       string `xml:"serviceType"`
	ControlURL string `xml:"controlURL"`
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
	Status, LastError, ExternalIP                                string
	Manufacturer, Model, Serial, Software, Hardware              string
	Uptime, DownloadRate, UploadRate, TotalDownload, TotalUpload string
	CallListURL                                                  string
	FaultCode, FaultDescription                                  string
}

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
	now                func() time.Time
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

func (c *Client) action(ctx context.Context, prefix, action string) (soapValues, error) {
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
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + svc.Type + `"></u:` + action + `></s:Body></s:Envelope>`
	u := c.base.ResolveReference(&url.URL{Path: svc.ControlURL})
	headers := http.Header{"Content-Type": {`text/xml; charset="utf-8"`}, "SOAPAction": {`"` + svc.Type + `#` + action + `"`}}
	body, status, err := c.request(ctx, http.MethodPost, u, []byte(envelope), headers)
	if err != nil {
		return soapValues{}, err
	}
	var values soapValues
	if err := xml.Unmarshal(body, &values); err != nil {
		return values, &Error{Kind: "protocol", Operation: action, Message: "invalid SOAP response"}
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
		auth, authErr := digestAuthorization(challenge, method, u.RequestURI(), c.username, c.password)
		if authErr != nil {
			return nil, 0, &Error{Kind: "auth", Operation: method + " " + u.Path, Message: authErr.Error()}
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

func digestAuthorization(challenge, method, uri, username, password string) (string, error) {
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
	response := md5hex(ha1 + ":" + nonce + ":00000001:" + cnonce + ":auth:" + ha2)
	return fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q, algorithm=MD5, qop=auth, nc=00000001, cnonce=%q`, username, realm, nonce, uri, response, cnonce), nil
}

func md5hex(value string) string { sum := md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }
