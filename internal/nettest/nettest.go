// Package nettest keeps ordinary tests off the network. A test package calls
// Main from TestMain; afterwards http.DefaultTransport refuses any dial to an
// address other than loopback, so a test can never reach a real router.
package nettest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"testing"
	"time"
)

var errNonLoopback = errors.New("test dialed a non-loopback address")

var live = http.DefaultTransport

func Main(m *testing.M) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialLoopback
	http.DefaultTransport = transport
	os.Exit(m.Run())
}

// LiveClient is the only client allowed to leave loopback. Opt-in live tests
// use it after they pass their own environment gate.
func LiveClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: live}
}

func dialLoopback(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if host != "localhost" {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.IsLoopback() {
			return nil, errNonLoopback
		}
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// AssertGuarded fails the test when http.DefaultTransport can still reach a
// non-loopback address.
func AssertGuarded(t *testing.T) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://fritz.box:49000/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, errNonLoopback) {
		t.Fatalf("dial to a non-loopback address error = %v, want %v", err, errNonLoopback)
	}
}
