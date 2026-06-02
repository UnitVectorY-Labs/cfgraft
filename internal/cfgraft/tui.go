package cfgraft

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const cfgraftLogo = `  ____  __                  __ _
 / ___|/ _| __ _ _ __ __ _ / _| |_
| |   | |_ / _` + "`" + ` | '__/ _` + "`" + ` | |_| __|
| |___|  _| (_| | | | (_| |  _| |_
 \____|_|  \__, |_|  \__,_|_|  \__|
           |___/`

const actionBarRow = 9

type tuiModel struct {
	paths          Paths
	config         Config
	err            error
	msg            string
	screen         tuiScreen
	cursor         int
	selectedSource string
	selectedMap    int
	formKind       tuiFormKind
	formTitle      string
	formFields     []tuiField
	formCursor     int
	outputTitle    string
	outputText     string
	hoverArea      string
	hoverIndex     int
	activeArea     string
	actionCursor   int
}

func runTUI() error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	model := tuiModel{paths: paths, config: cfg, err: err, screen: screenSources, hoverIndex: -1, activeArea: "action"}
	_, err = tea.NewProgram(model).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case tea.MouseClickMsg:
		return m.updateMouseClick(msg), nil
	case tea.MouseMotionMsg:
		return m.updateMouseMotion(msg), nil
	}
	return m, nil
}

func (m tuiModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
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
			m.screen = screenSources
			m.cursor = 0
			m.activeArea = "action"
			m.actionCursor = 0
		}
	}
	return m, nil
}

func (m tuiModel) updateSourcesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		m.activateFocusedRegion()
	case "a":
		m.startAddSource()
	case "s":
		m.runAllSync()
	case "d":
		m.runAllDiff()
	case "r":
		m.reloadConfig()
	default:
		m.updateActionListKey(key, len(m.sourceIDs()))
	}
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
		m.activeArea = "action"
		m.actionCursor = 0
	case "enter":
		m.activateFocusedRegion()
	case "m":
		m.startAddMapping()
	case "e":
		m.startEditSource()
	case "s":
		m.runSelectedSync()
	case "d":
		m.runSelectedDiff()
	case "x":
		m.startRemoveSource()
	case "a":
		m.startAddMapping()
	default:
		m.updateActionListKey(key, len(m.config.Sources[m.selectedSource].Mappings))
	}
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
		m.activeArea = "action"
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
	case "x":
		if m.cursor < len(mappings) {
			m.selectedMap = m.cursor
			m.startRemoveMapping()
		}
	}
	return m, nil
}

func (m tuiModel) updateFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.cancelForm()
		return m, nil
	case "up", "shift+tab":
		m.moveFormCursor(-1)
		return m, nil
	case "down", "tab":
		m.moveFormCursor(1)
		return m, nil
	case "enter":
		m.moveFormCursor(1)
		return m, nil
	case "ctrl+s":
		m.submitForm()
		return m, nil
	}
	var cmd tea.Cmd
	m.formFields[m.formCursor].Input, cmd = m.formFields[m.formCursor].Input.Update(msg)
	return m, cmd
}

func (m tuiModel) updateConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		m.submitConfirm()
	case "n", "N", "esc", "b":
		m.cancelForm()
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m *tuiModel) updateActionListKey(key string, listCount int) {
	switch key {
	case "tab":
		if m.activeArea == "action" && listCount > 0 {
			m.activeArea = "list"
			m.clampListCursor(listCount)
		} else {
			m.activeArea = "action"
			m.clampActionCursor()
		}
	case "shift+tab":
		if m.activeArea == "list" {
			m.activeArea = "action"
			m.clampActionCursor()
		} else if listCount > 0 {
			m.activeArea = "list"
			m.clampListCursor(listCount)
		}
	case "left":
		if m.activeArea == "action" {
			m.moveActionCursor(-1)
		}
	case "right":
		if m.activeArea == "action" {
			m.moveActionCursor(1)
		}
	case "up":
		if m.activeArea == "list" {
			m.moveCursor(-1, listCount)
		} else if listCount > 0 {
			m.activeArea = "list"
			m.cursor = listCount - 1
		}
	case "down":
		if m.activeArea == "action" {
			if listCount > 0 {
				m.activeArea = "list"
				m.clampListCursor(listCount)
			}
		} else {
			m.moveCursor(1, listCount)
		}
	}
}

func (m tuiModel) updateMouseClick(msg tea.MouseClickMsg) tuiModel {
	if msg.Button != tea.MouseLeft {
		return m
	}
	if idx, ok := m.actionAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = "action", idx
		m.activeArea, m.actionCursor = "action", idx
		m.activateAction(idx)
		return m
	}
	if idx, ok := m.listAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = "list", idx
		m.activeArea = "list"
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
	}
	return m
}

func (m tuiModel) updateMouseMotion(msg tea.MouseMotionMsg) tuiModel {
	m.hoverArea, m.hoverIndex = "", -1
	if idx, ok := m.actionAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = "action", idx
		return m
	}
	if idx, ok := m.listAt(msg.X, msg.Y); ok {
		m.hoverArea, m.hoverIndex = "list", idx
		if m.screen != screenForm {
			m.cursor = idx
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
		m.activeArea = "action"
		m.actionCursor = 0
	}
}

func (m *tuiModel) activateFocusedRegion() {
	if m.activeArea == "action" {
		m.activateAction(m.actionCursor)
		return
	}
	switch m.screen {
	case screenSources:
		m.activateSourcesRow()
	case screenSource:
		m.activateSourceRow()
	case screenMappings:
		m.activateMappingsRow()
	}
}

func (m *tuiModel) activateAction(idx int) {
	switch m.screen {
	case screenSources:
		switch idx {
		case 0:
			m.startAddSource()
		case 1:
			m.runAllSync()
		case 2:
			m.runAllDiff()
		}
	case screenSource:
		switch idx {
		case 0:
			m.startEditSource()
		case 1:
			m.runSelectedSync()
		case 2:
			m.runSelectedDiff()
		case 3:
			m.startRemoveSource()
		case 4:
			m.startAddMapping()
		case 5:
			m.screen = screenSources
			m.cursor = 0
			m.activeArea = "action"
			m.actionCursor = 0
		}
	}
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
	if y != actionBarRow {
		return -1, false
	}
	start := 0
	for i, label := range m.actionItems() {
		width := len(label) + 4
		if x >= start && x < start+width {
			return i, true
		}
		start += width + 2
	}
	return -1, false
}

func (m tuiModel) listAt(x, y int) (int, bool) {
	if x < 0 || y < m.listStartRow() {
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
	case screenForm:
		if idx >= 0 && idx < len(m.formFields) {
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
	var b strings.Builder
	fmt.Fprintln(&b, styled(titleStyle, cfgraftLogo))
	fmt.Fprintln(&b, styled(subtleStyle, m.breadcrumbs()))
	fmt.Fprintf(&b, "%s %s\n", styled(subtleStyle, "config:"), m.paths.Config)
	if m.err != nil {
		fmt.Fprintf(&b, "%s %v\n", styled(errorStyle, "error:"), m.err)
	} else if m.msg != "" {
		fmt.Fprintf(&b, "%s %s\n", styled(successStyle, "status:"), m.msg)
	} else {
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b)
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
	view := tea.NewView(b.String())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	return view
}

func (m tuiModel) viewSources(b *strings.Builder) {
	m.writeActionBar(b)
	fmt.Fprintln(b)
	fmt.Fprintln(b, styled(titleStyle, "Sources"))
	fmt.Fprintln(b, styled(subtleStyle, "Hover or click a source row. Use arrows and enter for keyboard navigation."))
	fmt.Fprintln(b)
	ids := m.sourceIDs()
	for i, id := range ids {
		src := m.config.Sources[id]
		m.writeRow(b, i, "%s  %s %s  mappings:%d", id, src.Ref.Type, src.Ref.Name, len(src.Mappings))
	}
}

func (m tuiModel) viewSourceMenu(b *strings.Builder) {
	if !m.hasSelectedSource() {
		fmt.Fprintln(b, "Selected source no longer exists.")
		return
	}
	src := m.config.Sources[m.selectedSource]
	m.writeActionBar(b)
	fmt.Fprintln(b)
	fmt.Fprintf(b, "%s %s\n", styled(titleStyle, "Source:"), m.selectedSource)
	fmt.Fprintf(b, "%s   %s\n", styled(subtleStyle, "Repo:"), src.Repo)
	fmt.Fprintf(b, "%s    %s %s\n", styled(subtleStyle, "Ref:"), src.Ref.Type, src.Ref.Name)
	fmt.Fprintf(b, "%s   %d\n\n", styled(subtleStyle, "Maps:"), len(src.Mappings))
	fmt.Fprintln(b, styled(titleStyle, "Mappings"))
	fmt.Fprintln(b, styled(subtleStyle, "Hover or click a mapping to edit it. Add Mapping is in the action bar."))
	fmt.Fprintln(b)
	for i, mapping := range src.Mappings {
		m.writeRow(b, i, "%s -> %s", mapping.Source, mapping.Target)
	}
}

func (m tuiModel) viewMappings(b *strings.Builder) {
	if !m.hasSelectedSource() {
		fmt.Fprintln(b, "Selected source no longer exists.")
		return
	}
	fmt.Fprintf(b, "%s %s\n", styled(titleStyle, "Mappings for"), m.selectedSource)
	fmt.Fprintln(b, styled(subtleStyle, "Click a mapping to edit. a add, e edit, x remove, b back."))
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
	fmt.Fprintln(b, styled(subtleStyle, "Type to edit. Tab moves fields. Ctrl+S saves. Esc cancels."))
	fmt.Fprintln(b)
	for i, field := range m.formFields {
		prefix := "  "
		if i == m.formCursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%s: %s", prefix, field.Label, field.Input.View())
		if i == m.formCursor {
			line = styled(selectedStyle, line)
		}
		fmt.Fprintln(b, line)
	}
}

func (m tuiModel) viewConfirm(b *strings.Builder) {
	fmt.Fprintln(b, styled(warningStyle, m.formTitle))
	fmt.Fprintln(b)
	for _, field := range m.formFields {
		fmt.Fprintf(b, "%s: %s\n", field.Label, field.Input.Value())
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, styled(subtleStyle, "Press y to confirm or n/esc to cancel."))
}

func (m tuiModel) viewOutput(b *strings.Builder) {
	fmt.Fprintln(b, styled(titleStyle, m.outputTitle))
	fmt.Fprintln(b, styled(subtleStyle, "Press enter, esc, or b to return."))
	fmt.Fprintln(b)
	if strings.TrimSpace(m.outputText) == "" {
		fmt.Fprintln(b, styled(subtleStyle, "No output."))
		return
	}
	fmt.Fprintln(b, m.outputText)
}

func (m tuiModel) writeActionBar(b *strings.Builder) {
	for i, label := range m.actionItems() {
		if i > 0 {
			fmt.Fprint(b, "  ")
		}
		button := "[ " + label + " ]"
		if (m.hoverArea == "action" && m.hoverIndex == i) || (m.activeArea == "action" && m.actionCursor == i) {
			button = styled(selectedStyle, button)
		} else {
			button = styled(actionStyle, button)
		}
		fmt.Fprint(b, button)
	}
	fmt.Fprintln(b)
}

func (m tuiModel) actionItems() []string {
	switch m.screen {
	case screenSources:
		return []string{"Add Source", "Sync All", "Diff All"}
	case screenSource:
		return []string{"Edit", "Sync", "Diff", "Remove", "Add Mapping", "Back"}
	default:
		return nil
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
}

func (m *tuiModel) submitForm() {
	switch m.formKind {
	case formAddSource, formEditSource:
		m.submitSourceForm()
	case formAddMapping, formEditMapping:
		m.submitMappingForm()
	}
}

func (m *tuiModel) submitSourceForm() {
	repo := strings.TrimSpace(m.formFields[0].Input.Value())
	refType := strings.TrimSpace(m.formFields[1].Input.Value())
	refName := strings.TrimSpace(m.formFields[2].Input.Value())
	if repo == "" || refType == "" || refName == "" {
		m.err = errors.New("Git URL, ref type, and ref name are required")
		return
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
				return
			}
			delete(next.Sources, m.selectedSource)
		}
		next.Sources[id] = src
	}
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return
	}
	m.config = next
	m.selectedSource = id
	m.err = nil
	m.msg = "saved source; checking out repository"
	var out bytes.Buffer
	err := refreshRepos(m.paths, filterConfig(m.config, id), true, &out)
	m.outputTitle = "Repository checkout"
	if err != nil {
		m.err = err
		m.outputText = strings.TrimSpace(out.String())
	} else {
		m.outputText = strings.TrimSpace(out.String())
		if m.outputText == "" {
			m.outputText = "Repository cache is ready."
		}
	}
	m.screen = screenOutput
	m.cursor = 0
}

func (m *tuiModel) submitMappingForm() {
	if !m.hasSelectedSource() {
		m.err = errors.New("selected source no longer exists")
		return
	}
	sourcePath := strings.TrimSpace(m.formFields[0].Input.Value())
	targetPath := strings.TrimSpace(m.formFields[1].Input.Value())
	next := cloneConfig(m.config)
	src := next.Sources[m.selectedSource]
	mapping := Mapping{Source: sourcePath, Target: targetPath}
	if m.formKind == formAddMapping {
		src.Mappings = append(src.Mappings, mapping)
	} else {
		if m.selectedMap < 0 || m.selectedMap >= len(src.Mappings) {
			m.err = errors.New("selected mapping no longer exists")
			return
		}
		src.Mappings[m.selectedMap] = mapping
	}
	next.Sources[m.selectedSource] = src
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return
	}
	m.config = next
	m.err = nil
	m.msg = "saved mapping"
	m.screen = screenMappings
	m.cursor = 0
}

func (m *tuiModel) submitConfirm() {
	switch m.formKind {
	case confirmRemoveSrc:
		next := cloneConfig(m.config)
		delete(next.Sources, m.selectedSource)
		if err := validateConfig(next, m.paths); err != nil {
			m.err = err
			return
		}
		if err := writeConfig(m.paths, next); err != nil {
			m.err = err
			return
		}
		m.config = next
		m.selectedSource = ""
		m.err = nil
		m.msg = "removed source from config; local files were left in place"
		m.screen = screenSources
		m.cursor = 0
	case confirmRemoveMap:
		if !m.hasSelectedSource() {
			m.err = errors.New("selected source no longer exists")
			return
		}
		next := cloneConfig(m.config)
		src := next.Sources[m.selectedSource]
		if m.selectedMap < 0 || m.selectedMap >= len(src.Mappings) {
			m.err = errors.New("selected mapping no longer exists")
			return
		}
		src.Mappings = append(src.Mappings[:m.selectedMap], src.Mappings[m.selectedMap+1:]...)
		next.Sources[m.selectedSource] = src
		if err := validateConfig(next, m.paths); err != nil {
			m.err = err
			return
		}
		if err := writeConfig(m.paths, next); err != nil {
			m.err = err
			return
		}
		m.config = next
		m.err = nil
		m.msg = "removed mapping from config; local files were left in place"
		m.screen = screenMappings
		m.cursor = 0
	}
}

func (m *tuiModel) cancelForm() {
	m.err = nil
	switch m.formKind {
	case formAddSource:
		m.screen = screenSources
	case formEditSource, confirmRemoveSrc:
		m.screen = screenSource
	case formAddMapping, formEditMapping, confirmRemoveMap:
		m.screen = screenMappings
	default:
		m.screen = screenSources
	}
	m.cursor = 0
}

func (m *tuiModel) runAllSync() {
	var b bytes.Buffer
	err := syncCommand(SyncOptions{Refresh: true}, &b)
	m.showCommandOutput("Sync all sources", b.String(), err)
}

func (m *tuiModel) runSelectedSync() {
	var b bytes.Buffer
	err := syncSourceCommand(m.selectedSource, SyncOptions{Refresh: true}, &b)
	m.showCommandOutput("Sync "+m.selectedSource, b.String(), err)
}

func (m *tuiModel) runAllDiff() {
	var b bytes.Buffer
	err := diffCommand(false, &b)
	m.showCommandOutput("Diff all sources", b.String(), err)
}

func (m *tuiModel) runSelectedDiff() {
	var b bytes.Buffer
	err := diffSourceCommand(m.selectedSource, false, &b)
	m.showCommandOutput("Diff "+m.selectedSource, b.String(), err)
}

func (m *tuiModel) showCommandOutput(title, text string, err error) {
	m.outputTitle = title
	m.outputText = strings.TrimSpace(text)
	m.err = err
	if err == nil {
		m.msg = "completed"
	} else {
		m.msg = ""
	}
	m.screen = screenOutput
	m.cursor = 0
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
			m.formFields[i].Input.Focus()
		} else {
			m.formFields[i].Input.Blur()
		}
	}
	m.formCursor = idx
}

func newTUIField(label, value string) tuiField {
	input := textinput.New()
	input.SetValue(value)
	input.SetWidth(56)
	input.Prompt = ""
	return tuiField{Label: label, Input: input}
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

func (m tuiModel) sourceMenuItems() []string {
	return []string{
		"Manage mappings",
		"Edit source",
		"Sync this source",
		"Diff this source",
		"Remove source",
		"Back",
	}
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
		return 14
	case screenSource:
		return 19
	case screenMappings:
		return 14
	case screenForm:
		return 12
	default:
		return 0
	}
}
