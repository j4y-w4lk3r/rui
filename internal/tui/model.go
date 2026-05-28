package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

// editMode tracks whether the user is in a modal sub-view (device edit,
// reboot confirmation, etc.). Only one modal is active at a time.
type editMode int

const (
	modeNormal editMode = iota
	modeConfirmReboot
	modeConfirmLogout
	modeDeviceDetail   // viewing a single device's info
	modeDeviceRename   // text-input modal
	modeDeviceTypePick // type picker modal
	modeLanguagePick   // UI language picker modal
)

// focusMode tracks which pane currently receives navigation keys. The two
// panes behave like vim splits: keys never leak between them. To move
// between panes the user explicitly presses `l` (sidebar → tab) or `esc`
// (tab → sidebar).
type focusMode int

const (
	focusSidebar focusMode = iota // j/k change tab, l/enter activates it
	focusTab                      // hjkl navigate tab content, esc returns
)

// Model is the root Bubble Tea model.
type Model struct {
	client *livebox.Client
	host   string

	width, height int

	// Top-level navigation.
	focus   focusMode // which pane keys go to
	tabIdx  int       // selected tab in the sidebar
	current tabID     // active tab (== tabs[tabIdx].id usually)

	// Within-tab selection (which tile / row).
	gridIdx   int // service/eko/etc. tile selection
	deviceIdx int // devices list selection

	// Per-modal state.
	mode      editMode
	rename    textinput.Model
	typeIdx   int // type picker selection
	editingKy string
	langIdx   int // language picker selection

	loggedIn bool
	loading  string
	status   string
	statusOK bool

	// Cached payloads.
	info    *livebox.DeviceInfo
	wan     *livebox.WANStatus
	devices []livebox.Device
	wifi    *livebox.WiFiStatus
	guest   *livebox.GuestWiFiStatus
	iptv    *livebox.IPTVStatus
	phone   *livebox.PhoneStatus
	power   *livebox.PowerProfileSummary
	lang    *livebox.LanguageInfo

	startedAt time.Time
}

// New constructs the model. Login is kicked off on Init.
func New(client *livebox.Client, host string) Model {
	ti := textinput.New()
	ti.Placeholder = "device name"
	ti.CharLimit = 64
	ti.Width = 40

	return Model{
		client:    client,
		host:      host,
		current:   tabOverview,
		startedAt: time.Now(),
		loading:   "Logging in...",
		rename:    ti,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loginCmd(m.client), tickEvery(time.Second))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		return m, tickEvery(time.Second)

	case loginDoneMsg:
		m.loading = ""
		if msg.err != nil {
			m.statusOK = false
			m.status = "Login failed: " + msg.err.Error()
			return m, nil
		}
		m.loggedIn = true
		m.statusOK = true
		role := m.client.Groups()
		if role == "" {
			role = "session"
		}
		m.status = fmt.Sprintf("Connected to %s as %s (%s)", m.host, m.client.Username(), role)
		// On first login pull everything so all tabs have data without
		// per-tab refreshes.
		return m, m.refreshAll()

	case deviceInfoMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError("device info", msg.err)
			return m, nil
		}
		m.info = msg.info
		return m, nil

	case wanStatusMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError("WAN status", msg.err)
			return m, nil
		}
		m.wan = msg.status
		return m, nil

	case devicesMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError("devices", msg.err)
			return m, nil
		}
		m.devices = msg.devices
		m.statusOK = true
		if len(m.devices) == 0 {
			m.status = "Device list empty"
		} else {
			m.status = fmt.Sprintf("Loaded %d devices", len(m.devices))
		}
		// Keep selection in range.
		if m.deviceIdx >= len(m.devices) {
			m.deviceIdx = len(m.devices) - 1
		}
		if m.deviceIdx < 0 {
			m.deviceIdx = 0
		}
		return m, nil

	case wifiStatusMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError("wifi status", msg.err)
			return m, nil
		}
		m.wifi = msg.status
		return m, nil

	case guestWiFiMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError("guest wifi", msg.err)
			return m, nil
		}
		m.guest = msg.status
		return m, nil

	case iptvMsg:
		m.loading = ""
		if msg.err == nil {
			m.iptv = msg.status
		}
		return m, nil

	case phoneMsg:
		m.loading = ""
		if msg.err == nil {
			m.phone = msg.status
		}
		return m, nil

	case powerMsg:
		m.loading = ""
		if msg.err == nil {
			m.power = msg.status
		}
		return m, nil

	case languageMsg:
		m.loading = ""
		if msg.err == nil {
			m.lang = msg.info
		}
		return m, nil

	case actionDoneMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError(msg.label, msg.err)
			return m, nil
		}
		m.statusOK = true
		m.status = msg.label + " — OK"
		// Refresh the current tab so the change is reflected.
		return m, m.refreshCurrent()

	case clipboardMsg:
		m.loading = ""
		if msg.err != nil {
			m.setError(msg.label, msg.err)
			return m, nil
		}
		m.statusOK = true
		m.status = msg.label
		// Deliberately no refresh — clipboard ops are router-side
		// no-ops and a refresh would clobber this status line.
		return m, nil
	}

	// Textinput updates while in rename mode.
	if m.mode == modeDeviceRename {
		var cmd tea.Cmd
		m.rename, cmd = m.rename.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) setError(what string, err error) {
	m.statusOK = false
	m.status = fmt.Sprintf("%s: %v", what, err)
}

// ---------- key handling ----------
//
// Design: two-pane focus model, like vim splits.
//
//   focusSidebar (default):
//     j/k or ↑/↓   move between tabs
//     l or enter   "open" the current tab → switch to focusTab
//     q / ctrl+c   quit
//     r / R        refresh
//
//   focusTab:
//     h/j/k/l      navigate inside the tab (grid or list)
//     enter        activate the current tile / open the current device
//     esc          one level back:
//                    rename modal  → detail view
//                    detail view   → device list
//                    list / grid   → back to sidebar (focusSidebar)
//     y/n          only when a confirm modal is up
//
// Modal sub-views (rename, type picker, confirm reboot/logout) capture all
// keys for as long as they're open, regardless of focus mode.

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Quit is always global.
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// The rename modal is the only place where letters (including `q`)
	// must be typeable, so route to it before the generic quit shortcut.
	if m.mode == modeDeviceRename {
		return m.handleRenameKeys(msg)
	}

	// `q` quits from anywhere else.
	if key == "q" {
		return m, tea.Quit
	}

	// Other modal sub-views.
	switch m.mode {
	case modeConfirmReboot:
		return m.handleConfirmReboot(msg)
	case modeConfirmLogout:
		return m.handleConfirmLogout(msg)
	case modeDeviceTypePick:
		return m.handleTypePickerKeys(msg)
	case modeLanguagePick:
		return m.handleLanguagePickerKeys(msg)
	case modeDeviceDetail:
		return m.handleDeviceDetailKeys(msg)
	}

	// Refresh works from either pane.
	if key == "r" {
		return m, m.refreshCurrent()
	}
	if key == "R" {
		m.loading = "Refreshing everything..."
		return m, m.refreshAll()
	}

	// Pane-specific bindings.
	switch m.focus {
	case focusSidebar:
		return m.handleSidebarKeys(msg)
	case focusTab:
		return m.handleTabKeys(msg)
	}
	return m, nil
}

// handleSidebarKeys: j/k change selection, l/enter activate the tab.
func (m Model) handleSidebarKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.tabIdx > 0 {
			m.tabIdx--
		}
	case "down", "j":
		if m.tabIdx < len(tabs)-1 {
			m.tabIdx++
		}
	case "right", "l", "enter", " ":
		// Enter the tab. Reset selection to the top-left tile / first row.
		m.current = tabs[m.tabIdx].id
		m.gridIdx = 0
		m.focus = focusTab
		return m, m.refreshCurrent()
	}
	return m, nil
}

// handleTabKeys: hjkl navigate, enter activates, esc returns to sidebar.
func (m Model) handleTabKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Devices list has its own layout; the others share the tile grid.
	if m.current == tabDevices {
		return m.handleDeviceListKeys(msg)
	}
	return m.handleGridKeys(msg)
}

// handleGridKeys: navigate tiles, enter activates, esc returns to sidebar.
func (m Model) handleGridKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tiles := m.currentTiles()
	cols := m.tileCols()
	if cols < 1 {
		cols = 1
	}

	switch msg.String() {
	case "esc":
		m.focus = focusSidebar
	case "h", "left":
		if m.gridIdx > 0 {
			m.gridIdx--
		}
	case "l", "right":
		if m.gridIdx < len(tiles)-1 {
			m.gridIdx++
		}
	case "k", "up":
		if m.gridIdx-cols >= 0 {
			m.gridIdx -= cols
		}
	case "j", "down":
		if m.gridIdx+cols < len(tiles) {
			m.gridIdx += cols
		}
	case "enter", " ":
		if m.gridIdx >= 0 && m.gridIdx < len(tiles) {
			return m.activateTile(tiles[m.gridIdx])
		}
	// Quick toggles still work without drilling into a tile.
	case "t":
		if m.wifi != nil {
			m.loading = "Toggling Wi-Fi..."
			return m, setWiFiCmd(m.client, !m.wifi.Enabled)
		}
	case "g":
		if m.guest != nil {
			m.loading = "Toggling Guest Wi-Fi..."
			return m, setGuestWiFiCmd(m.client, !m.guest.Enable)
		}
	case "b":
		m.mode = modeConfirmReboot
	case "x":
		if m.current == tabSession {
			m.mode = modeConfirmLogout
		}
	}
	return m, nil
}

// activateTile dispatches Enter on a tile to its action.
func (m Model) activateTile(t tile) (tea.Model, tea.Cmd) {
	switch t.id {
	case "devices":
		m.tabIdx = tabIndexOf(tabDevices)
		m.current = tabDevices
		m.deviceIdx = 0
		return m, m.refreshCurrent()
	case "wifi":
		if m.wifi != nil {
			m.loading = "Toggling Wi-Fi..."
			return m, setWiFiCmd(m.client, !m.wifi.Enabled)
		}
	case "guest":
		if m.guest != nil {
			m.loading = "Toggling Guest Wi-Fi..."
			return m, setGuestWiFiCmd(m.client, !m.guest.Enable)
		}
	case "restart":
		m.mode = modeConfirmReboot
	case "logout":
		m.mode = modeConfirmLogout
	case "sysinfo":
		m.tabIdx = tabIndexOf(tabOverview)
		m.current = tabOverview
	case "lang":
		// Make sure we have the live list before opening the picker;
		// if not, fetch it and reopen on arrival is unnecessary — open
		// optimistically with the cached/fallback values.
		m.langIdx = m.findLanguageIndex()
		m.mode = modeLanguagePick
		if m.lang == nil {
			return m, fetchLanguage(m.client)
		}
	}
	return m, nil
}

// handleDeviceListKeys: jk to scroll, enter to open detail, esc to sidebar.
// y/Y yank the highlighted row's IP/MAC to the system clipboard so the
// user can paste it straight into ssh, ping, mitmproxy, etc.
func (m Model) handleDeviceListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusSidebar
	case "up", "k":
		if m.deviceIdx > 0 {
			m.deviceIdx--
		}
	case "down", "j":
		if m.deviceIdx < len(m.devices)-1 {
			m.deviceIdx++
		}
	case "home":
		m.deviceIdx = 0
	case "end", "G":
		if len(m.devices) > 0 {
			m.deviceIdx = len(m.devices) - 1
		}
	case "enter", "l":
		if m.deviceIdx >= 0 && m.deviceIdx < len(m.devices) {
			m.mode = modeDeviceDetail
		}
	case "y":
		return m.yankDeviceField(yankIP)
	case "Y":
		return m.yankDeviceField(yankMAC)
	}
	return m, nil
}

// handleDeviceDetailKeys: e=rename, T=type, y/Y=copy IP/MAC,
// esc=back to list.
func (m Model) handleDeviceDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.deviceIdx < 0 || m.deviceIdx >= len(m.devices) {
		m.mode = modeNormal
		return m, nil
	}
	d := m.devices[m.deviceIdx]
	switch msg.String() {
	case "esc", "h", "backspace":
		m.mode = modeNormal // back to list (still focusTab)
	case "e":
		m.rename.SetValue(d.Name)
		m.rename.Focus()
		m.editingKy = d.Key
		m.mode = modeDeviceRename
		return m, textinput.Blink
	case "T":
		m.editingKy = d.Key
		m.typeIdx = findTypeIndex(d.DeviceType)
		m.mode = modeDeviceTypePick
	case "y":
		return m.yankDeviceField(yankIP)
	case "Y":
		return m.yankDeviceField(yankMAC)
	}
	return m, nil
}

// yankField identifies which device attribute the user wants to copy.
type yankField int

const (
	yankIP yankField = iota
	yankMAC
)

// yankDeviceField copies the selected device's IP or MAC to the system
// clipboard. Result lands in the status bar via actionDoneMsg, with
// loading feedback in between so a hung pbcopy/xclip is visible.
func (m Model) yankDeviceField(field yankField) (tea.Model, tea.Cmd) {
	if m.deviceIdx < 0 || m.deviceIdx >= len(m.devices) {
		return m, nil
	}
	d := m.devices[m.deviceIdx]
	var value, what string
	switch field {
	case yankIP:
		value, what = d.IPAddress, "IP"
	case yankMAC:
		value, what = d.PhysAddress, "MAC"
	}
	if value == "" {
		m.statusOK = false
		m.status = what + " is empty for this device"
		return m, nil
	}
	m.loading = "Copying " + what + " to clipboard..."
	return m, copyToClipboardCmd(value, what)
}

func (m Model) handleRenameKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rename.Blur()
		m.mode = modeDeviceDetail
		return m, nil
	case "enter":
		newName := strings.TrimSpace(m.rename.Value())
		if newName == "" || m.editingKy == "" {
			m.mode = modeDeviceDetail
			return m, nil
		}
		m.rename.Blur()
		m.mode = modeDeviceDetail
		m.loading = "Renaming..."
		return m, renameDeviceCmd(m.client, m.editingKy, newName)
	}
	var cmd tea.Cmd
	m.rename, cmd = m.rename.Update(msg)
	return m, cmd
}

func (m Model) handleTypePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeDeviceDetail
	case "up", "k":
		if m.typeIdx > 0 {
			m.typeIdx--
		}
	case "down", "j":
		if m.typeIdx < len(livebox.DeviceTypes)-1 {
			m.typeIdx++
		}
	case "home":
		m.typeIdx = 0
	case "end", "G":
		m.typeIdx = len(livebox.DeviceTypes) - 1
	case "enter":
		if m.editingKy == "" {
			m.mode = modeDeviceDetail
			return m, nil
		}
		newType := livebox.DeviceTypes[m.typeIdx]
		m.mode = modeDeviceDetail
		m.loading = "Setting type..."
		return m, setDeviceTypeCmd(m.client, m.editingKy, newType)
	}
	return m, nil
}

func (m Model) handleLanguagePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	langs := m.languageChoices()
	switch msg.String() {
	case "esc", "h", "backspace":
		m.mode = modeNormal
	case "up", "k":
		if m.langIdx > 0 {
			m.langIdx--
		}
	case "down", "j":
		if m.langIdx < len(langs)-1 {
			m.langIdx++
		}
	case "home":
		m.langIdx = 0
	case "end", "G":
		if len(langs) > 0 {
			m.langIdx = len(langs) - 1
		}
	case "enter", " ", "l":
		if m.langIdx < 0 || m.langIdx >= len(langs) {
			m.mode = modeNormal
			return m, nil
		}
		choice := langs[m.langIdx]
		// No-op if the user re-picked the current language.
		if m.lang != nil && strings.EqualFold(choice, m.lang.Current) {
			m.mode = modeNormal
			m.statusOK = true
			m.status = "Język już ustawiony na " + choice
			return m, nil
		}
		m.mode = modeNormal
		m.loading = "Setting language..."
		return m, tea.Batch(
			setLanguageCmd(m.client, choice),
			fetchLanguage(m.client),
		)
	}
	return m, nil
}

func (m Model) handleConfirmReboot(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = modeNormal
		m.loading = "Rebooting router..."
		return m, rebootCmd(m.client)
	case "n", "N", "esc":
		m.mode = modeNormal
		m.statusOK = true
		m.status = "Reboot cancelled"
	}
	return m, nil
}

func (m Model) handleConfirmLogout(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = modeNormal
		m.loading = "Logging out..."
		return m, tea.Sequence(logoutCmd(m.client), tea.Quit)
	case "n", "N", "esc":
		m.mode = modeNormal
		m.statusOK = true
		m.status = "Logout cancelled"
	}
	return m, nil
}

// ---------- refresh helpers ----------

func (m Model) refreshAll() tea.Cmd {
	return tea.Batch(
		fetchDeviceInfo(m.client),
		fetchWAN(m.client),
		fetchDevices(m.client),
		fetchWiFi(m.client),
		fetchGuestWiFi(m.client),
		fetchIPTV(m.client),
		fetchPhone(m.client),
		fetchPower(m.client),
		fetchLanguage(m.client),
	)
}

func (m Model) refreshCurrent() tea.Cmd {
	if !m.loggedIn {
		return nil
	}
	switch m.current {
	case tabOverview:
		return tea.Batch(fetchDeviceInfo(m.client), fetchWAN(m.client))
	case tabServices:
		return tea.Batch(
			fetchDevices(m.client),
			fetchWAN(m.client),
			fetchWiFi(m.client),
			fetchGuestWiFi(m.client),
			fetchIPTV(m.client),
			fetchPhone(m.client),
		)
	case tabDevices:
		return fetchDevices(m.client)
	case tabEko:
		return fetchPower(m.client)
	case tabAdvanced:
		return tea.Batch(fetchWAN(m.client), fetchDeviceInfo(m.client), fetchLanguage(m.client))
	case tabSupport:
		return fetchDeviceInfo(m.client)
	}
	return nil
}

// ---------- view ----------

func (m Model) View() string {
	if m.width == 0 {
		return "starting up..."
	}

	title := titleStyle.Render(" Orange Livebox TUI ") +
		" " + hintStyle.Render(m.host)

	sidebarW := 18
	detailW := m.width - sidebarW - 4
	if detailW < 20 {
		detailW = 20
	}
	bodyH := m.height - 4
	if bodyH < 6 {
		bodyH = 6
	}

	sidebarBox := paneBlurred
	detailBox := paneBlurred
	if m.focus == focusSidebar {
		sidebarBox = paneFocused
	} else {
		detailBox = paneFocused
	}

	sidebar := sidebarBox.Width(sidebarW).Height(bodyH).Render(m.renderSidebar())
	detail := detailBox.Width(detailW).Height(bodyH).Render(m.renderDetail(detailW - 2))

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, detail)
	status := m.renderStatus()

	return lipgloss.JoinVertical(lipgloss.Left, title, body, status)
}

func (m Model) renderSidebar() string {
	var b strings.Builder

	// Focus label so it's obvious at a glance which pane gets keys.
	if m.focus == focusSidebar {
		b.WriteString(okStyle.Render("◆ sidebar") + "\n\n")
	} else {
		b.WriteString(hintStyle.Render("◇ sidebar") + "\n\n")
	}

	for i, it := range tabs {
		line := it.label
		if i == m.tabIdx {
			if m.focus == focusSidebar {
				line = menuItemSel.Render("▸ " + line)
			} else {
				// Indicate the tab is currently open but the user is
				// editing inside it (focus is on the tab pane).
				line = menuItemStyle.Render("· " + line)
			}
		} else {
			line = menuItemStyle.Render("  " + line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.focus == focusSidebar {
		b.WriteString(hintStyle.Render(
			"j/k  tab up/down\n" +
				"l    enter tab\n" +
				"r/R  refresh\n" +
				"q    quit",
		))
	} else {
		b.WriteString(hintStyle.Render(
			"esc  back to sidebar\n" +
				"h/j/k/l  navigate\n" +
				"enter  activate\n" +
				"t    toggle Wi-Fi\n" +
				"g    toggle guest\n" +
				"b    reboot\n" +
				"r/R  refresh",
		))
	}
	return b.String()
}

func (m Model) renderDetail(_ int) string {
	if !m.loggedIn && m.loading != "" {
		return m.loading
	}
	if !m.loggedIn {
		return badStyle.Render("Not connected. Check .env credentials.")
	}

	// Modal sub-views.
	switch m.mode {
	case modeConfirmReboot:
		return badStyle.Render("Reboot the Livebox?") + "\n\n" +
			"This will drop your connection for ~2 minutes.\n\n" +
			hintStyle.Render("press y to confirm, n to cancel")
	case modeConfirmLogout:
		return badStyle.Render("Log out and exit?") + "\n\n" +
			"This releases the session on the router and quits the TUI.\n\n" +
			hintStyle.Render("press y to confirm, n to cancel")
	case modeDeviceDetail:
		return m.renderDeviceDetail()
	case modeDeviceRename:
		return m.renderRename()
	case modeDeviceTypePick:
		return m.renderTypePicker()
	case modeLanguagePick:
		return m.renderLanguagePicker()
	}

	switch m.current {
	case tabOverview:
		return m.renderOverview()
	case tabServices:
		return m.renderServiceGrid()
	case tabDevices:
		return m.renderDevices()
	case tabEko:
		return sectionTitle("Eko") + renderTileGrid(m.ekoTiles(), m.gridIdx, m.tileCols(), 0)
	case tabAdvanced:
		return sectionTitle("Ustawienia zaawansowane") + renderTileGrid(m.advancedTiles(), m.gridIdx, m.tileCols(), 0)
	case tabSupport:
		return sectionTitle("Wsparcie") + renderTileGrid(m.supportTiles(), m.gridIdx, m.tileCols(), 0)
	case tabSession:
		return sectionTitle("Zarządzanie sesją") + renderTileGrid(m.sessionTiles(), m.gridIdx, m.tileCols(), 0)
	}
	return ""
}

func (m Model) renderOverview() string {
	if m.info == nil && m.wan == nil {
		return "Loading..."
	}
	var b strings.Builder
	b.WriteString(sectionTitle("System"))
	if m.info != nil {
		b.WriteString(kv("Model", m.info.ModelName))
		b.WriteString(kv("Product", m.info.ProductClass))
		b.WriteString(kv("Software", m.info.SoftwareVersion))
		b.WriteString(kv("Hardware", m.info.HardwareVersion))
		b.WriteString(kv("Serial", m.info.SerialNumber))
		b.WriteString(kv("Uptime", formatUptime(m.info.UpTime)))
	}
	b.WriteString("\n")
	b.WriteString(sectionTitle("WAN"))
	if m.wan != nil {
		b.WriteString(kv("Link type", m.wan.LinkType))
		b.WriteString(kv("Link state", coloredState(m.wan.LinkState, "up")))
		b.WriteString(kv("Protocol", m.wan.Protocol))
		b.WriteString(kv("Connection", coloredState(m.wan.ConnectionState, "Connected")))
		b.WriteString(kv("Public IP", m.wan.IPAddress))
		b.WriteString(kv("Gateway", m.wan.RemoteGateway))
		b.WriteString(kv("DNS", m.wan.DNSServers))
		b.WriteString(kv("MAC", m.wan.MACAddress))
	}
	return b.String()
}

func (m Model) renderServiceGrid() string {
	return sectionTitle("Usługi") +
		renderTileGrid(m.servicesTiles(), m.gridIdx, m.tileCols(), 0) + "\n" +
		hintStyle.Render("enter tile  •  t toggle wifi  •  g toggle guest")
}

func (m Model) renderDevices() string {
	if m.devices == nil {
		return "Loading devices..."
	}
	if len(m.devices) == 0 {
		return "No devices reported."
	}
	var b strings.Builder
	b.WriteString(sectionTitle(fmt.Sprintf("Podłączone urządzenia (%d)", len(m.devices))))
	header := fmt.Sprintf("  %-22s %-15s %-17s %s", "Name", "IP", "MAC", "Type")
	b.WriteString(keyStyle.Copy().Width(0).Render(header))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", 70))
	b.WriteString("\n")

	// Display a scrolling window around the selection.
	maxRows := m.height - 14
	if maxRows < 5 {
		maxRows = 5
	}
	start := 0
	if m.deviceIdx >= maxRows {
		start = m.deviceIdx - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.devices) {
		end = len(m.devices)
	}
	for i := start; i < end; i++ {
		d := m.devices[i]
		name := d.Name
		if name == "" {
			name = "(unknown)"
		}
		name = truncStr(name, 22)
		marker := "○"
		if d.Active {
			marker = "●"
		}
		row := fmt.Sprintf("%s %-22s %-15s %-17s %s",
			marker, name, d.IPAddress, d.PhysAddress, d.DeviceType)
		if i == m.deviceIdx {
			row = menuItemSel.Render("▸ " + row)
		} else {
			row = "  " + row
			if !d.Active {
				row = hintStyle.Render(row)
			}
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render(
		"enter — open device  •  ↑/↓ navigate  •  y — copy IP  •  Y — copy MAC",
	))
	return b.String()
}

func (m Model) renderDeviceDetail() string {
	if m.deviceIdx < 0 || m.deviceIdx >= len(m.devices) {
		return "no device"
	}
	d := m.devices[m.deviceIdx]
	name := d.Name
	if name == "" {
		name = "(unknown)"
	}

	var b strings.Builder
	b.WriteString(sectionTitle("Personalizuj urządzenie"))
	b.WriteString(kv("Name", name))
	b.WriteString(kv("Type", d.DeviceType))
	b.WriteString(kv("IP", d.IPAddress))
	b.WriteString(kv("MAC", d.PhysAddress))
	b.WriteString(kv("Interface", d.Layer2Iface))
	b.WriteString(kv("Active", boolText(d.Active)))
	b.WriteString(kv("Last seen", d.LastConnect))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render(
		"e — rename\n" +
			"T — change type\n" +
			"y — copy IP    Y — copy MAC\n" +
			"esc — back",
	))
	return b.String()
}

func (m Model) renderRename() string {
	if m.deviceIdx < 0 || m.deviceIdx >= len(m.devices) {
		return "no device"
	}
	d := m.devices[m.deviceIdx]
	var b strings.Builder
	b.WriteString(sectionTitle("Rename device"))
	b.WriteString(kv("Current", d.Name))
	b.WriteString(kv("MAC", d.PhysAddress))
	b.WriteString("\n")
	b.WriteString(m.rename.View())
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("enter — save   •   esc — cancel"))
	return b.String()
}

func (m Model) renderTypePicker() string {
	var b strings.Builder
	if m.deviceIdx >= 0 && m.deviceIdx < len(m.devices) {
		d := m.devices[m.deviceIdx]
		b.WriteString(sectionTitle(fmt.Sprintf("Zdefiniuj typ urządzenia — %s", d.Name)))
	} else {
		b.WriteString(sectionTitle("Zdefiniuj typ urządzenia"))
	}

	// Vertical scrolling list, ~14 rows window around selection.
	const win = 14
	start := m.typeIdx - win/2
	if start < 0 {
		start = 0
	}
	end := start + win
	if end > len(livebox.DeviceTypes) {
		end = len(livebox.DeviceTypes)
		start = end - win
		if start < 0 {
			start = 0
		}
	}
	for i := start; i < end; i++ {
		row := "  " + livebox.DeviceTypes[i]
		if i == m.typeIdx {
			row = menuItemSel.Render("▸ " + livebox.DeviceTypes[i])
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("↑/↓ move  •  enter — apply  •  esc — cancel"))
	return b.String()
}

func (m Model) renderLanguagePicker() string {
	var b strings.Builder
	b.WriteString(sectionTitle("Wybierz język"))

	current := "—"
	if m.lang != nil && m.lang.Current != "" {
		current = languageLabel(m.lang.Current)
	}
	b.WriteString(kv("Aktualny", current))
	b.WriteString("\n")

	langs := m.languageChoices()
	if len(langs) == 0 {
		b.WriteString(hintStyle.Render("loading languages from router..."))
		return b.String()
	}

	for i, code := range langs {
		label := fmt.Sprintf("%s  %s", code, languageLabel(code))
		if m.lang != nil && strings.EqualFold(code, m.lang.Current) {
			label += "  " + okStyle.Render("(active)")
		}
		row := "  " + label
		if i == m.langIdx {
			row = menuItemSel.Render("▸ " + label)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("↑/↓ wybór  •  enter — zastosuj  •  esc — anuluj"))
	return b.String()
}

// languageChoices returns the list to show in the picker. We prefer the
// list reported by the box; if it's not loaded yet, fall back to the
// known firmware-supported pair so the user never sees an empty modal.
func (m Model) languageChoices() []string {
	if m.lang != nil && len(m.lang.Available) > 0 {
		return m.lang.Available
	}
	return []string{"en", "pl"}
}

func (m Model) findLanguageIndex() int {
	if m.lang == nil {
		return 0
	}
	for i, code := range m.languageChoices() {
		if strings.EqualFold(code, m.lang.Current) {
			return i
		}
	}
	return 0
}

// languageLabel maps an ISO code to a friendlier name for display.
// The Funbox firmware only ships English and Polish.
func languageLabel(code string) string {
	switch strings.ToLower(code) {
	case "pl":
		return "Polski"
	case "en":
		return "English"
	case "fr":
		return "Français"
	case "es":
		return "Español"
	case "de":
		return "Deutsch"
	default:
		return code
	}
}

func (m Model) renderStatus() string {
	var left string
	switch {
	case m.loading != "":
		left = hintStyle.Render("⏳ " + m.loading)
	case m.status == "":
		left = hintStyle.Render("idle")
	case m.statusOK:
		left = okStyle.Render("✓ ") + m.status
	default:
		left = badStyle.Render("✗ ") + m.status
	}
	right := hintStyle.Render(fmt.Sprintf("session %s", time.Since(m.startedAt).Truncate(time.Second)))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return statusBarStyle.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

// ---------- per-tab tile helpers ----------

func (m Model) currentTiles() []tile {
	switch m.current {
	case tabServices:
		return m.servicesTiles()
	case tabEko:
		return m.ekoTiles()
	case tabAdvanced:
		return m.advancedTiles()
	case tabSupport:
		return m.supportTiles()
	case tabSession:
		return m.sessionTiles()
	}
	return nil
}

// tileCols guesses how many tiles fit horizontally in the detail pane.
// Each tile is ~20 cols wide; reserve a few for the sidebar and borders.
func (m Model) tileCols() int {
	w := m.width - 22
	cols := w / 21
	if cols < 1 {
		cols = 1
	}
	return cols
}

func tabIndexOf(id tabID) int {
	for i, t := range tabs {
		if t.id == id {
			return i
		}
	}
	return 0
}

func findTypeIndex(t string) int {
	for i, v := range livebox.DeviceTypes {
		if v == t {
			return i
		}
	}
	return 0
}

// ---------- helpers ----------

func kv(k, v string) string {
	if v == "" {
		v = "—"
	}
	return keyStyle.Render(k) + valStyle.Render(v) + "\n"
}

func boolText(b bool) string {
	if b {
		return okStyle.Render("yes")
	}
	return badStyle.Render("no")
}

func coloredState(state, goodValue string) string {
	if state == "" {
		return "—"
	}
	if strings.EqualFold(state, goodValue) {
		return okStyle.Render(state)
	}
	return badStyle.Render(state)
}

func formatUptime(seconds int64) string {
	if seconds <= 0 {
		return "—"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dd %02dh %02dm", days, hours, mins)
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func sectionTitle(s string) string {
	return titleStyle.Copy().Background(colorBg).Foreground(colorOrange).Render(s) + "\n\n"
}
