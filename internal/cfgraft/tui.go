package cfgraft

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const cfgraftLogo = `  ____  __                  __ _
 / ___|/ _| __ _ _ __ __ _ / _| |_
| |   | |_ / _` + "`" + ` | '__/ _` + "`" + ` | |_| __|
| |___|  _| (_| | | | (_| |  _| |_
 \____|_|  \__, |_|  \__,_|_|  \__|
           |___/`

const actionArea = "action"
const listArea = "list"

const progressTickInterval = 120 * time.Millisecond

type tuiModel struct {
	paths              Paths
	config             Config
	err                error
	msg                string
	screen             tuiScreen
	cursor             int
	selectedSource     string
	selectedMap        int
	formKind           tuiFormKind
	pendingFormKind    tuiFormKind
	formTitle          string
	formFields         []tuiField
	formCursor         int
	outputTitle        string
	outputText         string
	outputReturnScreen tuiScreen
	hoverArea          string
	hoverIndex         int
	activeArea         string
	actionCursor       int
	spinner            spinner.Model
	help               help.Model
	sourceList         list.Model
	mappingList        list.Model
	sourceTable        table.Model
	progress           progress.Model
	progressValue      float64
	busy               bool
	busyTitle          string
	outputViewport     viewport.Model
	width              int
	height             int
	pendingParents     []string
	formEditing        bool
	suggestionCursor   int
	sourceSuggestions  []string
	targetSuggestions  []string
	listStart          int
	actionStart        int
}

type tuiCommandDoneMsg struct {
	title string
	text  string
	err   error
}

type tuiProgressTickMsg time.Time

type tuiAction struct {
	Label    string
	Shortcut string
}

type tuiHelpKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

func (k tuiHelpKeyMap) ShortHelp() []key.Binding {
	return k.short
}

func (k tuiHelpKeyMap) FullHelp() [][]key.Binding {
	return k.full
}

func runTUI() error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	model := tuiModel{paths: paths, config: cfg, err: err, screen: screenSources, hoverIndex: -1, activeArea: listArea}
	model.spinner = spinner.New()
	model.help = help.New()
	model.initComponents()
	model.outputViewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	model.resizeViewport()
	model.syncComponents()
	_, err = tea.NewProgram(model).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func progressTick() tea.Cmd {
	return tea.Tick(progressTickInterval, func(t time.Time) tea.Msg {
		return tuiProgressTickMsg(t)
	})
}

func newTUIList(title string) list.Model {
	items := []list.Item{}
	l := list.New(items, tuiListDelegate{}, 80, 12)
	l.Title = title
	l.SetShowTitle(false)
	l.SetFilteringEnabled(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	return l
}

func newSourceTable(width int) table.Model {
	cols := []table.Column{
		{Title: "Field", Width: 14},
		{Title: "Value", Width: max(24, width-20)},
	}
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Foreground(lipgloss.Color("245")).Bold(true)
	styles.Cell = styles.Cell.Foreground(lipgloss.Color("252"))
	styles.Selected = styles.Cell
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(nil),
		table.WithHeight(6),
		table.WithWidth(width),
		table.WithFocused(false),
		table.WithStyles(styles),
	)
	return t
}

func (m *tuiModel) initComponents() {
	if m.sourceList.Title == "" {
		m.sourceList = newTUIList("Sources")
	}
	if m.mappingList.Title == "" {
		m.mappingList = newTUIList("Mappings")
	}
	if len(m.sourceTable.Columns()) == 0 {
		m.sourceTable = newSourceTable(m.contentWidth())
	}
	if m.progress.PercentFormat == "" {
		m.progress = progress.New(progress.WithWidth(42), progress.WithoutPercentage(), progress.WithColors(lipgloss.Color("42")))
	}
	if m.outputViewport.Width() == 0 {
		m.outputViewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	}
	if m.activeArea == "" {
		m.activeArea = listArea
	}
}

func (m *tuiModel) syncComponents() {
	m.initComponents()
	width := m.contentWidth()
	listHeight := max(5, m.height-16)
	if m.screen == screenSource {
		listHeight = max(4, m.height-22)
	}
	m.sourceList.SetSize(width, listHeight)
	m.sourceList.SetItems(m.sourceItems())
	m.sourceList.Select(m.cursor)
	m.mappingList.SetSize(width, listHeight)
	m.mappingList.SetItems(m.mappingItems())
	m.mappingList.Select(m.cursor)
	m.sourceTable.SetColumns([]table.Column{
		{Title: "Field", Width: 14},
		{Title: "Value", Width: max(24, width-20)},
	})
	m.sourceTable.SetWidth(width)
	m.sourceTable.SetRows(m.sourceRows())
	m.progress.SetWidth(min(48, max(20, width-8)))
}

func (m tuiModel) contentWidth() int {
	if m.width > 4 {
		return m.width - 4
	}
	return 80
}

func (m tuiModel) sourceItems() []list.Item {
	ids := m.sourceIDs()
	items := make([]list.Item, 0, len(ids))
	for _, id := range ids {
		items = append(items, tuiListItem{title: id})
	}
	return items
}

func (m tuiModel) mappingItems() []list.Item {
	if !m.hasSelectedSource() {
		return nil
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	items := make([]list.Item, 0, len(mappings))
	for _, mapping := range mappings {
		items = append(items, tuiListItem{title: fmt.Sprintf("%s -> %s", mapping.Source, mapping.Target)})
	}
	return items
}

func (m tuiModel) sourceRows() []table.Row {
	if !m.hasSelectedSource() {
		return nil
	}
	src := m.config.Sources[m.selectedSource]
	return []table.Row{
		{"Name", m.selectedSource},
		{"Repo", src.Repo},
		{"Ref", strings.TrimSpace(src.Ref.Type + " " + src.Ref.Name)},
		{"Mappings", fmt.Sprintf("%d", len(src.Mappings))},
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		m.resizeViewport()
		m.syncComponents()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.busy {
			return m, cmd
		}
		return m, nil
	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd
	case tuiProgressTickMsg:
		if !m.busy {
			return m, nil
		}
		m.progressValue += 0.08
		if m.progressValue > 0.92 {
			m.progressValue = 0.18
		}
		return m, tea.Batch(m.progress.SetPercent(m.progressValue), progressTick())
	case tuiCommandDoneMsg:
		m.busy = false
		m.progressValue = 1
		m.showCommandOutput(msg.title, msg.text, msg.err)
		m.syncComponents()
		return m, nil
	case tea.KeyPressMsg:
		if m.busy && msg.String() != "ctrl+c" && msg.String() != "q" {
			return m, nil
		}
		return m.updateKey(msg)
	case tea.PasteMsg:
		if m.screen == screenForm {
			return m.updateFormInput(msg)
		}
	case tea.MouseClickMsg:
		if m.busy {
			return m, nil
		}
		return m.updateMouseClick(msg)
	case tea.MouseMotionMsg:
		if m.busy {
			return m, nil
		}
		return m.updateMouseMotion(msg), nil
	case tea.MouseWheelMsg:
		if m.screen == screenOutput {
			var cmd tea.Cmd
			m.outputViewport, cmd = m.outputViewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m tuiModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if key == "?" {
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	}
	switch m.screen {
	case screenSources:
		return m.updateSourcesKey(key)
	case screenSource:
		return m.updateSourceKey(key)
	case screenMappings:
		return m.updateMappingsKey(key)
	case screenForm:
		return m.updateFormKey(msg)
	case screenConfirm:
		return m.updateConfirmKey(key)
	case screenOutput:
		if key == "q" || key == "esc" || key == "enter" || key == "b" {
			m.leaveOutput()
			return m, nil
		}
		var cmd tea.Cmd
		m.outputViewport, cmd = m.outputViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m tuiModel) updateSourcesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		return m, m.activateFocusedRegion()
	case "a":
		m.startAddSource()
	case "s":
		return m, m.runAllSync()
	case "d":
		return m, m.runAllDiff()
	case "r":
		m.reloadConfig()
	default:
		m.updateActionListKey(key, len(m.sourceIDs()))
	}
	m.syncComponents()
	return m, nil
}

func (m tuiModel) updateSourceKey(key string) (tea.Model, tea.Cmd) {
	if !m.hasSelectedSource() {
		m.screen = screenSources
		m.cursor = 0
		return m, nil
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenSources
		m.cursor = 0
		m.activeArea = listArea
		m.actionCursor = 0
	case "enter":
		return m, m.activateFocusedRegion()
	case "e":
		m.startEditSource()
	case "r":
		m.startRemoveSource()
	case "a":
		m.startAddMapping()
	case "s":
		return m, m.runSelectedSync()
	case "d":
		return m, m.runSelectedDiff()
	default:
		m.updateActionListKey(key, len(m.config.Sources[m.selectedSource].Mappings))
	}
	m.syncComponents()
	return m, nil
}

func (m tuiModel) updateMappingsKey(key string) (tea.Model, tea.Cmd) {
	if !m.hasSelectedSource() {
		m.screen = screenSources
		m.cursor = 0
		return m, nil
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	count := len(mappings) + 2
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenSource
		m.cursor = 0
		m.activeArea = listArea
		m.actionCursor = 0
	case "up", "shift+tab":
		m.moveCursor(-1, count)
	case "down", "tab":
		m.moveCursor(1, count)
	case "enter":
		m.activateMappingsRow()
	case "a":
		m.startAddMapping()
	case "e":
		if m.cursor < len(mappings) {
			m.selectedMap = m.cursor
			m.startEditMapping()
		}
	case "r":
		if m.cursor < len(mappings) {
			m.selectedMap = m.cursor
			m.startRemoveMapping()
		}
	}
	m.syncComponents()
	return m, nil
}

func (m tuiModel) updateFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.formEditing {
		switch key {
		case "enter", "esc":
			m.formEditing = false
			m.focusFormField(m.formCursor)
			return m, nil
		case "up":
			if m.moveActiveSuggestion(-1) {
				return m, nil
			}
		case "down":
			if m.moveActiveSuggestion(1) {
				return m, nil
			}
		case "tab":
			if m.acceptActiveSuggestion() {
				return m, nil
			}
		}
		return m.updateFormInput(msg)
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "b":
		m.cancelForm()
	case "up", "shift+tab":
		m.moveFormCursor(-1)
	case "down", "tab":
		m.moveFormCursor(1)
	case "enter":
		m.formEditing = true
		m.suggestionCursor = 0
		m.focusFormField(m.formCursor)
	case "a":
		if m.formKind == formAddMapping {
			return m, m.submitForm()
		}
	case "s":
		if m.formKind == formEditSource || m.formKind == formEditMapping || m.formKind == formAddSource {
			return m, m.submitForm()
		}
	case "r":
		switch m.formKind {
		case formEditSource:
			m.startRemoveSource()
		case formEditMapping:
			m.startRemoveMapping()
		}
	}
	m.syncComponents()
	return m, nil
}

func (m tuiModel) updateConfirmKey(key string) (tea.Model, tea.Cmd) {
	if m.formKind == confirmRemoveSrcMaps || m.formKind == confirmRemoveMapFiles {
		switch key {
		case "d", "enter":
			if m.formKind == confirmRemoveMapFiles {
				return m, m.finishRemoveMapping(true)
			}
			return m, m.finishRemoveSource(true)
		case "k":
			if m.formKind == confirmRemoveMapFiles {
				return m, m.finishRemoveMapping(false)
			}
			return m, m.finishRemoveSource(false)
		case "esc", "b":
			m.cancelForm()
		case "q":
			return m, tea.Quit
		default:
			m.updateActionListKey(key, 0)
		}
		m.syncComponents()
		return m, nil
	}
	switch key {
	case "c", "enter":
		return m, m.submitConfirm()
	case "esc", "b":
		m.cancelForm()
	case "q":
		return m, tea.Quit
	default:
		m.updateActionListKey(key, 0)
	}
	m.syncComponents()
	return m, nil
}

func (m *tuiModel) updateActionListKey(key string, listCount int) {
	switch key {
	case "tab":
		if m.activeArea == actionArea && listCount > 0 {
			m.activeArea = listArea
			m.clampListCursor(listCount)
		} else {
			m.activeArea = actionArea
			m.clampActionCursor()
		}
	case "shift+tab":
		if m.activeArea == listArea {
			m.activeArea = actionArea
			m.clampActionCursor()
		} else if listCount > 0 {
			m.activeArea = listArea
			m.clampListCursor(listCount)
		}
	case "left":
		if m.activeArea == actionArea {
			m.moveActionCursor(-1)
		}
	case "right":
		if m.activeArea == actionArea {
			m.moveActionCursor(1)
		}
	case "up":
		if m.activeArea == listArea {
			m.moveCursor(-1, listCount)
		} else if listCount > 0 {
			m.activeArea = listArea
			m.cursor = listCount - 1
		}
	case "down":
		if m.activeArea == actionArea {
			if listCount > 0 {
				m.activeArea = listArea
				m.clampListCursor(listCount)
			}
		} else {
			m.moveCursor(1, listCount)
		}
	}
}

func (m tuiModel) updateMouseClick(msg tea.MouseClickMsg) (tuiModel, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if idx, ok := m.actionAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = actionArea, idx
		m.activeArea, m.actionCursor = actionArea, idx
		return m, m.activateAction(idx)
	}
	if idx, ok := m.listAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = listArea, idx
		m.activeArea = listArea
		m.cursor = idx
		switch m.screen {
		case screenSources:
			m.activateSourcesRow()
		case screenSource:
			m.activateSourceRow()
		case screenMappings:
			m.activateMappingsRow()
		case screenForm:
			m.focusFormField(idx)
		}
		m.syncComponents()
	}
	return m, nil
}

func (m tuiModel) updateMouseMotion(msg tea.MouseMotionMsg) tuiModel {
	m.hoverArea, m.hoverIndex = "", -1
	if idx, ok := m.actionAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = actionArea, idx
		return m
	}
	if idx, ok := m.listAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = listArea, idx
		if m.screen != screenForm {
			m.cursor = idx
			m.syncComponents()
		}
		return m
	}
	return m
}

func (m *tuiModel) activateSourcesRow() {
	ids := m.sourceIDs()
	if m.cursor >= 0 && m.cursor < len(ids) {
		m.selectedSource = ids[m.cursor]
		m.cursor = 0
		m.screen = screenSource
		m.activeArea = listArea
		m.actionCursor = 0
	}
}

func (m *tuiModel) activateFocusedRegion() tea.Cmd {
	if m.activeArea == actionArea {
		return m.activateAction(m.actionCursor)
	}
	switch m.screen {
	case screenSources:
		m.activateSourcesRow()
	case screenSource:
		m.activateSourceRow()
	case screenMappings:
		m.activateMappingsRow()
	}
	return nil
}

func (m *tuiModel) activateAction(idx int) tea.Cmd {
	switch m.screen {
	case screenSources:
		switch idx {
		case 0:
			m.startAddSource()
		case 1:
			return m.runAllSync()
		case 2:
			return m.runAllDiff()
		case 3:
			return tea.Quit
		}
	case screenSource:
		switch idx {
		case 0:
			m.startEditSource()
		case 1:
			m.startRemoveSource()
		case 2:
			m.startAddMapping()
		case 3:
			return m.runSelectedSync()
		case 4:
			return m.runSelectedDiff()
		case 5:
			m.screen = screenSources
			m.cursor = 0
			m.activeArea = listArea
			m.actionCursor = 0
		}
	case screenForm:
		actions := m.actionItems()
		if idx < 0 || idx >= len(actions) {
			return nil
		}
		switch strings.ToLower(actions[idx].Shortcut) {
		case "a", "s":
			return m.submitForm()
		case "r":
			if m.formKind == formEditSource {
				m.startRemoveSource()
			} else if m.formKind == formEditMapping {
				m.startRemoveMapping()
			}
		case "b":
			m.cancelForm()
		}
	case screenConfirm:
		actions := m.actionItems()
		if idx < 0 || idx >= len(actions) {
			return nil
		}
		switch strings.ToLower(actions[idx].Shortcut) {
		case "d", "c":
			if m.formKind == confirmRemoveMapFiles {
				return m.finishRemoveMapping(true)
			}
			if m.formKind == confirmRemoveSrcMaps {
				return m.finishRemoveSource(true)
			}
			return m.submitConfirm()
		case "k":
			if m.formKind == confirmRemoveMapFiles {
				return m.finishRemoveMapping(false)
			}
			if m.formKind == confirmRemoveSrcMaps {
				return m.finishRemoveSource(false)
			}
			m.cancelForm()
		case "b":
			m.cancelForm()
		}
	case screenOutput:
		if idx == 0 {
			m.leaveOutput()
		}
	}
	return nil
}

func (m *tuiModel) leaveOutput() {
	next := m.outputReturnScreen
	if next == "" || next == screenOutput {
		next = screenSources
	}
	if next == screenSource && !m.hasSelectedSource() {
		next = screenSources
	}
	m.screen = next
	m.cursor = 0
	m.activeArea = listArea
	m.actionCursor = 0
	m.hoverArea = ""
	m.hoverIndex = -1
}

func (m *tuiModel) moveActionCursor(delta int) {
	count := len(m.actionItems())
	if count <= 0 {
		m.actionCursor = 0
		return
	}
	m.actionCursor += delta
	if m.actionCursor < 0 {
		m.actionCursor = count - 1
	}
	if m.actionCursor >= count {
		m.actionCursor = 0
	}
}

func (m *tuiModel) clampActionCursor() {
	count := len(m.actionItems())
	if count <= 0 {
		m.actionCursor = 0
		return
	}
	if m.actionCursor < 0 {
		m.actionCursor = 0
	}
	if m.actionCursor >= count {
		m.actionCursor = count - 1
	}
}

func (m *tuiModel) clampListCursor(count int) {
	if count <= 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= count {
		m.cursor = count - 1
	}
}

func (m *tuiModel) activateSourceRow() {
	if !m.hasSelectedSource() {
		return
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	if m.cursor >= 0 && m.cursor < len(mappings) {
		m.selectedMap = m.cursor
		m.startEditMapping()
	}
}

func (m tuiModel) actionAt(x, y int) (int, bool) {
	if y != m.actionStartRow() {
		return -1, false
	}
	start := 2
	for i, action := range m.actionItems() {
		width := lipgloss.Width(m.buttonView(action, false))
		if x >= start && x < start+width {
			return i, true
		}
		start += width + 3
	}
	return -1, false
}

func (m tuiModel) listAt(x, y int) (int, bool) {
	if x < 0 {
		return -1, false
	}
	if m.screen == screenForm {
		return m.formFieldAt(y)
	}
	if y < m.listStartRow() {
		return -1, false
	}
	idx := y - m.listStartRow()
	switch m.screen {
	case screenSources:
		if idx >= 0 && idx < len(m.sourceIDs()) {
			return idx, true
		}
	case screenSource:
		if m.hasSelectedSource() && idx >= 0 && idx < len(m.config.Sources[m.selectedSource].Mappings) {
			return idx, true
		}
	case screenMappings:
		if m.hasSelectedSource() && idx >= 0 && idx < len(m.config.Sources[m.selectedSource].Mappings)+2 {
			return idx, true
		}
	}
	return -1, false
}

func (m *tuiModel) activateMappingsRow() {
	if !m.hasSelectedSource() {
		return
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	switch {
	case m.cursor < len(mappings):
		m.selectedMap = m.cursor
		m.startEditMapping()
	case m.cursor == len(mappings):
		m.startAddMapping()
	case m.cursor == len(mappings)+1:
		m.screen = screenSource
		m.cursor = 0
	}
}

func (m tuiModel) breadcrumbs() string {
	if m.screen == screenSources {
		return ""
	}
	parts := []string{"Sources"}
	if m.selectedSource != "" && m.screen != screenSources {
		parts = append(parts, m.selectedSource)
	}
	switch m.screen {
	case screenMappings:
		parts = append(parts, "Mappings")
	case screenForm:
		parts = append(parts, m.formTitle)
	case screenConfirm:
		parts = append(parts, "Confirm")
	case screenOutput:
		parts = append(parts, m.outputTitle)
	}
	return strings.Join(parts, " / ")
}

func (m tuiModel) View() tea.View {
	m.syncComponents()
	if m.busy {
		return m.busyView()
	}
	var b strings.Builder
	fmt.Fprintln(&b, m.headerView())
	if m.err != nil {
		fmt.Fprintf(&b, "%s %v\n", styled(errorStyle, "error:"), m.err)
	} else {
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b)
	m.listStart = lipgloss.Height(b.String())
	switch m.screen {
	case screenSources:
		m.viewSources(&b)
	case screenSource:
		m.viewSourceMenu(&b)
	case screenMappings:
		m.viewMappings(&b)
	case screenForm:
		m.viewForm(&b)
	case screenConfirm:
		m.viewConfirm(&b)
	case screenOutput:
		m.viewOutput(&b)
	}
	m.actionStart = lipgloss.Height(b.String())
	if actions := m.actionItems(); len(actions) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, m.buttonBar(actions))
	}
	if helpView := m.help.View(m.helpKeyMap()); helpView != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, helpView)
	}
	view := tea.NewView(b.String())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	return view
}

func (m tuiModel) headerView() string {
	logo := titleStyle.Render(cfgraftLogo)
	breadcrumbs := m.breadcrumbs()
	if breadcrumbs == "" {
		return logo
	}
	crumbs := subtleStyle.Render(breadcrumbs)
	return lipgloss.JoinHorizontal(lipgloss.Top, logo, "  ", crumbs)
}

func (m tuiModel) busyView() tea.View {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	header := m.headerView()
	availableHeight := max(6, height-lipgloss.Height(header)-1)
	modal := m.busyModal()
	body := lipgloss.Place(width, availableHeight, lipgloss.Center, lipgloss.Center, modal)
	view := tea.NewView(header + "\n" + body)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	return view
}

func (m tuiModel) viewSources(b *strings.Builder) {
	if len(m.sourceIDs()) == 0 {
		fmt.Fprintln(b, styled(emptyStyle, "No Sources"))
		return
	}
	fmt.Fprintln(b, m.sourceList.View())
}

func (m tuiModel) viewSourceMenu(b *strings.Builder) {
	if !m.hasSelectedSource() {
		fmt.Fprintln(b, "Selected source no longer exists.")
		return
	}
	src := m.config.Sources[m.selectedSource]
	fmt.Fprintln(b, m.sourceTable.View())
	fmt.Fprintln(b)
	fmt.Fprintln(b, styled(titleStyle, "Mappings"))
	if len(src.Mappings) == 0 {
		fmt.Fprintln(b)
		fmt.Fprintln(b, styled(emptyStyle, "No Mappings"))
		return
	}
	fmt.Fprintln(b, m.mappingList.View())
}

func (m tuiModel) viewMappings(b *strings.Builder) {
	if !m.hasSelectedSource() {
		fmt.Fprintln(b, "Selected source no longer exists.")
		return
	}
	fmt.Fprintf(b, "%s %s\n", styled(titleStyle, "Mappings for"), m.selectedSource)
	fmt.Fprintln(b)
	mappings := m.config.Sources[m.selectedSource].Mappings
	for i, mapping := range mappings {
		m.writeRow(b, i, "%s -> %s", mapping.Source, mapping.Target)
	}
	m.writeRow(b, len(mappings), "+ Add mapping")
	m.writeRow(b, len(mappings)+1, "Back")
}

func (m tuiModel) viewForm(b *strings.Builder) {
	fmt.Fprintln(b, styled(titleStyle, m.formTitle))
	fmt.Fprintln(b, m.formModeHint())
	fmt.Fprintln(b)
	if m.formKind == formAddMapping || m.formKind == formEditMapping {
		fmt.Fprintln(b, m.mappingStatusTable())
		fmt.Fprintln(b)
	}
	for i, field := range m.formFields {
		fmt.Fprintln(b, m.formFieldView(i, field))
		if m.formKind == formAddMapping || m.formKind == formEditMapping {
			switch i {
			case 0:
				if i == m.formCursor {
					m.writeSuggestions(b, field)
				}
			case 1:
				if missing := missingParentDirs(field.Input.Value()); len(missing) > 0 {
					fmt.Fprintf(b, "  %s %s\n", styled(warningStyle, "[!]"), "missing parent folders will require confirmation")
				}
				if i == m.formCursor {
					m.writeSuggestions(b, field)
				}
			}
		}
		fmt.Fprintln(b)
	}
}

func (m tuiModel) formModeHint() string {
	if m.formEditing {
		return styled(successStyle, "Editing field: up/down choose suggestions, tab completes, enter returns to field navigation")
	}
	return styled(subtleStyle, "Field navigation: up/down choose fields, enter edits, tab moves to buttons")
}

func (m tuiModel) formFieldView(idx int, field tuiField) string {
	label := styled(subtleStyle, field.Label)
	value := field.Input.View()
	if !field.Input.Focused() && strings.TrimSpace(field.Input.Value()) == "" {
		value = styled(subtleStyle, "-")
	}
	content := fmt.Sprintf("%s\n%s", label, value)
	width := min(76, max(32, m.contentWidth()-4))
	if idx == m.formCursor {
		return focusedBoxStyle.Width(width).Render(content)
	}
	return blurredBoxStyle.Width(width).Render(content)
}

func (m tuiModel) viewConfirm(b *strings.Builder) {
	fmt.Fprintln(b, styled(warningStyle, m.formTitle))
	fmt.Fprintln(b)
	for _, field := range m.formFields {
		fmt.Fprintf(b, "%s: %s\n", field.Label, field.Input.Value())
	}
	fmt.Fprintln(b)
}

func (m tuiModel) viewOutput(b *strings.Builder) {
	fmt.Fprintln(b, styled(titleStyle, m.outputTitle))
	fmt.Fprintln(b)
	width := min(m.contentWidth(), max(30, m.outputViewport.Width()+4))
	if strings.TrimSpace(m.outputText) == "" {
		fmt.Fprintln(b, outputBoxStyle.Width(width).Render(styled(subtleStyle, "No output.")))
		return
	}
	fmt.Fprintln(b, outputBoxStyle.Width(width).Render(m.outputViewport.View()))
}

func (m tuiModel) buttonBar(actions []tuiAction) string {
	var parts []string
	for i, action := range actions {
		selected := (m.hoverArea == actionArea && m.hoverIndex == i) || (m.activeArea == actionArea && m.actionCursor == i)
		parts = append(parts, m.buttonView(action, selected))
	}
	return "  " + strings.Join(parts, "   ")
}

func (m tuiModel) buttonView(action tuiAction, selected bool) string {
	label := action.Label
	idx := strings.Index(strings.ToLower(label), strings.ToLower(action.Shortcut))
	if idx < 0 {
		if selected {
			return selectedButtonStyle.Render("[" + label + "]")
		}
		return buttonStyle.Render("[" + label + "]")
	}
	before := "[" + label[:idx]
	shortcut := label[idx : idx+1]
	after := label[idx+1:] + "]"
	if selected {
		return selectedButtonStyle.Render(before) + selectedShortcutStyle.Render(shortcut) + selectedButtonStyle.Render(after)
	}
	return buttonStyle.Render(before) + shortcutStyle.Render(shortcut) + buttonStyle.Render(after)
}

func (m tuiModel) actionItems() []tuiAction {
	switch m.screen {
	case screenSources:
		return []tuiAction{{"Add", "A"}, {"Sync", "S"}, {"Diff", "D"}, {"Quit", "Q"}}
	case screenSource:
		return []tuiAction{{"Edit", "E"}, {"Remove", "R"}, {"Add Mapping", "A"}, {"Sync", "S"}, {"Diff", "D"}, {"Back", "B"}}
	case screenForm:
		switch m.formKind {
		case formAddMapping:
			return []tuiAction{{"Add", "A"}, {"Back", "B"}}
		case formEditMapping:
			return []tuiAction{{"Save", "S"}, {"Remove", "R"}, {"Back", "B"}}
		case formAddSource:
			return []tuiAction{{"Save", "S"}, {"Back", "B"}}
		case formEditSource:
			return []tuiAction{{"Save", "S"}, {"Remove", "R"}, {"Back", "B"}}
		}
	case screenConfirm:
		if m.formKind == confirmRemoveSrcMaps || m.formKind == confirmRemoveMapFiles {
			return []tuiAction{{"Delete", "D"}, {"Keep", "K"}, {"Back", "B"}}
		}
		return []tuiAction{{"Confirm", "C"}, {"Back", "B"}}
	case screenOutput:
		return []tuiAction{{"Back", "B"}}
	}
	return nil
}

func (m tuiModel) writeSuggestions(b *strings.Builder, field tuiField) {
	if !field.Input.Focused() {
		return
	}
	suggestions := m.activePathSuggestions()
	if len(suggestions) == 0 {
		fmt.Fprintln(b, styled(subtleStyle, "    No suggestions"))
		return
	}
	limit := min(6, len(suggestions))
	for i := 0; i < limit; i++ {
		line := "    " + suggestions[i]
		if i == m.suggestionCursor {
			line = styled(selectedStyle, line)
		} else {
			line = styled(subtleStyle, line)
		}
		fmt.Fprintln(b, line)
	}
}

func (m tuiModel) activePathSuggestions() []string {
	if !(m.formKind == formAddMapping || m.formKind == formEditMapping) || m.formCursor < 0 || m.formCursor >= len(m.formFields) {
		return nil
	}
	value := strings.TrimSpace(m.formFields[m.formCursor].Input.Value())
	var candidates []string
	switch m.formCursor {
	case 0:
		candidates = m.sourceSuggestions
	case 1:
		if value == "" {
			return nil
		}
		candidates = m.targetSuggestions
	default:
		return nil
	}
	if value == "" {
		return limitSuggestions(candidates, 8)
	}
	filtered := make([]string, 0, len(candidates))
	for _, suggestion := range candidates {
		if strings.HasPrefix(strings.ToLower(suggestion), strings.ToLower(value)) {
			filtered = append(filtered, suggestion)
		}
	}
	return limitSuggestions(filtered, 8)
}

func limitSuggestions(suggestions []string, limit int) []string {
	if len(suggestions) <= limit {
		return suggestions
	}
	return suggestions[:limit]
}

func (m *tuiModel) moveActiveSuggestion(delta int) bool {
	suggestions := m.activePathSuggestions()
	if len(suggestions) == 0 {
		return false
	}
	m.suggestionCursor += delta
	if m.suggestionCursor < 0 {
		m.suggestionCursor = len(suggestions) - 1
	}
	if m.suggestionCursor >= len(suggestions) {
		m.suggestionCursor = 0
	}
	return true
}

func (m *tuiModel) acceptActiveSuggestion() bool {
	suggestions := m.activePathSuggestions()
	if len(suggestions) == 0 {
		return false
	}
	if m.suggestionCursor < 0 || m.suggestionCursor >= len(suggestions) {
		m.suggestionCursor = 0
	}
	m.formFields[m.formCursor].Input.SetValue(suggestions[m.suggestionCursor])
	m.formFields[m.formCursor].Input.CursorEnd()
	m.refreshMappingSuggestions()
	return true
}

func (m tuiModel) mappingStatusTable() string {
	status := "-"
	style := errorStyle
	if m.validateMappingSourcePath(strings.TrimSpace(m.formFields[0].Input.Value())) == nil {
		status = "✓"
		style = successStyle
	}
	t := table.New(
		table.WithColumns([]table.Column{{Title: "Check", Width: 18}, {Title: "Valid", Width: 8}}),
		table.WithRows([]table.Row{{"Source path exists", styled(style, status)}}),
		table.WithHeight(3),
		table.WithWidth(m.contentWidth()),
		table.WithFocused(false),
	)
	return t.View()
}

func (m tuiModel) busyModal() string {
	width := min(64, max(34, m.contentWidth()-8))
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		styled(titleStyle, m.busyTitle),
		"",
		m.spinner.View()+" "+m.progress.View(),
		styled(subtleStyle, "Running command..."),
	)
	return modalStyle.Width(width).Render(body)
}

func (m tuiModel) helpKeyMap() tuiHelpKeyMap {
	global := []key.Binding{
		key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
	navigation := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("up/down", "move")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "buttons")),
		key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "content")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}
	switch m.screen {
	case screenSources:
		actions := []key.Binding{
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync")),
			key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
		return tuiHelpKeyMap{
			short: []key.Binding{navigation[0], navigation[3], actions[0], actions[1], actions[2], actions[4]},
			full:  [][]key.Binding{navigation, actions, global},
		}
	case screenSource:
		actions := []key.Binding{
			key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit source")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "remove")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add mapping")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync source")),
			key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff source")),
			key.NewBinding(key.WithKeys("b", "esc"), key.WithHelp("b/esc", "back")),
		}
		return tuiHelpKeyMap{
			short: []key.Binding{navigation[0], navigation[3], actions[2], actions[3], actions[4], global[0]},
			full:  [][]key.Binding{navigation, actions, global},
		}
	case screenMappings:
		actions := []key.Binding{
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add mapping")),
			key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit mapping")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "remove mapping")),
			key.NewBinding(key.WithKeys("b", "esc"), key.WithHelp("b/esc", "back")),
		}
		return tuiHelpKeyMap{
			short: []key.Binding{navigation[0], navigation[3], actions[0], actions[1], actions[2], global[0]},
			full:  [][]key.Binding{navigation, actions, global},
		}
	case screenForm:
		form := []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit/done")),
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("up/down", "field/suggest")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
			key.NewBinding(key.WithKeys("s", "a"), key.WithHelp("s/a", "save/add")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "remove")),
			key.NewBinding(key.WithKeys("b", "esc"), key.WithHelp("b/esc", "back")),
		}
		return tuiHelpKeyMap{
			short: []key.Binding{form[0], form[1], form[2], form[3], form[5], global[0]},
			full:  [][]key.Binding{form, global},
		}
	case screenConfirm:
		if m.formKind == confirmRemoveSrcMaps || m.formKind == confirmRemoveMapFiles {
			confirm := []key.Binding{
				key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "delete files")),
				key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "keep files")),
				key.NewBinding(key.WithKeys("esc", "b"), key.WithHelp("esc/b", "cancel")),
			}
			return tuiHelpKeyMap{
				short: []key.Binding{confirm[0], confirm[1], confirm[2], global[0]},
				full:  [][]key.Binding{confirm, global},
			}
		}
		confirm := []key.Binding{
			key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm")),
			key.NewBinding(key.WithKeys("n", "esc", "b"), key.WithHelp("n/esc/b", "cancel")),
		}
		return tuiHelpKeyMap{
			short: []key.Binding{confirm[0], confirm[1], global[0]},
			full:  [][]key.Binding{confirm, global},
		}
	case screenOutput:
		output := []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("up/down", "scroll")),
			key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "page")),
			key.NewBinding(key.WithKeys("enter", "esc", "b"), key.WithHelp("enter/esc/b", "back")),
		}
		return tuiHelpKeyMap{
			short: []key.Binding{output[0], output[1], output[2], global[0]},
			full:  [][]key.Binding{output, global},
		}
	default:
		return tuiHelpKeyMap{short: global, full: [][]key.Binding{global}}
	}
}

func (m *tuiModel) startAddSource() {
	m.formKind = formAddSource
	m.formTitle = "Add source"
	m.formFields = []tuiField{
		newTUIField("Git URL", ""),
		newTUIField("Ref type", "branch"),
		newTUIField("Ref name", "main"),
	}
	m.formCursor = 0
	m.focusFormField(0)
	m.formEditing = false
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startEditSource() {
	if !m.hasSelectedSource() {
		return
	}
	src := m.config.Sources[m.selectedSource]
	m.formKind = formEditSource
	m.formTitle = "Edit source"
	m.formFields = []tuiField{
		newTUIField("Git URL", src.Repo),
		newTUIField("Ref type", src.Ref.Type),
		newTUIField("Ref name", src.Ref.Name),
	}
	m.formCursor = 0
	m.focusFormField(0)
	m.formEditing = false
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startAddMapping() {
	m.formKind = formAddMapping
	m.formTitle = "Add mapping"
	m.formFields = []tuiField{
		newTUIField("Source path", ""),
		newTUIField("Target path", ""),
	}
	m.formCursor = 0
	m.focusFormField(0)
	m.formEditing = false
	m.refreshMappingSuggestions()
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startEditMapping() {
	if !m.hasSelectedSource() {
		return
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	if m.selectedMap < 0 || m.selectedMap >= len(mappings) {
		return
	}
	mapping := mappings[m.selectedMap]
	m.formKind = formEditMapping
	m.formTitle = "Edit mapping"
	m.formFields = []tuiField{
		newTUIField("Source path", mapping.Source),
		newTUIField("Target path", mapping.Target),
	}
	m.formCursor = 0
	m.focusFormField(0)
	m.formEditing = false
	m.refreshMappingSuggestions()
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startRemoveSource() {
	if !m.hasSelectedSource() {
		return
	}
	src := m.config.Sources[m.selectedSource]
	m.formKind = confirmRemoveSrc
	m.formTitle = "Remove source from config?"
	m.formFields = []tuiField{
		newTUIField("ID", m.selectedSource),
		newTUIField("Repo", src.Repo),
		newTUIField("Mappings", fmt.Sprintf("%d", len(src.Mappings))),
	}
	m.screen = screenConfirm
	m.formEditing = false
	m.activeArea = actionArea
	m.actionCursor = 0
}

func (m *tuiModel) startRemoveMapping() {
	if !m.hasSelectedSource() {
		return
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	if m.selectedMap < 0 || m.selectedMap >= len(mappings) {
		return
	}
	mapping := mappings[m.selectedMap]
	m.formKind = confirmRemoveMap
	m.formTitle = "Remove mapping from config?"
	m.formFields = []tuiField{
		newTUIField("Source path", mapping.Source),
		newTUIField("Target path", mapping.Target),
	}
	m.screen = screenConfirm
	m.formEditing = false
	m.activeArea = actionArea
	m.actionCursor = 0
}

func (m *tuiModel) submitForm() tea.Cmd {
	switch m.formKind {
	case formAddSource, formEditSource:
		return m.submitSourceForm()
	case formAddMapping, formEditMapping:
		return m.submitMappingForm(false)
	}
	return nil
}

func (m *tuiModel) submitSourceForm() tea.Cmd {
	repo := strings.TrimSpace(m.formFields[0].Input.Value())
	refType := strings.TrimSpace(m.formFields[1].Input.Value())
	refName := strings.TrimSpace(m.formFields[2].Input.Value())
	if repo == "" || refType == "" || refName == "" {
		m.err = errors.New("Git URL, ref type, and ref name are required")
		return nil
	}
	next := cloneConfig(m.config)
	id := deriveUniqueSourceID(repo, next, "")
	if m.formKind == formAddSource {
		next.Sources[id] = Source{Repo: repo, Ref: Ref{Type: refType, Name: refName}}
	} else {
		id = deriveUniqueSourceID(repo, next, m.selectedSource)
		src := next.Sources[m.selectedSource]
		src.Repo = repo
		src.Ref = Ref{Type: refType, Name: refName}
		if id != m.selectedSource {
			if _, exists := next.Sources[id]; exists {
				m.err = fmt.Errorf("source %q already exists", id)
				return nil
			}
			delete(next.Sources, m.selectedSource)
		}
		next.Sources[id] = src
	}
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return nil
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return nil
	}
	m.config = next
	m.selectedSource = id
	m.err = nil
	m.msg = "saved source; checking out repository"
	m.outputReturnScreen = screenSources
	cmd := m.startBackground("Repository checkout", func(out *bytes.Buffer) error {
		return refreshRepos(m.paths, filterConfig(m.config, id), true, out)
	})
	m.screen = screenSources
	m.cursor = 0
	return cmd
}

func (m *tuiModel) submitMappingForm(confirmedParents bool) tea.Cmd {
	if !m.hasSelectedSource() {
		m.err = errors.New("selected source no longer exists")
		return nil
	}
	sourcePath := strings.TrimSpace(m.formFields[0].Input.Value())
	targetPath := strings.TrimSpace(m.formFields[1].Input.Value())
	if err := m.validateMappingSourcePath(sourcePath); err != nil {
		m.err = err
		return nil
	}
	if missing := missingParentDirs(targetPath); len(missing) > 0 && !confirmedParents {
		m.pendingParents = missing
		m.pendingFormKind = m.formKind
		m.formTitle = "Create missing parent folders?"
		m.formKind = confirmCreateParents
		m.screen = screenConfirm
		m.activeArea = actionArea
		m.actionCursor = 0
		return nil
	}
	next := cloneConfig(m.config)
	src := next.Sources[m.selectedSource]
	mapping := Mapping{Source: sourcePath, Target: targetPath}
	if m.formKind == formAddMapping {
		src.Mappings = append(src.Mappings, mapping)
	} else {
		if m.selectedMap < 0 || m.selectedMap >= len(src.Mappings) {
			m.err = errors.New("selected mapping no longer exists")
			return nil
		}
		src.Mappings[m.selectedMap] = mapping
	}
	next.Sources[m.selectedSource] = src
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return nil
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return nil
	}
	m.config = next
	m.err = nil
	m.msg = "saved mapping"
	m.screen = screenSource
	m.cursor = 0
	m.activeArea = listArea
	return nil
}

func (m *tuiModel) submitConfirm() tea.Cmd {
	switch m.formKind {
	case confirmCreateParents:
		m.formKind = m.pendingFormKind
		return m.submitMappingForm(true)
	case confirmRemoveSrc:
		return m.promptRemoveSourceMappings()
	case confirmRemoveMap:
		return m.promptRemoveMappingFiles()
	}
	return nil
}

func (m *tuiModel) promptRemoveSourceMappings() tea.Cmd {
	entries, err := m.selectedSourceStateFiles()
	if err != nil {
		m.err = err
		return nil
	}
	if len(entries) == 0 {
		return m.finishRemoveSource(false)
	}
	m.formKind = confirmRemoveSrcMaps
	m.formTitle = "Delete mapped files from disk?"
	m.formFields = []tuiField{
		newTUIField("Source", m.selectedSource),
		newTUIField("Tracked files", fmt.Sprintf("%d", len(entries))),
		newTUIField("Yes", "delete unchanged mapped files"),
		newTUIField("No", "keep mapped files"),
	}
	m.screen = screenConfirm
	m.formEditing = false
	m.err = nil
	m.activeArea = actionArea
	m.actionCursor = 0
	return nil
}

func (m *tuiModel) finishRemoveSource(deleteMapped bool) tea.Cmd {
	if !m.hasSelectedSource() {
		m.err = errors.New("selected source no longer exists")
		return nil
	}
	sourceID := m.selectedSource
	src := m.config.Sources[sourceID]
	state, err := loadState(m.paths)
	if err != nil {
		m.err = err
		return nil
	}
	sourceFiles, nextState := removeSourceFromState(state, sourceID)
	if deleteMapped {
		if err := preflightMappedFileRemoval(sourceFiles); err != nil {
			m.err = err
			return nil
		}
	}
	next := cloneConfig(m.config)
	delete(next.Sources, sourceID)
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return nil
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return nil
	}
	if err := writeState(m.paths, nextState); err != nil {
		m.err = err
		return nil
	}
	if deleteMapped {
		if err := deleteMappedFiles(sourceFiles); err != nil {
			m.err = err
			return nil
		}
	}
	if err := removeSourceRepoCache(m.paths, sourceID, src); err != nil {
		m.err = err
		return nil
	}
	m.config = next
	m.selectedSource = ""
	m.err = nil
	m.msg = ""
	m.screen = screenSources
	m.cursor = 0
	m.activeArea = listArea
	return nil
}

func (m *tuiModel) promptRemoveMappingFiles() tea.Cmd {
	files, _, err := m.selectedMappingStateFiles()
	if err != nil {
		m.err = err
		return nil
	}
	if len(files) == 0 {
		return m.finishRemoveMapping(false)
	}
	mapping := m.config.Sources[m.selectedSource].Mappings[m.selectedMap]
	m.formKind = confirmRemoveMapFiles
	m.formTitle = "Delete managed files from disk?"
	m.formFields = []tuiField{
		newTUIField("Source path", mapping.Source),
		newTUIField("Target path", mapping.Target),
		newTUIField("Tracked files", fmt.Sprintf("%d", len(files))),
		newTUIField("Yes", "delete unchanged managed files"),
		newTUIField("No", "keep managed files"),
	}
	m.screen = screenConfirm
	m.formEditing = false
	m.err = nil
	m.activeArea = actionArea
	m.actionCursor = 0
	return nil
}

func (m *tuiModel) finishRemoveMapping(deleteMapped bool) tea.Cmd {
	if !m.hasSelectedSource() {
		m.err = errors.New("selected source no longer exists")
		return nil
	}
	next := cloneConfig(m.config)
	src := next.Sources[m.selectedSource]
	if m.selectedMap < 0 || m.selectedMap >= len(src.Mappings) {
		m.err = errors.New("selected mapping no longer exists")
		return nil
	}
	sourceFiles, nextState, err := m.selectedMappingStateFiles()
	if err != nil {
		m.err = err
		return nil
	}
	if deleteMapped {
		if err := preflightMappedFileRemoval(sourceFiles); err != nil {
			m.err = err
			return nil
		}
	}
	src.Mappings = append(src.Mappings[:m.selectedMap], src.Mappings[m.selectedMap+1:]...)
	next.Sources[m.selectedSource] = src
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return nil
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return nil
	}
	if err := writeState(m.paths, nextState); err != nil {
		m.err = err
		return nil
	}
	if deleteMapped {
		if err := deleteMappedFiles(sourceFiles); err != nil {
			m.err = err
			return nil
		}
	}
	m.config = next
	m.err = nil
	m.msg = ""
	m.screen = screenSource
	m.cursor = 0
	m.activeArea = listArea
	return nil
}

func (m tuiModel) selectedSourceStateFiles() ([]StateFile, error) {
	state, err := loadState(m.paths)
	if err != nil {
		return nil, err
	}
	files, _ := removeSourceFromState(state, m.selectedSource)
	return files, nil
}

func (m tuiModel) selectedMappingStateFiles() ([]StateFile, State, error) {
	if !m.hasSelectedSource() {
		return nil, State{}, errors.New("selected source no longer exists")
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	if m.selectedMap < 0 || m.selectedMap >= len(mappings) {
		return nil, State{}, errors.New("selected mapping no longer exists")
	}
	state, err := loadState(m.paths)
	if err != nil {
		return nil, State{}, err
	}
	files, next := removeMappingFromState(state, m.selectedSource, mappings[m.selectedMap])
	return files, next, nil
}

func removeSourceFromState(state State, sourceID string) ([]StateFile, State) {
	sourceFiles := make([]StateFile, 0)
	next := State{Files: make([]StateFile, 0, len(state.Files))}
	for _, file := range state.Files {
		if file.SourceID == sourceID {
			sourceFiles = append(sourceFiles, file)
			continue
		}
		next.Files = append(next.Files, file)
	}
	return sourceFiles, next
}

func removeMappingFromState(state State, sourceID string, mapping Mapping) ([]StateFile, State) {
	sourceRoot := filepath.Clean(mapping.Source)
	targetRoot := filepath.Clean(mapping.Target)
	mappingFiles := make([]StateFile, 0)
	next := State{Files: make([]StateFile, 0, len(state.Files))}
	for _, file := range state.Files {
		if file.SourceID == sourceID && pathEqualOrNested(file.Source, sourceRoot) && pathEqualOrNested(file.Target, targetRoot) {
			mappingFiles = append(mappingFiles, file)
			continue
		}
		next.Files = append(next.Files, file)
	}
	return mappingFiles, next
}

func preflightMappedFileRemoval(files []StateFile) error {
	for _, file := range files {
		hash, exists, err := existingFileHash(file.Target)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if hash != file.Hash {
			return fmt.Errorf("mapped file %q changed; refusing to delete mapped files", file.Target)
		}
	}
	return nil
}

func deleteMappedFiles(files []StateFile) error {
	for _, file := range files {
		if err := os.Remove(file.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func removeSourceRepoCache(paths Paths, sourceID string, src Source) error {
	cache, err := repoCachePath(paths, sourceID, src)
	if err != nil {
		return err
	}
	if filepath.Clean(cache) == filepath.Clean(paths.Repos) || !isWithin(paths.Repos, cache) {
		return fmt.Errorf("refusing to remove repository cache outside repos directory: %s", cache)
	}
	if err := os.RemoveAll(cache); err != nil {
		return err
	}
	return nil
}

func (m *tuiModel) cancelForm() {
	m.err = nil
	m.formEditing = false
	if m.formKind == confirmCreateParents {
		m.formKind = m.pendingFormKind
		m.formTitle = "Mapping"
		m.screen = screenForm
		return
	}
	switch m.formKind {
	case formAddSource:
		m.screen = screenSources
	case formEditSource, confirmRemoveSrc, confirmRemoveSrcMaps:
		m.screen = screenSource
	case formAddMapping, formEditMapping, confirmRemoveMap, confirmRemoveMapFiles:
		m.screen = screenSource
	default:
		m.screen = screenSources
	}
	m.cursor = 0
}

func (m *tuiModel) runAllSync() tea.Cmd {
	m.outputReturnScreen = screenSources
	return m.startBackground("Sync all sources", func(out *bytes.Buffer) error {
		return syncCommand(SyncOptions{Refresh: true}, out)
	})
}

func (m *tuiModel) runSelectedSync() tea.Cmd {
	sourceID := m.selectedSource
	m.outputReturnScreen = screenSource
	return m.startBackground("Sync "+sourceID, func(out *bytes.Buffer) error {
		return syncSourceCommand(sourceID, SyncOptions{Refresh: true}, out)
	})
}

func (m *tuiModel) runAllDiff() tea.Cmd {
	m.outputReturnScreen = screenSources
	return m.startBackground("Diff all sources", func(out *bytes.Buffer) error {
		return diffCommand(false, out)
	})
}

func (m *tuiModel) runSelectedDiff() tea.Cmd {
	sourceID := m.selectedSource
	m.outputReturnScreen = screenSource
	return m.startBackground("Diff "+sourceID, func(out *bytes.Buffer) error {
		return diffSourceCommand(sourceID, false, out)
	})
}

func (m *tuiModel) showCommandOutput(title, text string, err error) {
	m.outputTitle = title
	m.outputText = strings.TrimSpace(text)
	m.outputViewport.SetContent(m.outputText)
	m.outputViewport.GotoTop()
	m.err = err
	m.msg = ""
	m.screen = screenOutput
	m.cursor = 0
	m.activeArea = actionArea
	m.actionCursor = 0
	m.hoverArea = ""
	m.hoverIndex = -1
}

func (m *tuiModel) reloadConfig() {
	cfg, err := loadConfig(m.paths)
	m.config = cfg
	m.err = err
	if err == nil {
		m.msg = "reloaded config"
	}
	m.cursor = 0
}

func (m *tuiModel) moveCursor(delta, count int) {
	if count <= 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = count - 1
	}
	if m.cursor >= count {
		m.cursor = 0
	}
}

func (m *tuiModel) moveFormCursor(delta int) {
	if len(m.formFields) == 0 {
		return
	}
	next := m.formCursor + delta
	if next < 0 {
		next = len(m.formFields) - 1
	}
	if next >= len(m.formFields) {
		next = 0
	}
	m.focusFormField(next)
}

func (m *tuiModel) focusFormField(idx int) {
	if idx < 0 || idx >= len(m.formFields) {
		return
	}
	for i := range m.formFields {
		if i == idx {
			if m.formEditing {
				m.formFields[i].Input.Focus()
			} else {
				m.formFields[i].Input.Blur()
			}
		} else {
			m.formFields[i].Input.Blur()
		}
	}
	m.formCursor = idx
	m.suggestionCursor = 0
}

func newTUIField(label, value string) tuiField {
	input := textinput.New()
	input.SetValue(value)
	input.SetWidth(56)
	input.Prompt = ""
	return tuiField{Label: label, Input: input}
}

func (m tuiModel) updateFormInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if len(m.formFields) == 0 || m.formCursor < 0 || m.formCursor >= len(m.formFields) {
		return m, nil
	}
	var cmd tea.Cmd
	m.formFields[m.formCursor].Input, cmd = m.formFields[m.formCursor].Input.Update(msg)
	if m.formKind == formAddMapping || m.formKind == formEditMapping {
		m.refreshMappingSuggestions()
		if m.suggestionCursor >= len(m.activePathSuggestions()) {
			m.suggestionCursor = 0
		}
	}
	return m, cmd
}

func (m *tuiModel) refreshMappingSuggestions() {
	if len(m.formFields) < 2 || !m.hasSelectedSource() {
		return
	}
	sourceSuggestions := m.sourcePathSuggestions(m.formFields[0].Input.Value())
	m.sourceSuggestions = sourceSuggestions
	m.formFields[0].Input.ShowSuggestions = false
	m.formFields[0].Input.SetSuggestions(sourceSuggestions)
	targetSuggestions := targetPathSuggestions(m.formFields[1].Input.Value())
	m.targetSuggestions = targetSuggestions
	m.formFields[1].Input.ShowSuggestions = false
	m.formFields[1].Input.SetSuggestions(targetSuggestions)
}

func (m tuiModel) validateMappingSourcePath(sourcePath string) error {
	if sourcePath == "" {
		return errors.New("source path is required")
	}
	if !m.hasSelectedSource() {
		return errors.New("selected source no longer exists")
	}
	clean := filepath.Clean(sourcePath)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source path %q escapes repository root", sourcePath)
	}
	src := m.config.Sources[m.selectedSource]
	cache, err := repoCachePath(m.paths, m.selectedSource, src)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(cache, clean)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("source path %q does not exist in repository cache", sourcePath)
		}
		return err
	}
	return nil
}

func (m tuiModel) sourcePathSuggestions(value string) []string {
	if !m.hasSelectedSource() {
		return nil
	}
	src := m.config.Sources[m.selectedSource]
	cache, err := repoCachePath(m.paths, m.selectedSource, src)
	if err != nil {
		return nil
	}
	dir, prefix, ok := sourceSuggestionScope(value)
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(cache, filepath.FromSlash(dir)))
	if err != nil {
		return nil
	}
	suggestions := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		suggestion := name
		if dir != "" {
			suggestion = path.Join(dir, name)
		}
		if entry.IsDir() {
			suggestion += "/"
		}
		suggestions = append(suggestions, suggestion)
	}
	sort.Strings(suggestions)
	return limitSuggestions(suggestions, 50)
}

func sourceSuggestionScope(value string) (string, string, bool) {
	value = strings.TrimSpace(filepath.ToSlash(value))
	if value == "" {
		return "", "", true
	}
	if strings.HasPrefix(value, "/") {
		return "", "", false
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", false
	}
	if strings.HasSuffix(value, "/") {
		dir := strings.TrimSuffix(value, "/")
		if dir == "." {
			dir = ""
		}
		return dir, "", true
	}
	dir := path.Dir(value)
	if dir == "." {
		dir = ""
	}
	return dir, path.Base(value), true
}

func targetPathSuggestions(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	dir := value
	if !strings.HasSuffix(value, string(filepath.Separator)) {
		dir = filepath.Dir(value)
	}
	if dir == "." || dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	suggestions := make([]string, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			path += string(filepath.Separator)
		}
		suggestions = append(suggestions, path)
	}
	sort.Strings(suggestions)
	if len(suggestions) > 250 {
		return suggestions[:250]
	}
	return suggestions
}

func missingParentDirs(targetPath string) []string {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" || !filepath.IsAbs(targetPath) {
		return nil
	}
	parent := filepath.Dir(filepath.Clean(targetPath))
	if parent == "." || parent == string(filepath.Separator) {
		return nil
	}
	if _, err := os.Stat(parent); err == nil {
		return nil
	}
	var missing []string
	for {
		if parent == "." || parent == string(filepath.Separator) {
			break
		}
		_, err := os.Stat(parent)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil
		}
		missing = append(missing, parent)
		parent = filepath.Dir(parent)
	}
	for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
		missing[i], missing[j] = missing[j], missing[i]
	}
	return missing
}

func (m *tuiModel) startBackground(title string, fn func(*bytes.Buffer) error) tea.Cmd {
	m.busy = true
	m.busyTitle = title
	m.progressValue = 0.12
	m.err = nil
	m.msg = ""
	m.outputTitle = title
	m.outputText = ""
	return tea.Batch(m.spinner.Tick, m.progress.SetPercent(m.progressValue), progressTick(), func() tea.Msg {
		var out bytes.Buffer
		err := fn(&out)
		return tuiCommandDoneMsg{title: title, text: out.String(), err: err}
	})
}

func (m *tuiModel) resizeViewport() {
	baseWidth := m.width
	if baseWidth <= 0 {
		baseWidth = 86
	}
	width := baseWidth - 6
	if width < 20 {
		width = 20
	}
	baseHeight := m.height
	if baseHeight <= 0 {
		baseHeight = 24
	}
	height := baseHeight - 18
	if height < 5 {
		height = 5
	}
	if height > 12 {
		height = 12
	}
	m.outputViewport.SetWidth(width)
	m.outputViewport.SetHeight(height)
}

func (m tuiModel) writeRow(b *strings.Builder, idx int, format string, args ...any) {
	prefix := "  "
	if idx == m.cursor {
		prefix = "> "
	}
	line := prefix + fmt.Sprintf(format, args...)
	if (m.activeArea == "list" && idx == m.cursor) || (m.hoverArea == "list" && m.hoverIndex == idx) {
		line = styled(selectedStyle, line)
	}
	fmt.Fprintln(b, line)
}

func (m tuiModel) hasSelectedSource() bool {
	_, ok := m.config.Sources[m.selectedSource]
	return ok
}

func (m tuiModel) sourceIDs() []string {
	ids := make([]string, 0, len(m.config.Sources))
	for id := range m.config.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m tuiModel) listStartRow() int {
	switch m.screen {
	case screenSources:
		return m.contentStartRow()
	case screenSource:
		return m.contentStartRow() + lipgloss.Height(m.sourceTable.View()) + 2
	case screenMappings:
		return 14
	case screenForm:
		rows := m.formFieldStartRows()
		if len(rows) > 0 {
			return rows[0]
		}
		return m.contentStartRow()
	default:
		return 0
	}
}

func (m tuiModel) actionStartRow() int {
	contentHeight := m.screenContentHeight()
	switch m.screen {
	case screenSources, screenSource, screenForm, screenOutput:
		return m.contentStartRow() + contentHeight
	case screenConfirm:
		return m.contentStartRow() + contentHeight
	default:
		return 0
	}
}

func (m tuiModel) contentStartRow() int {
	return lipgloss.Height(m.headerView()) + 2
}

func (m tuiModel) screenContentHeight() int {
	var b strings.Builder
	switch m.screen {
	case screenSources:
		m.viewSources(&b)
	case screenSource:
		m.viewSourceMenu(&b)
	case screenForm:
		m.viewForm(&b)
	case screenOutput:
		m.viewOutput(&b)
	case screenConfirm:
		m.viewConfirm(&b)
	}
	return lipgloss.Height(b.String())
}

func (m tuiModel) formFieldAt(y int) (int, bool) {
	starts := m.formFieldStartRows()
	for i, start := range starts {
		if y >= start && y < start+4 {
			return i, true
		}
	}
	return -1, false
}

func (m tuiModel) formFieldStartRows() []int {
	if len(m.formFields) == 0 {
		return nil
	}
	row := m.contentStartRow() + 3
	if m.formKind == formAddMapping || m.formKind == formEditMapping {
		row += lipgloss.Height(m.mappingStatusTable()) + 1
	}
	starts := make([]int, 0, len(m.formFields))
	for i, field := range m.formFields {
		starts = append(starts, row)
		row += lipgloss.Height(m.formFieldView(i, field))
		if m.formKind == formAddMapping || m.formKind == formEditMapping {
			if i == m.formCursor && field.Input.Focused() {
				row += max(1, min(6, len(m.activePathSuggestions())))
			}
			if i == 1 && len(missingParentDirs(field.Input.Value())) > 0 {
				row++
			}
		}
		row++
	}
	return starts
}
