package cfgraft

import "io/fs"

const (
	configFileName = "config.yaml"
	stateDirName   = "state"
)

type Config struct {
	Sources map[string]Source `yaml:"sources"`
}

type Source struct {
	Repo      string    `yaml:"repo"`
	Condition string    `yaml:"condition,omitempty"`
	Ref       Ref       `yaml:"ref"`
	LocalID   string    `yaml:"local_id,omitempty"`
	Mappings  []Mapping `yaml:"mappings"`
}

type Ref struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
}

type Mapping struct {
	Source    string `yaml:"source"`
	Target    string `yaml:"target"`
	Condition string `yaml:"condition,omitempty"`
}

type State struct {
	Files []StateFile `yaml:"files"`
}

type StateFile struct {
	SourceID string `yaml:"source_id"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	Hash     string `yaml:"hash"`
	Type     string `yaml:"type"`
	Mode     uint32 `yaml:"mode,omitempty"`
}

type Paths struct {
	Base     string
	Config   string
	Repos    string
	StateDir string
}

type SyncOptions struct {
	Force   bool
	DryRun  bool
	Verbose bool
	Refresh bool
}

type PlannedOp struct {
	Kind      string
	SourceID  string
	SourceRel string
	SourceAbs string
	Target    string
	Hash      string
	Mode      fs.FileMode
	OldHash   string
	Reason    string
	Binary    bool
}

type Plan struct {
	Ops       []PlannedOp
	Conflicts []PlannedOp
	Warnings  []string
	Stale     []StateFile
}
