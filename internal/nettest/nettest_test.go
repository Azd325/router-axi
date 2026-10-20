package nettest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

func TestGuardRefusesNonLoopback(t *testing.T) { AssertGuarded(t) }

func TestGuardAllowsLoopback(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func TestGuardRefusesNamesThatMayResolveToLoopbackOrNot(t *testing.T) {
	for _, address := range []string{"router.invalid:49000", "192.0.2.1:49000", "[2001:db8::1]:49000", "no-port"} {
		if _, err := dialLoopback(t.Context(), "tcp", address); err == nil {
			t.Errorf("dialLoopback(%q) succeeded", address)
		}
	}
}
