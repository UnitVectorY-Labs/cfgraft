package cfgraft

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
