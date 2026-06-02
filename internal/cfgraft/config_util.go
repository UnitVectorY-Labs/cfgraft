package cfgraft

import (
	"path"
	"strconv"
	"strings"
)

func cloneConfig(cfg Config) Config {
	next := Config{Sources: make(map[string]Source, len(cfg.Sources))}
	for id, src := range cfg.Sources {
		copied := Source{
			Repo:    src.Repo,
			Ref:     src.Ref,
			LocalID: src.LocalID,
		}
		if len(src.Mappings) > 0 {
			copied.Mappings = append([]Mapping(nil), src.Mappings...)
		}
		next.Sources[id] = copied
	}
	return next
}

func filterConfig(cfg Config, sourceID string) Config {
	src, ok := cfg.Sources[sourceID]
	if !ok {
		return Config{Sources: map[string]Source{}}
	}
	return Config{Sources: map[string]Source{sourceID: src}}
}

func deriveUniqueSourceID(repo string, cfg Config, currentID string) string {
	base := sourceIDFromRepo(repo)
	if base == "" {
		base = "source"
	}
	if currentID != "" {
		if src, ok := cfg.Sources[currentID]; ok && src.Repo == repo {
			return currentID
		}
	}
	if _, exists := cfg.Sources[base]; !exists || base == currentID {
		return base
	}
	for i := 2; ; i++ {
		candidate := base + "-" + strconv.Itoa(i)
		if _, exists := cfg.Sources[candidate]; !exists || candidate == currentID {
			return candidate
		}
	}
}

func sourceIDFromRepo(repo string) string {
	trimmed := strings.TrimSpace(repo)
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if idx := strings.LastIndex(trimmed, ":"); idx >= 0 && strings.Contains(trimmed[:idx], "@") {
		trimmed = trimmed[idx+1:]
	}
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	base := path.Base(trimmed)
	if base == "." || base == "/" {
		base = ""
	}
	return strings.ToLower(safeName(base))
}
