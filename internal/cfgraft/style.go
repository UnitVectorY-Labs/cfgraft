package cfgraft

import (
	"os"

	"charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	actionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Bold(true)
	panelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2)
)

func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

func styled(style lipgloss.Style, text string) string {
	if !colorEnabled() {
		return text
	}
	return style.Render(text)
}
