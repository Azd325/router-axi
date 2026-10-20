package tr064

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestSOAPLayerRefusesUnapprovedActionsBeforeSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	target := service{Type: "urn:dslforum-org:service:WLANConfiguration:1", ControlURL: "/upnp/control/wlanconfig1"}
	for _, action := range []string{"SetSSID", "X_AVM-DE_SetWPSConfig"} {
		if _, err := client.actionOnService(t.Context(), target, action); err == nil || !strings.Contains(err.Error(), "approved mutation") {
			t.Errorf("actionOnService(%q) error = %v", action, err)
		}
		if _, _, sent, err := client.postOnce(t.Context(), target, action, ""); err == nil || sent {
			t.Errorf("postOnce(%q) sent=%t error=%v", action, sent, err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("%d requests reached the server", got)
	}
}

func TestApprovedMutationsPassCheckAction(t *testing.T) {
	for action := range approvedMutations {
		if err := checkAction(action); err != nil {
			t.Errorf("checkAction(%q) = %v", action, err)
		}
	}
}
