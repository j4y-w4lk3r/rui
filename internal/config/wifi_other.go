//go:build !linux

package config

import "fmt"

func currentWiFiSSID() (string, error) {
	return "", fmt.Errorf("WiFi location detection is only supported on Linux")
}
