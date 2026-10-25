package livebox

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetDialKeepsRouterHost(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)

	c, err := New("192.168.1.1", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetDial(srv.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	resp, err := c.http.Get(c.baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %s", resp.Status)
	}
	if gotHost != "192.168.1.1" {
		t.Fatalf("Host = %q, want 192.168.1.1", gotHost)
	}
}

func TestIsPermissionDenied(t *testing.T) {
	if !IsPermissionDenied(errors.New("NMC.Guest.get: Permission denied")) {
		t.Fatal("expected permission denied")
	}
	if IsPermissionDenied(errors.New("timeout")) {
		t.Fatal("unexpected permission denied")
	}
	if IsPermissionDenied(nil) {
		t.Fatal("nil is not permission denied")
	}
}
