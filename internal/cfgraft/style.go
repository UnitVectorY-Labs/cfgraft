package cfgraft

import (
	"os"

	"charm.land/lipgloss/v2"
)

var (
	titleStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	subtleStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	successStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warningStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	actionStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	selectedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("33")).Bold(true)
	emptyStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true).Padding(1, 2)
	buttonStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	selectedButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("33")).
				Bold(true)
	shortcutStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	selectedShortcutStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Background(lipgloss.Color("33")).Bold(true)
	normalRowStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	selectedRowStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("33")).Bold(true)
	focusedBoxStyle       = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("33")).Padding(0, 1)
	blurredBoxStyle       = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	outputBoxStyle        = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("33")).Padding(0, 1)
	modalStyle            = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("33")).Padding(1, 2)
	panelStyle            = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2)
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
