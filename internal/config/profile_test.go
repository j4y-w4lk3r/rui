package config

import "testing"

func TestHostFromURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://192.168.1.1", "192.168.1.1"},
		{"http://192.168.1.1/", "192.168.1.1"},
		{"192.168.1.1", "192.168.1.1"},
		{"http://router.local:8080/admin", "router.local:8080"},
	}
	for _, tc := range tests {
		got, err := hostFromURL(tc.in)
		if err != nil {
			t.Fatalf("hostFromURL(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("hostFromURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApplyProfilePL(t *testing.T) {
	if err := ApplyProfile("PL"); err != nil {
		t.Skip("op not available or PL profile unreachable:", err)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "admin" {
		t.Fatalf("username = %q, want admin", cfg.Username)
	}
	if cfg.Host != "192.168.1.1" {
		t.Fatalf("host = %q, want 192.168.1.1", cfg.Host)
	}
	if cfg.Password == "" {
		t.Fatal("password is empty")
	}
}
