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

	model = model.updateMouseMotion(tea.MouseMotionMsg{X: 1, Y: actionBarRow})
	if model.hoverArea != "action" || model.hoverIndex != 0 {
		t.Fatalf("expected Add Source action hover, got %q/%d", model.hoverArea, model.hoverIndex)
	}

	model = model.updateMouseClick(tea.MouseClickMsg{X: 1, Y: model.listStartRow(), Button: tea.MouseLeft})
	if model.screen != screenSource || model.selectedSource != "home" {
		t.Fatalf("expected source click to open home, got screen=%s source=%s", model.screen, model.selectedSource)
	}

	model = model.updateMouseMotion(tea.MouseMotionMsg{X: 1, Y: model.listStartRow()})
	if model.hoverArea != "list" || model.hoverIndex != 0 {
		t.Fatalf("expected mapping row hover, got %q/%d", model.hoverArea, model.hoverIndex)
	}
	model = model.updateMouseClick(tea.MouseClickMsg{X: 1, Y: model.listStartRow(), Button: tea.MouseLeft})
	if model.screen != screenForm || model.formKind != formEditMapping {
		t.Fatalf("expected mapping click to edit mapping, got screen=%s form=%s", model.screen, model.formKind)
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
		activeArea:   "action",
		actionCursor: 0,
		hoverIndex:   -1,
	}

	next, _ := model.updateSourcesKey("right")
	model = next.(tuiModel)
	if model.activeArea != "action" || model.actionCursor != 1 {
		t.Fatalf("expected action cursor on Sync All, got %s/%d", model.activeArea, model.actionCursor)
	}
	next, _ = model.updateSourcesKey("tab")
	model = next.(tuiModel)
	if model.activeArea != "list" || model.cursor != 0 {
		t.Fatalf("expected tab to focus source list, got %s/%d", model.activeArea, model.cursor)
	}
	next, _ = model.updateSourcesKey("enter")
	model = next.(tuiModel)
	if model.screen != screenSource || model.selectedSource != "home" || model.activeArea != "action" {
		t.Fatalf("expected enter to open selected source and focus action bar, got screen=%s source=%s area=%s", model.screen, model.selectedSource, model.activeArea)
	}
	next, _ = model.updateSourceKey("right")
	model = next.(tuiModel)
	if model.actionCursor != 1 {
		t.Fatalf("expected source action cursor to move right, got %d", model.actionCursor)
	}
	next, _ = model.updateSourceKey("tab")
	model = next.(tuiModel)
	if model.activeArea != "list" || model.cursor != 0 {
		t.Fatalf("expected tab to focus mapping list, got %s/%d", model.activeArea, model.cursor)
	}
	next, _ = model.updateSourceKey("shift+tab")
	model = next.(tuiModel)
	if model.activeArea != "action" {
		t.Fatalf("expected shift-tab to return to action bar, got %s", model.activeArea)
	}
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
