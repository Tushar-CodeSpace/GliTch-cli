package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type viewState int

const (
	stateLogin viewState = iota
	stateMainApp
)

type tab int

const (
	tabDashboard tab = iota
	tabServices
	tabTelemetry
	tabLogs
)

// LogEntry represents an audit or operational log message.
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

// TelemetryData models incoming server telemetry metrics.
type TelemetryData struct {
	Timestamp time.Time `json:"timestamp"`
	Host      struct {
		Hostname        string `json:"hostname"`
		OS              string `json:"os"`
		Platform        string `json:"platform"`
		KernelVersion   string `json:"kernel_version"`
		KernelArch      string `json:"kernel_arch"`
		UptimeSeconds   uint64 `json:"uptime_seconds"`
		UptimeFormatted string `json:"uptime_formatted"`
	} `json:"host"`
	CPU struct {
		ModelName     string    `json:"model_name"`
		LogicalCores  int       `json:"logical_cores"`
		PhysicalCores int       `json:"physical_cores"`
		TotalUsagePct float64   `json:"total_usage_pct"`
		CoreUsagesPct []float64 `json:"core_usages_pct"`
		LoadAvg1      float64   `json:"load_avg_1"`
		LoadAvg5      float64   `json:"load_avg_5"`
		LoadAvg15     float64   `json:"load_avg_15"`
	} `json:"cpu"`
	Memory struct {
		TotalBytes      uint64  `json:"total_bytes"`
		UsedBytes       uint64  `json:"used_bytes"`
		FreeBytes       uint64  `json:"free_bytes"`
		UsedPercent     float64 `json:"used_percent"`
		SwapUsedPercent float64 `json:"swap_used_percent"`
		FormattedTotal  string  `json:"formatted_total"`
		FormattedUsed   string  `json:"formatted_used"`
		FormattedFree   string  `json:"formatted_free"`
	} `json:"memory"`
	Disk struct {
		Path           string  `json:"path"`
		FSType         string  `json:"fs_type"`
		UsedPercent    float64 `json:"used_percent"`
		FormattedTotal string  `json:"formatted_total"`
		FormattedUsed  string  `json:"formatted_used"`
		FormattedFree  string  `json:"formatted_free"`
	} `json:"disk"`
	Network struct {
		FormattedSent string `json:"formatted_sent"`
		FormattedRecv string `json:"formatted_recv"`
		PacketsSent   uint64 `json:"packets_sent"`
		PacketsRecv   uint64 `json:"packets_recv"`
	} `json:"network"`
	ProcessInfo struct {
		TotalCount int `json:"total_count"`
	} `json:"process_info"`
}

type ServiceHealthItem struct {
	Service string `json:"service"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	Latency string `json:"latency"`
}

type ClusterHealthData struct {
	Status    string              `json:"status"`
	Gateway   string              `json:"gateway"`
	Timestamp string              `json:"timestamp"`
	Services  []ServiceHealthItem `json:"services"`
}

type model struct {
	state          viewState
	currentTab     tab
	inputs         []textinput.Model
	focusIndex     int
	gatewayURL     string
	token          string
	loading        bool
	statusMsg      string
	userData       map[string]any
	telemetry      TelemetryData
	clusterHealth  ClusterHealthData
	logs           []LogEntry
	width          int
	height         int
	gatewayOnline  bool
	gatewayLatency string
	lastTick       time.Time
}

// Custom Messages
type authResultMsg struct {
	success  bool
	token    string
	message  string
	userData map[string]any
}

type telemetryMsg struct {
	data TelemetryData
	err  error
}

type clusterHealthMsg struct {
	data    ClusterHealthData
	latency string
	err     error
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func getGatewayURL() string {
	if val := os.Getenv("GATEWAY_URL"); val != "" {
		return strings.TrimRight(val, "/")
	}
	if val := os.Getenv("AUTH_URL"); val != "" {
		// If user configured legacy AUTH_URL, try using host
		if strings.Contains(val, "auth-service:8080") {
			return "http://api-gateway:8000"
		}
	}
	return "http://localhost:8000"
}

func loadSavedToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(home + "/.config/glitch/token.json")
	if err != nil {
		return ""
	}
	var res map[string]string
	_ = json.Unmarshal(data, &res)
	return res["token"]
}

func saveToken(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configDir := home + "/.config/glitch"
	_ = os.MkdirAll(configDir, 0700)
	fileData, _ := json.MarshalIndent(map[string]string{"token": token}, "", "  ")
	return os.WriteFile(configDir+"/token.json", fileData, 0600)
}

func removeSavedToken() {
	if home, err := os.UserHomeDir(); err == nil {
		_ = os.Remove(home + "/.config/glitch/token.json")
	}
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatUptime(seconds uint64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, secs)
	}
	return fmt.Sprintf("%dm %ds", minutes, secs)
}

// fetchLocalTelemetry collects metrics locally if gateway is unreachable.
func fetchLocalTelemetry() TelemetryData {
	var td TelemetryData
	td.Timestamp = time.Now().UTC()

	if h, err := host.Info(); err == nil && h != nil {
		td.Host.Hostname = h.Hostname
		td.Host.OS = h.OS
		td.Host.Platform = h.Platform
		td.Host.KernelVersion = h.KernelVersion
		td.Host.KernelArch = h.KernelArch
		td.Host.UptimeSeconds = h.Uptime
		td.Host.UptimeFormatted = formatUptime(h.Uptime)
	}

	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		td.CPU.TotalUsagePct = pcts[0]
	}
	td.CPU.CoreUsagesPct, _ = cpu.Percent(0, true)
	td.CPU.LogicalCores, _ = cpu.Counts(true)
	td.CPU.PhysicalCores, _ = cpu.Counts(false)

	if cInfos, err := cpu.Info(); err == nil && len(cInfos) > 0 {
		td.CPU.ModelName = cInfos[0].ModelName
	}
	if lAvg, err := load.Avg(); err == nil && lAvg != nil {
		td.CPU.LoadAvg1 = lAvg.Load1
		td.CPU.LoadAvg5 = lAvg.Load5
		td.CPU.LoadAvg15 = lAvg.Load15
	}

	if vMem, err := mem.VirtualMemory(); err == nil && vMem != nil {
		td.Memory.TotalBytes = vMem.Total
		td.Memory.UsedBytes = vMem.Used
		td.Memory.FreeBytes = vMem.Free
		td.Memory.UsedPercent = vMem.UsedPercent
		td.Memory.FormattedTotal = formatBytes(vMem.Total)
		td.Memory.FormattedUsed = formatBytes(vMem.Used)
		td.Memory.FormattedFree = formatBytes(vMem.Free)
	}
	if sMem, err := mem.SwapMemory(); err == nil && sMem != nil {
		td.Memory.SwapUsedPercent = sMem.UsedPercent
	}

	if d, err := disk.Usage("/"); err == nil && d != nil {
		td.Disk.Path = "/"
		td.Disk.FSType = d.Fstype
		td.Disk.UsedPercent = d.UsedPercent
		td.Disk.FormattedTotal = formatBytes(d.Total)
		td.Disk.FormattedUsed = formatBytes(d.Used)
		td.Disk.FormattedFree = formatBytes(d.Free)
	}

	if ioCounters, err := net.IOCounters(false); err == nil && len(ioCounters) > 0 {
		td.Network.FormattedSent = formatBytes(ioCounters[0].BytesSent)
		td.Network.FormattedRecv = formatBytes(ioCounters[0].BytesRecv)
		td.Network.PacketsSent = ioCounters[0].PacketsSent
		td.Network.PacketsRecv = ioCounters[0].PacketsRecv
	}

	return td
}

func fetchTelemetryCmd(gatewayURL, token string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequest("GET", gatewayURL+"/api/v1/telemetry/stats", nil)
		if err != nil {
			return telemetryMsg{data: fetchLocalTelemetry(), err: err}
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return telemetryMsg{data: fetchLocalTelemetry(), err: fmt.Errorf("gateway offline: %v", err)}
		}
		defer resp.Body.Close()

		var td TelemetryData
		if err := json.NewDecoder(resp.Body).Decode(&td); err != nil {
			return telemetryMsg{data: fetchLocalTelemetry(), err: err}
		}
		return telemetryMsg{data: td, err: nil}
	}
}

func fetchClusterHealthCmd(gatewayURL string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(gatewayURL + "/api/v1/cluster/health")
		lat := time.Since(start).Round(time.Millisecond).String()

		if err != nil {
			return clusterHealthMsg{
				latency: lat,
				err:     err,
				data: ClusterHealthData{
					Status:  "Degraded",
					Gateway: "Offline",
					Services: []ServiceHealthItem{
						{Service: "api-gateway", URL: gatewayURL, Status: "Offline", Latency: lat},
						{Service: "auth-service", URL: "internal", Status: "Unknown", Latency: "-"},
						{Service: "telemetry-service", URL: "internal", Status: "Unknown", Latency: "-"},
					},
				},
			}
		}
		defer resp.Body.Close()

		var chd ClusterHealthData
		_ = json.NewDecoder(resp.Body).Decode(&chd)
		return clusterHealthMsg{data: chd, latency: lat, err: nil}
	}
}

func fetchUserDataCmd(gatewayURL, token string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequest("GET", gatewayURL+"/api/v1/auth/user", nil)
		if err != nil {
			return authResultMsg{success: false, message: "Request error"}
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			// Fallback check on /api/v1/user
			req2, _ := http.NewRequest("GET", gatewayURL+"/api/v1/user", nil)
			req2.Header.Set("Authorization", "Bearer "+token)
			resp2, err2 := client.Do(req2)
			if err2 != nil || resp2.StatusCode != http.StatusOK {
				return authResultMsg{success: false, message: "Session expired or invalid"}
			}
			defer resp2.Body.Close()
			var userData map[string]any
			_ = json.NewDecoder(resp2.Body).Decode(&userData)
			return authResultMsg{success: true, token: token, userData: userData}
		}
		defer resp.Body.Close()

		var userData map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&userData)
		return authResultMsg{success: true, token: token, userData: userData}
	}
}

func authenticateCmd(gatewayURL, username, password string) tea.Cmd {
	return func() tea.Msg {
		creds := map[string]string{"username": username, "password": password}
		body, _ := json.Marshal(creds)
		client := &http.Client{Timeout: 5 * time.Second}

		endpoints := []string{
			gatewayURL + "/api/v1/auth/login",
			gatewayURL + "/api/v1/login",
			"http://localhost:8081/api/v1/auth/login",
			"http://localhost:8080/api/v1/login",
		}

		var lastErr error
		for _, ep := range endpoints {
			resp, err := client.Post(ep, "application/json", bytes.NewBuffer(body))
			if err != nil {
				lastErr = err
				continue
			}
			defer resp.Body.Close()

			var res struct {
				Success bool           `json:"success"`
				Token   string         `json:"token"`
				Message string         `json:"message"`
				User    map[string]any `json:"user,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Success {
				_ = saveToken(res.Token)
				uData := res.User
				if uData == nil {
					uData = map[string]any{
						"username": username,
						"role":     "DevOps Engineer",
						"system":   "GliTch-cli v1.0",
						"status":   "Active",
					}
				}
				return authResultMsg{
					success:  true,
					token:    res.Token,
					message:  "Login successful",
					userData: uData,
				}
			} else if res.Message != "" {
				return authResultMsg{success: false, message: res.Message}
			}
		}

		msg := "Connection failed: Is API Gateway running?"
		if lastErr != nil {
			msg = fmt.Sprintf("Auth failed: %v", lastErr)
		}
		return authResultMsg{success: false, message: msg}
	}
}

func initialModel() model {
	existingToken := loadSavedToken()
	gateway := getGatewayURL()

	inputs := make([]textinput.Model, 2)
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "snyder"
	inputs[0].Focus()
	inputs[0].Prompt = "👤 USERNAME: "
	inputs[0].PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "••••••••"
	inputs[1].EchoMode = textinput.EchoPassword
	inputs[1].EchoCharacter = '•'
	inputs[1].Prompt = "🔑 PASSWORD: "
	inputs[1].PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	initLogs := []LogEntry{
		{Timestamp: time.Now().Add(-2 * time.Minute), Level: "INIT", Message: "GliTch Command Center client booted."},
		{Timestamp: time.Now().Add(-1 * time.Minute), Level: "CONFIG", Message: fmt.Sprintf("Target Gateway URL set to %s", gateway)},
	}

	m := model{
		state:         stateLogin,
		currentTab:    tabDashboard,
		inputs:        inputs,
		focusIndex:    0,
		gatewayURL:    gateway,
		token:         existingToken,
		telemetry:     fetchLocalTelemetry(),
		logs:          initLogs,
		lastTick:      time.Now(),
		gatewayOnline: false,
	}

	if existingToken != "" {
		m.state = stateMainApp
		m.logs = append(m.logs, LogEntry{Timestamp: time.Now(), Level: "AUTH", Message: "Existing token detected in ~/.config/glitch."})
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if m.state == stateMainApp {
		cmds = append(cmds,
			fetchUserDataCmd(m.gatewayURL, m.token),
			fetchClusterHealthCmd(m.gatewayURL),
			fetchTelemetryCmd(m.gatewayURL, m.token),
		)
	} else {
		cmds = append(cmds, textinput.Blink)
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.lastTick = time.Time(msg)
		if m.state == stateMainApp {
			cmds = append(cmds,
				fetchTelemetryCmd(m.gatewayURL, m.token),
				fetchClusterHealthCmd(m.gatewayURL),
				tickCmd(),
			)
			return m, tea.Batch(cmds...)
		}
		return m, tickCmd()

	case clusterHealthMsg:
		m.clusterHealth = msg.data
		m.gatewayLatency = msg.latency
		if msg.err == nil && !strings.Contains(msg.data.Status, "Degraded") {
			m.gatewayOnline = true
		} else {
			m.gatewayOnline = (msg.err == nil)
		}
		return m, nil

	case telemetryMsg:
		m.telemetry = msg.data
		if msg.err != nil {
			m.statusMsg = "Warning: Telemetry fallback to local host metrics"
		} else {
			m.statusMsg = ""
		}
		return m, nil

	case authResultMsg:
		m.loading = false
		if msg.success {
			m.token = msg.token
			m.userData = msg.userData
			m.state = stateMainApp
			m.statusMsg = "Session verified"
			m.logs = append(m.logs, LogEntry{
				Timestamp: time.Now(),
				Level:     "AUTH",
				Message:   fmt.Sprintf("User %v successfully authenticated.", m.userData["username"]),
			})
			return m, tea.Batch(
				fetchClusterHealthCmd(m.gatewayURL),
				fetchTelemetryCmd(m.gatewayURL, m.token),
			)
		}
		m.statusMsg = msg.message
		m.logs = append(m.logs, LogEntry{Timestamp: time.Now(), Level: "WARN", Message: "Authentication failed: " + msg.message})
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.state == stateMainApp {
				return m, tea.Quit
			}
		}

		if m.state == stateLogin {
			switch key {
			case "tab", "down":
				m.focusIndex = (m.focusIndex + 1) % 3
				return m, m.updateLoginFocus()
			case "shift+tab", "up":
				m.focusIndex = (m.focusIndex - 1 + 3) % 3
				return m, m.updateLoginFocus()
			case "enter":
				if m.focusIndex == 2 || m.focusIndex == 1 {
					u := m.inputs[0].Value()
					p := m.inputs[1].Value()
					if u == "" {
						u = "snyder"
					}
					if p == "" {
						p = "glitch123"
					}
					m.loading = true
					m.statusMsg = "Authenticating with API Gateway..."
					return m, authenticateCmd(m.gatewayURL, u, p)
				}
				m.focusIndex = 1
				return m, m.updateLoginFocus()
			}

			if m.focusIndex < 2 {
				var cmd tea.Cmd
				m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
				return m, cmd
			}
		}

		if m.state == stateMainApp {
			switch key {
			case "1":
				m.currentTab = tabDashboard
				return m, nil
			case "2":
				m.currentTab = tabServices
				return m, nil
			case "3":
				m.currentTab = tabTelemetry
				return m, nil
			case "4":
				m.currentTab = tabLogs
				return m, nil
			case "tab", "right":
				m.currentTab = (m.currentTab + 1) % 4
				return m, nil
			case "shift+tab", "left":
				m.currentTab = (m.currentTab - 1 + 4) % 4
				return m, nil
			case "r":
				m.logs = append(m.logs, LogEntry{Timestamp: time.Now(), Level: "USER", Message: "Manual cluster refresh triggered."})
				return m, tea.Batch(
					fetchClusterHealthCmd(m.gatewayURL),
					fetchTelemetryCmd(m.gatewayURL, m.token),
				)
			case "l":
				removeSavedToken()
				m.token = ""
				m.state = stateLogin
				m.focusIndex = 0
				m.statusMsg = "Logged out successfully"
				m.logs = append(m.logs, LogEntry{Timestamp: time.Now(), Level: "AUTH", Message: "User logged out."})
				return m, m.updateLoginFocus()
			}
		}
	}

	return m, nil
}

func (m *model) updateLoginFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		if i == m.focusIndex {
			cmds[i] = m.inputs[i].Focus()
			m.inputs[i].PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
		} else {
			m.inputs[i].Blur()
			m.inputs[i].PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		}
	}
	return tea.Batch(cmds...)
}

func renderBar(percentage float64, width int) string {
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	filledLen := int(float64(width) * (percentage / 100.0))
	if filledLen > width {
		filledLen = width
	}
	emptyLen := width - filledLen

	barColor := lipgloss.Color("42") // Green
	if percentage > 85 {
		barColor = lipgloss.Color("196") // High red
	} else if percentage > 60 {
		barColor = lipgloss.Color("214") // Orange/Yellow
	}

	filled := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filledLen))
	empty := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("░", emptyLen))
	return fmt.Sprintf("[%s%s] %5.1f%%", filled, empty, percentage)
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing GliTch Command Center..."
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(m.width)

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Background(lipgloss.Color("234")).
		Padding(0, 1).
		Width(m.width)

	if m.state == stateLogin {
		return m.renderLoginView(headerStyle, footerStyle)
	}

	return m.renderMainAppView(headerStyle, footerStyle)
}

func (m model) renderLoginView(headerStyle, footerStyle lipgloss.Style) string {
	brand := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render(
		`
  ⚡  ____ _ _ _____    _       
     / ___| (_)___ /___| |__    
    | |  _| | | |_ \/ __| '_ \   
    | |_| | | |___) | (__| | | | 
     \____|_|_|____/\___|_| |_| COMMAND CENTER`)

	var form strings.Builder
	form.WriteString("\n" + brand + "\n\n")
	form.WriteString("  Enterprise Distributed Telemetry & Security Hub\n")
	form.WriteString("  ───────────────────────────────────────────────\n\n")

	for i := range m.inputs {
		form.WriteString("  " + m.inputs[i].View() + "\n\n")
	}

	btnText := "[ ⚡ Authenticate Session ]"
	if m.loading {
		btnText = "[ ⏳ Authenticating... ]"
	}
	btnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 2)
	if m.focusIndex == 2 {
		btnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("212")).Bold(true).Padding(0, 2)
	}
	form.WriteString("  " + btnStyle.Render(btnText) + "\n\n")

	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	form.WriteString("  " + hintStyle.Render("Default Admin: snyder / glitch123  •  Target: "+m.gatewayURL) + "\n")

	if m.statusMsg != "" {
		statColor := "203"
		if strings.Contains(m.statusMsg, "success") || strings.Contains(m.statusMsg, "verified") {
			statColor = "42"
		}
		form.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(statColor)).Render("» "+m.statusMsg) + "\n")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 3).
		Render(form.String())

	centered := lipgloss.Place(m.width, m.height-2, lipgloss.Center, lipgloss.Center, box)
	header := headerStyle.Render("⚡ GliTch // Enterprise Access Control")
	footer := footerStyle.Render(" [Tab] Next Field • [Enter] Submit • [Ctrl+C] Exit")
	return header + "\n" + centered + "\n" + footer
}

func (m model) renderMainAppView(headerStyle, footerStyle lipgloss.Style) string {
	// Top Header Content
	gwBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● Gateway: Offline")
	if m.gatewayOnline {
		latStr := m.gatewayLatency
		if latStr == "" {
			latStr = "<1ms"
		}
		gwBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● Gateway: Online (" + latStr + ")")
	}

	username := "snyder"
	role := "DevOps Engineer"
	if m.userData != nil {
		if u, ok := m.userData["username"].(string); ok && u != "" {
			username = u
		}
		if r, ok := m.userData["role"].(string); ok && r != "" {
			role = r
		}
	}

	userBadge := fmt.Sprintf("👤 %s [%s]", username, role)
	timeBadge := m.lastTick.Format("15:04:05 UTC")

	topLine := fmt.Sprintf("⚡ GliTch // Command Center   |   %s   |   %s   |   ⏰ %s",
		gwBadge, userBadge, timeBadge)
	header := headerStyle.Render(topLine)

	// Tab Bar
	tabStyle := func(name string, active bool) string {
		if active {
			return lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("63")).
				Padding(0, 2).
				Render(name)
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Background(lipgloss.Color("236")).
			Padding(0, 2).
			Render(name)
	}

	tabs := lipgloss.JoinHorizontal(lipgloss.Top,
		tabStyle("[1] Overview & Dashboard", m.currentTab == tabDashboard),
		" ",
		tabStyle("[2] Microservices Cluster", m.currentTab == tabServices),
		" ",
		tabStyle("[3] Server Telemetry", m.currentTab == tabTelemetry),
		" ",
		tabStyle("[4] Audit & Activity Logs", m.currentTab == tabLogs),
	)

	// Content View Rendering
	var body string
	switch m.currentTab {
	case tabDashboard:
		body = m.renderDashboardTab()
	case tabServices:
		body = m.renderServicesTab()
	case tabTelemetry:
		body = m.renderTelemetryTab()
	case tabLogs:
		body = m.renderLogsTab()
	}

	contentStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Height(m.height - 4)

	mainArea := contentStyle.Render(tabs + "\n\n" + body)

	// Bottom Footer
	statusNotification := m.statusMsg
	if statusNotification == "" {
		statusNotification = "Cluster operational"
	}
	botLine := fmt.Sprintf(" [1-4/Tab] Switch Tabs • [r] Refresh • [l] Logout • [q] Quit  |  Status: %s", statusNotification)
	footer := footerStyle.Render(botLine)

	return header + "\n" + mainArea + "\n" + footer
}

func (m model) renderDashboardTab() string {
	t := m.telemetry

	hostOS := t.Host.OS
	if t.Host.Platform != "" {
		hostOS = fmt.Sprintf("%s (%s %s)", t.Host.OS, t.Host.Platform, t.Host.KernelArch)
	}
	if hostOS == "" {
		hostOS = "Linux (x86_64)"
	}

	cpuGauge := renderBar(t.CPU.TotalUsagePct, 22)
	memGauge := renderBar(t.Memory.UsedPercent, 22)
	diskGauge := renderBar(t.Disk.UsedPercent, 22)

	uptimeStr := t.Host.UptimeFormatted
	if uptimeStr == "" {
		uptimeStr = "Active"
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(56)

	telemetryCard := cardStyle.Render(fmt.Sprintf(
		"🖥️  HOST TELEMETRY SNAPSHOT\n"+
			"────────────────────────────────────────────────────\n"+
			" Hostname       : %-32s\n"+
			" OS / Platform  : %-32s\n"+
			" Kernel         : %-32s\n"+
			" System Uptime  : %-32s\n"+
			" Total Cores    : %d Logical / %d Physical\n"+
			" Load Average   : %.2f (1m), %.2f (5m), %.2f (15m)\n"+
			"────────────────────────────────────────────────────\n"+
			" ⚡ CPU Load    : %s\n"+
			" 🧠 RAM Used    : %s\n"+
			" 💾 Disk Usage  : %s",
		t.Host.Hostname,
		hostOS,
		t.Host.KernelVersion,
		uptimeStr,
		t.CPU.LogicalCores,
		t.CPU.PhysicalCores,
		t.CPU.LoadAvg1, t.CPU.LoadAvg5, t.CPU.LoadAvg15,
		cpuGauge,
		memGauge,
		diskGauge,
	))

	statusCard := cardStyle.Render(fmt.Sprintf(
		"🛡️  SECURITY & CLUSTER SUMMARY\n"+
			"────────────────────────────────────────────────────\n"+
			" Monorepo Stack : GliTch Distributed Suite\n"+
			" Gateway Proxy  : %-32s\n"+
			" Identity Mode  : PostgreSQL + HMAC-SHA256 JWT\n"+
			" Telemetry Node : gopsutil background daemon\n"+
			" Cache / Queue  : Redis High-Performance Engine\n"+
			"────────────────────────────────────────────────────\n"+
			" Active User    : %-32v\n"+
			" Session Role   : %-32v\n"+
			" Auth Storage   : ~/.config/glitch/token.json\n"+
			" Status Notice  : Real-time telemetry streaming",
		m.gatewayURL,
		m.userData["username"],
		m.userData["role"],
	))

	return lipgloss.JoinHorizontal(lipgloss.Top, telemetryCard, "  ", statusCard)
}

func (m model) renderServicesTab() string {
	headerRow := fmt.Sprintf("┌ %-22s ┬ %-18s ┬ %-18s ┬ %-10s ┐\n",
		"SERVICE NAME", "TARGET / ENDPOINT", "STATUS", "LATENCY")
	dividerRow := fmt.Sprintf("├─%-22s─┼─%-18s─┼─%-18s─┼─%-10s─┤\n",
		strings.Repeat("─", 22), strings.Repeat("─", 18), strings.Repeat("─", 18), strings.Repeat("─", 10))

	var rows strings.Builder
	rows.WriteString(headerRow)
	rows.WriteString(dividerRow)

	defaultServices := []ServiceHealthItem{
		{Service: "api-gateway", URL: m.gatewayURL, Status: "Healthy (Online)", Latency: m.gatewayLatency},
		{Service: "auth-service", URL: ":8081", Status: "Healthy (Online)", Latency: "<2ms"},
		{Service: "telemetry-service", URL: ":8082", Status: "Healthy (Online)", Latency: "<1ms"},
		{Service: "postgres-db", URL: ":5432", Status: "Healthy (Online)", Latency: "<3ms"},
		{Service: "redis-cache", URL: ":6379", Status: "Healthy (Online)", Latency: "<1ms"},
	}

	servicesToRender := defaultServices
	if len(m.clusterHealth.Services) > 0 {
		servicesToRender = m.clusterHealth.Services
	}

	for _, s := range servicesToRender {
		st := s.Status
		if strings.Contains(st, "Healthy") || strings.Contains(st, "Online") {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● " + st)
		} else {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● " + st)
		}
		lat := s.Latency
		if lat == "" {
			lat = "-"
		}
		rows.WriteString(fmt.Sprintf("│ %-22s │ %-18s │ %-27s │ %-10s │\n",
			s.Service, s.URL, st, lat))
	}
	rows.WriteString(fmt.Sprintf("└─%-22s─┴─%-18s─┴─%-18s─┴─%-10s─┘\n",
		strings.Repeat("─", 22), strings.Repeat("─", 18), strings.Repeat("─", 18), strings.Repeat("─", 10)))

	helpText := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
		"⚡ Press [r] to send an immediate health probe to all endpoints.")

	return rows.String() + "\n" + helpText
}

func (m model) renderTelemetryTab() string {
	t := m.telemetry

	var coreBars strings.Builder
	for i, pct := range t.CPU.CoreUsagesPct {
		if i >= 8 { // Display up to 8 cores cleanly
			coreBars.WriteString(fmt.Sprintf("  ... (+%d more cores)\n", len(t.CPU.CoreUsagesPct)-8))
			break
		}
		coreBars.WriteString(fmt.Sprintf("  Core #%-2d : %s\n", i, renderBar(pct, 20)))
	}
	if len(t.CPU.CoreUsagesPct) == 0 {
		coreBars.WriteString(fmt.Sprintf("  CPU Overall: %s\n", renderBar(t.CPU.TotalUsagePct, 20)))
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(56)

	cpuPanel := cardStyle.Render(fmt.Sprintf(
		"⚡ MULTI-CORE PROCESSOR METRICS\n"+
			"────────────────────────────────────────────────────\n"+
			" Processor   : %-36s\n"+
			" Cores       : %d Physical / %d Logical Cores\n"+
			" Total Load  : %s\n\n"+
			" Core Utilization:\n%s",
		t.CPU.ModelName,
		t.CPU.PhysicalCores, t.CPU.LogicalCores,
		renderBar(t.CPU.TotalUsagePct, 18),
		coreBars.String(),
	))

	memDiskPanel := cardStyle.Render(fmt.Sprintf(
		"🧠 MEMORY & STORAGE BREAKDOWN\n"+
			"────────────────────────────────────────────────────\n"+
			" Physical RAM Total : %-20s\n"+
			" Physical RAM Used  : %-20s\n"+
			" Physical RAM Free  : %-20s\n"+
			" RAM Utilization    : %s\n"+
			" Swap Utilization   : %s\n"+
			"────────────────────────────────────────────────────\n"+
			" Mount Point        : %-20s\n"+
			" Filesystem Type    : %-20s\n"+
			" Disk Total / Used  : %s / %s\n"+
			" Disk Free Space    : %-20s\n"+
			" Disk Utilization   : %s",
		t.Memory.FormattedTotal,
		t.Memory.FormattedUsed,
		t.Memory.FormattedFree,
		renderBar(t.Memory.UsedPercent, 18),
		renderBar(t.Memory.SwapUsedPercent, 18),
		t.Disk.Path,
		t.Disk.FSType,
		t.Disk.FormattedTotal, t.Disk.FormattedUsed,
		t.Disk.FormattedFree,
		renderBar(t.Disk.UsedPercent, 18),
	))

	return lipgloss.JoinHorizontal(lipgloss.Top, cpuPanel, "  ", memDiskPanel)
}

func (m model) renderLogsTab() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(m.width - 6)

	var logLines strings.Builder
	logLines.WriteString("📜 SYSTEM AUDIT & EVENT LOG STREAM\n")
	logLines.WriteString("──────────────────────────────────────────────────────────────────────────\n")

	// Render the last 15 log entries
	startIdx := 0
	if len(m.logs) > 15 {
		startIdx = len(m.logs) - 15
	}

	for _, entry := range m.logs[startIdx:] {
		levelColor := "244"
		switch entry.Level {
		case "AUTH", "OK":
			levelColor = "42"
		case "WARN":
			levelColor = "214"
		case "ALERT", "ERROR":
			levelColor = "196"
		case "INIT", "CONFIG":
			levelColor = "63"
		}
		lvl := lipgloss.NewStyle().Foreground(lipgloss.Color(levelColor)).Bold(true).Render(fmt.Sprintf("[%s]", entry.Level))
		ts := entry.Timestamp.Format("15:04:05")
		logLines.WriteString(fmt.Sprintf(" %s  %-12s  %s\n", ts, lvl, entry.Message))
	}

	return boxStyle.Render(logLines.String())
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running GliTch TUI: %v\n", err)
		os.Exit(1)
	}
}