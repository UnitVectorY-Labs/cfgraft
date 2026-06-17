package cfgraft

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestValidateConfigRejectsOverlappingTargets(t *testing.T) {
	paths := Paths{Repos: filepath.Join(t.TempDir(), "repos")}
	cfg := Config{Sources: map[string]Source{
		"home": {
			Repo: "https://example.invalid/repo.git",
			Ref:  Ref{Type: "branch", Name: "main"},
			Mappings: []Mapping{
				{Source: "config", Target: "/tmp/cfgraft-test/config"},
				{Source: "nvim", Target: "/tmp/cfgraft-test/config/nvim"},
			},
		},
	}}
	err := validateConfig(cfg, paths)
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap validation error, got %v", err)
	}
}

func TestSourceIDDerivedFromRepoURL(t *testing.T) {
	cfg := Config{Sources: map[string]Source{
		"dotfiles": {Repo: "https://github.com/example/dotfiles.git"},
	}}
	if got := deriveUniqueSourceID("git@github.com:example/dotfiles.git", cfg, ""); got != "dotfiles-2" {
		t.Fatalf("unexpected derived ID: %s", got)
	}
	if got := deriveUniqueSourceID("https://github.com/example/tools.git", cfg, ""); got != "tools" {
		t.Fatalf("unexpected derived ID: %s", got)
	}
}

func TestRunWithoutSubcommandPrintsRootHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(nil, &stdout, &stderr, "dev"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Usage:",
		"cfgraft                 show this help",
		"cfgraft tui             launch the interactive TUI",
		"cfgraft sync [flags]",
		"cfgraft diff [flags]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected help to contain %q, got:\n%s", want, out)
		}
	}
}

func TestSubcommandHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "tui",
			args: []string{"tui", "--help"},
			want: []string{"Usage:", "cfgraft tui", "interactive cfgraft terminal UI"},
		},
		{
			name: "sync",
			args: []string{"sync", "--help"},
			want: []string{"Usage:", "cfgraft sync [flags]", "--dry-run", "--interactive", "--force", "--verbose"},
		},
		{
			name: "diff",
			args: []string{"diff", "--help"},
			want: []string{"Usage:", "cfgraft diff [flags]", "--verbose"},
		},
		{
			name: "version",
			args: []string{"version", "--help"},
			want: []string{"Usage:", "cfgraft version", "cfgraft --version", "cfgraft -v"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := Run(tt.args, &stdout, &stderr, "dev"); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected no stderr, got %q", stderr.String())
			}
			out := stdout.String()
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Fatalf("expected help to contain %q, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestTUIMouseHoverAndClickRegions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	model := tuiModel{
		config: Config{Sources: map[string]Source{
			"home": {
				Repo: "https://example.invalid/home.git",
				Ref:  Ref{Type: "branch", Name: "main"},
				Mappings: []Mapping{
					{Source: "zshrc", Target: filepath.Join(home, ".zshrc")},
				},
			},
		}},
		screen:     screenSources,
		hoverIndex: -1,
	}
	model.syncComponents()

	model = model.updateMouseMotion(tea.MouseMotionMsg{X: 3, Y: model.actionStartRow()})
	if model.hoverArea != actionArea || model.hoverIndex != 0 {
		t.Fatalf("expected Add action hover, got %q/%d", model.hoverArea, model.hoverIndex)
	}
	model = model.updateMouseMotion(tea.MouseMotionMsg{X: 8, Y: model.actionStartRow()})
	if model.hoverArea == actionArea {
		t.Fatalf("expected gap between actions not to hover, got %q/%d", model.hoverArea, model.hoverIndex)
	}

	model, _ = model.updateMouseClick(tea.MouseClickMsg{X: 1, Y: model.listStartRow(), Button: tea.MouseLeft})
	if model.screen != screenSource || model.selectedSource != "home" {
		t.Fatalf("expected source click to open home, got screen=%s source=%s", model.screen, model.selectedSource)
	}

	model = model.updateMouseMotion(tea.MouseMotionMsg{X: 1, Y: model.listStartRow()})
	if model.hoverArea != listArea || model.hoverIndex != 0 {
		t.Fatalf("expected mapping row hover, got %q/%d", model.hoverArea, model.hoverIndex)
	}
	model, _ = model.updateMouseClick(tea.MouseClickMsg{X: 1, Y: model.listStartRow(), Button: tea.MouseLeft})
	if model.screen != screenForm || model.formKind != formEditMapping {
		t.Fatalf("expected mapping click to edit mapping, got screen=%s form=%s", model.screen, model.formKind)
	}
}

func TestTUIMouseHoverAfterSourceBackUsesCurrentLayout(t *testing.T) {
	home := t.TempDir()
	model := tuiModel{
		config: Config{Sources: map[string]Source{
			"home": {
				Repo: "https://example.invalid/home.git",
				Ref:  Ref{Type: "branch", Name: "main"},
				Mappings: []Mapping{
					{Source: "zshrc", Target: filepath.Join(home, ".zshrc")},
				},
			},
		}},
		screen:         screenSource,
		selectedSource: "home",
		activeArea:     actionArea,
		actionCursor:   5,
		hoverIndex:     -1,
		width:          80,
		height:         40,
	}
	model.syncComponents()

	model, _ = model.updateMouseClick(tea.MouseClickMsg{X: 58, Y: model.actionStartRow(), Button: tea.MouseLeft})
	if model.screen != screenSources {
		t.Fatalf("expected Back action to return to sources, got %s", model.screen)
	}

	// syncComponents on a copy to compute the action row offset for the new screen
	rendered := model
	rendered.syncComponents()
	row := rendered.actionStartRow()
	model = model.updateMouseMotion(tea.MouseMotionMsg{X: 3, Y: row})
	if model.hoverArea != actionArea || model.hoverIndex != 0 {
		t.Fatalf("expected Add action hover after returning to sources, got %q/%d", model.hoverArea, model.hoverIndex)
	}
}

func TestTUIKeyboardActionAndListNavigation(t *testing.T) {
	home := t.TempDir()
	model := tuiModel{
		config: Config{Sources: map[string]Source{
			"home": {
				Repo: "https://example.invalid/home.git",
				Ref:  Ref{Type: "branch", Name: "main"},
				Mappings: []Mapping{
					{Source: "zshrc", Target: filepath.Join(home, ".zshrc")},
				},
			},
		}},
		screen:       screenSources,
		activeArea:   listArea,
		actionCursor: 0,
		hoverIndex:   -1,
	}

	next, _ := model.updateSourcesKey("tab")
	model = next.(tuiModel)
	if model.activeArea != actionArea || model.actionCursor != 0 {
		t.Fatalf("expected tab to focus bottom buttons, got %s/%d", model.activeArea, model.actionCursor)
	}
	model.actionCursor = 3
	if cmd := model.activateFocusedRegion(); cmd == nil {
		t.Fatal("expected Quit action to return a command")
	}
	model.actionCursor = 0
	next, _ = model.updateSourcesKey("shift+tab")
	model = next.(tuiModel)
	if model.activeArea != listArea || model.cursor != 0 {
		t.Fatalf("expected shift-tab to focus source list, got %s/%d", model.activeArea, model.cursor)
	}
	next, _ = model.updateSourcesKey("enter")
	model = next.(tuiModel)
	if model.screen != screenSource || model.selectedSource != "home" || model.activeArea != listArea {
		t.Fatalf("expected enter to open selected source and focus action bar, got screen=%s source=%s area=%s", model.screen, model.selectedSource, model.activeArea)
	}
	next, _ = model.updateSourceKey("tab")
	model = next.(tuiModel)
	if model.activeArea != actionArea {
		t.Fatalf("expected tab to focus source buttons, got %s", model.activeArea)
	}
	next, _ = model.updateSourceKey("shift+tab")
	model = next.(tuiModel)
	if model.activeArea != listArea || model.cursor != 0 {
		t.Fatalf("expected shift-tab to focus mapping list, got %s/%d", model.activeArea, model.cursor)
	}
	next, _ = model.updateSourceKey("up")
	model = next.(tuiModel)
	if model.activeArea != listArea {
		t.Fatalf("expected up to stay within mapping list, got %s", model.activeArea)
	}
	next, _ = model.updateSourceKey("tab")
	model = next.(tuiModel)
	if model.activeArea != actionArea {
		t.Fatalf("expected tab to return to buttons, got %s", model.activeArea)
	}
}

func TestTUIMappingSourceSuggestionsWorkBeforeTyping(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(cache, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "config", "app.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cache, "config", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "config", "nested", "deep.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := tuiModel{
		paths: Paths{Repos: root},
		config: Config{Sources: map[string]Source{
			"home": {
				Repo:    "https://example.invalid/home.git",
				Ref:     Ref{Type: "branch", Name: "main"},
				LocalID: "repo",
			},
		}},
		screen:         screenSource,
		selectedSource: "home",
		activeArea:     listArea,
	}

	model.startAddMapping()
	model.formEditing = true
	model.focusFormField(0)
	suggestions := model.activePathSuggestions()
	if len(suggestions) != 1 || suggestions[0] != "config/" {
		t.Fatalf("expected only top-level source directory suggestions before typing, got %#v", suggestions)
	}
	if !model.acceptActiveSuggestion() {
		t.Fatal("expected tab completion to accept source suggestion")
	}
	if got := model.formFields[0].Input.Value(); got != "config/" {
		t.Fatalf("expected accepted source suggestion, got %q", got)
	}
	suggestions = model.activePathSuggestions()
	want := []string{"config/app.yaml", "config/nested/"}
	if strings.Join(suggestions, ",") != strings.Join(want, ",") {
		t.Fatalf("expected one-level suggestions inside config, got %#v", suggestions)
	}
}

func TestTUIOutputBackReturnsToCaller(t *testing.T) {
	model := tuiModel{
		config: Config{Sources: map[string]Source{
			"home": {Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}},
		}},
		screen:             screenOutput,
		selectedSource:     "home",
		outputReturnScreen: screenSource,
		activeArea:         actionArea,
		actionCursor:       0,
		outputText:         "done",
		outputTitle:        "Sync home",
		width:              80,
		height:             24,
	}
	model.outputViewport.SetContent(model.outputText)
	model.syncComponents()

	next, _ := model.updateKey(tea.KeyPressMsg{Code: 'b', Text: "b"})
	model = next.(tuiModel)
	if model.screen != screenSource || model.selectedSource != "home" {
		t.Fatalf("expected keyboard back to return to source, got screen=%s source=%s", model.screen, model.selectedSource)
	}

	model.screen = screenOutput
	model.outputReturnScreen = screenSource
	model, _ = model.updateMouseClick(tea.MouseClickMsg{X: 3, Y: model.actionStartRow(), Button: tea.MouseLeft})
	if model.screen != screenSource || model.selectedSource != "home" {
		t.Fatalf("expected clicked Back to return to source, got screen=%s source=%s", model.screen, model.selectedSource)
	}
}

func TestTUICommandOutputDoesNotSetTopMessage(t *testing.T) {
	model := tuiModel{}
	model.showCommandOutput("Sync all sources", "done", nil)
	if model.msg != "" {
		t.Fatalf("expected command output not to set top message, got %q", model.msg)
	}
}

func TestTUISelectedSourceCommandsReturnToSource(t *testing.T) {
	model := tuiModel{
		config: Config{Sources: map[string]Source{
			"home": {Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}},
		}},
		screen:         screenSource,
		selectedSource: "home",
	}

	cmd := model.runSelectedSync()
	if cmd == nil {
		t.Fatal("expected selected sync to start background command")
	}
	if model.outputReturnScreen != screenSource {
		t.Fatalf("expected selected sync to return to source, got %s", model.outputReturnScreen)
	}

	model.outputReturnScreen = ""
	cmd = model.runSelectedDiff()
	if cmd == nil {
		t.Fatal("expected selected diff to start background command")
	}
	if model.outputReturnScreen != screenSource {
		t.Fatalf("expected selected diff to return to source, got %s", model.outputReturnScreen)
	}
}

func TestTUIOutputViewportHeightIsBounded(t *testing.T) {
	model := tuiModel{width: 100, height: 40}
	model.outputViewport.SetWidth(80)
	model.outputViewport.SetHeight(20)
	model.resizeViewport()
	if got := model.outputViewport.Height(); got != 12 {
		t.Fatalf("expected output viewport height capped at 12, got %d", got)
	}

	model.height = 18
	model.resizeViewport()
	if got := model.outputViewport.Height(); got != 5 {
		t.Fatalf("expected small output viewport floor of 5, got %d", got)
	}
}

func TestTUIRemoveSourceDeletesRepoCacheAndMappedFilesWhenConfirmed(t *testing.T) {
	paths, cache, target, model := setupRemoveSourceModel(t)

	model.finishRemoveSource(true)

	if model.err != nil {
		t.Fatalf("unexpected remove error: %v", model.err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("expected repository cache to be removed, got %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected mapped file to be removed, got %v", err)
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("expected source state to be removed, got %#v", state.Files)
	}
	if _, ok := model.config.Sources["home"]; ok {
		t.Fatal("expected source to be removed from model config")
	}
}

func TestTUIRemoveSourceKeepsMappedFilesWhenDeclined(t *testing.T) {
	paths, cache, target, model := setupRemoveSourceModel(t)

	model.finishRemoveSource(false)

	if model.err != nil {
		t.Fatalf("unexpected remove error: %v", model.err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("expected repository cache to be removed, got %v", err)
	}
	assertFile(t, target, "managed\n")
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("expected source state to be removed, got %#v", state.Files)
	}
}

func TestTUIRemoveSourcePromptsBeforeDeletingTrackedMappedFiles(t *testing.T) {
	_, _, _, model := setupRemoveSourceModel(t)

	model.submitConfirm()

	if model.screen != screenConfirm || model.formKind != confirmRemoveSrcMaps {
		t.Fatalf("expected mapped-file confirmation, got screen=%s form=%s", model.screen, model.formKind)
	}
}

func TestTUIRemoveSourceMappedFilePromptIsClickable(t *testing.T) {
	_, cache, target, model := setupRemoveSourceModel(t)
	model.submitConfirm()

	row := model.actionStartRow()
	model, _ = model.updateMouseClick(tea.MouseClickMsg{X: 14, Y: row, Button: tea.MouseLeft})

	if model.err != nil {
		t.Fatalf("unexpected remove error: %v", model.err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("expected repository cache to be removed, got %v", err)
	}
	assertFile(t, target, "managed\n")
}

func TestTUIRemoveMappingDeletesManagedFilesWhenConfirmed(t *testing.T) {
	paths, cache, target, model := setupRemoveSourceModel(t)
	model.formKind = confirmRemoveMap

	model.finishRemoveMapping(true)

	if model.err != nil {
		t.Fatalf("unexpected remove error: %v", model.err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("expected repository cache to remain, got %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected managed file to be removed, got %v", err)
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("expected mapping state to be removed, got %#v", state.Files)
	}
	if got := len(model.config.Sources["home"].Mappings); got != 0 {
		t.Fatalf("expected mapping to be removed from model config, got %d", got)
	}
}

func TestTUIRemoveMappingKeepsManagedFilesWhenDeclined(t *testing.T) {
	paths, _, target, model := setupRemoveSourceModel(t)
	model.formKind = confirmRemoveMap

	model.finishRemoveMapping(false)

	if model.err != nil {
		t.Fatalf("unexpected remove error: %v", model.err)
	}
	assertFile(t, target, "managed\n")
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("expected mapping state to be removed, got %#v", state.Files)
	}
}

func TestTUIRemoveMappingPromptsBeforeDeletingTrackedFiles(t *testing.T) {
	_, _, _, model := setupRemoveSourceModel(t)
	model.formKind = confirmRemoveMap

	model.submitConfirm()

	if model.screen != screenConfirm || model.formKind != confirmRemoveMapFiles {
		t.Fatalf("expected managed-file confirmation, got screen=%s form=%s", model.screen, model.formKind)
	}
}

func TestTUIRemoveMappingManagedFilePromptIsClickable(t *testing.T) {
	_, _, target, model := setupRemoveSourceModel(t)
	model.formKind = confirmRemoveMap
	model.submitConfirm()

	row := model.actionStartRow()
	model, _ = model.updateMouseClick(tea.MouseClickMsg{X: 14, Y: row, Button: tea.MouseLeft})

	if model.err != nil {
		t.Fatalf("unexpected remove error: %v", model.err)
	}
	assertFile(t, target, "managed\n")
	if got := len(model.config.Sources["home"].Mappings); got != 0 {
		t.Fatalf("expected mapping to be removed from model config, got %d", got)
	}
}

func setupRemoveSourceModel(t *testing.T) (Paths, string, string, tuiModel) {
	t.Helper()
	root := t.TempDir()
	paths := Paths{
		Base:     root,
		Config:   filepath.Join(root, "config.yaml"),
		Repos:    filepath.Join(root, "repos"),
		State:    filepath.Join(root, "state.yaml"),
		StateDir: filepath.Join(root, "state"),
	}
	cache := filepath.Join(paths.Repos, "home-repo")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "README.md"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target", "tool")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := fileHash(target)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Sources: map[string]Source{
		"home": {
			Repo:    "https://example.invalid/home.git",
			Ref:     Ref{Type: "branch", Name: "main"},
			LocalID: "home-repo",
			Mappings: []Mapping{
				{Source: "README.md", Target: target},
			},
		},
	}}
	if err := writeConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{
		{SourceID: "home", Source: "README.md", Target: target, Hash: hash, Type: "file", Mode: 0o644},
	}}); err != nil {
		t.Fatal(err)
	}
	model := tuiModel{
		paths:          paths,
		config:         cfg,
		screen:         screenConfirm,
		formKind:       confirmRemoveSrc,
		selectedSource: "home",
		activeArea:     listArea,
	}
	return paths, cache, target, model
}

func TestSyncSafetyFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	srcRepo := createGitRepo(t)
	target := filepath.Join(home, "managed", "tool")
	writeConfigForTest(t, srcRepo, target)

	var out bytes.Buffer
	if err := syncCommand(SyncOptions{Refresh: true}, &out); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, out.String())
	}
	assertFile(t, target, "repo-v1\n")
	if _, err := os.Stat(filepath.Join(home, ".config", "cfgraft", "state", "home.yaml")); err != nil {
		t.Fatalf("expected per-source state file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "cfgraft", "state.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy state.yaml to be absent after split-state write, got %v", err)
	}

	if err := os.WriteFile(target, []byte("local-change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := syncCommand(SyncOptions{Refresh: true}, &out)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict, got err=%v out=%s", err, out.String())
	}
	assertFile(t, target, "local-change\n")

	out.Reset()
	if err := syncCommand(SyncOptions{Refresh: true, DryRun: true}, &out); err != nil {
		t.Fatalf("dry-run should report conflicts without failing: %v\n%s", err, out.String())
	}
	assertFile(t, target, "local-change\n")

	out.Reset()
	if err := syncCommand(SyncOptions{Refresh: true, Force: true}, &out); err != nil {
		t.Fatalf("force sync failed: %v\n%s", err, out.String())
	}
	assertFile(t, target, "repo-v1\n")
}

func createGitRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertCmd(t, repo, "git", "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "bin", "tool"), []byte("repo-v1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertCmd(t, repo, "git", "add", ".")
	assertCmd(t, repo, "git", "-c", "user.name=cfgraft", "-c", "user.email=cfgraft@example.invalid", "commit", "-m", "initial")
	return repo
}

func writeConfigForTest(t *testing.T, repo, target string) {
	t.Helper()
	paths, err := cfgPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Sources: map[string]Source{
		"home": {
			Repo: repo,
			Ref:  Ref{Type: "branch", Name: "main"},
			Mappings: []Mapping{
				{Source: "bin/tool", Target: target},
			},
		},
	}}
	if err := writeConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
}

func assertCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected %s content: got %q want %q", path, string(got), want)
	}
}
