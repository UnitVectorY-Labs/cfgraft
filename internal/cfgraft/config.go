package cfgraft

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func loadConfig(paths Paths) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(paths.Config)
	if errors.Is(err, os.ErrNotExist) {
		cfg.Sources = map[string]Source{}
		if err := ensureLayout(paths); err != nil {
			return cfg, err
		}
		return cfg, writeConfig(paths, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Sources == nil {
		cfg.Sources = map[string]Source{}
	}
	return cfg, validateConfig(cfg, paths)
}

func writeConfig(paths Paths, cfg Config) error {
	if err := ensureLayout(paths); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.Config, data, 0o644)
}

func validateConfig(cfg Config, paths Paths) error {
	for id, src := range cfg.Sources {
		if strings.TrimSpace(id) == "" {
			return errors.New("source identifier must not be empty")
		}
		if strings.TrimSpace(src.Repo) == "" {
			return fmt.Errorf("source %q has no repo", id)
		}
		if src.Ref.Type != "branch" && src.Ref.Type != "tag" && src.Ref.Type != "commit" {
			return fmt.Errorf("source %q ref type must be branch, tag, or commit", id)
		}
		if strings.TrimSpace(src.Ref.Name) == "" {
			return fmt.Errorf("source %q ref name must not be empty", id)
		}
		cache, err := repoCachePath(paths, id, src)
		if err != nil {
			return err
		}
		if !isWithin(paths.Repos, cache) {
			return fmt.Errorf("source %q repository cache escapes repos directory", id)
		}
		for _, m := range src.Mappings {
			if strings.TrimSpace(m.Source) == "" {
				return fmt.Errorf("source %q has mapping with empty source path", id)
			}
			cleanSource := filepath.Clean(m.Source)
			if filepath.IsAbs(cleanSource) || cleanSource == ".." || strings.HasPrefix(cleanSource, ".."+string(filepath.Separator)) {
				return fmt.Errorf("source %q mapping source %q escapes repository root", id, m.Source)
			}
			if !filepath.IsAbs(m.Target) {
				return fmt.Errorf("source %q mapping target %q is not absolute", id, m.Target)
			}
		}
	}
	return nil
}

func destinationOverlapWarnings(cfg Config) []string {
	targets := make([]string, 0)
	for _, src := range cfg.Sources {
		for _, mapping := range src.Mappings {
			if filepath.IsAbs(mapping.Target) {
				targets = append(targets, filepath.Clean(mapping.Target))
			}
		}
	}
	sort.Strings(targets)
	warnings := make([]string, 0)
	for i := 0; i < len(targets); i++ {
		for j := i + 1; j < len(targets); j++ {
			parent, child := targets[i], targets[j]
			if parent != child && !strings.HasPrefix(child, parent+string(filepath.Separator)) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("destination mappings overlap: %s and %s; sync is allowed only when their files do not collide", parent, child))
		}
	}
	return warnings
}
