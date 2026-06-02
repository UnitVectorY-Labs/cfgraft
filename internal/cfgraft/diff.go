package cfgraft

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

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
			fmt.Fprintf(out, "%s %s  %s\n", styled(actionStyle, "changed "), op.Target, styled(subtleStyle, "binary files differ"))
			continue
		}
		if err := printUnifiedDiff(op.SourceAbs, op.Target, out); err != nil {
			return err
		}
		if verbose && op.Kind == "conflict" {
			fmt.Fprintf(out, "%s %s  %s\n", styled(warningStyle, "safety  "), op.Target, styled(warningStyle, op.Reason))
		}
	}
	for _, op := range plan.Ops {
		if op.Kind == "delete" {
			changed++
			fmt.Fprintf(out, "%s %s  %s\n", styled(warningStyle, "delete  "), op.Target, styled(subtleStyle, "source removed"))
		}
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(out, "%s %s\n", styled(warningStyle, "warning "), warning)
	}
	if changed == 0 {
		fmt.Fprintln(out, styled(successStyle, "no differences"))
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
			fmt.Fprintf(out, "%s %s  %s\n", styled(actionStyle, "changed "), op.Target, styled(subtleStyle, "binary files differ"))
			continue
		}
		if err := printUnifiedDiff(op.SourceAbs, op.Target, out); err != nil {
			return err
		}
		if verbose && op.Kind == "conflict" {
			fmt.Fprintf(out, "%s %s  %s\n", styled(warningStyle, "safety  "), op.Target, styled(warningStyle, op.Reason))
		}
	}
	for _, op := range plan.Ops {
		if op.Kind == "delete" {
			changed++
			fmt.Fprintf(out, "%s %s  %s\n", styled(warningStyle, "delete  "), op.Target, styled(subtleStyle, "source removed"))
		}
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(out, "%s %s\n", styled(warningStyle, "warning "), warning)
	}
	if changed == 0 {
		fmt.Fprintln(out, styled(successStyle, "no differences"))
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
