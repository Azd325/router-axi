package tr064

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type watchReply struct {
	body     string
	status   int
	location string
}

func watchServices() []service {
	return []service{
		{Type: "urn:dslforum-org:service:Layer3Forwarding:1", ControlURL: "/layer3"},
		{Type: "urn:dslforum-org:service:WANCommonInterfaceConfig:1", ID: "common1", ControlURL: "/common"},
		{Type: "urn:dslforum-org:service:WANIPConnection:1", ID: "urn:WANIPConnection-com:serviceId:WANIPConnection1", ControlURL: "/ip1"},
		{Type: "urn:dslforum-org:service:WANPPPConnection:1", ID: "urn:WANPPPConnection-com:serviceId:WANPPPConnection1", ControlURL: "/ppp1"},
	}
}

func watchDescription(t *testing.T, services []service) string {
	t.Helper()
	body, err := xml.Marshal(struct {
		XMLName  xml.Name  `xml:"root"`
		Services []service `xml:"device>serviceList>service"`
	}{Services: services})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func watchResponses(t *testing.T, services []service) map[string]watchReply {
	t.Helper()
	return map[string]watchReply{
		"description":                 {body: watchDescription(t, services)},
		"GetDefaultConnectionService": {body: "<r><NewDefaultConnectionService>1.WANIPConnection.1</NewDefaultConnectionService></r>"},
		"GetStatusInfo":               {body: "<r><NewConnectionStatus>Connected</NewConnectionStatus><NewUptime>42</NewUptime><NewExternalIPAddress>private-canary</NewExternalIPAddress><NewLastConnectionError>private-canary</NewLastConnectionError></r>"},
		"GetTotalBytesReceived":       {body: "<r><NewTotalBytesReceived>18446744073709551615</NewTotalBytesReceived></r>"},
		"GetTotalBytesSent":           {body: "<r><NewTotalBytesSent>0</NewTotalBytesSent></r>"},
	}
}

func watchSOAPAction(r *http.Request) string {
	for _, key := range []string{"Soapaction", "SOAPAction"} {
		if values := r.Header[key]; len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func watchFixtureClient(t *testing.T, services []service, overrides map[string]watchReply) (*Client, func() []string) {
	t.Helper()
	if services == nil {
		services = watchServices()
	}
	responses := watchResponses(t, services)
	for key, value := range overrides {
		responses[key] = value
	}
	requests := make(chan string, 100)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "description"
		if r.Method == http.MethodGet && r.URL.Path == descriptionPath {
			requests <- "description"
		} else {
			_, key, _ = strings.Cut(strings.Trim(watchSOAPAction(r), `"`), "#")
			requests <- r.URL.Path + "#" + key
			if r.Method != http.MethodPost || key == "" {
				t.Error("unexpected request method or action")
			}
		}
		response, ok := responses[key]
		if !ok {
			t.Error("action outside watch allowlist")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if response.location != "" {
			w.Header().Set("Location", response.location)
		}
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		_, _ = io.WriteString(w, strings.ReplaceAll(response.body, "{origin}", origin))
	}))
	t.Cleanup(server.Close)
	origin = server.URL
	client, err := New(origin, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, func() []string {
		var result []string
		for len(requests) > 0 {
			result = append(result, <-requests)
		}
		return result
	}
}

func requireWatchError(t *testing.T, snapshot WatchSnapshot, err error, kind string) {
	t.Helper()
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Kind != kind || protocolErr.Operation != "watch" {
		t.Fatalf("wanted sanitized %s error, got %#v", kind, err)
	}
	if strings.Contains(fmt.Sprintf("%#v", protocolErr), "private-canary") || protocolErr.FaultCode != "" {
		t.Fatal("error disclosed router data")
	}
	if !reflect.DeepEqual(snapshot, WatchSnapshot{}) {
		t.Fatal("error returned a partial snapshot")
	}
}

func TestWatchSnapshotSelectionAndActionAllowlist(t *testing.T) {
	for _, target := range []string{
		"1.WANIPConnection.1", "urn:upnp-org:serviceId:WANIPConnection1",
		"urn:WANIPConnection-com:serviceId:WANIPConnection1", "urn:dslforum-org:service:WANIPConnection:1",
		"1.WANPPPConnection.1", "uuid:synthetic:WANPPPConnection.1",
	} {
		t.Run(target, func(t *testing.T) {
			client, requests := watchFixtureClient(t, nil, map[string]watchReply{
				"GetDefaultConnectionService": {body: "<r><NewDefaultConnectionService>" + target + "</NewDefaultConnectionService></r>"},
			})
			start := time.Now()
			times := []time.Time{start, start.Add(2 * time.Second)}
			client.now = func() time.Time {
				value := times[0]
				times = times[1:]
				return value
			}
			snapshot, err := client.WatchSnapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			path := "/ip1"
			if strings.Contains(target, "WANPPP") {
				path = "/ppp1"
			}
			want := []string{"description", "/layer3#GetDefaultConnectionService", path + "#GetStatusInfo", "/common#GetTotalBytesReceived", "/common#GetTotalBytesSent"}
			if got := requests(); !reflect.DeepEqual(got, want) {
				t.Fatalf("requests = %v, want %v", got, want)
			}
			if snapshot.WANStatus != "connected" || snapshot.WANUptimeSeconds == nil || *snapshot.WANUptimeSeconds != 42 || snapshot.TotalDownloadBytes == nil || *snapshot.TotalDownloadBytes != math.MaxUint64 || snapshot.TotalUploadBytes == nil || *snapshot.TotalUploadBytes != 0 {
				t.Fatal("incorrect snapshot values")
			}
			if snapshot.DownloadAt != start || snapshot.UploadAt != start.Add(2*time.Second) || snapshot.ObservedAt != snapshot.UploadAt || len(times) != 0 {
				t.Fatal("completion timestamps did not preserve original time values")
			}
			body, err := json.Marshal(snapshot)
			if err != nil || snapshot.Source == "" || strings.Contains(string(body), snapshot.Source) || strings.Contains(string(body), "private-canary") {
				t.Fatal("snapshot disclosed provenance or private response fields")
			}
		})
	}
}

func TestWatchSnapshotRejectsAmbiguousOrMissingServices(t *testing.T) {
	for _, index := range []int{0, 1, 2} {
		for _, duplicate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", index, duplicate), func(t *testing.T) {
				services := watchServices()
				if duplicate {
					services = append(services, services[index])
				} else {
					services = append(services[:index], services[index+1:]...)
				}
				client, requests := watchFixtureClient(t, services, nil)
				snapshot, err := client.WatchSnapshot(t.Context())
				requireWatchError(t, snapshot, err, "unsupported")
				for _, request := range requests() {
					if request != "description" && request != "/layer3#GetDefaultConnectionService" {
						t.Fatal("ambiguous target was contacted")
					}
				}
			})
		}
	}
	for _, target := range []string{"", "private-canary", "1.WANIPConnection.99"} {
		client, _ := watchFixtureClient(t, nil, map[string]watchReply{"GetDefaultConnectionService": {body: "<r><NewDefaultConnectionService>" + target + "</NewDefaultConnectionService></r>"}})
		snapshot, err := client.WatchSnapshot(t.Context())
		requireWatchError(t, snapshot, err, "unsupported")
	}
}

func TestWatchSnapshotOptionalIntegers(t *testing.T) {
	for _, field := range []struct{ action, name string }{
		{"GetStatusInfo", "NewUptime"}, {"GetTotalBytesReceived", "NewTotalBytesReceived"}, {"GetTotalBytesSent", "NewTotalBytesSent"},
	} {
		for _, value := range []string{"missing", "", " \t ", "0", "18446744073709551615", "private-canary", "-1", "+1", "1.5", "18446744073709551616"} {
			t.Run(field.name+"/"+value, func(t *testing.T) {
				body := "<r><" + field.name + ">" + value + "</" + field.name + "></r>"
				if value == "missing" {
					body = "<r/>"
				}
				client, _ := watchFixtureClient(t, nil, map[string]watchReply{field.action: {body: body}})
				snapshot, err := client.WatchSnapshot(t.Context())
				switch value {
				case "missing", "", " \t ", "0", "18446744073709551615":
					if err != nil {
						t.Fatal(err)
					}
					actual := map[string]*uint64{"NewUptime": snapshot.WANUptimeSeconds, "NewTotalBytesReceived": snapshot.TotalDownloadBytes, "NewTotalBytesSent": snapshot.TotalUploadBytes}[field.name]
					if value == "0" || value == "18446744073709551615" {
						want := uint64(0)
						if value != "0" {
							want = math.MaxUint64
						}
						if actual == nil || *actual != want {
							t.Fatal("integer value changed")
						}
					} else if actual != nil {
						t.Fatal("missing integer became zero")
					}
				default:
					requireWatchError(t, snapshot, err, "protocol")
				}
			})
		}
	}
}

func TestWatchSnapshotStatusAllowlist(t *testing.T) {
	for _, state := range []string{"Unconfigured", "Connecting", "Authenticating", "Connected", "PendingDisconnect", "Disconnecting", "Disconnected", "private-canary", ""} {
		client, _ := watchFixtureClient(t, nil, map[string]watchReply{"GetStatusInfo": {body: "<r><NewConnectionStatus>" + state + "</NewConnectionStatus></r>"}})
		snapshot, err := client.WatchSnapshot(t.Context())
		want := strings.ToLower(state)
		if state == "" || state == "private-canary" {
			want = "unknown"
		}
		if err != nil || snapshot.WANStatus != want {
			t.Fatalf("status normalization failed: %v", err)
		}
	}
}

func TestWatchSnapshotErrorsAreSanitized(t *testing.T) {
	for _, action := range []string{"description", "GetDefaultConnectionService", "GetStatusInfo", "GetTotalBytesReceived", "GetTotalBytesSent"} {
		for _, test := range []struct {
			kind  string
			reply watchReply
		}{
			{"auth", watchReply{status: 401, body: "private-canary"}},
			{"auth", watchReply{status: 403, body: "private-canary"}},
			{"router", watchReply{status: 503, body: "<r><errorCode>private-canary</errorCode><errorDescription>private-canary</errorDescription></r>"}},
			{"protocol", watchReply{body: "<private-canary"}},
		} {
			t.Run(action+"/"+test.kind, func(t *testing.T) {
				client, _ := watchFixtureClient(t, nil, map[string]watchReply{action: test.reply})
				snapshot, err := client.WatchSnapshot(t.Context())
				requireWatchError(t, snapshot, err, test.kind)
			})
		}
		if action != "description" {
			client, _ := watchFixtureClient(t, nil, map[string]watchReply{action: {status: 500, body: "<r><errorCode>401</errorCode><errorDescription>private-canary</errorDescription></r>"}})
			snapshot, err := client.WatchSnapshot(t.Context())
			requireWatchError(t, snapshot, err, "unsupported")
		}
	}
}

func TestWatchSnapshotRejectsUnsafeURLs(t *testing.T) {
	for _, index := range []int{0, 1, 2} {
		for _, control := range []string{"", "http://private-canary.invalid/control", "//private-canary.invalid/control", "{origin}/control?private-canary", "{origin}/control?", "{origin}/control#private-canary", "http://private-canary@localhost/control", "/%private-canary"} {
			t.Run(fmt.Sprintf("%d/%s", index, control), func(t *testing.T) {
				services := watchServices()
				services[index].ControlURL = control
				client, requests := watchFixtureClient(t, services, nil)
				snapshot, err := client.WatchSnapshot(t.Context())
				requireWatchError(t, snapshot, err, "protocol")
				for _, request := range requests() {
					if request != "description" && request != "/layer3#GetDefaultConnectionService" {
						t.Fatal("unsafe control URL was contacted")
					}
				}
			})
		}
	}
}

func TestWatchSnapshotNormalizesSameOriginURLs(t *testing.T) {
	services := watchServices()
	for i := range services {
		services[i].ControlURL = "{origin}/a/.." + services[i].ControlURL
	}
	client, requests := watchFixtureClient(t, services, nil)
	if _, err := client.WatchSnapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{"description", "/layer3#GetDefaultConnectionService", "/ip1#GetStatusInfo", "/common#GetTotalBytesReceived", "/common#GetTotalBytesSent"}
	if got := requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized requests = %v", got)
	}
}

func TestWatchSnapshotRejectsDirtyOrigin(t *testing.T) {
	for _, suffix := range []string{"/private-canary", "?private-canary", "?", "#private-canary"} {
		client, requests := watchFixtureClient(t, nil, nil)
		base, err := client.base.Parse(suffix)
		if err != nil {
			t.Fatal(err)
		}
		client.base = base
		snapshot, err := client.WatchSnapshot(t.Context())
		requireWatchError(t, snapshot, err, "protocol")
		if len(requests()) != 0 {
			t.Fatal("dirty origin was contacted")
		}
	}
}

func TestWatchSnapshotRefusesRedirects(t *testing.T) {
	for _, action := range []string{"description", "GetDefaultConnectionService", "GetStatusInfo", "GetTotalBytesReceived", "GetTotalBytesSent"} {
		client, requests := watchFixtureClient(t, nil, map[string]watchReply{action: {status: 307, location: "/private-canary"}})
		snapshot, err := client.WatchSnapshot(t.Context())
		requireWatchError(t, snapshot, err, "network")
		for _, request := range requests() {
			if strings.Contains(request, "private-canary") {
				t.Fatal("followed a redirect")
			}
		}
		if client.http.CheckRedirect != nil {
			t.Fatal("watch changed the caller HTTP client")
		}
	}
}

type watchRoundTripper func(*http.Request) (*http.Response, error)

func (f watchRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWatchSnapshotCancellationStopsBeforeEveryAction(t *testing.T) {
	for cancelAfter := 0; cancelAfter < 5; cancelAfter++ {
		t.Run(fmt.Sprint(cancelAfter), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			responses := watchResponses(t, watchServices())
			count := 0
			client, err := New("http://router.invalid:49000", "", "", &http.Client{Transport: watchRoundTripper(func(r *http.Request) (*http.Response, error) {
				count++
				key := "description"
				if r.Method == http.MethodPost {
					_, key, _ = strings.Cut(strings.Trim(watchSOAPAction(r), `"`), "#")
				}
				if count == cancelAfter {
					cancel()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responses[key].body)), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			if cancelAfter == 0 {
				cancel()
			}
			snapshot, err := client.WatchSnapshot(ctx)
			requireWatchError(t, snapshot, err, "network")
			if count != cancelAfter {
				t.Fatalf("sent %d requests after cancellation at %d", count, cancelAfter)
			}
		})
	}
}

func TestWatchSnapshotFreshDiscoveryAndProvenance(t *testing.T) {
	client, requests := watchFixtureClient(t, nil, nil)
	client.services = map[string]service{"private-canary": {ControlURL: "/private-canary"}}
	client.allServices = []service{{ControlURL: "/private-canary"}}
	first, err := client.WatchSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.WatchSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(requests()) != 10 || first.Source != second.Source || client.services["private-canary"].ControlURL != "/private-canary" || len(client.allServices) != 1 {
		t.Fatal("discovery cache or provenance was not isolated")
	}
	responses := watchResponses(t, watchServices())
	responses["GetDefaultConnectionService"] = watchReply{body: "<r><NewDefaultConnectionService>1.WANPPPConnection.1</NewDefaultConnectionService></r>"}
	client.http = &http.Client{Transport: watchRoundTripper(func(r *http.Request) (*http.Response, error) {
		key := "description"
		if r.Method == http.MethodPost {
			_, key, _ = strings.Cut(strings.Trim(watchSOAPAction(r), `"`), "#")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responses[key].body)), Request: r}, nil
	})}
	third, err := client.WatchSnapshot(t.Context())
	if err != nil || third.Source == first.Source {
		t.Fatal("WAN source change was not detected")
	}
}

func TestWatchSnapshotNetworkErrorsAndTLS(t *testing.T) {
	for _, failAfter := range []int{0, 1, 2, 3, 4} {
		client, _ := watchFixtureClient(t, nil, nil)
		next := client.http.Transport
		count := 0
		client.http.Transport = watchRoundTripper(func(r *http.Request) (*http.Response, error) {
			count++
			if count > failAfter {
				return nil, errors.New("private-canary")
			}
			return next.RoundTrip(r)
		})
		snapshot, err := client.WatchSnapshot(t.Context())
		requireWatchError(t, snapshot, err, "network")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("contacted an untrusted TLS endpoint")
	}))
	defer server.Close()
	client, err := New(server.URL, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.WatchSnapshot(t.Context())
	requireWatchError(t, snapshot, err, "network")
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Code != tlsUntrustedCode || strings.Contains(protocolErr.Error(), server.URL) {
		t.Fatal("TLS trust error was lost or disclosed the endpoint")
	}
}

func TestLiveWatchSnapshot(t *testing.T) {
	settings, enabled := liveTestConfig(os.Getenv)
	if !enabled {
		t.Skip("live router tests require explicit local opt-in and complete configuration")
	}
	client, err := New(settings.host, settings.username, settings.password, nil)
	if err != nil {
		t.Fatal("live router configuration was rejected")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var previous WatchSnapshot
	for read := 0; read < 2; read++ {
		snapshot, err := client.WatchSnapshot(ctx)
		if err != nil {
			t.Fatal("live watch snapshot read failed")
		}
		if snapshot.Source == "" || snapshot.WANStatus == "" || !snapshot.ObservedAt.Equal(snapshot.UploadAt) || snapshot.DownloadAt.After(snapshot.UploadAt) {
			t.Fatal("live watch snapshot returned an invalid shape")
		}
		if previous.Source != "" && previous.Source != snapshot.Source {
			t.Fatal("live watch snapshot provenance changed between reads")
		}
		for _, pair := range [][2]*uint64{{previous.TotalDownloadBytes, snapshot.TotalDownloadBytes}, {previous.TotalUploadBytes, snapshot.TotalUploadBytes}} {
			if pair[0] != nil && pair[1] != nil && *pair[1] < *pair[0] {
				t.Fatal("live watch snapshot counters decreased")
			}
		}
		previous = snapshot
	}
}
