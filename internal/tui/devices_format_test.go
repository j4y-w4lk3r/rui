package tui

import "testing"

func TestFormatDeviceIP(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"192.168.1.44", "192.168.1.44"},
		{"fe80::d2ea:11ff:febb:c859", "IPv6 only"},
		{"", "—"},
	}
	for _, tc := range tests {
		if got := formatDeviceIP(tc.in); got != tc.want {
			t.Fatalf("formatDeviceIP(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
