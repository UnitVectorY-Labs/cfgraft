package cfgraft

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConditionsControlSyncAndState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := createGitRepo(t)
	target := filepath.Join(home, "tool")
	cfg := Config{Sources: map[string]Source{
		"enabled":  {Repo: repo, Ref: Ref{Type: "branch", Name: "main"}, Mappings: []Mapping{{Source: "bin/tool", Target: target, Condition: "linux"}}},
		"disabled": {Repo: filepath.Join(home, "missing-repo"), Ref: Ref{Type: "branch", Name: "main"}, Condition: "mac"},
	}}
	paths := writeCachedConfigForTest(t, home, cfg)
	var out bytes.Buffer
	if err := syncCommand(SyncOptions{Refresh: true}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("disabled mapping created target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.Base, "condition-linux"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syncCommand(SyncOptions{Refresh: true}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(paths.Base, "condition-linux")); err != nil {
		t.Fatal(err)
	}
	if err := syncCommand(SyncOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("disabled mapping removed target: %v", err)
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("state not pruned: %#v", state.Files)
	}
}

func TestConditionValidationAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := Config{Sources: map[string]Source{"test": {Repo: "repo", Ref: Ref{Type: "branch", Name: "main"}, Condition: "mac", Mappings: []Mapping{{Source: "file", Target: "~/file", Condition: "linux"}}}}}
	paths := writeCachedConfigForTest(t, home, cfg)
	if err := os.WriteFile(filepath.Join(paths.Base, "condition-linux"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if err := Run([]string{"validate"}, &out, &errout, "dev"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"condition mac: disabled", "condition linux: enabled", "source disabled"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
	cfg.Sources["test"] = Source{Repo: "repo", Ref: Ref{Type: "branch", Name: "main"}, Condition: "../escape"}
	if err := validateConfig(cfg, paths); err == nil {
		t.Fatal("accepted invalid condition")
	}
	if err := os.Mkdir(filepath.Join(paths.Base, "condition-mac"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(paths); err == nil {
		t.Fatal("accepted directory as marker")
	}
}
