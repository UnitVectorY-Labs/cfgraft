package cfgraft

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestValidateListsSourcesAndResolvedMappings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NO_COLOR", "1")
	dir := filepath.Join(home, ".config", "cfgraft")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	config := "sources:\n  zed:\n    repo: https://example.com/z.git\n    ref: {type: tag, name: v1}\n    mappings: []\n  alpha:\n    repo: https://example.com/a.git\n    ref: {type: branch, name: main}\n    mappings:\n      - {source: folder, target: ~/output}\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if err := Run([]string{"validate"}, &out, &errout, "dev"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"source   alpha  https://example.com/a.git  branch:main", "mapping  alpha:folder -> " + filepath.Join(home, "output"), "source   zed  https://example.com/z.git  tag:v1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Index(got, "source   alpha") > strings.Index(got, "source   zed") {
		t.Fatalf("sources not sorted: %s", got)
	}
}

func TestUnmanagedDestinationGuidance(t *testing.T) {
	for _, binary := range []bool{false, true} {
		name := "text"
		if binary {
			name = "binary"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			target := filepath.Join(home, "existing")
			paths := writeCachedConfigForTest(t, home, Config{Sources: map[string]Source{
				"home": {Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo", Mappings: []Mapping{{Source: "file", Target: target}}},
			}})
			source, local := "repository\n", "local\n"
			if binary {
				source, local = "repository\x00", "local\x00"
			}
			writeCacheFile(t, paths, "home-repo", "file", source)
			if err := os.WriteFile(target, []byte(local), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, sourceOnly := range []bool{false, true} {
				var out bytes.Buffer
				var err error
				if sourceOnly {
					err = diffSourceCommand("home", false, &out)
				} else {
					err = diffCommand(false, &out)
				}
				if err != nil {
					t.Fatal(err)
				}
				requireOutput(t, out.String(), target, "not managed by cfgraft yet", "cfgraft sync --force", "overwrite with source content", "begin managing this file")
				if binary {
					requireOutput(t, out.String(), "binary files differ")
				} else {
					requireOutput(t, out.String(), "-local", "+repository")
				}
				for _, force := range []bool{false, true} {
					out.Reset()
					opts := SyncOptions{DryRun: true, Force: force}
					if sourceOnly {
						err = syncSourceCommand("home", opts, &out)
					} else {
						err = syncCommand(opts, &out)
					}
					if err != nil {
						t.Fatal(err)
					}
					requireOutput(t, out.String(), "not managed by cfgraft yet", "overwrite with source content", "begin managing this file", "dry-run: no destination files or state were changed")
					if force {
						requireOutput(t, out.String(), "force", "--force will overwrite", "next: apply with cfgraft sync --force")
						if strings.Contains(out.String(), "conflict") {
							t.Fatalf("forced preview still reported blocked conflict: %s", &out)
						}
					} else {
						requireOutput(t, out.String(), "conflict", "cfgraft sync --force", "cfgraft sync --dry-run --force")
					}
					assertFile(t, target, local)
					state, err := loadState(paths)
					if err != nil {
						t.Fatal(err)
					}
					if len(state.Files) != 0 {
						t.Fatalf("preview adopted file: %#v", state)
					}
				}
			}
			var out bytes.Buffer
			if err := syncCommand(SyncOptions{}, &out); err == nil {
				t.Fatal("unmanaged destination overwritten without force")
			}
			assertFile(t, target, local)
			if err := syncCommand(SyncOptions{Force: true}, &out); err != nil {
				t.Fatal(err)
			}
			assertFile(t, target, source)
			state, err := loadState(paths)
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Files) != 1 || state.Files[0].Target != target {
				t.Fatalf("forced sync did not adopt destination: %#v", state)
			}
		})
	}
}

func TestRootFlagsSuggestSyncCommand(t *testing.T) {
	for _, args := range [][]string{{"--dry-run"}, {"--dry-run", "--force"}} {
		var out bytes.Buffer
		err := Run(args, &out, &out, "dev")
		if err == nil {
			t.Fatal("expected missing subcommand error")
		}
		requireOutput(t, err.Error(), "flags require a subcommand", "cfgraft sync "+strings.Join(args, " "))
	}
}

func requireOutput(t *testing.T, output string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected %q in output:\n%s", fragment, output)
		}
	}
}

func TestSyncUsesReplacementConfigInOneInvocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := createGitRepo(t)
	addedRepo := createGitRepo(t)
	configTarget := filepath.Join(home, ".config", "cfgraft", "config.yaml")
	oldTarget := filepath.Join(home, "obsolete")
	newTarget := filepath.Join(home, "added")
	selfMapping := Mapping{Source: "cfgraft.yaml", Target: configTarget}
	original := Config{Sources: map[string]Source{
		"home": {Repo: repo, Ref: Ref{Type: "branch", Name: "main"}, Mappings: []Mapping{selfMapping, {Source: "bin/tool", Target: oldTarget}}},
	}}
	paths := writeCachedConfigForTest(t, home, original)
	originalBytes, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	replacement := cloneConfig(original)
	src := replacement.Sources["home"]
	src.Mappings = []Mapping{selfMapping}
	replacement.Sources["home"] = src
	replacement.Sources["added"] = Source{Repo: addedRepo, Ref: Ref{Type: "branch", Name: "main"}, Mappings: []Mapping{{Source: "bin/tool", Target: newTarget}}}
	replacement.Sources["disabled"] = Source{Repo: filepath.Join(home, "missing"), Ref: Ref{Type: "branch", Name: "main"}, Condition: "disabled"}
	data, err := yaml.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cfgraft.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	assertCmd(t, repo, "git", "add", "cfgraft.yaml")
	assertCmd(t, repo, "git", "-c", "user.name=cfgraft", "-c", "user.email=cfgraft@example.invalid", "commit", "-m", "Manage cfgraft configuration")
	if err := os.WriteFile(oldTarget, []byte("old content"), 0600); err != nil {
		t.Fatal(err)
	}
	oldHash, err := fileHash(oldTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{{SourceID: "home", Source: "bin/tool", Target: oldTarget, Hash: oldHash, Type: "file"}}}); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(sourceStatePath(paths, "home"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := syncCommand(SyncOptions{Refresh: true, DryRun: true}, &out); err != nil {
		t.Fatal(err)
	}
	requireOutput(t, out.String(), "not managed by cfgraft yet", "cfgraft sync --force")
	if strings.Contains(out.String(), newTarget) {
		t.Fatalf("unaccepted config was followed: %s", &out)
	}
	out.Reset()
	if err := syncCommand(SyncOptions{Refresh: true, DryRun: true, Force: true}, &out); err != nil {
		t.Fatal(err)
	}
	requireOutput(t, out.String(), "planning with updated configuration", "create", newTarget, "stale", oldTarget)
	assertFile(t, paths.Config, string(originalBytes))
	assertFile(t, sourceStatePath(paths, "home"), string(stateBefore))
	if _, err := os.Stat(newTarget); !os.IsNotExist(err) {
		t.Fatalf("dry-run created target: %v", err)
	}
	out.Reset()
	if err := syncCommand(SyncOptions{Refresh: true, Force: true}, &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, paths.Config, string(data))
	assertFile(t, newTarget, "repo-v1\n")
	assertFile(t, oldTarget, "old content")
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 2 {
		t.Fatalf("expected only config and added file in state: %#v", state)
	}
	for _, file := range state.Files {
		if file.Target == oldTarget {
			t.Fatalf("stale state survived: %#v", state)
		}
	}
	out.Reset()
	if err := syncCommand(SyncOptions{Refresh: true}, &out); err != nil {
		t.Fatal(err)
	}
	requireOutput(t, out.String(), "no changes")
	if strings.Contains(out.String(), "create") || strings.Contains(out.String(), "stale") {
		t.Fatalf("second sync still needed work: %s", &out)
	}
}

func TestSelfManagedConfigValidationAndCycles(t *testing.T) {
	for _, name := range []string{"invalid", "cycle", "removes self mapping"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			configTarget := filepath.Join(home, ".config", "cfgraft", "config.yaml")
			cfg := Config{Sources: map[string]Source{"home": {Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo", Mappings: []Mapping{{Source: "a.yaml", Target: configTarget}}}}}
			paths := writeCachedConfigForTest(t, home, cfg)
			initial, err := os.ReadFile(paths.Config)
			if err != nil {
				t.Fatal(err)
			}
			var replacement []byte
			switch name {
			case "invalid":
				replacement = []byte("sources: invalid\n")
			case "cycle":
				next := cloneConfig(cfg)
				src := next.Sources["home"]
				src.Mappings[0].Source = "b.yaml"
				next.Sources["home"] = src
				replacement, err = yaml.Marshal(next)
				if err != nil {
					t.Fatal(err)
				}
				writeCacheFile(t, paths, "home-repo", "b.yaml", string(initial))
			default:
				replacement = []byte("sources: {}\n")
			}
			writeCacheFile(t, paths, "home-repo", "a.yaml", string(replacement))
			for _, dryRun := range []bool{true, false} {
				var out bytes.Buffer
				err := syncCommand(SyncOptions{Force: true, DryRun: dryRun}, &out)
				if name == "removes self mapping" {
					if err != nil {
						t.Fatal(err)
					}
					if dryRun {
						assertFile(t, paths.Config, string(initial))
					} else {
						assertFile(t, paths.Config, string(replacement))
					}
				} else {
					if err == nil {
						t.Fatal("expected invalid replacement or cycle error")
					}
					if name == "cycle" {
						requireOutput(t, err.Error(), "configuration cycle", "cfgraft sync")
					} else {
						requireOutput(t, err.Error(), "replacement configuration")
					}
					assertFile(t, paths.Config, string(initial))
				}
			}
		})
	}
}

func TestManagedConfigUpdatePreservesConflictChecks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configTarget := filepath.Join(home, ".config", "cfgraft", "config.yaml")
	target := filepath.Join(home, "added")
	cfg := Config{Sources: map[string]Source{"home": {Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo", Mappings: []Mapping{{Source: "config", Target: filepath.Dir(configTarget)}}}}}
	paths := writeCachedConfigForTest(t, home, cfg)
	initial, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := fileHash(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{{SourceID: "home", Source: "config/config.yaml", Target: configTarget, Hash: hash, Type: "file"}}}); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(sourceStatePath(paths, "home"))
	if err != nil {
		t.Fatal(err)
	}
	src := cfg.Sources["home"]
	src.Mappings = append(src.Mappings, Mapping{Source: "new", Target: target})
	cfg.Sources["home"] = src
	replacement, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeCacheFile(t, paths, "home-repo", "config/config.yaml", string(replacement))
	writeCacheFile(t, paths, "home-repo", "new", "repository\n")
	if err := os.WriteFile(target, []byte("local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = syncSourceCommand("home", SyncOptions{}, &out)
	if err == nil {
		t.Fatal("new mapping conflict was ignored")
	}
	requireOutput(t, out.String(), "planning with updated configuration", target, "not managed by cfgraft yet")
	assertFile(t, paths.Config, string(initial))
	assertFile(t, sourceStatePath(paths, "home"), string(stateBefore))
	assertFile(t, target, "local\n")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := syncSourceCommand("home", SyncOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, paths.Config, string(replacement))
	assertFile(t, target, "repository\n")
	out.Reset()
	if err := syncSourceCommand("home", SyncOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	requireOutput(t, out.String(), "no changes")
}

func TestSyncRefusesToDeleteActiveConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configTarget := filepath.Join(home, ".config", "cfgraft", "config.yaml")
	cfg := Config{Sources: map[string]Source{"home": {Repo: "https://example.invalid/home.git", Ref: Ref{Type: "branch", Name: "main"}, LocalID: "home-repo", Mappings: []Mapping{{Source: "config", Target: filepath.Dir(configTarget)}}}}}
	paths := writeCachedConfigForTest(t, home, cfg)
	hash, err := fileHash(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(paths, State{Files: []StateFile{{SourceID: "home", Source: "config/config.yaml", Target: configTarget, Hash: hash, Type: "file"}}}); err != nil {
		t.Fatal(err)
	}
	writeCacheFile(t, paths, "home-repo", "config/other", "other")
	var out bytes.Buffer
	err = syncCommand(SyncOptions{Force: true}, &out)
	if err == nil {
		t.Fatal("expected refusal to delete active config")
	}
	requireOutput(t, err.Error(), "refusing to delete active configuration", "keep its source file or remove the mapping")
	if _, err := os.Stat(paths.Config); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.Base, "other")); !os.IsNotExist(err) {
		t.Fatalf("wrote a destination before refusing config deletion: %v", err)
	}
}
