package tui

import (
	"net"
	"strings"

	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

// formatDeviceIP shows a fixed-width friendly IP column value.
func formatDeviceIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "—"
	}
	lower := strings.ToLower(ip)
	if strings.HasPrefix(lower, "fe80:") {
		return "IPv6 only"
	}
	if strings.Contains(ip, ":") && len(ip) > 15 {
		// Global IPv6 — keep column readable.
		return truncStr(ip, 15)
	}
	return ip
}

func deviceRow(marker, name, ip, mac, dtype string, selected, active bool) string {
	const (
		colName = 22
		colIP   = 15
		colMAC  = 17
	)
	if name == "" {
		name = "(unknown)"
	}
	name = truncStr(name, colName)
	ip = truncStr(formatDeviceIP(ip), colIP)
	mac = truncStr(mac, colMAC)
	if dtype == "" {
		dtype = "—"
	}
	row := fmtDeviceRow(marker, name, ip, mac, dtype, colName, colIP, colMAC)
	if selected {
		return menuItemSel.Render("▸ " + row)
	}
	row = "  " + row
	if !active {
		return hintStyle.Render(row)
	}
	return row
}

func fmtDeviceRow(marker, name, ip, mac, dtype string, colName, colIP, colMAC int) string {
	pad := func(s string, w int) string {
		if len(s) >= w {
			return s
		}
		return s + strings.Repeat(" ", w-len(s))
	}
	return marker + " " + pad(name, colName) + " " + pad(ip, colIP) + " " + pad(mac, colMAC) + " " + dtype
}

func deviceMAC(d livebox.Device) string {
	if mac := strings.TrimSpace(d.PhysAddress); mac != "" {
		return mac
	}
	return strings.TrimSpace(d.Key)
}

func isIPv4(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)).To4() != nil
}

func findStaticLeaseIP(leases []livebox.StaticDHCPLease, mac string) string {
	want := strings.ToLower(strings.ReplaceAll(mac, "-", ":"))
	for _, l := range leases {
		got := strings.ToLower(strings.ReplaceAll(l.MACAddress, "-", ":"))
		if got == want {
			return l.IPAddress
		}
	}
	return ""
}

func suggestReserveIP(d livebox.Device, leases []livebox.StaticDHCPLease) string {
	if ip := findStaticLeaseIP(leases, deviceMAC(d)); ip != "" {
		return ip
	}
	ip := strings.TrimSpace(d.IPAddress)
	if isIPv4(ip) {
		return ip
	}
	return ""
}
