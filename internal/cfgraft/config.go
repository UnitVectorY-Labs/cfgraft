package cfgraft

import (
	"bytes"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	cfg, err = parseConfig(data, paths)
	if err != nil {
		return cfg, err
	}
	return activeConfig(cfg, paths)
}

var conditionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func conditionEnabled(paths Paths, name string) (bool, error) {
	if name == "" {
		return true, nil
	}
	path := filepath.Join(paths.Base, "condition-"+name)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check condition %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("condition %q: %s is not a regular file", name, path)
	}
	return true, nil
}

func activeConfig(cfg Config, paths Paths) (Config, error) {
	active := Config{Sources: make(map[string]Source)}
	for id, src := range cfg.Sources {
		enabled, err := conditionEnabled(paths, src.Condition)
		if err != nil {
			return Config{}, err
		}
		if !enabled {
			continue
		}
		mappings := make([]Mapping, 0, len(src.Mappings))
		for _, m := range src.Mappings {
			enabled, err := conditionEnabled(paths, m.Condition)
			if err != nil {
				return Config{}, err
			}
			if enabled {
				mappings = append(mappings, m)
			}
		}
		src.Mappings = mappings
		active.Sources[id] = src
	}
	return active, nil
}

func readConfig(paths Paths) (Config, error) {
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		return Config{}, err
	}
	return parseConfig(data, paths)
}

func parseConfig(data []byte, paths Paths) (Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
		return cfg, err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return cfg, err
		}
		return cfg, errors.New("multiple YAML documents are not supported")
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
		if src.Condition != "" && !conditionName.MatchString(src.Condition) {
			return fmt.Errorf("source %q has invalid condition %q", id, src.Condition)
		}
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
			if m.Condition != "" && !conditionName.MatchString(m.Condition) {
				return fmt.Errorf("source %q mapping has invalid condition %q", id, m.Condition)
			}
			if strings.TrimSpace(m.Source) == "" {
				return fmt.Errorf("source %q has mapping with empty source path", id)
			}
			cleanSource := filepath.Clean(m.Source)
			if filepath.IsAbs(cleanSource) || cleanSource == ".." || strings.HasPrefix(cleanSource, ".."+string(filepath.Separator)) {
				return fmt.Errorf("source %q mapping source %q escapes repository root", id, m.Source)
			}
			if _, err := resolveTarget(paths, m.Target); err != nil {
				return fmt.Errorf("source %q mapping target %q: %w", id, m.Target, err)
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

// resolveTarget expands only a leading home-directory marker. The configuration
// remains unexpanded so it can be shared and edited across machines.
func resolveTarget(paths Paths, target string) (string, error) {
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	if target != "~" && !strings.HasPrefix(target, "~/") {
		return "", errors.New("is not absolute or a home path")
	}
	homeFile := filepath.Join(paths.Base, "home")
	data, err := os.ReadFile(homeFile)
	var home string
	if errors.Is(err, os.ErrNotExist) {
		home, err = os.UserHomeDir()
	} else if err == nil {
		home = strings.TrimSpace(string(data))
	}
	if err != nil {
		return "", fmt.Errorf("read home directory from %s: %w", homeFile, err)
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory %q must be absolute (configured in %s)", home, homeFile)
	}
	if target == "~" {
		return filepath.Clean(home), nil
	}
	return filepath.Join(home, target[2:]), nil
}

func resolveConfigTargets(paths Paths, cfg Config) (Config, error) {
	resolved := cloneConfig(cfg)
	for id, src := range resolved.Sources {
		for i := range src.Mappings {
			target, err := resolveTarget(paths, src.Mappings[i].Target)
			if err != nil {
				return Config{}, fmt.Errorf("source %q mapping target %q: %w", id, src.Mappings[i].Target, err)
			}
			src.Mappings[i].Target = target
		}
		resolved.Sources[id] = src
	}
	return resolved, nil
}
