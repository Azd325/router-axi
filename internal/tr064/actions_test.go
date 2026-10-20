package tr064

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestCheckAction(t *testing.T) {
	for action, want := range map[string]bool{
		"GetInfo":                            true,
		"X_GetDNSServers":                    true,
		"X_AVM-DE_GetCallList":               true,
		"X_AVM_DE_GetAutoDisconnectTimeSpan": true,
		"SetEnable":                          true,
		"Reboot":                             true,
		"ForceTermination":                   true,
		"X_AVM-DE_WakeOnLANByMACAddress":     true,
		"X_AVM-DE_CheckUpdate":               true,
		"SetSSID":                            false,
		"X_AVM-DE_SetWPSConfig":              false,
		"AddPortMapping":                     false,
		"DeletePortMapping":                  false,
		"X_AVM-DE_DoUpdate":                  false,
		"getinfo":                            false,
		"":                                   false,
	} {
		if got := checkAction(action) == nil; got != want {
			t.Errorf("checkAction(%q) allowed=%t, want %t", action, got, want)
		}
	}
}

type unreachableTransport struct{ t *testing.T }

func (u unreachableTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u.t.Errorf("request reached the transport: %s %s", r.Method, r.Header.Get("SOAPAction"))
	return nil, http.ErrNotSupported
}

func TestSOAPLayerRefusesUnapprovedActionsBeforeSending(t *testing.T) {
	client, err := New("http://router.invalid:49000", "", "", &http.Client{Transport: unreachableTransport{t}})
	if err != nil {
		t.Fatal(err)
	}
	target := service{Type: "urn:dslforum-org:service:WLANConfiguration:1", ControlURL: "/upnp/control/wlanconfig1"}
	if _, err := client.actionOnService(t.Context(), target, "SetSSID"); err == nil || !strings.Contains(err.Error(), "approved mutation") {
		t.Fatalf("actionOnService error = %v", err)
	}
	if _, _, sent, err := client.postOnce(t.Context(), target, "SetSSID", ""); err == nil || sent {
		t.Fatalf("postOnce sent=%t error=%v", sent, err)
	}
}

var mutatingActionLiteral = regexp.MustCompile(`^(X_AVM[-_]DE_|X_)?(Set|Add|Delete|Remove|Reset|Start|Stop|Check|Do|Force|Reboot|Wake|Update|Enable|Disable|Send|Dial|Install|Apply|Create|Clear|Write)[A-Z][A-Za-z]*$`)

func sourceStringLiterals(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	literals := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				if value, err := strconv.Unquote(literal.Value); err == nil {
					literals[value] = true
				}
			}
			return true
		})
	}
	return literals
}

func TestSourceNamesOnlyApprovedMutations(t *testing.T) {
	literals := sourceStringLiterals(t)
	var unapproved []string
	for value := range literals {
		if mutatingActionLiteral.MatchString(value) && !strings.HasSuffix(value, "Response") && !approvedMutations[value] {
			unapproved = append(unapproved, value)
		}
	}
	sort.Strings(unapproved)
	if len(unapproved) > 0 {
		t.Fatalf("non-test source names mutating SOAP actions %q; add one to approvedMutations only after an approved design change (AGENTS.md)", unapproved)
	}
	for action := range approvedMutations {
		if !literals[action] {
			t.Errorf("approvedMutations lists %q, which no command sends", action)
		}
	}
}
