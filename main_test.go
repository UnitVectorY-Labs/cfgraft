package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
