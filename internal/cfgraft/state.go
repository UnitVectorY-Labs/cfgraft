package cfgraft

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func loadState(paths Paths) (State, error) {
	var state State
	byKey := make(map[string]StateFile)

	legacy, err := readStateFile(paths.State)
	if err != nil {
		return state, err
	}
	for _, f := range legacy.Files {
		byKey[stateKey(f.SourceID, f.Source, f.Target)] = f
	}

	entries, err := os.ReadDir(paths.StateDir)
	if errors.Is(err, os.ErrNotExist) {
		return stateFromMap(byKey), nil
	}
	if err != nil {
		return state, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		sourceState, err := readStateFile(filepath.Join(paths.StateDir, entry.Name()))
		if err != nil {
			return state, err
		}
		for _, f := range sourceState.Files {
			byKey[stateKey(f.SourceID, f.Source, f.Target)] = f
		}
	}
	return stateFromMap(byKey), nil
}

func readStateFile(path string) (State, error) {
	var state State
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := yaml.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func writeState(paths Paths, state State) error {
	if err := ensureLayout(paths); err != nil {
		return err
	}
	bySource := make(map[string][]StateFile)
	for _, file := range state.Files {
		bySource[file.SourceID] = append(bySource[file.SourceID], file)
	}
	entries, err := os.ReadDir(paths.StateDir)
	if err != nil {
		return err
	}
	keep := make(map[string]bool)
	for sourceID, files := range bySource {
		name := safeName(sourceID) + ".yaml"
		keep[name] = true
		sortStateFiles(files)
		data, err := yaml.Marshal(State{Files: files})
		if err != nil {
			return err
		}
		if err := os.WriteFile(sourceStatePath(paths, sourceID), data, 0o644); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") || keep[entry.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(paths.StateDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(paths.State); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func stateFromMap(files map[string]StateFile) State {
	state := State{Files: make([]StateFile, 0, len(files))}
	for _, file := range files {
		state.Files = append(state.Files, file)
	}
	sortStateFiles(state.Files)
	return state
}

func sortStateFiles(files []StateFile) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].SourceID == files[j].SourceID {
			if files[i].Target == files[j].Target {
				return files[i].Source < files[j].Source
			}
			return files[i].Target < files[j].Target
		}
		return files[i].SourceID < files[j].SourceID
	})
}

func sourceStatePath(paths Paths, sourceID string) string {
	return filepath.Join(paths.StateDir, safeName(sourceID)+".yaml")
}
