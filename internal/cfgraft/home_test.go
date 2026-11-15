package cfgraft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTarget(t *testing.T) {
	base := t.TempDir()
	paths := Paths{Base: base}
	t.Setenv("HOME", filepath.Join(base, "default"))
	for _, tc := range []struct{ input, want string }{
		{"~", filepath.Join(base, "default")},
		{"~/.config/test", filepath.Join(base, "default", ".config/test")},
		{"/absolute", "/absolute"},
	} {
		got, err := resolveTarget(paths, tc.input)
		if err != nil || got != tc.want {
			t.Errorf("resolveTarget(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	override := filepath.Join(base, "other home")
	if err := os.WriteFile(filepath.Join(base, "home"), []byte(override+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveTarget(paths, "~/file")
	if err != nil || got != filepath.Join(override, "file") {
		t.Fatalf("override: %q, %v", got, err)
	}
	for _, input := range []string{"relative", "~other/file", "$HOME/file"} {
		if _, err := resolveTarget(paths, input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "home"), []byte("relative\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveTarget(paths, "~/file"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("invalid override: %v", err)
	}
}

func TestHomeMappingPlanAndConfigPreservation(t *testing.T) {
	base := t.TempDir()
	paths := Paths{Base: base, Repos: filepath.Join(base, "repos"), StateDir: filepath.Join(base, "state"), Config: filepath.Join(base, "config.yaml")}
	home := filepath.Join(base, "custom")
	if err := os.WriteFile(filepath.Join(base, "home"), []byte(home+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Sources: map[string]Source{"test": {Repo: "https://example.com/test.git", Ref: Ref{Type: "branch", Name: "main"}, Mappings: []Mapping{{Source: "file", Target: "~/.config/file"}}}}}
	if err := validateConfig(cfg, paths); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Sources["test"].Mappings[0].Target; got != "~/.config/file" {
		t.Fatalf("config target changed: %q", got)
	}
	cache, err := repoCachePath(paths, "test", cfg.Sources["test"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "file"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, state, err := buildPlan(paths, loaded, State{}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config/file")
	if len(plan.Ops) != 1 || plan.Ops[0].Target != want || len(state.Files) != 1 || state.Files[0].Target != want {
		t.Fatalf("plan = %+v, state = %+v", plan, state)
	}
}
