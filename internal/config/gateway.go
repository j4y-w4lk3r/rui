package config

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// Site detection on a cable: both homes can use the same LAN addresses, so the
// router's IP says nothing, but its hardware (MAC) address is unique per home.
// The MAC -> site table lives in the user's settings.json (gateway_macs), never
// in this repository.

var macRe = regexp.MustCompile(`(?i)\b([0-9a-f]{1,2}(?::[0-9a-f]{1,2}){5})\b`)

// gatewayMAC returns the default gateway's IP and MAC address.
func gatewayMAC() (string, string, error) {
	gw, err := defaultGateway()
	if err != nil {
		return "", "", err
	}
	if mac := neighborMAC(gw); mac != "" {
		return gw, mac, nil
	}
	// not in the neighbour table yet: one ping fills it
	_ = exec.Command("ping", "-c", "1", "-W", "1", gw).Run()
	if mac := neighborMAC(gw); mac != "" {
		return gw, mac, nil
	}
	return gw, "", fmt.Errorf("no MAC address for gateway %s", gw)
}

func defaultGateway() (string, error) {
	if runtime.GOOS == "linux" {
		f, err := os.Open("/proc/net/route")
		if err != nil {
			return "", err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fl := strings.Fields(sc.Text())
			// Iface Destination Gateway Flags ... ; default route = destination 00000000
			if len(fl) > 2 && fl[1] == "00000000" && fl[2] != "00000000" {
				b, err := hex.DecodeString(fl[2])
				if err != nil || len(b) != 4 {
					continue
				}
				ip := make(net.IP, 4)
				binary.LittleEndian.PutUint32(ip, binary.BigEndian.Uint32(b))
				return ip.String(), nil
			}
		}
		return "", fmt.Errorf("no default route")
	}
	out, err := exec.Command("route", "-n", "get", "default").Output() // macOS / BSD
	if err != nil {
		return "", fmt.Errorf("route get default: %w", err)
	}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), ":"); ok && k == "gateway" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("no default gateway")
}

func neighborMAC(ip string) string {
	if runtime.GOOS == "linux" {
		raw, err := os.ReadFile("/proc/net/arp")
		if err == nil {
			for _, l := range strings.Split(string(raw), "\n") {
				fl := strings.Fields(l)
				if len(fl) >= 4 && fl[0] == ip && fl[3] != "00:00:00:00:00:00" {
					return normMAC(fl[3])
				}
			}
		}
		return ""
	}
	out, _ := exec.Command("arp", "-n", ip).Output()
	if m := macRe.FindString(string(out)); m != "" {
		return normMAC(m)
	}
	return ""
}

// normMAC writes a MAC as six lowercase two-digit groups (macOS arp drops leading zeros).
func normMAC(m string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(m)), ":")
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}

// locationFromGateway matches the default gateway's MAC against settings.json gateway_macs.
func locationFromGateway() (Location, string, error) {
	s, err := LoadSettings()
	if err != nil {
		return Location{}, "", err
	}
	if len(s.GatewayMACs) == 0 {
		return Location{}, "", fmt.Errorf("no gateway_macs in settings.json")
	}
	gw, mac, err := gatewayMAC()
	if err != nil {
		return Location{}, "", err
	}
	for id, macs := range s.GatewayMACs {
		for _, want := range macs {
			if normMAC(want) == mac {
				for _, loc := range locations {
					if loc.Active && strings.EqualFold(loc.ID, id) {
						return loc, "router " + gw + " " + mac, nil
					}
				}
			}
		}
	}
	return Location{}, "", fmt.Errorf("router %s (%s) is not a known home", gw, mac)
}
