package tr064

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultEventLogLimit  = 100
	MaxEventLogLimit      = 1000
	eventLogServicePrefix = "urn:dslforum-org:service:DeviceInfo:"
	eventLogAction        = "X_AVM-DE_GetDeviceLogPath"
	eventLogRemediation   = "use firmware advertising DeviceInfo:X_AVM-DE_GetDeviceLogPath with documented XML event groups"
	maxEventLogBytes      = 8 << 20
)

type EventLogLine struct {
	Group string `json:"group"`
	Date  string `json:"date"`
	Time  string `json:"time"`
	Text  string `json:"text"`
}

type EventLog struct {
	Groups  []string       `json:"groups"`
	Lines   []EventLogLine `json:"lines"`
	Total   int            `json:"total"`
	Omitted int            `json:"omitted"`
	More    string         `json:"more,omitempty"`
}

// EventLogGroups expands only AVM's documented filters. Phone logs require
// an explicit fon or all selection; the empty selection excludes them.
func EventLogGroups(selection string) ([]string, error) {
	if selection == "" {
		return []string{"sys", "net", "wlan", "usb"}, nil
	}
	if selection == "all" {
		return []string{"sys", "net", "fon", "wlan", "usb"}, nil
	}
	var groups []string
	seen := map[string]bool{}
	for _, group := range strings.Split(selection, ",") {
		if !eventLogGroupKnown(group) || seen[group] {
			return nil, &Error{Kind: "usage", Operation: "event-log", Message: "--group requires distinct sys, net, fon, wlan, usb groups separated by commas, or all (includes telephony)"}
		}
		seen[group] = true
		groups = append(groups, group)
	}
	return groups, nil
}

func eventLogGroupKnown(group string) bool {
	return group == "sys" || group == "net" || group == "fon" || group == "wlan" || group == "usb"
}

func (c *Client) EventLog(ctx context.Context, selection string, limit int) (EventLog, error) {
	groups, err := EventLogGroups(selection)
	if err != nil {
		return EventLog{}, err
	}
	if limit < 1 || limit > MaxEventLogLimit {
		return EventLog{}, &Error{Kind: "usage", Operation: "event-log", Message: "--limit must be an integer from 1 to 1000"}
	}
	if c.base.User != nil || c.base.RawQuery != "" || c.base.ForceQuery || c.base.Fragment != "" || (c.base.EscapedPath() != "" && c.base.EscapedPath() != "/") {
		return EventLog{}, &Error{Kind: "usage", Code: "invalid_configuration", Operation: "event-log", Message: "event-log requires a router origin without user information, query, fragment, or non-root path"}
	}
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("event-log refuses redirects") }
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return EventLog{}, eventLogError(err)
	}
	target, err := client.uniqueService(eventLogServicePrefix, "event-log", eventLogRemediation)
	if err != nil {
		return EventLog{}, err
	}
	scpdURL, err := client.base.Parse(target.SCPDURL)
	if err != nil || target.SCPDURL == "" || !sameOrigin(client.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return EventLog{}, eventLogInvalid("service description URL")
	}
	body, err := client.get(ctx, scpdURL)
	if err != nil {
		return EventLog{}, eventLogError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil || scpd.XMLName.Local != "scpd" {
		return EventLog{}, eventLogInvalid("service description")
	}
	found := false
	for _, action := range scpd.Actions {
		found = found || strings.TrimSpace(action.Name) == eventLogAction
	}
	if !found {
		return EventLog{}, &Error{Kind: "unsupported", Operation: "event-log", Message: "router does not advertise grouped event logs; " + eventLogRemediation}
	}
	control := client.base.ResolveReference(&url.URL{Path: target.ControlURL})
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="` + soapNamespace + `" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + eventLogAction + ` xmlns:u="` + target.Type + `"></u:` + eventLogAction + `></s:Body></s:Envelope>`
	headers := http.Header{"Content-Type": {`text/xml; charset="utf-8"`}, "SOAPAction": {`"` + target.Type + `#` + eventLogAction + `"`}}
	body, status, err := client.request(ctx, http.MethodPost, control, []byte(envelope), headers)
	if err != nil {
		return EventLog{}, eventLogError(err)
	}
	if status == http.StatusForbidden {
		return EventLog{}, eventLogError(&Error{Kind: "auth", StatusCode: status})
	}
	var values soapValues
	if err := xml.Unmarshal(body, &values); err != nil {
		return EventLog{}, eventLogInvalid("SOAP response")
	}
	if status >= 400 || values.FaultCode != "" {
		return EventLog{}, eventLogError(&Error{Kind: "router", StatusCode: status, FaultCode: values.FaultCode})
	}
	var response struct {
		XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
		Body    struct {
			Response struct {
				XMLName xml.Name
				Path    []string `xml:"NewDeviceLogPath"`
			} `xml:"X_AVM-DE_GetDeviceLogPathResponse"`
		} `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
	}
	if err := xml.Unmarshal(body, &response); err != nil || response.Body.Response.XMLName.Space != target.Type || len(response.Body.Response.Path) != 1 {
		return EventLog{}, eventLogInvalid("SOAP response")
	}
	path := strings.TrimSpace(response.Body.Response.Path[0])
	u, err := client.base.Parse(path)
	if err != nil || path == "" || !sameOrigin(client.base, u) || u.User != nil || u.Fragment != "" || u.Path == "" {
		return EventLog{}, eventLogInvalid("download URL")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return EventLog{}, eventLogInvalid("download query")
	}
	filter := "all"
	if len(groups) == 1 {
		filter = groups[0]
	}
	query.Set("filter", filter)
	u.RawQuery = query.Encode()
	body, err = client.get(ctx, u)
	if err != nil {
		return EventLog{}, eventLogError(err)
	}
	if len(body) >= maxEventLogBytes {
		return EventLog{}, &Error{Kind: "protocol", Code: "event_log_too_large", Operation: "event-log", Message: "event-log download reached the 8 MiB body limit; request a single group with --group sys, net, fon, wlan, or usb"}
	}
	return parseEventLog(body, groups, limit)
}

func parseEventLog(body []byte, groups []string, limit int) (EventLog, error) {
	var document struct {
		XMLName xml.Name `xml:"DeviceLog"`
		Events  []struct {
			Group   []string `xml:"group"`
			Date    string   `xml:"date"`
			Time    string   `xml:"time"`
			Message []string `xml:"msg"`
		} `xml:"Event"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil {
		return EventLog{}, eventLogInvalid("XML download")
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return EventLog{}, eventLogInvalid("XML download")
		}
		if data, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(data)) != "" {
			return EventLog{}, eventLogInvalid("XML download")
		}
	}
	result := EventLog{Groups: groups, Lines: []EventLogLine{}}
	selected := map[string]bool{}
	for _, group := range groups {
		selected[group] = true
	}
	for _, event := range document.Events {
		if len(event.Group) != 1 || !eventLogGroupKnown(event.Group[0]) || len(event.Message) != 1 {
			return EventLog{}, eventLogInvalid("event group or message; cannot safely filter telephony")
		}
		if !selected[event.Group[0]] {
			continue
		}
		message := strings.ReplaceAll(strings.ReplaceAll(event.Message[0], "\r\n", "\n"), "\r", "\n")
		for _, line := range strings.Split(message, "\n") {
			result.Total++
			if len(result.Lines) < limit {
				result.Lines = append(result.Lines, EventLogLine{event.Group[0], event.Date, event.Time, line})
			} else {
				result.Omitted++
			}
		}
	}
	if result.Omitted != 0 {
		result.More = "repeat event-log with the same --host and --group selection and --limit 1000; at the hard maximum, narrow --group to sys, net, fon, wlan, or usb"
	}
	return result, nil
}

func eventLogInvalid(field string) *Error {
	return &Error{Kind: "protocol", Operation: "event-log", Message: "router returned an invalid event-log " + field}
}

func eventLogError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "event-log", Message: "event-log inspection failed"}
	var source *Error
	if errors.As(err, &source) {
		result.Kind, result.StatusCode, result.FaultCode = source.Kind, source.StatusCode, source.FaultCode
		if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden || result.FaultCode == "606" {
			result.Kind = "auth"
		}
		if result.Kind == "router" && result.FaultCode == "401" {
			result.Kind = "unsupported"
		}
		if source.Code == tlsUntrustedCode {
			result.Code, result.Message = tlsUntrustedCode, tlsRemediation
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support grouped event logs; " + eventLogRemediation
	}
	return result
}
