package cfgraft

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfigWarnsAboutOverlappingTargets(t *testing.T) {
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
	if err != nil {
		t.Fatalf("expected overlapping mappings to remain valid, got %v", err)
	}
	warnings := destinationOverlapWarnings(cfg)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "overlap") {
		t.Fatalf("expected overlap warning, got %#v", warnings)
	}
}

func TestSyncAllowsOverlappingMappingsWithDistinctFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{
		"home": {
			Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo",
			Mappings: []Mapping{
				{Source: "config", Target: filepath.Join(home, "target")},
				{Source: "nvim", Target: filepath.Join(home, "target", "nvim")},
			},
		},
	}})
	writeCacheFile(t, paths, "home-repo", "config/shell.conf", "shell\n")
	writeCacheFile(t, paths, "home-repo", "nvim/init.lua", "nvim\n")

	var out bytes.Buffer
	if err := syncCommand(SyncOptions{}, &out); err != nil {
		t.Fatalf("expected distinct files under overlapping mappings to sync successfully: %v\n%s", err, out.String())
	}
	assertFile(t, filepath.Join(home, "target", "shell.conf"), "shell\n")
	assertFile(t, filepath.Join(home, "target", "nvim", "init.lua"), "nvim\n")
}

func TestSyncRejectsConcreteDestinationFileOverlapBeforeWriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{
		"home": {
			Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo",
			Mappings: []Mapping{
				{Source: "config", Target: filepath.Join(home, "target")},
				{Source: "nvim", Target: filepath.Join(home, "target", "nvim")},
			},
		},
	}})
	writeCacheFile(t, paths, "home-repo", "config/nvim/init.lua", "first\n")
	writeCacheFile(t, paths, "home-repo", "nvim/init.lua", "second\n")

	var out bytes.Buffer
	err := syncCommand(SyncOptions{}, &out)
	if err == nil || !strings.Contains(err.Error(), "destination files overlap") {
		t.Fatalf("expected concrete destination overlap error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "target")); !os.IsNotExist(statErr) {
		t.Fatalf("expected sync to stop before writing destinations, got %v", statErr)
	}
}

func TestSyncRejectsDestinationFileNeededAsDirectoryBeforeWriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{
		"home": {
			Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo",
			Mappings: []Mapping{
				{Source: "config", Target: filepath.Join(home, "target")},
				{Source: "nvim", Target: filepath.Join(home, "target", "nvim")},
			},
		},
	}})
	writeCacheFile(t, paths, "home-repo", "config/nvim", "file\n")
	writeCacheFile(t, paths, "home-repo", "nvim/init.lua", "nested\n")

	var out bytes.Buffer
	err := syncCommand(SyncOptions{}, &out)
	if err == nil || !strings.Contains(err.Error(), "destination files overlap") {
		t.Fatalf("expected file-versus-directory destination overlap error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "target")); !os.IsNotExist(statErr) {
		t.Fatalf("expected sync to stop before writing destinations, got %v", statErr)
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
		"cfgraft <command> [flags]",
		"validate",
		"config",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected help to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRemovedTUICommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"tui"}, &stdout, &stderr, "dev"); err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}

func TestSubcommandHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "sync",
			args: []string{"sync", "--help"},
			want: []string{"Usage:", "cfgraft sync [flags]", "--dry-run", "CFGRAFT_DRY_RUN", "--force", "--verbose"},
		},
		{
			name: "diff",
			args: []string{"diff", "--help"},
			want: []string{"Usage:", "cfgraft diff [flags]", "--verbose"},
		},
		{
			name: "version",
			args: []string{"version", "--help"},
			want: []string{"Usage:", "cfgraft version"},
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

func TestSyncPrunesStateForRemovedMapping(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{
		"home": {
			Repo:    "https://example.invalid/home.git",
			Ref:     Ref{Type: "branch", Name: "main"},
			LocalID: "home-repo",
			Mappings: []Mapping{
				{Source: "kept.txt", Target: filepath.Join(home, "target", "kept.txt")},
			},
		},
	}})
	writeCacheFile(t, paths, "home-repo", "kept.txt", "kept\n")
	staleTarget := filepath.Join(home, "target", "removed.txt")
	if err := os.MkdirAll(filepath.Dir(staleTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleTarget, []byte("removed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleHash, err := fileHash(staleTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{
		{SourceID: "home", Source: "removed.txt", Target: staleTarget, Hash: staleHash, Type: "file", Mode: 0o644},
	}}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := syncCommand(SyncOptions{}, &out); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out.String())
	}
	assertFile(t, staleTarget, "removed\n")
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range state.Files {
		if file.Target == staleTarget {
			t.Fatalf("expected stale removed mapping state to be pruned, got %#v", state.Files)
		}
	}
	if !strings.Contains(out.String(), "stale") {
		t.Fatalf("expected sync to audit stale state, got:\n%s", out.String())
	}
}

func TestSyncPrunesStateForRemovedSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{}})
	target := filepath.Join(home, "target", "old.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := fileHash(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{
		{SourceID: "old", Source: "old.txt", Target: target, Hash: hash, Type: "file", Mode: 0o644},
	}}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := syncCommand(SyncOptions{}, &out); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out.String())
	}
	assertFile(t, target, "old\n")
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("expected removed source state to be pruned, got %#v", state.Files)
	}
	if _, err := os.Stat(filepath.Join(paths.StateDir, "old.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected removed source state file to be deleted, got %v", err)
	}
}

func TestDryRunDoesNotPruneStaleState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{}})
	target := filepath.Join(home, "target", "old.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := fileHash(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{
		{SourceID: "old", Source: "old.txt", Target: target, Hash: hash, Type: "file", Mode: 0o644},
	}}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := syncCommand(SyncOptions{DryRun: true}, &out); err != nil {
		t.Fatalf("dry-run sync failed: %v\n%s", err, out.String())
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 1 || state.Files[0].Target != target {
		t.Fatalf("expected dry-run to preserve stale state, got %#v", state.Files)
	}
}

func TestSyncSourcePreservesOtherSourceState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homeTarget := filepath.Join(home, "target", "home.txt")
	workTarget := filepath.Join(home, "target", "work.txt")
	paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{
		"home": {
			Repo:    "https://example.invalid/home.git",
			Ref:     Ref{Type: "branch", Name: "main"},
			LocalID: "home-repo",
			Mappings: []Mapping{
				{Source: "home.txt", Target: homeTarget},
			},
		},
		"work": {
			Repo:    "https://example.invalid/work.git",
			Ref:     Ref{Type: "branch", Name: "main"},
			LocalID: "work-repo",
			Mappings: []Mapping{
				{Source: "work.txt", Target: workTarget},
			},
		},
	}})
	writeCacheFile(t, paths, "home-repo", "home.txt", "home\n")
	writeCacheFile(t, paths, "work-repo", "work.txt", "work\n")
	if err := os.MkdirAll(filepath.Dir(workTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workTarget, []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workHash, err := fileHash(workTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{
		{SourceID: "work", Source: "work.txt", Target: workTarget, Hash: workHash, Type: "file", Mode: 0o644},
	}}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := syncSourceCommand("home", SyncOptions{}, &out); err != nil {
		t.Fatalf("source sync failed: %v\n%s", err, out.String())
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 2 {
		t.Fatalf("expected selected-source sync to preserve unrelated state, got %#v", state.Files)
	}
	targets := map[string]bool{}
	for _, file := range state.Files {
		targets[file.Target] = true
	}
	if !targets[homeTarget] || !targets[workTarget] {
		t.Fatalf("expected home and work state, got %#v", state.Files)
	}
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

func writeCachedConfigForTest(t *testing.T, home string, cfg Config) Paths {
	t.Helper()
	paths := Paths{
		Base:     filepath.Join(home, ".config", "cfgraft"),
		Config:   filepath.Join(home, ".config", "cfgraft", "config.yaml"),
		Repos:    filepath.Join(home, ".cache", "cfgraft", "repos"),
		StateDir: filepath.Join(home, ".config", "cfgraft", "state"),
	}
	if err := writeConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func writeCacheFile(t *testing.T, paths Paths, localID, rel, content string) {
	t.Helper()
	path := filepath.Join(paths.Repos, localID, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestValidateIsReadOnlyAndRejectsUnknownFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errout bytes.Buffer
	if err := Run([]string{"validate"}, &out, &errout, "dev"); err == nil {
		t.Fatal("missing config should fail")
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("validate created files")
	}
	dir := filepath.Join(home, ".config", "cfgraft")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("sources: {}\ntypo: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"validate"}, &out, &errout, "dev"); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := os.WriteFile(path, []byte("sources: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"validate"}, &out, &errout, "dev"); err != nil {
		t.Fatal(err)
	}
}

func TestFlagEnvironmentAndRemovedInteractive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CFGRAFT_VERBOSE", "invalid")
	var out, errout bytes.Buffer
	if err := Run([]string{"diff"}, &out, &errout, "dev"); err == nil {
		t.Fatal("invalid boolean accepted")
	}
	if err := Run([]string{"sync", "--interactive"}, &out, &errout, "dev"); err == nil {
		t.Fatal("interactive flag accepted")
	}
	// Explicit flags override even an invalid environment default.
	if err := Run([]string{"diff", "--verbose=false"}, &out, &errout, "dev"); err != nil {
		t.Fatal(err)
	}
}
