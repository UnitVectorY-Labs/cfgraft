package cfgraft

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func syncCommand(opts SyncOptions, out io.Writer) error {
	return syncConfiguredSource("", opts, out)
}

func syncSourceCommand(sourceID string, opts SyncOptions, out io.Writer) error {
	return syncConfiguredSource(sourceID, opts, out)
}

func syncConfiguredSource(sourceID string, opts SyncOptions, out io.Writer) error {
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
	if sourceID != "" {
		if _, ok := cfg.Sources[sourceID]; !ok {
			return fmt.Errorf("source %q does not exist", sourceID)
		}
	}
	state, err := loadState(paths)
	if err != nil {
		return err
	}
	plan, nextState, err := buildSyncPlan(paths, cfg, state, sourceID, opts, out)
	if err != nil {
		return err
	}
	printPlan(plan, opts, out)
	if opts.DryRun {
		return nil
	}
	if len(plan.Conflicts) > 0 && !opts.Force {
		return fmt.Errorf("%d conflict(s); rerun with cfgraft sync --force", len(plan.Conflicts))
	}
	for _, op := range append(plan.Ops, plan.Conflicts...) {
		if err := applyOp(op); err != nil {
			return err
		}
	}
	return writeState(paths, nextState)
}

// Resolve self-managed configuration before writing any destinations. Planning
// against the candidate bytes also lets dry-run show the same mappings as sync.
func buildSyncPlan(paths Paths, cfg Config, state State, sourceID string, opts SyncOptions, out io.Writer) (result Plan, nextState State, resultErr error) {
	// Include matching files while resolving config chains so a cycle back to
	// the on-disk config cannot disappear as an omitted no-op.
	planningOpts := opts
	planningOpts.Verbose = true
	defer func() {
		if !opts.Verbose {
			ops := result.Ops[:0]
			for _, op := range result.Ops {
				if op.Kind != "noop" {
					ops = append(ops, op)
				}
			}
			result.Ops = ops
		}
	}()
	configHash, err := fileHash(paths.Config)
	if err != nil {
		return Plan{}, State{}, err
	}
	seen := map[string]bool{configHash: true}
	type refreshedRepo struct {
		cache, repo string
		ref         Ref
	}
	refreshed := make(map[string]refreshedRepo)
	var pending *PlannedOp
	for pass := 0; pass < 16; pass++ {
		activeCfg := cfg
		if sourceID != "" {
			activeCfg = filterConfig(cfg, sourceID)
		}
		if opts.Refresh {
			for id, src := range activeCfg.Sources {
				cache, err := repoCachePath(paths, id, src)
				if err != nil {
					return Plan{}, State{}, err
				}
				key := refreshedRepo{cache: cache, repo: src.Repo, ref: src.Ref}
				if refreshed[cache] != key {
					if err := refreshRepos(paths, Config{Sources: map[string]Source{id: src}}, opts.Verbose, out); err != nil {
						return Plan{}, State{}, err
					}
					refreshed[cache] = key
				}
			}
		}
		plan, next, err := buildPlanWithReference(paths, activeCfg, cfg, state, planningOpts)
		if err != nil {
			return plan, next, err
		}
		var configOp *PlannedOp
		for _, ops := range [][]PlannedOp{plan.Ops, plan.Conflicts} {
			for i := range ops {
				if filepath.Clean(ops[i].Target) == filepath.Clean(paths.Config) {
					configOp = &ops[i]
				}
			}
		}
		if configOp == nil {
			// A replacement config may remove its own mapping. Still install the
			// accepted replacement, but let the final config determine state.
			if pending != nil {
				plan.Ops = append(plan.Ops, *pending)
			}
			return plan, next, nil
		}
		if configOp.Kind == "noop" && pending == nil {
			return plan, next, nil
		}
		if configOp.Kind == "noop" {
			return Plan{}, State{}, fmt.Errorf("self-managed configuration cycle at %s; fix the repository config mappings before rerunning cfgraft sync", paths.Config)
		}
		if configOp.SourceAbs == "" {
			return Plan{}, State{}, fmt.Errorf("refusing to delete active configuration %s; keep its source file or remove the mapping", paths.Config)
		}
		if configOp.Kind == "conflict" && !opts.Force {
			return plan, next, nil
		}
		data, err := os.ReadFile(configOp.SourceAbs)
		if err != nil {
			return Plan{}, State{}, err
		}
		// Keep the exact bytes used for planning even if a later repository
		// refresh changes the source cache before the plan is applied.
		configOp.Content = data
		if configOp.Hash == configHash {
			return plan, next, nil
		}
		if seen[configOp.Hash] {
			return Plan{}, State{}, fmt.Errorf("self-managed configuration cycle at %s; fix the repository config mappings before rerunning cfgraft sync", paths.Config)
		}
		nextCfg, err := parseConfig(data, paths)
		if err != nil {
			return Plan{}, State{}, fmt.Errorf("replacement configuration %s: %w", configOp.SourceAbs, err)
		}
		nextCfg, err = activeConfig(nextCfg, paths)
		if err != nil {
			return Plan{}, State{}, err
		}
		fmt.Fprintf(out, "config   %s  planning with updated configuration\n", paths.Config)
		pending = configOp
		configHash = configOp.Hash
		seen[configHash] = true
		cfg = nextCfg
	}
	return Plan{}, State{}, fmt.Errorf("self-managed configuration did not stabilize after 16 updates; fix the repository config mappings before rerunning cfgraft sync")
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
