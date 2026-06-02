package cfgraft

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

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
