package livebox

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// DeviceInfo is a subset of the system-wide Livebox identity payload.
type DeviceInfo struct {
	ModelName       string `json:"ModelName"`
	ProductClass    string `json:"ProductClass"`
	SerialNumber    string `json:"SerialNumber"`
	SoftwareVersion string `json:"SoftwareVersion"`
	HardwareVersion string `json:"HardwareVersion"`
	UpTime          int64  `json:"UpTime"`
	ExternalIPv4    string `json:"ExternalIPAddress"`
	Manufacturer    string `json:"Manufacturer"`
}

// GetDeviceInfo returns identification info about the box.
func (c *Client) GetDeviceInfo(ctx context.Context) (*DeviceInfo, error) {
	resp, err := c.call(ctx, "DeviceInfo", "get", nil)
	if err != nil {
		return nil, err
	}
	var info DeviceInfo
	if err := json.Unmarshal(firstNonNil(resp.Status, resp.Data, resp.Result), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// WANStatus describes the upstream connection.
type WANStatus struct {
	LinkType      string `json:"LinkType"`
	LinkState     string `json:"LinkState"`
	WANState      string `json:"WANState"`
	Protocol      string `json:"Protocol"`
	ConnectionState string `json:"ConnectionState"`
	IPAddress     string `json:"IPAddress"`
	RemoteGateway string `json:"RemoteGateway"`
	DNSServers    string `json:"DNSServers"`
	MACAddress    string `json:"MACAddress"`
}

// GetWANStatus returns upstream link information.
func (c *Client) GetWANStatus(ctx context.Context) (*WANStatus, error) {
	resp, err := c.call(ctx, "NMC", "getWANStatus", nil)
	if err != nil {
		return nil, err
	}
	var s WANStatus
	if err := json.Unmarshal(firstNonNil(resp.Data, resp.Status, resp.Result), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Device is a single host/device known to the box.
type Device struct {
	Key         string `json:"Key"`
	Name        string `json:"Name"`
	DeviceType  string `json:"DeviceType"`
	Active      bool   `json:"Active"`
	IPAddress   string `json:"IPAddress"`
	PhysAddress string `json:"PhysAddress"`
	Layer2Iface string `json:"Layer2Interface"`
	LastConnect string `json:"LastConnection"`
}

// ListDevices returns the LAN/WLAN devices the box knows about.
//
// We try TopologyDiagnostics.buildTopology first because that's what the
// Funbox/Livebox web UI uses (confirmed by capturing the browser's real
// traffic). The other endpoints are fallbacks for older firmwares.
func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	attempts := []struct {
		service, method string
		params          map[string]any
	}{
		{"TopologyDiagnostics", "buildTopology", map[string]any{"SendXmlFile": false}},
		{"Devices", "get", nil},
		{"Hosts", "getDevices", nil},
		{"HostManager", "getDevices", nil},
	}

	var lastErr error
	for _, a := range attempts {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		perAttempt, cancel := context.WithTimeout(ctx, 6*time.Second)
		resp, err := c.call(perAttempt, a.service, a.method, a.params)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		devices, err := extractDevices(firstNonNil(resp.Status, resp.Data, resp.Result))
		if err != nil {
			lastErr = err
			continue
		}
		if len(devices) > 0 {
			return devices, nil
		}
		// Parsed OK but empty — still a successful fetch.
		return []Device{}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return []Device{}, nil
}

// extractDevices accepts any of the several shapes the box uses for device
// lists ([]Device, map[string][]Device, or a nested topology tree) and
// returns a flat de-duplicated, sorted slice.
//
// We try the tree shape FIRST because TopologyDiagnostics.buildTopology
// (our preferred call) returns the entire LAN as a tree rooted at the
// HGW itself — and the HGW has Name="FUNBOX" but no IP/MAC at the root.
// If we tried []Device first, json.Unmarshal would silently succeed with
// a single-element list containing just the router (no children), and
// hasUsefulFields would accept it because Name is non-empty. That's the
// exact failure mode we hit: the TUI showed only "FUNBOX" and none of
// the actual devices.
func extractDevices(raw json.RawMessage) ([]Device, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	// 1) Tree (TopologyDiagnostics.buildTopology).
	var tree []topoNode
	if err := json.Unmarshal(raw, &tree); err == nil && len(tree) > 0 {
		var all []Device
		collectTopology(tree, &all)
		if hasUsefulFields(all) {
			return finalize(all), nil
		}
	}

	// 2) Grouped by interface kind (Devices.get with filter params).
	var grouped map[string][]Device
	if err := json.Unmarshal(raw, &grouped); err == nil && len(grouped) > 0 {
		var all []Device
		for _, list := range grouped {
			all = append(all, list...)
		}
		if hasUsefulFields(all) {
			return finalize(all), nil
		}
	}

	// 3) Flat list (Hosts.getDevices on older firmwares).
	var flat []Device
	if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 && hasUsefulFields(flat) {
		return finalize(flat), nil
	}

	return nil, nil
}

type topoNode struct {
	Device
	Tags     string     `json:"Tags"`
	Children []topoNode `json:"Children"`
}

// collectTopology walks the topology tree depth-first and emits every node
// that looks like an actual device (has an L2 or L3 address) while skipping:
//   - the router itself (Tags contain "self" or "hgw"), and
//   - synthetic "interface" nodes that are children of the root and only
//     serve as a grouping container (no PhysAddress at all, e.g. "lan",
//     "guest", "iptv").
//
// Their children — the real hosts — are still walked normally.
func collectTopology(nodes []topoNode, out *[]Device) {
	for _, n := range nodes {
		if (n.PhysAddress != "" || n.IPAddress != "") && !isSelfNode(n) {
			*out = append(*out, n.Device)
		}
		collectTopology(n.Children, out)
	}
}

// isSelfNode reports whether the topology node represents the HGW itself
// (the box we're talking to). Funbox 7 tags the root with "self physical
// hgw ..." and gives its bridge interface ("lan") the same base MAC, but
// the interface has no Tags entry so we only match the literal root here.
func isSelfNode(n topoNode) bool {
	t := strings.ToLower(n.Tags)
	if t == "" {
		return false
	}
	return strings.Contains(t, " self ") ||
		strings.HasPrefix(t, "self ") ||
		strings.HasSuffix(t, " self") ||
		t == "self" ||
		strings.Contains(t, "hgw")
}

func hasUsefulFields(list []Device) bool {
	for _, d := range list {
		if d.PhysAddress != "" || d.IPAddress != "" {
			return true
		}
	}
	return false
}

func finalize(devices []Device) []Device {
	seen := map[string]struct{}{}
	var out []Device
	for _, d := range devices {
		key := d.Key
		if key == "" {
			key = d.PhysAddress
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// WiFiStatus reports the high-level Wi-Fi state.
type WiFiStatus struct {
	Enabled bool `json:"Enable"`
	Status  bool `json:"Status"`
}

// GetWiFiStatus returns whether the radio is enabled and broadcasting.
func (c *Client) GetWiFiStatus(ctx context.Context) (*WiFiStatus, error) {
	resp, err := c.call(ctx, "NMC.Wifi", "get", nil)
	if err != nil {
		return nil, err
	}
	var s WiFiStatus
	if err := json.Unmarshal(firstNonNil(resp.Status, resp.Data, resp.Result), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SetWiFi toggles the Wi-Fi radio on or off.
func (c *Client) SetWiFi(ctx context.Context, enable bool) error {
	_, err := c.call(ctx, "NMC.Wifi", "set", map[string]any{
		"Enable": enable,
		"Status": enable,
	})
	return err
}

// GuestWiFiStatus reports the state of the guest Wi-Fi network.
type GuestWiFiStatus struct {
	Enable             bool   `json:"Enable"`
	Status             string `json:"Status"`
	ActivationDuration int    `json:"ActivationDuration"`
}

// GetGuestWiFi returns whether the guest network is currently enabled.
func (c *Client) GetGuestWiFi(ctx context.Context) (*GuestWiFiStatus, error) {
	resp, err := c.call(ctx, "NMC.Guest", "get", nil)
	if err != nil {
		return nil, err
	}
	var s GuestWiFiStatus
	if err := json.Unmarshal(firstNonNil(resp.Status, resp.Data, resp.Result), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SetGuestWiFi enables or disables the guest Wi-Fi network. The shape mirrors
// NMC.Wifi.set on the Funbox 7 firmware.
func (c *Client) SetGuestWiFi(ctx context.Context, enable bool) error {
	_, err := c.call(ctx, "NMC.Guest", "set", map[string]any{
		"Enable": enable,
	})
	return err
}

// Reboot asks the Livebox to restart. The connection will drop shortly after.
func (c *Client) Reboot(ctx context.Context) error {
	_, err := c.call(ctx, "NMC", "reboot", map[string]any{"reason": "GUI_Reboot"})
	return err
}

// Logout releases the current session context. The browser does this on
// "Wyloguj" so the session ID is invalidated server-side. After this any
// further call will fail with permission-denied.
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.call(ctx, "sah.Device.Information", "releaseContext", map[string]any{
		"applicationName": "webui",
	})
	return err
}

// IPTVStatus is what NMC.OrangeTV.getIPTVStatus returns.
type IPTVStatus struct {
	IPTVStatus string `json:"IPTVStatus"`
}

func (c *Client) GetIPTV(ctx context.Context) (*IPTVStatus, error) {
	resp, err := c.call(ctx, "NMC.OrangeTV", "getIPTVStatus", nil)
	if err != nil {
		return nil, err
	}
	var s IPTVStatus
	if err := json.Unmarshal(firstNonNil(resp.Data, resp.Status, resp.Result), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// LanguageInfo describes the UI language settings.
type LanguageInfo struct {
	Current   string   // e.g. "pl"
	Available []string // e.g. ["en","pl"]
}

// GetLanguage returns the active UI language and the catalogue of values
// the box accepts. The wire shape is
//   { "status": "pl", "data": { "availableLanguages": ["en", "pl"] } }
// so the current value lives in the top-level "status" string.
func (c *Client) GetLanguage(ctx context.Context) (*LanguageInfo, error) {
	resp, err := c.call(ctx, "UserInterface", "getLanguage", nil)
	if err != nil {
		return nil, err
	}
	var info LanguageInfo
	// status is a quoted string like "pl"
	if len(resp.Status) > 0 {
		_ = json.Unmarshal(resp.Status, &info.Current)
	}
	if len(resp.Data) > 0 {
		var d struct {
			AvailableLanguages []string `json:"availableLanguages"`
		}
		if err := json.Unmarshal(resp.Data, &d); err == nil {
			info.Available = d.AvailableLanguages
		}
	}
	return &info, nil
}

// SetLanguage switches the UI language. The setter parameter is named
// "currentLanguage" — confirmed against the Funbox 7 firmware via probe;
// other names produce a "Missing mandatory argument" error.
func (c *Client) SetLanguage(ctx context.Context, lang string) error {
	_, err := c.call(ctx, "UserInterface", "setLanguage", map[string]any{
		"currentLanguage": lang,
	})
	return err
}

// PhoneStatus is a flattened view of VoiceService.listTrunks: whether any
// trunk reports itself as enabled and any of its lines as Up.
type PhoneStatus struct {
	Enabled bool
	Up      bool
	Trunks  int
}

func (c *Client) GetPhone(ctx context.Context) (*PhoneStatus, error) {
	resp, err := c.call(ctx, "VoiceService.VoiceApplication", "listTrunks", nil)
	if err != nil {
		return nil, err
	}
	// status: [{enable:"Enabled|Disabled", trunk_lines:[{status:"Up|..."}]}]
	var trunks []struct {
		Enable     string `json:"enable"`
		TrunkLines []struct {
			Status string `json:"status"`
		} `json:"trunk_lines"`
	}
	if err := json.Unmarshal(firstNonNil(resp.Status, resp.Data, resp.Result), &trunks); err != nil {
		return nil, err
	}
	st := &PhoneStatus{Trunks: len(trunks)}
	for _, t := range trunks {
		if strings.EqualFold(t.Enable, "Enabled") {
			st.Enabled = true
		}
		for _, ln := range t.TrunkLines {
			if strings.EqualFold(ln.Status, "Up") {
				st.Up = true
			}
		}
	}
	return st, nil
}

// PowerProfileSummary is the subset of PowerManagement.getProfiles we care
// about: whether each named profile is currently active.
type PowerProfileSummary struct {
	LEDActive       bool
	WiFiSchedActive bool
	DeepActive      bool
}

func (c *Client) GetPowerProfiles(ctx context.Context) (*PowerProfileSummary, error) {
	resp, err := c.call(ctx, "PowerManagement", "getProfiles", nil)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Activate bool `json:"Activate"`
		Status   bool `json:"Status"`
	}
	if err := json.Unmarshal(firstNonNil(resp.Status, resp.Data, resp.Result), &raw); err != nil {
		return nil, err
	}
	out := &PowerProfileSummary{}
	if p, ok := raw["LED"]; ok {
		out.LEDActive = p.Activate || p.Status
	}
	if p, ok := raw["WiFi"]; ok {
		out.WiFiSchedActive = p.Activate || p.Status
	}
	if p, ok := raw["Deep"]; ok {
		out.DeepActive = p.Activate || p.Status
	}
	return out, nil
}

// RenameDevice asks the box to rename a single device. The key is the
// device's Key field (e.g. "E0:63:DA:67:AD:78" or "lan"). The "source"
// argument tells the box where the change came from — the web UI uses
// "webui" so we mirror that.
func (c *Client) RenameDevice(ctx context.Context, key, name string) error {
	if key == "" {
		return errors.New("RenameDevice: empty key")
	}
	_, err := c.call(ctx, "Devices.Device."+key, "setName", map[string]any{
		"name":   name,
		"source": "webui",
	})
	return err
}

// SetDeviceType changes the categorical "type" of a device (Smartphone,
// Computer, Printer, etc.). The list of accepted values comes from the
// firmware's catalog and matches what the dropdown in the Funbox web UI
// shows. Unknown values silently fall back to "Generic Device".
func (c *Client) SetDeviceType(ctx context.Context, key, dtype string) error {
	if key == "" {
		return errors.New("SetDeviceType: empty key")
	}
	_, err := c.call(ctx, "Devices.Device."+key, "setType", map[string]any{
		"type":   dtype,
		"source": "webui",
	})
	return err
}

// DeviceTypes is the set of categorical device types the Funbox 7 web UI
// offers in its "Zdefiniuj typ urządzenia" dropdown. Values are the canonical
// English identifiers the box stores internally (the UI shows Polish labels
// because of UILang=pl, but the wire format is English).
//
// This list is intentionally a Go constant: the firmware ships it as a
// hardcoded JS table and there is no API to enumerate it. If a value the
// firmware no longer accepts is sent, setType returns "Object or parameter
// not found".
var DeviceTypes = []string{
	"Airbox",
	"Apple Airport",
	"Apple TV",
	"Apple Time Capsule",
	"Apple Time Capsule Airport",
	"Home Library",
	"Chromecast",
	"Door Sensor",
	"Smoke Detector",
	"Window Sensor",
	"Motion Sensor",
	"Domestic Robot",
	"Domino",
	"Printer",
	"Extender Wi-Fi Plus",
	"Femtocell",
	"AC Outlet",
	"Google OnHub",
	"HiFi",
	"Home Live",
	"Home Point",
	"Computer",
	"Linux Computer",
	"MacOS Computer",
	"Windows Computer",
	"Game Console",
	"Laptop",
	"Linux Laptop",
	"Windows Laptop",
	"Energy Meter",
	"LiveRadio",
	"Liveplug",
	"Macbook",
	"NAS",
	"Notebook",
	"Linux Notebook",
	"Windows Notebook",
	"Simple Button",
	"Set-top Box",
	"Set-top Box 4",
	"Set-top Box Play",
	"Set-top Box UHD",
	"Set-top Box Universel",
	"Smart Plug",
	"Smart Bulb",
	"Smartphone",
	"Android Smartphone",
	"Windows Smartphone",
	"Old Phone",
	"4-Port Switch",
	"8-Port Switch",
	"TV",
	"TV Stick",
	"TV Stick v2",
	"Tablet",
	"Android Tablet",
	"Windows Tablet",
	"Telephone",
	"WiFi_Access_Point",
}

// firstNonNil returns the first raw message that has content; useful because
// different Livebox firmwares put the payload under "data", "status", or
// "result" depending on the call.
func firstNonNil(candidates ...json.RawMessage) json.RawMessage {
	for _, c := range candidates {
		if len(c) > 0 && string(c) != "null" {
			return c
		}
	}
	return json.RawMessage("null")
}
