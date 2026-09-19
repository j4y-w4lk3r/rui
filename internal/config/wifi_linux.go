//go:build linux

package config

import (
	"fmt"
	"os/exec"
	"strings"
)

func currentWiFiSSID() (string, error) {
	if ssid, err := ssidFromNMCLI(); err == nil && ssid != "" {
		return ssid, nil
	}
	if ssid, err := ssidFromIwgetid(); err == nil && ssid != "" {
		return ssid, nil
	}
	return "", nil
}

func ssidFromNMCLI() (string, error) {
	out, err := exec.Command("nmcli", "-t", "-f", "ACTIVE,SSID", "dev", "wifi").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == "yes" && parts[1] != "" && parts[1] != "--" {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("nmcli: no active WiFi SSID")
}

func ssidFromIwgetid() (string, error) {
	out, err := exec.Command("iwgetid", "-r").Output()
	if err != nil {
		return "", err
	}
	ssid := strings.TrimSpace(string(out))
	if ssid == "" {
		return "", fmt.Errorf("iwgetid: empty SSID")
	}
	return ssid, nil
}
