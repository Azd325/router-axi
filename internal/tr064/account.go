package tr064

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	accountSecurityPrefix = "urn:dslforum-org:service:LANConfigSecurity:"
	accountAuthPrefix     = "urn:dslforum-org:service:X_AVM-DE_Auth:"
	accountRemediation    = "use firmware advertising LANConfigSecurity account reads and X_AVM-DE_Auth:GetInfo"
	accountMaxRights      = 32
)

var (
	accountRightPath   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
	accountKnownRights = map[string]int{"App": 0, "BoxAdmin": 1, "Phone": 2, "Dial": 3, "NAS": 4, "HomeAuto": 5}
)

type AccountRight struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

type Account struct {
	Username              string         `json:"username"`
	Rights                []AccountRight `json:"rights"`
	AnonymousLoginEnabled bool           `json:"anonymous_login_enabled"`
	DefaultPasswordActive *bool          `json:"default_password_active"`
	SecondFactorEnabled   bool           `json:"second_factor_enabled"`
}

func (c *Client) accountCapability() DoctorCheck {
	for _, prefix := range []string{accountSecurityPrefix, accountAuthPrefix} {
		count := 0
		for _, svc := range c.allServices {
			if strings.HasPrefix(svc.Type, prefix) {
				count++
			}
		}
		if count != 1 {
			return DoctorCheck{State: "unsupported", Remediation: accountRemediation}
		}
	}
	return DoctorCheck{State: "advertised"}
}

func (c *Client) Account(ctx context.Context) (Account, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("account inspection refuses redirects") }
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return Account{}, accountError(err)
	}
	security, err := client.accountService(ctx, accountSecurityPrefix, []string{"X_AVM-DE_GetCurrentUser", "GetInfo", "X_AVM-DE_GetAnonymousLogin"})
	if err != nil {
		return Account{}, err
	}
	auth, err := client.accountService(ctx, accountAuthPrefix, []string{"GetInfo"})
	if err != nil {
		return Account{}, err
	}
	current, err := client.actionOnService(ctx, security, "X_AVM-DE_GetCurrentUser")
	if err != nil {
		result := accountError(err)
		if result.Kind == "router" && result.FaultCode == "606" {
			result.Message = "router denied LANConfigSecurity:X_AVM-DE_GetCurrentUser; the logged-in account needs the App, Dial, Phone, NAS, or Homeauto right"
		}
		return Account{}, result
	}
	if current.CurrentUsername == nil || len(*current.CurrentUsername) > 256 || strings.ContainsFunc(*current.CurrentUsername, unicode.IsControl) {
		return Account{}, accountProtocolError("current username")
	}
	rights, err := parseAccountRights(current.CurrentUserRights)
	if err != nil {
		return Account{}, err
	}
	info, err := client.actionOnService(ctx, security, "GetInfo")
	if err != nil {
		return Account{}, accountError(err)
	}
	defaultPassword, err := accountBool(info.DefaultPasswordActive, "default password state", true)
	if err != nil {
		return Account{}, err
	}
	anonymous, err := client.actionOnService(ctx, security, "X_AVM-DE_GetAnonymousLogin")
	if err != nil {
		return Account{}, accountError(err)
	}
	anonymousEnabled, err := accountBool(anonymous.AnonymousLoginEnabled, "anonymous login state", false)
	if err != nil {
		return Account{}, err
	}
	secondFactor, err := client.actionOnService(ctx, auth, "GetInfo")
	if err != nil {
		return Account{}, accountError(err)
	}
	secondFactorEnabled, err := accountBool(secondFactor.Enabled, "second-factor state", false)
	if err != nil {
		return Account{}, err
	}
	return Account{Username: *current.CurrentUsername, Rights: rights, AnonymousLoginEnabled: *anonymousEnabled, DefaultPasswordActive: defaultPassword, SecondFactorEnabled: *secondFactorEnabled}, nil
}

func (c *Client) accountService(ctx context.Context, prefix string, required []string) (service, error) {
	var matches []service
	for _, svc := range c.allServices {
		if strings.HasPrefix(svc.Type, prefix) {
			matches = append(matches, svc)
		}
	}
	if len(matches) != 1 {
		return service{}, &Error{Kind: "unsupported", Operation: "account", Message: "router does not advertise exactly one required account service; " + accountRemediation}
	}
	target := matches[0]
	control, controlErr := c.base.Parse(target.ControlURL)
	scpdURL, scpdErr := c.base.Parse(target.SCPDURL)
	if controlErr != nil || target.ControlURL == "" || !sameOrigin(c.base, control) || control.User != nil || control.RawQuery != "" || control.ForceQuery || control.Fragment != "" || scpdErr != nil || target.SCPDURL == "" || !sameOrigin(c.base, scpdURL) || scpdURL.User != nil || scpdURL.RawQuery != "" || scpdURL.ForceQuery || scpdURL.Fragment != "" {
		return service{}, accountProtocolError("service URL")
	}
	body, err := c.get(ctx, scpdURL)
	if err != nil {
		return service{}, accountError(err)
	}
	var scpd struct {
		XMLName xml.Name `xml:"scpd"`
		Actions []struct {
			Name string `xml:"name"`
		} `xml:"actionList>action"`
	}
	if err := xml.Unmarshal(body, &scpd); err != nil {
		return service{}, accountProtocolError("service description")
	}
	actions := make(map[string]bool, len(scpd.Actions))
	for _, action := range scpd.Actions {
		actions[strings.TrimSpace(action.Name)] = true
	}
	for _, action := range required {
		if !actions[action] {
			return service{}, &Error{Kind: "unsupported", Operation: "account", Message: "router does not advertise all documented account reads; " + accountRemediation}
		}
	}
	target.ControlURL = control.Path
	return target, nil
}

func parseAccountRights(value *string) ([]AccountRight, error) {
	if value == nil {
		return nil, nil
	}
	if len(*value) > 65536 {
		return nil, accountProtocolError("rights list")
	}
	var rights struct {
		XMLName xml.Name `xml:"rights"`
		Text    string   `xml:",chardata"`
		Entries []struct {
			XMLName  xml.Name
			Value    string     `xml:",chardata"`
			Children []struct{} `xml:",any"`
		} `xml:",any"`
	}
	decoder := xml.NewDecoder(strings.NewReader(*value))
	if err := decoder.Decode(&rights); err != nil || strings.TrimSpace(rights.Text) != "" || len(rights.Entries)%2 != 0 || len(rights.Entries) > 2*accountMaxRights {
		return nil, accountProtocolError("rights list")
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, accountProtocolError("rights list")
		}
		switch token := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(token)) != "" {
				return nil, accountProtocolError("rights list")
			}
		case xml.Comment:
		default:
			return nil, accountProtocolError("rights list")
		}
	}
	result := make([]AccountRight, 0, len(rights.Entries)/2)
	seen := map[string]bool{}
	for i := 0; i < len(rights.Entries); i += 2 {
		path, access := rights.Entries[i], rights.Entries[i+1]
		name, permission := strings.TrimSpace(path.Value), strings.TrimSpace(access.Value)
		if path.XMLName.Local != "path" || access.XMLName.Local != "access" || len(path.Children) != 0 || len(access.Children) != 0 || !accountRightPath.MatchString(name) || seen[name] || (permission != "none" && permission != "readonly" && permission != "readwrite") {
			return nil, accountProtocolError("rights list")
		}
		seen[name] = true
		result = append(result, AccountRight{Path: name, Access: permission})
	}
	sort.SliceStable(result, func(i, j int) bool { return accountRightRank(result[i].Path) < accountRightRank(result[j].Path) })
	return result, nil
}

func accountRightRank(path string) int {
	if rank, known := accountKnownRights[path]; known {
		return rank
	}
	return len(accountKnownRights)
}

func accountBool(value, field string, optional bool) (*bool, error) {
	switch strings.TrimSpace(value) {
	case "":
		if optional {
			return nil, nil
		}
	case "1", "true":
		result := true
		return &result, nil
	case "0", "false":
		result := false
		return &result, nil
	}
	return nil, accountProtocolError(field)
}

func accountProtocolError(field string) *Error {
	return &Error{Kind: "protocol", Operation: "account", Message: "router returned an invalid or missing account " + field}
}

func accountError(err error) *Error {
	result := &Error{Kind: "protocol", Operation: "account", Message: "account rights and login posture inspection failed"}
	var protocolErr *Error
	if errors.As(err, &protocolErr) {
		result.Kind, result.StatusCode, result.FaultCode = protocolErr.Kind, protocolErr.StatusCode, protocolErr.FaultCode
		if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
			result.Kind = "auth"
		} else if result.Kind == "router" && result.FaultCode == "401" {
			result.Kind = "unsupported"
		}
	}
	if result.Kind == "unsupported" {
		result.Message = "router does not support documented account reads; " + accountRemediation
	}
	return result
}
