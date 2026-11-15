package cfgraft

import (
	"os"

	"charm.land/lipgloss/v2"
)

var (
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	actionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
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
