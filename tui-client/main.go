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
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
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
	tabLogs
)

type model struct {
	state      viewState
	currentTab tab
	inputs     []textinput.Model
	focusIndex int
	success    bool
	token      string
	message    string
	loading    bool
	userData   map[string]any
	services   []string
	width      int
	height     int

	// Real-time Host Stats
	cpuUsage float64
	memUsage float64
	hostInfo *host.InfoStat
}

type authResultMsg struct {
	success  bool
	token    string
	message  string
	userData map[string]any
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
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
	json.Unmarshal(data, &res)
	return res["token"]
}

func saveToken(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configDir := home + "/.config/glitch"
	os.MkdirAll(configDir, 0700)
	fileData, _ := json.MarshalIndent(map[string]string{"token": token}, "", "  ")
	return os.WriteFile(configDir+"/token.json", fileData, 0600)
}

func fetchUserData(token string) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequest("GET", "http://localhost:8080/api/v1/user", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return authResultMsg{success: false, message: "Failed to fetch dashboard data"}
		}
		defer resp.Body.Close()

		var userData map[string]any
		json.NewDecoder(resp.Body).Decode(&userData)

		return authResultMsg{
			success:  true,
			token:    token,
			userData: userData,
		}
	}
}

func fetchSystemStats() (float64, float64, *host.InfoStat) {
	var cpuVal float64
	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		cpuVal = percents[0]
	}

	var memVal float64
	if vMem, err := mem.VirtualMemory(); err == nil {
		memVal = vMem.UsedPercent
	}

	hInfo, _ := host.Info()
	return cpuVal, memVal, hInfo
}

func authenticate(username, password string) tea.Cmd {
	return func() tea.Msg {
		creds := map[string]string{"username": username, "password": password}
		body, _ := json.Marshal(creds)

		apiURL := os.Getenv("AUTH_URL")
		if apiURL == "" {
			apiURL = "http://localhost:8080/api/v1/login"
		}

		resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(body))
		if err != nil {
			return authResultMsg{success: false, message: "Connection failed: Is auth-service running?"}
		}
		defer resp.Body.Close()

		var res struct {
			Success bool   `json:"success"`
			Token   string `json:"token"`
			Message string `json:"message"`
		}
		json.NewDecoder(resp.Body).Decode(&res)

		if res.Success {
			saveToken(res.Token)
			return fetchUserData(res.Token)()
		}

		return authResultMsg{success: false, message: res.Message}
	}
}

func initialModel() model {
	existingToken := loadSavedToken()

	inputs := make([]textinput.Model, 2)
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "Username"
	inputs[0].Focus()
	inputs[0].Prompt = "👤 Username: "

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "Password"
	inputs[1].EchoMode = textinput.EchoPassword
	inputs[1].EchoCharacter = '•'
	inputs[1].Prompt = "🔑 Password: "

	cpuV, memV, hInfo := fetchSystemStats()

	m := model{
		inputs:     inputs,
		focusIndex: 0,
		token:      existingToken,
		services:   []string{"auth-service [Online]", "database-worker [Online]", "gateway-proxy [Active]"},
		cpuUsage:   cpuV,
		memUsage:   memV,
		hostInfo:   hInfo,
	}

	if existingToken != "" {
		m.state = stateMainApp
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if m.state == stateMainApp {
		cmds = append(cmds, fetchUserData(m.token))
	} else {
		cmds = append(cmds, textinput.Blink)
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		cpuV, memV, hInfo := fetchSystemStats()
		m.cpuUsage = cpuV
		m.memUsage = memV
		m.hostInfo = hInfo
		return m, tickCmd()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "tab":
			if m.state == stateMainApp {
				m.currentTab = (m.currentTab + 1) % 3
				return m, nil
			}
			m.focusIndex = (m.focusIndex + 1) % (len(m.inputs) + 1)
			return m, m.updateFocus()

		case "shift+tab":
			if m.state == stateMainApp {
				m.currentTab = (m.currentTab - 2 + 3) % 3
				return m, nil
			}
			m.focusIndex = (m.focusIndex - 1 + len(m.inputs) + 1) % (len(m.inputs) + 1)
			return m, m.updateFocus()

		case "enter":
			if m.state == stateLogin && m.focusIndex == len(m.inputs) {
				m.loading = true
				m.message = "Authenticating..."
				return m, authenticate(m.inputs[0].Value(), m.inputs[1].Value())
			}
		}

	case authResultMsg:
		m.loading = false
		if msg.success {
			m.success = true
			m.token = msg.token
			m.userData = msg.userData
			m.state = stateMainApp
		} else {
			m.message = msg.message
		}
		return m, nil
	}

	if m.state == stateLogin && m.focusIndex < len(m.inputs) {
		var cmd tea.Cmd
		m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *model) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := 0; i < len(m.inputs); i++ {
		if i == m.focusIndex {
			cmds[i] = m.inputs[i].Focus()
			m.inputs[i].PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
		} else {
			m.inputs[i].Blur()
			m.inputs[i].PromptStyle = lipgloss.NewStyle()
		}
	}
	return tea.Batch(cmds...)
}

func renderProgressBar(percentage float64, width int) string {
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
	if percentage > 75 {
		barColor = lipgloss.Color("203") // Red warning
	} else if percentage > 50 {
		barColor = lipgloss.Color("220") // Yellow warning
	}

	filledStyle := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filledLen))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("░", emptyLen))

	return fmt.Sprintf("[%s%s] %5.1f%%", filledStyle, emptyStyle, percentage)
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing full-screen view..."
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Width(m.width)

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(m.width)

	contentStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Height(m.height - 4)

	if m.state == stateLogin {
		var form string
		for i := range m.inputs {
			form += m.inputs[i].View() + "\n\n"
		}

		btnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		if m.focusIndex == len(m.inputs) {
			btnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Underline(true)
		}
		submitBtn := btnStyle.Render("[ Submit Login ]")
		msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(m.message)

		loginBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(2, 6).
			Render(fmt.Sprintf("⚡ GliTch-cli Authentication\n\n%s\n%s\n\n%s", form, submitBtn, msgStyle))

		centeredLogin := lipgloss.Place(m.width, m.height-2, lipgloss.Center, lipgloss.Center, loginBox)
		return headerStyle.Render("⚡ GliTch-cli // Secure Gateway") + "\n" + centeredLogin + "\n" + footerStyle.Render(" Use [Tab] to navigate • [Enter] to submit • [q] to quit")
	}

	activeTab := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Underline(true).Render
	inactiveTab := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render

	t1, t2, t3 := inactiveTab("[1] Dashboard"), inactiveTab("[2] Services"), inactiveTab("[3] Logs")
	switch m.currentTab {
	case tabDashboard:
		t1 = activeTab("[1] Dashboard")
	case tabServices:
		t2 = activeTab("[2] Services")
	case tabLogs:
		t3 = activeTab("[3] Logs")
	}
	tabsBar := fmt.Sprintf(" %s   %s   %s\n", t1, t2, t3)

	var bodyContent string
	switch m.currentTab {
	case tabDashboard:
		osName := "Linux"
		platform := "Unknown"
		if m.hostInfo != nil {
			osName = m.hostInfo.OS
			platform = m.hostInfo.Platform
		}

		cpuBar := renderProgressBar(m.cpuUsage, 20)
		memBar := renderProgressBar(m.memUsage, 20)

		bodyContent = fmt.Sprintf(
			"┌─ Host Server Telemetry (Real-time) ────────┐\n"+
				"│ 🖥️ OS / Platform : %-22s │\n"+
				"│ ⚡ CPU Usage     : %s │\n"+
				"│ 🧠 Memory Usage  : %s │\n"+
				"├────────────────────────────────────────────┤\n"+
				"│ 👤 User Session  : %-22s │\n"+
				"│ 💼 Role          : %-22s │\n"+
				"│ 🟢 Cluster Status: Online                  │\n"+
				"└────────────────────────────────────────────┘",
			fmt.Sprintf("%s (%s)", osName, platform),
			cpuBar,
			memBar,
			m.userData["username"],
			m.userData["role"],
		)
	case tabServices:
		bodyContent = "┌─ Microservices Cluster ────────────────────┐\n"
		for _, s := range m.services {
			bodyContent += fmt.Sprintf("│ • %-40s │\n", s)
		}
		bodyContent += "└────────────────────────────────────────────┘"
	case tabLogs:
		bodyContent = "┌─ System Audit Logs ────────────────────────┐\n" +
			"│ [INFO] Live telemetry streaming active     │\n" +
			"│ [OK]   Token verified & session active     │\n" +
			"│ [INFO] Connected to docker network bridge  │\n" +
			"└────────────────────────────────────────────┘"
	}

	header := headerStyle.Render("⚡ GliTch-cli // Command Center")
	mainPanel := contentStyle.Render(tabsBar + "\n" + bodyContent)
	footer := footerStyle.Render(" [Tab] Switch Tabs • [q] Quit CLI")

	return header + "\n" + mainPanel + "\n" + footer
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}