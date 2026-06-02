package cfgraft

import (
	"os"
	"path/filepath"
)

func cfgPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	base := filepath.Join(home, ".config", "cfgraft")
	return Paths{
		Base:     base,
		Config:   filepath.Join(base, configFileName),
		Repos:    filepath.Join(base, "repos"),
		State:    filepath.Join(base, stateFileName),
		StateDir: filepath.Join(base, stateDirName),
	}, nil
}

func ensureLayout(paths Paths) error {
	if err := os.MkdirAll(paths.Repos, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(paths.StateDir, 0o755)
}
