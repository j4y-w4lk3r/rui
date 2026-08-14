package livebox

import (
	"context"
	"fmt"
	"strings"
)

// FindByName returns the first device whose Name contains the query
// (case-insensitive). Exact matches are preferred over partial matches.
func FindByName(ctx context.Context, c *Client, query string) (Device, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Device{}, fmt.Errorf("empty device name")
	}

	devices, err := c.ListDevices(ctx)
	if err != nil {
		return Device{}, err
	}

	q := strings.ToLower(query)
	var (
		partial    Device
		hasPartial bool
	)

	for _, d := range devices {
		name := strings.ToLower(strings.TrimSpace(d.Name))
		if name == "" || d.IPAddress == "" {
			continue
		}
		if name == q {
			return d, nil
		}
		if strings.Contains(name, q) {
			if !hasPartial {
				partial = d
				hasPartial = true
			}
		}
	}

	if hasPartial {
		return partial, nil
	}
	return Device{}, fmt.Errorf("device %q not found on router", query)
}
