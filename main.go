package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
)

var Version = "dev" // This will be set by the build systems to the release version

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

const (
	configFileName = "config.yaml"
	stateFileName  = "state.yaml"
)

type Config struct {
	Sources map[string]Source `yaml:"sources"`
}

type Source struct {
	Repo     string    `yaml:"repo"`
	Ref      Ref       `yaml:"ref"`
	LocalID  string    `yaml:"local_id,omitempty"`
	Mappings []Mapping `yaml:"mappings"`
}

type Ref struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
}

type Mapping struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
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
	Base   string
	Config string
	Repos  string
	State  string
}

type SyncOptions struct {
	Force       bool
	Interactive bool
	DryRun      bool
	Verbose     bool
	Refresh     bool
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

type tuiScreen string

const (
	screenSources  tuiScreen = "sources"
	screenSource   tuiScreen = "source"
	screenMappings tuiScreen = "mappings"
	screenForm     tuiScreen = "form"
	screenConfirm  tuiScreen = "confirm"
	screenOutput   tuiScreen = "output"
)

type tuiFormKind string

const (
	formAddSource    tuiFormKind = "add-source"
	formEditSource   tuiFormKind = "edit-source"
	formAddMapping   tuiFormKind = "add-mapping"
	formEditMapping  tuiFormKind = "edit-mapping"
	confirmRemoveSrc tuiFormKind = "remove-source"
	confirmRemoveMap tuiFormKind = "remove-mapping"
)

type tuiField struct {
	Label string
	Value string
}

func normalizedVersion(version string) string {
	if semverRe.MatchString(version) && !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func versionString(version string) string {
	return fmt.Sprintf("cfgraft version %s (%s, %s/%s)", normalizedVersion(version), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func main() {
	if Version == "dev" || Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				Version = bi.Main.Version
			}
		}
	}
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return runTUI()
	}
	switch args[0] {
	case "sync":
		fs := flag.NewFlagSet("sync", flag.ContinueOnError)
		fs.SetOutput(stderr)
		opts := SyncOptions{Refresh: true}
		fs.BoolVar(&opts.Force, "force", false, "overwrite conflicting destinations with repository content")
		fs.BoolVar(&opts.Interactive, "interactive", false, "resolve conflicts one at a time")
		fs.BoolVar(&opts.DryRun, "dry-run", false, "plan changes without writing destinations or state")
		fs.BoolVar(&opts.Verbose, "verbose", false, "show no-op and extra detail")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return syncCommand(opts, stdout)
	case "diff":
		fs := flag.NewFlagSet("diff", flag.ContinueOnError)
		fs.SetOutput(stderr)
		verbose := fs.Bool("verbose", false, "show safety information")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return diffCommand(*verbose, stdout)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, versionString(Version))
		return nil
	case "help", "--help", "-h":
		printHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, `cfgraft safely synchronizes files from Git repositories.

Usage:
  cfgraft                 launch the interactive TUI
  cfgraft sync [flags]    refresh repositories and synchronize mappings
  cfgraft diff [flags]    compare cached sources with destinations
  cfgraft version         print version

Sync flags:
  --force        overwrite conflicts with repository content
  --interactive  prompt for each conflict
  --dry-run      report planned changes without writing files or state
  --verbose      show detailed planning output`)
}

func cfgPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	base := filepath.Join(home, ".config", "cfgraft")
	return Paths{
		Base:   base,
		Config: filepath.Join(base, configFileName),
		Repos:  filepath.Join(base, "repos"),
		State:  filepath.Join(base, stateFileName),
	}, nil
}

func ensureLayout(paths Paths) error {
	return os.MkdirAll(paths.Repos, 0o755)
}

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

func loadState(paths Paths) (State, error) {
	var state State
	data, err := os.ReadFile(paths.State)
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
	sort.Slice(state.Files, func(i, j int) bool {
		if state.Files[i].Target == state.Files[j].Target {
			return state.Files[i].Source < state.Files[j].Source
		}
		return state.Files[i].Target < state.Files[j].Target
	})
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.State, data, 0o644)
}

func validateConfig(cfg Config, paths Paths) error {
	targets := make([]string, 0)
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
			targets = append(targets, filepath.Clean(m.Target))
		}
	}
	sort.Strings(targets)
	for i := 1; i < len(targets); i++ {
		prev, cur := targets[i-1], targets[i]
		if prev == cur || strings.HasPrefix(cur, prev+string(filepath.Separator)) {
			return fmt.Errorf("destination mappings overlap: %s and %s", prev, cur)
		}
	}
	return nil
}

func repoCachePath(paths Paths, id string, src Source) (string, error) {
	name := src.LocalID
	if name == "" {
		sum := sha256.Sum256([]byte(id + "\x00" + src.Repo))
		name = safeName(id) + "-" + hex.EncodeToString(sum[:])[:12]
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source %q local_id escapes repos directory", id)
	}
	return filepath.Join(paths.Repos, clean), nil
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "source"
	}
	return b.String()
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func syncCommand(opts SyncOptions, out io.Writer) error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	if err := ensureLayout(paths); err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		return err
	}
	state, err := loadState(paths)
	if err != nil {
		return err
	}
	if opts.Refresh {
		if err := refreshRepos(paths, cfg, opts.Verbose, out); err != nil {
			return err
		}
	}
	plan, nextState, err := buildPlan(paths, cfg, state, opts)
	if err != nil {
		return err
	}
	printPlan(plan, opts, out)
	if opts.DryRun {
		return nil
	}
	if len(plan.Conflicts) > 0 && !opts.Force && !opts.Interactive {
		return fmt.Errorf("%d conflict(s); rerun with --force or --interactive", len(plan.Conflicts))
	}
	if opts.Interactive && !opts.Force {
		accepted, err := promptConflicts(plan.Conflicts, out)
		if err != nil {
			return err
		}
		if !accepted {
			return errors.New("unresolved conflicts")
		}
	}
	for _, op := range append(plan.Ops, plan.Conflicts...) {
		if err := applyOp(op); err != nil {
			return err
		}
	}
	return writeState(paths, nextState)
}

func syncSourceCommand(sourceID string, opts SyncOptions, out io.Writer) error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	if err := ensureLayout(paths); err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		return err
	}
	if _, ok := cfg.Sources[sourceID]; !ok {
		return fmt.Errorf("source %q does not exist", sourceID)
	}
	activeCfg := filterConfig(cfg, sourceID)
	state, err := loadState(paths)
	if err != nil {
		return err
	}
	if opts.Refresh {
		if err := refreshRepos(paths, activeCfg, opts.Verbose, out); err != nil {
			return err
		}
	}
	plan, nextState, err := buildPlanWithReference(paths, activeCfg, cfg, state, opts)
	if err != nil {
		return err
	}
	printPlan(plan, opts, out)
	if opts.DryRun {
		return nil
	}
	if len(plan.Conflicts) > 0 && !opts.Force && !opts.Interactive {
		return fmt.Errorf("%d conflict(s); rerun with --force or --interactive", len(plan.Conflicts))
	}
	if opts.Interactive && !opts.Force {
		accepted, err := promptConflicts(plan.Conflicts, out)
		if err != nil {
			return err
		}
		if !accepted {
			return errors.New("unresolved conflicts")
		}
	}
	for _, op := range append(plan.Ops, plan.Conflicts...) {
		if err := applyOp(op); err != nil {
			return err
		}
	}
	return writeState(paths, nextState)
}

func refreshRepos(paths Paths, cfg Config, verbose bool, out io.Writer) error {
	for id, src := range cfg.Sources {
		cache, err := repoCachePath(paths, id, src)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(cache, ".git")); errors.Is(err, os.ErrNotExist) {
			if verbose {
				fmt.Fprintf(out, "clone    %s -> %s\n", id, cache)
			}
			if err := runGit("", "clone", src.Repo, cache); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if verbose {
				fmt.Fprintf(out, "fetch    %s\n", id)
			}
			if err := runGit(cache, "fetch", "--tags", "--prune", "origin"); err != nil {
				return err
			}
		}
		if err := runGit(cache, "reset", "--hard"); err != nil {
			return err
		}
		if err := runGit(cache, "clean", "-fdx"); err != nil {
			return err
		}
		switch src.Ref.Type {
		case "branch":
			if err := runGit(cache, "checkout", "-B", src.Ref.Name, "origin/"+src.Ref.Name); err != nil {
				return err
			}
			if err := runGit(cache, "reset", "--hard", "origin/"+src.Ref.Name); err != nil {
				return err
			}
		case "tag":
			if err := runGit(cache, "checkout", "--detach", "refs/tags/"+src.Ref.Name); err != nil {
				return err
			}
		case "commit":
			if err := runGit(cache, "checkout", "--detach", src.Ref.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func buildPlan(paths Paths, cfg Config, state State, opts SyncOptions) (Plan, State, error) {
	return buildPlanWithReference(paths, cfg, cfg, state, opts)
}

func buildPlanWithReference(paths Paths, activeCfg, referenceCfg Config, state State, opts SyncOptions) (Plan, State, error) {
	var plan Plan
	stateByKey := make(map[string]StateFile)
	active := make(map[string]bool)
	for _, f := range state.Files {
		stateByKey[stateKey(f.SourceID, f.Source, f.Target)] = f
	}
	next := State{}
	for id, src := range activeCfg.Sources {
		cache, err := repoCachePath(paths, id, src)
		if err != nil {
			return plan, next, err
		}
		for _, m := range src.Mappings {
			srcRoot := filepath.Join(cache, filepath.Clean(m.Source))
			info, err := os.Lstat(srcRoot)
			if err != nil {
				return plan, next, fmt.Errorf("mapping %s:%s unavailable in cache: %w", id, m.Source, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("skip symlink source %s:%s", id, m.Source))
				continue
			}
			if info.IsDir() {
				err = filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if path == srcRoot || d.IsDir() {
						return nil
					}
					info, err := d.Info()
					if err != nil {
						return err
					}
					rel, err := filepath.Rel(srcRoot, path)
					if err != nil {
						return err
					}
					sourceRel := filepath.ToSlash(filepath.Join(filepath.Clean(m.Source), rel))
					target := filepath.Join(filepath.Clean(m.Target), rel)
					return planFile(path, id, sourceRel, target, info, stateByKey, active, &plan, &next, opts)
				})
				if err != nil {
					return plan, next, err
				}
				for _, f := range state.Files {
					if f.SourceID == id && pathEqualOrNested(f.Source, filepath.Clean(m.Source)) && pathEqualOrNested(f.Target, filepath.Clean(m.Target)) {
						if !active[stateKey(f.SourceID, f.Source, f.Target)] {
							addDeleteOp(f, &plan, &next, opts)
						}
					}
				}
				continue
			}
			if err := planFile(srcRoot, id, filepath.Clean(m.Source), filepath.Clean(m.Target), info, stateByKey, active, &plan, &next, opts); err != nil {
				return plan, next, err
			}
		}
	}
	for _, f := range state.Files {
		if !active[stateKey(f.SourceID, f.Source, f.Target)] && !stateStillMapped(referenceCfg, f) {
			plan.Stale = append(plan.Stale, f)
			next.Files = append(next.Files, f)
		}
	}
	return plan, next, nil
}

func planFile(sourceAbs, sourceID, sourceRel, target string, info fs.FileInfo, stateByKey map[string]StateFile, active map[string]bool, plan *Plan, next *State, opts SyncOptions) error {
	if info.Mode()&os.ModeSymlink != 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("skip symlink source %s:%s", sourceID, sourceRel))
		return nil
	}
	hash, err := fileHash(sourceAbs)
	if err != nil {
		return err
	}
	key := stateKey(sourceID, sourceRel, target)
	active[key] = true
	record := StateFile{SourceID: sourceID, Source: sourceRel, Target: target, Hash: hash, Type: "file", Mode: uint32(info.Mode().Perm())}
	currentHash, exists, err := existingFileHash(target)
	if err != nil {
		return err
	}
	prev, hadState := stateByKey[key]
	op := PlannedOp{Kind: "copy", SourceID: sourceID, SourceRel: sourceRel, SourceAbs: sourceAbs, Target: target, Hash: hash, Mode: info.Mode().Perm(), OldHash: currentHash}
	switch {
	case !exists:
		op.Kind = "create"
		op.Reason = "missing destination"
		plan.Ops = append(plan.Ops, op)
		next.Files = append(next.Files, record)
	case currentHash == hash:
		if opts.Verbose {
			plan.Ops = append(plan.Ops, PlannedOp{Kind: "noop", SourceID: sourceID, SourceRel: sourceRel, Target: target, Hash: hash, Reason: "already matches source"})
		}
		next.Files = append(next.Files, record)
	case !hadState:
		op.Kind = "conflict"
		op.Reason = "existing destination has no state entry"
		plan.Conflicts = append(plan.Conflicts, op)
		next.Files = append(next.Files, record)
	case currentHash != prev.Hash:
		op.Kind = "conflict"
		op.Reason = "destination drifted from last accepted state"
		plan.Conflicts = append(plan.Conflicts, op)
		next.Files = append(next.Files, record)
	default:
		op.Kind = "update"
		op.Reason = "source changed"
		plan.Ops = append(plan.Ops, op)
		next.Files = append(next.Files, record)
	}
	return nil
}

func addDeleteOp(f StateFile, plan *Plan, next *State, opts SyncOptions) {
	currentHash, exists, err := existingFileHash(f.Target)
	if err != nil {
		plan.Conflicts = append(plan.Conflicts, PlannedOp{Kind: "conflict", SourceID: f.SourceID, SourceRel: f.Source, Target: f.Target, Reason: err.Error()})
		return
	}
	if !exists {
		return
	}
	op := PlannedOp{Kind: "delete", SourceID: f.SourceID, SourceRel: f.Source, Target: f.Target, OldHash: currentHash, Reason: "source removed"}
	if currentHash == f.Hash {
		plan.Ops = append(plan.Ops, op)
		return
	}
	op.Kind = "conflict"
	op.Reason = "managed file removed from source but destination drifted"
	plan.Conflicts = append(plan.Conflicts, op)
	next.Files = append(next.Files, f)
}

func stateStillMapped(cfg Config, f StateFile) bool {
	src, ok := cfg.Sources[f.SourceID]
	if !ok {
		return false
	}
	for _, m := range src.Mappings {
		if pathEqualOrNested(f.Source, filepath.Clean(m.Source)) && pathEqualOrNested(f.Target, filepath.Clean(m.Target)) {
			return true
		}
	}
	return false
}

func pathEqualOrNested(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator)) || strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(root)+"/")
}

func stateKey(sourceID, source, target string) string {
	return sourceID + "\x00" + filepath.ToSlash(filepath.Clean(source)) + "\x00" + filepath.Clean(target)
}

func existingFileHash(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", true, fmt.Errorf("%s is a directory where a file is expected", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", true, fmt.Errorf("%s is a symlink; symlink targets are not managed", path)
	}
	hash, err := fileHash(path)
	return hash, true, err
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func applyOp(op PlannedOp) error {
	switch op.Kind {
	case "create", "update", "copy", "conflict":
		if err := os.MkdirAll(filepath.Dir(op.Target), 0o755); err != nil {
			return err
		}
		src, err := os.Open(op.SourceAbs)
		if err != nil {
			return err
		}
		defer src.Close()
		tmp := op.Target + ".cfgraft.tmp"
		dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, op.Mode.Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			return err
		}
		if err := dst.Close(); err != nil {
			return err
		}
		if err := os.Chmod(tmp, op.Mode.Perm()); err != nil {
			return err
		}
		return os.Rename(tmp, op.Target)
	case "delete":
		return os.Remove(op.Target)
	case "noop":
		return nil
	default:
		return fmt.Errorf("unknown operation %q", op.Kind)
	}
}

func printPlan(plan Plan, opts SyncOptions, out io.Writer) {
	for _, warning := range plan.Warnings {
		fmt.Fprintln(out, "warning ", warning)
	}
	for _, op := range plan.Ops {
		if op.Kind == "noop" && !opts.Verbose {
			continue
		}
		fmt.Fprintf(out, "%-8s %s", op.Kind, op.Target)
		if op.Reason != "" {
			fmt.Fprintf(out, "  %s", op.Reason)
		}
		fmt.Fprintln(out)
	}
	for _, op := range plan.Conflicts {
		fmt.Fprintf(out, "%-8s %s  %s\n", "conflict", op.Target, op.Reason)
	}
	for _, f := range plan.Stale {
		fmt.Fprintf(out, "%-8s %s  no longer referenced by config\n", "stale", f.Target)
	}
	if len(plan.Ops) == 0 && len(plan.Conflicts) == 0 && len(plan.Stale) == 0 {
		fmt.Fprintln(out, "no changes")
	}
}

func promptConflicts(conflicts []PlannedOp, out io.Writer) (bool, error) {
	in := bufio.NewReader(os.Stdin)
	for _, op := range conflicts {
		fmt.Fprintf(out, "overwrite %s with %s:%s? [y/N] ", op.Target, op.SourceID, op.SourceRel)
		answer, err := in.ReadString('\n')
		if err != nil {
			return false, err
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			return false, nil
		}
	}
	return true, nil
}

func diffCommand(verbose bool, out io.Writer) error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		return err
	}
	state, err := loadState(paths)
	if err != nil {
		return err
	}
	opts := SyncOptions{Verbose: verbose}
	plan, _, err := buildPlan(paths, cfg, state, opts)
	if err != nil {
		return err
	}
	changed := 0
	for _, op := range append(plan.Ops, plan.Conflicts...) {
		if op.Kind == "noop" || op.Kind == "delete" {
			continue
		}
		changed++
		if binaryFilesDiffer(op.SourceAbs, op.Target) {
			fmt.Fprintf(out, "changed  %s  binary files differ\n", op.Target)
			continue
		}
		if err := printUnifiedDiff(op.SourceAbs, op.Target, out); err != nil {
			return err
		}
		if verbose && op.Kind == "conflict" {
			fmt.Fprintf(out, "safety   %s  %s\n", op.Target, op.Reason)
		}
	}
	for _, op := range plan.Ops {
		if op.Kind == "delete" {
			changed++
			fmt.Fprintf(out, "delete   %s  source removed\n", op.Target)
		}
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintln(out, "warning ", warning)
	}
	if changed == 0 {
		fmt.Fprintln(out, "no differences")
	}
	return nil
}

func diffSourceCommand(sourceID string, verbose bool, out io.Writer) error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		return err
	}
	if _, ok := cfg.Sources[sourceID]; !ok {
		return fmt.Errorf("source %q does not exist", sourceID)
	}
	activeCfg := filterConfig(cfg, sourceID)
	state, err := loadState(paths)
	if err != nil {
		return err
	}
	opts := SyncOptions{Verbose: verbose}
	plan, _, err := buildPlanWithReference(paths, activeCfg, cfg, state, opts)
	if err != nil {
		return err
	}
	changed := 0
	for _, op := range append(plan.Ops, plan.Conflicts...) {
		if op.Kind == "noop" || op.Kind == "delete" {
			continue
		}
		changed++
		if binaryFilesDiffer(op.SourceAbs, op.Target) {
			fmt.Fprintf(out, "changed  %s  binary files differ\n", op.Target)
			continue
		}
		if err := printUnifiedDiff(op.SourceAbs, op.Target, out); err != nil {
			return err
		}
		if verbose && op.Kind == "conflict" {
			fmt.Fprintf(out, "safety   %s  %s\n", op.Target, op.Reason)
		}
	}
	for _, op := range plan.Ops {
		if op.Kind == "delete" {
			changed++
			fmt.Fprintf(out, "delete   %s  source removed\n", op.Target)
		}
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintln(out, "warning ", warning)
	}
	if changed == 0 {
		fmt.Fprintln(out, "no differences")
	}
	return nil
}

func binaryFilesDiffer(source, target string) bool {
	if source == "" || target == "" {
		return false
	}
	a, errA := os.ReadFile(source)
	b, errB := os.ReadFile(target)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.IndexByte(a, 0) >= 0 || bytes.IndexByte(b, 0) >= 0
}

func printUnifiedDiff(source, target string, out io.Writer) error {
	src, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	dst, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		dst = nil
	} else if err != nil {
		return err
	}
	srcLines := strings.SplitAfter(string(src), "\n")
	dstLines := strings.SplitAfter(string(dst), "\n")
	fmt.Fprintf(out, "--- %s\n+++ %s\n", target, source)
	fmt.Fprintln(out, "@@")
	max := len(srcLines)
	if len(dstLines) > max {
		max = len(dstLines)
	}
	for i := 0; i < max; i++ {
		var oldLine, newLine string
		if i < len(dstLines) {
			oldLine = dstLines[i]
		}
		if i < len(srcLines) {
			newLine = srcLines[i]
		}
		if oldLine == newLine {
			if oldLine != "" {
				fmt.Fprint(out, " "+oldLine)
			}
			continue
		}
		if oldLine != "" {
			fmt.Fprint(out, "-"+oldLine)
			if !strings.HasSuffix(oldLine, "\n") {
				fmt.Fprintln(out)
			}
		}
		if newLine != "" {
			fmt.Fprint(out, "+"+newLine)
			if !strings.HasSuffix(newLine, "\n") {
				fmt.Fprintln(out)
			}
		}
	}
	return nil
}

type tuiModel struct {
	paths          Paths
	config         Config
	err            error
	msg            string
	screen         tuiScreen
	cursor         int
	selectedSource string
	selectedMap    int
	formKind       tuiFormKind
	formTitle      string
	formFields     []tuiField
	formCursor     int
	outputTitle    string
	outputText     string
}

func runTUI() error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(paths)
	model := tuiModel{paths: paths, config: cfg, err: err, screen: screenSources}
	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateKey(msg)
	case tea.MouseMsg:
		return m.updateMouse(msg), nil
	}
	return m, nil
}

func (m tuiModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.screen {
	case screenSources:
		return m.updateSourcesKey(key)
	case screenSource:
		return m.updateSourceKey(key)
	case screenMappings:
		return m.updateMappingsKey(key)
	case screenForm:
		return m.updateFormKey(msg)
	case screenConfirm:
		return m.updateConfirmKey(key)
	case screenOutput:
		if key == "q" || key == "esc" || key == "enter" || key == "b" {
			m.screen = screenSources
			m.cursor = 0
		}
	}
	return m, nil
}

func (m tuiModel) updateSourcesKey(key string) (tea.Model, tea.Cmd) {
	count := len(m.sourceIDs()) + 3
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "up", "shift+tab":
		m.moveCursor(-1, count)
	case "down", "tab":
		m.moveCursor(1, count)
	case "enter":
		ids := m.sourceIDs()
		switch {
		case m.cursor < len(ids):
			m.selectedSource = ids[m.cursor]
			m.cursor = 0
			m.screen = screenSource
		case m.cursor == len(ids):
			m.startAddSource()
		case m.cursor == len(ids)+1:
			m.runAllSync()
		case m.cursor == len(ids)+2:
			m.runAllDiff()
		}
	case "a":
		m.startAddSource()
	case "s":
		m.runAllSync()
	case "d":
		m.runAllDiff()
	case "r":
		m.reloadConfig()
	}
	return m, nil
}

func (m tuiModel) updateSourceKey(key string) (tea.Model, tea.Cmd) {
	if !m.hasSelectedSource() {
		m.screen = screenSources
		m.cursor = 0
		return m, nil
	}
	count := len(m.sourceMenuItems())
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenSources
		m.cursor = 0
	case "up", "shift+tab":
		m.moveCursor(-1, count)
	case "down", "tab":
		m.moveCursor(1, count)
	case "enter":
		switch m.cursor {
		case 0:
			m.screen = screenMappings
			m.cursor = 0
		case 1:
			m.startEditSource()
		case 2:
			m.runSelectedSync()
		case 3:
			m.runSelectedDiff()
		case 4:
			m.startRemoveSource()
		case 5:
			m.screen = screenSources
			m.cursor = 0
		}
	case "m":
		m.screen = screenMappings
		m.cursor = 0
	case "e":
		m.startEditSource()
	case "s":
		m.runSelectedSync()
	case "d":
		m.runSelectedDiff()
	case "x":
		m.startRemoveSource()
	}
	return m, nil
}

func (m tuiModel) updateMappingsKey(key string) (tea.Model, tea.Cmd) {
	if !m.hasSelectedSource() {
		m.screen = screenSources
		m.cursor = 0
		return m, nil
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	count := len(mappings) + 2
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenSource
		m.cursor = 0
	case "up", "shift+tab":
		m.moveCursor(-1, count)
	case "down", "tab":
		m.moveCursor(1, count)
	case "enter":
		switch {
		case m.cursor < len(mappings):
			m.selectedMap = m.cursor
			m.startEditMapping()
		case m.cursor == len(mappings):
			m.startAddMapping()
		case m.cursor == len(mappings)+1:
			m.screen = screenSource
			m.cursor = 0
		}
	case "a":
		m.startAddMapping()
	case "e":
		if m.cursor < len(mappings) {
			m.selectedMap = m.cursor
			m.startEditMapping()
		}
	case "x":
		if m.cursor < len(mappings) {
			m.selectedMap = m.cursor
			m.startRemoveMapping()
		}
	}
	return m, nil
}

func (m tuiModel) updateFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.cancelForm()
	case "up", "shift+tab":
		m.moveFormCursor(-1)
	case "down", "tab":
		m.moveFormCursor(1)
	case "enter":
		m.moveFormCursor(1)
	case "ctrl+s":
		m.submitForm()
	case "backspace", "ctrl+h":
		m.backspaceField()
	case "left", "right", "home", "end":
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.formFields[m.formCursor].Value += string(msg.Runes)
		}
	}
	return m, nil
}

func (m tuiModel) updateConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		m.submitConfirm()
	case "n", "N", "esc", "b":
		m.cancelForm()
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) updateMouse(msg tea.MouseMsg) tuiModel {
	if msg.Type != tea.MouseLeft {
		return m
	}
	idx := int(msg.Y) - m.listStartRow()
	if idx < 0 {
		return m
	}
	switch m.screen {
	case screenSources:
		if idx < len(m.sourceIDs())+3 {
			m.cursor = idx
		}
	case screenSource:
		if idx < len(m.sourceMenuItems()) {
			m.cursor = idx
		}
	case screenMappings:
		if m.hasSelectedSource() && idx < len(m.config.Sources[m.selectedSource].Mappings)+2 {
			m.cursor = idx
		}
	case screenForm:
		if idx >= 0 && idx < len(m.formFields) {
			m.formCursor = idx
		}
	}
	return m
}

func (m tuiModel) View() string {
	var b strings.Builder
	fmt.Fprintln(&b, "cfgraft")
	fmt.Fprintf(&b, "config: %s\n", m.paths.Config)
	if m.err != nil {
		fmt.Fprintf(&b, "error: %v\n", m.err)
	}
	if m.msg != "" {
		fmt.Fprintf(&b, "status: %s\n", m.msg)
	}
	fmt.Fprintln(&b)
	switch m.screen {
	case screenSources:
		m.viewSources(&b)
	case screenSource:
		m.viewSourceMenu(&b)
	case screenMappings:
		m.viewMappings(&b)
	case screenForm:
		m.viewForm(&b)
	case screenConfirm:
		m.viewConfirm(&b)
	case screenOutput:
		m.viewOutput(&b)
	}
	return b.String()
}

func (m tuiModel) viewSources(b *strings.Builder) {
	fmt.Fprintln(b, "Sources")
	fmt.Fprintln(b, "Use arrows/tab, enter to select, a add source, s sync all, d diff all, q quit.")
	fmt.Fprintln(b)
	ids := m.sourceIDs()
	for i, id := range ids {
		src := m.config.Sources[id]
		m.writeRow(b, i, "%s  %s %s  mappings:%d", id, src.Ref.Type, src.Ref.Name, len(src.Mappings))
	}
	m.writeRow(b, len(ids), "+ Add source")
	m.writeRow(b, len(ids)+1, "Sync all sources")
	m.writeRow(b, len(ids)+2, "Diff all sources")
}

func (m tuiModel) viewSourceMenu(b *strings.Builder) {
	if !m.hasSelectedSource() {
		fmt.Fprintln(b, "Selected source no longer exists.")
		return
	}
	src := m.config.Sources[m.selectedSource]
	fmt.Fprintf(b, "Source: %s\n", m.selectedSource)
	fmt.Fprintf(b, "Repo:   %s\n", src.Repo)
	fmt.Fprintf(b, "Ref:    %s %s\n", src.Ref.Type, src.Ref.Name)
	fmt.Fprintf(b, "Maps:   %d\n\n", len(src.Mappings))
	fmt.Fprintln(b, "Use arrows/tab, enter to choose, b back.")
	fmt.Fprintln(b)
	for i, item := range m.sourceMenuItems() {
		m.writeRow(b, i, "%s", item)
	}
}

func (m tuiModel) viewMappings(b *strings.Builder) {
	if !m.hasSelectedSource() {
		fmt.Fprintln(b, "Selected source no longer exists.")
		return
	}
	fmt.Fprintf(b, "Mappings for %s\n", m.selectedSource)
	fmt.Fprintln(b, "Enter edits selected mapping. a add, e edit, x remove, b back.")
	fmt.Fprintln(b)
	mappings := m.config.Sources[m.selectedSource].Mappings
	for i, mapping := range mappings {
		m.writeRow(b, i, "%s -> %s", mapping.Source, mapping.Target)
	}
	m.writeRow(b, len(mappings), "+ Add mapping")
	m.writeRow(b, len(mappings)+1, "Back")
}

func (m tuiModel) viewForm(b *strings.Builder) {
	fmt.Fprintln(b, m.formTitle)
	fmt.Fprintln(b, "Type to edit. Tab moves fields. Ctrl+S saves. Esc cancels.")
	fmt.Fprintln(b)
	for i, field := range m.formFields {
		prefix := "  "
		if i == m.formCursor {
			prefix = "> "
		}
		fmt.Fprintf(b, "%s%s: %s\n", prefix, field.Label, field.Value)
	}
}

func (m tuiModel) viewConfirm(b *strings.Builder) {
	fmt.Fprintln(b, m.formTitle)
	fmt.Fprintln(b)
	for _, field := range m.formFields {
		fmt.Fprintf(b, "%s: %s\n", field.Label, field.Value)
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Press y to confirm or n/esc to cancel.")
}

func (m tuiModel) viewOutput(b *strings.Builder) {
	fmt.Fprintln(b, m.outputTitle)
	fmt.Fprintln(b, "Press enter, esc, or b to return.")
	fmt.Fprintln(b)
	if strings.TrimSpace(m.outputText) == "" {
		fmt.Fprintln(b, "No output.")
		return
	}
	fmt.Fprintln(b, m.outputText)
}

func (m *tuiModel) startAddSource() {
	m.formKind = formAddSource
	m.formTitle = "Add source"
	m.formFields = []tuiField{
		{Label: "ID"},
		{Label: "Git URL"},
		{Label: "Ref type", Value: "branch"},
		{Label: "Ref name", Value: "main"},
	}
	m.formCursor = 0
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startEditSource() {
	if !m.hasSelectedSource() {
		return
	}
	src := m.config.Sources[m.selectedSource]
	m.formKind = formEditSource
	m.formTitle = "Edit source"
	m.formFields = []tuiField{
		{Label: "ID", Value: m.selectedSource},
		{Label: "Git URL", Value: src.Repo},
		{Label: "Ref type", Value: src.Ref.Type},
		{Label: "Ref name", Value: src.Ref.Name},
	}
	m.formCursor = 0
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startAddMapping() {
	m.formKind = formAddMapping
	m.formTitle = "Add mapping"
	m.formFields = []tuiField{
		{Label: "Source path"},
		{Label: "Target path"},
	}
	m.formCursor = 0
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startEditMapping() {
	if !m.hasSelectedSource() {
		return
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	if m.selectedMap < 0 || m.selectedMap >= len(mappings) {
		return
	}
	mapping := mappings[m.selectedMap]
	m.formKind = formEditMapping
	m.formTitle = "Edit mapping"
	m.formFields = []tuiField{
		{Label: "Source path", Value: mapping.Source},
		{Label: "Target path", Value: mapping.Target},
	}
	m.formCursor = 0
	m.screen = screenForm
	m.err = nil
	m.msg = ""
}

func (m *tuiModel) startRemoveSource() {
	if !m.hasSelectedSource() {
		return
	}
	src := m.config.Sources[m.selectedSource]
	m.formKind = confirmRemoveSrc
	m.formTitle = "Remove source from config?"
	m.formFields = []tuiField{
		{Label: "ID", Value: m.selectedSource},
		{Label: "Repo", Value: src.Repo},
		{Label: "Mappings", Value: fmt.Sprintf("%d", len(src.Mappings))},
	}
	m.screen = screenConfirm
}

func (m *tuiModel) startRemoveMapping() {
	if !m.hasSelectedSource() {
		return
	}
	mappings := m.config.Sources[m.selectedSource].Mappings
	if m.selectedMap < 0 || m.selectedMap >= len(mappings) {
		return
	}
	mapping := mappings[m.selectedMap]
	m.formKind = confirmRemoveMap
	m.formTitle = "Remove mapping from config?"
	m.formFields = []tuiField{
		{Label: "Source path", Value: mapping.Source},
		{Label: "Target path", Value: mapping.Target},
	}
	m.screen = screenConfirm
}

func (m *tuiModel) submitForm() {
	switch m.formKind {
	case formAddSource, formEditSource:
		m.submitSourceForm()
	case formAddMapping, formEditMapping:
		m.submitMappingForm()
	}
}

func (m *tuiModel) submitSourceForm() {
	id := strings.TrimSpace(m.formFields[0].Value)
	repo := strings.TrimSpace(m.formFields[1].Value)
	refType := strings.TrimSpace(m.formFields[2].Value)
	refName := strings.TrimSpace(m.formFields[3].Value)
	if id == "" || repo == "" || refType == "" || refName == "" {
		m.err = errors.New("source ID, Git URL, ref type, and ref name are required")
		return
	}
	next := cloneConfig(m.config)
	if m.formKind == formAddSource {
		if _, exists := next.Sources[id]; exists {
			m.err = fmt.Errorf("source %q already exists", id)
			return
		}
		next.Sources[id] = Source{Repo: repo, Ref: Ref{Type: refType, Name: refName}}
	} else {
		src := next.Sources[m.selectedSource]
		src.Repo = repo
		src.Ref = Ref{Type: refType, Name: refName}
		if id != m.selectedSource {
			if _, exists := next.Sources[id]; exists {
				m.err = fmt.Errorf("source %q already exists", id)
				return
			}
			delete(next.Sources, m.selectedSource)
		}
		next.Sources[id] = src
	}
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return
	}
	m.config = next
	m.selectedSource = id
	m.err = nil
	m.msg = "saved source; checking out repository"
	var out bytes.Buffer
	err := refreshRepos(m.paths, filterConfig(m.config, id), true, &out)
	m.outputTitle = "Repository checkout"
	if err != nil {
		m.err = err
		m.outputText = strings.TrimSpace(out.String())
	} else {
		m.outputText = strings.TrimSpace(out.String())
		if m.outputText == "" {
			m.outputText = "Repository cache is ready."
		}
	}
	m.screen = screenOutput
	m.cursor = 0
}

func (m *tuiModel) submitMappingForm() {
	if !m.hasSelectedSource() {
		m.err = errors.New("selected source no longer exists")
		return
	}
	sourcePath := strings.TrimSpace(m.formFields[0].Value)
	targetPath := strings.TrimSpace(m.formFields[1].Value)
	next := cloneConfig(m.config)
	src := next.Sources[m.selectedSource]
	mapping := Mapping{Source: sourcePath, Target: targetPath}
	if m.formKind == formAddMapping {
		src.Mappings = append(src.Mappings, mapping)
	} else {
		if m.selectedMap < 0 || m.selectedMap >= len(src.Mappings) {
			m.err = errors.New("selected mapping no longer exists")
			return
		}
		src.Mappings[m.selectedMap] = mapping
	}
	next.Sources[m.selectedSource] = src
	if err := validateConfig(next, m.paths); err != nil {
		m.err = err
		return
	}
	if err := writeConfig(m.paths, next); err != nil {
		m.err = err
		return
	}
	m.config = next
	m.err = nil
	m.msg = "saved mapping"
	m.screen = screenMappings
	m.cursor = 0
}

func (m *tuiModel) submitConfirm() {
	switch m.formKind {
	case confirmRemoveSrc:
		next := cloneConfig(m.config)
		delete(next.Sources, m.selectedSource)
		if err := validateConfig(next, m.paths); err != nil {
			m.err = err
			return
		}
		if err := writeConfig(m.paths, next); err != nil {
			m.err = err
			return
		}
		m.config = next
		m.selectedSource = ""
		m.err = nil
		m.msg = "removed source from config; local files were left in place"
		m.screen = screenSources
		m.cursor = 0
	case confirmRemoveMap:
		if !m.hasSelectedSource() {
			m.err = errors.New("selected source no longer exists")
			return
		}
		next := cloneConfig(m.config)
		src := next.Sources[m.selectedSource]
		if m.selectedMap < 0 || m.selectedMap >= len(src.Mappings) {
			m.err = errors.New("selected mapping no longer exists")
			return
		}
		src.Mappings = append(src.Mappings[:m.selectedMap], src.Mappings[m.selectedMap+1:]...)
		next.Sources[m.selectedSource] = src
		if err := validateConfig(next, m.paths); err != nil {
			m.err = err
			return
		}
		if err := writeConfig(m.paths, next); err != nil {
			m.err = err
			return
		}
		m.config = next
		m.err = nil
		m.msg = "removed mapping from config; local files were left in place"
		m.screen = screenMappings
		m.cursor = 0
	}
}

func (m *tuiModel) cancelForm() {
	m.err = nil
	switch m.formKind {
	case formAddSource:
		m.screen = screenSources
	case formEditSource, confirmRemoveSrc:
		m.screen = screenSource
	case formAddMapping, formEditMapping, confirmRemoveMap:
		m.screen = screenMappings
	default:
		m.screen = screenSources
	}
	m.cursor = 0
}

func (m *tuiModel) runAllSync() {
	var b bytes.Buffer
	err := syncCommand(SyncOptions{Refresh: true}, &b)
	m.showCommandOutput("Sync all sources", b.String(), err)
}

func (m *tuiModel) runSelectedSync() {
	var b bytes.Buffer
	err := syncSourceCommand(m.selectedSource, SyncOptions{Refresh: true}, &b)
	m.showCommandOutput("Sync "+m.selectedSource, b.String(), err)
}

func (m *tuiModel) runAllDiff() {
	var b bytes.Buffer
	err := diffCommand(false, &b)
	m.showCommandOutput("Diff all sources", b.String(), err)
}

func (m *tuiModel) runSelectedDiff() {
	var b bytes.Buffer
	err := diffSourceCommand(m.selectedSource, false, &b)
	m.showCommandOutput("Diff "+m.selectedSource, b.String(), err)
}

func (m *tuiModel) showCommandOutput(title, text string, err error) {
	m.outputTitle = title
	m.outputText = strings.TrimSpace(text)
	m.err = err
	if err == nil {
		m.msg = "completed"
	} else {
		m.msg = ""
	}
	m.screen = screenOutput
	m.cursor = 0
}

func (m *tuiModel) reloadConfig() {
	cfg, err := loadConfig(m.paths)
	m.config = cfg
	m.err = err
	if err == nil {
		m.msg = "reloaded config"
	}
	m.cursor = 0
}

func (m *tuiModel) moveCursor(delta, count int) {
	if count <= 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = count - 1
	}
	if m.cursor >= count {
		m.cursor = 0
	}
}

func (m *tuiModel) moveFormCursor(delta int) {
	if len(m.formFields) == 0 {
		return
	}
	m.formCursor += delta
	if m.formCursor < 0 {
		m.formCursor = len(m.formFields) - 1
	}
	if m.formCursor >= len(m.formFields) {
		m.formCursor = 0
	}
}

func (m *tuiModel) backspaceField() {
	if len(m.formFields) == 0 {
		return
	}
	value := m.formFields[m.formCursor].Value
	if value == "" {
		return
	}
	runes := []rune(value)
	m.formFields[m.formCursor].Value = string(runes[:len(runes)-1])
}

func (m tuiModel) writeRow(b *strings.Builder, idx int, format string, args ...any) {
	prefix := "  "
	if idx == m.cursor {
		prefix = "> "
	}
	fmt.Fprint(b, prefix)
	fmt.Fprintf(b, format, args...)
	fmt.Fprintln(b)
}

func (m tuiModel) sourceMenuItems() []string {
	return []string{
		"Manage mappings",
		"Edit source",
		"Sync this source",
		"Diff this source",
		"Remove source",
		"Back",
	}
}

func (m tuiModel) hasSelectedSource() bool {
	_, ok := m.config.Sources[m.selectedSource]
	return ok
}

func (m tuiModel) sourceIDs() []string {
	ids := make([]string, 0, len(m.config.Sources))
	for id := range m.config.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m tuiModel) listStartRow() int {
	switch m.screen {
	case screenSources:
		return 5
	case screenSource:
		return 9
	case screenMappings:
		return 5
	case screenForm:
		return 5
	default:
		return 0
	}
}

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
