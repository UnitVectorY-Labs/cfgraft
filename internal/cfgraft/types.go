package cfgraft

import (
	"fmt"
	"io"
	"io/fs"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	configFileName = "config.yaml"
	stateFileName  = "state.yaml"
	stateDirName   = "state"
)

type Config struct {
	Sources map[string]Source `yaml:"sources"`
}

type Source struct {
	Repo     string    `yaml:"repo"`
	Ref      Ref       `yaml:"ref"`
	LocalID  string    `yaml:"local_id,omitempty"`
	Mappings []Mapping `yaml:"mappings"`
}

type Ref struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
}

type Mapping struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type State struct {
	Files []StateFile `yaml:"files"`
}

type StateFile struct {
	SourceID string `yaml:"source_id"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	Hash     string `yaml:"hash"`
	Type     string `yaml:"type"`
	Mode     uint32 `yaml:"mode,omitempty"`
}

type Paths struct {
	Base     string
	Config   string
	Repos    string
	State    string
	StateDir string
}

type SyncOptions struct {
	Force       bool
	Interactive bool
	DryRun      bool
	Verbose     bool
	Refresh     bool
}

type PlannedOp struct {
	Kind      string
	SourceID  string
	SourceRel string
	SourceAbs string
	Target    string
	Hash      string
	Mode      fs.FileMode
	OldHash   string
	Reason    string
	Binary    bool
}

type Plan struct {
	Ops       []PlannedOp
	Conflicts []PlannedOp
	Warnings  []string
	Stale     []StateFile
}

type tuiScreen string

const (
	screenSources  tuiScreen = "sources"
	screenSource   tuiScreen = "source"
	screenMappings tuiScreen = "mappings"
	screenForm     tuiScreen = "form"
	screenConfirm  tuiScreen = "confirm"
	screenOutput   tuiScreen = "output"
)

type tuiFormKind string

const (
	formAddSource         tuiFormKind = "add-source"
	formEditSource        tuiFormKind = "edit-source"
	formAddMapping        tuiFormKind = "add-mapping"
	formEditMapping       tuiFormKind = "edit-mapping"
	confirmRemoveSrc      tuiFormKind = "remove-source"
	confirmRemoveSrcMaps  tuiFormKind = "remove-source-mappings"
	confirmRemoveMap      tuiFormKind = "remove-mapping"
	confirmRemoveMapFiles tuiFormKind = "remove-mapping-files"
	confirmCreateParents  tuiFormKind = "create-parents"
)

type tuiField struct {
	Label string
	Input textinput.Model
}

type tuiListItem struct {
	title string
	desc  string
}

func (i tuiListItem) Title() string {
	return i.title
}

func (i tuiListItem) Description() string {
	return i.desc
}

func (i tuiListItem) FilterValue() string {
	return i.title
}

var _ list.DefaultItem = tuiListItem{}

type tuiListDelegate struct{}

func (d tuiListDelegate) Height() int {
	return 1
}

func (d tuiListDelegate) Spacing() int {
	return 0
}

func (d tuiListDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func (d tuiListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	listItem, ok := item.(tuiListItem)
	if !ok {
		return
	}
	width := max(12, m.Width()-2)
	title := listItem.Title()
	if lipgloss.Width(title) > width-3 {
		title = truncateCells(title, width-4) + "…"
	}
	line := "  " + title
	if index == m.Index() {
		line = "▶ " + title
		line = selectedRowStyle.Width(width).Render(line)
	} else {
		line = normalRowStyle.Width(width).Render(line)
	}
	fmt.Fprint(w, line)
}

func truncateCells(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	var out []rune
	width := 0
	for _, r := range value {
		next := lipgloss.Width(string(r))
		if width+next > maxWidth {
			break
		}
		out = append(out, r)
		width += next
	}
	return string(out)
}
