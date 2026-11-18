package cfgraft

import (
	"fmt"
	"io"

	"charm.land/lipgloss/v2"
)

func printPlan(plan Plan, opts SyncOptions, out io.Writer) {
	for _, warning := range plan.Warnings {
		fmt.Fprintf(out, "%s %s\n", planLabel("warning", warningStyle), warning)
	}
	for _, op := range plan.Ops {
		if op.Kind == "noop" && !opts.Verbose {
			continue
		}
		fmt.Fprintf(out, "%s %s", planLabel(op.Kind, operationStyle(op.Kind)), op.Target)
		if op.Reason != "" {
			fmt.Fprintf(out, "  %s", styled(subtleStyle, op.Reason))
		}
		fmt.Fprintln(out)
	}
	for _, op := range plan.Conflicts {
		label, reason := "conflict", conflictReason(op)
		if opts.Force {
			label = "force"
			reason = op.Reason + "; --force will overwrite with source content"
			if op.SourceAbs == "" {
				reason = op.Reason + "; --force will delete destination"
			} else if op.Reason == unmanagedDestinationReason {
				reason += " and begin managing this file"
			}
		}
		fmt.Fprintf(out, "%s %s  %s\n", planLabel(label, errorStyle), op.Target, styled(errorStyle, reason))
	}
	for _, f := range plan.Stale {
		fmt.Fprintf(out, "%s %s  %s\n", planLabel("stale", warningStyle), f.Target, styled(subtleStyle, "no longer referenced by config"))
	}
	if len(plan.Ops) == 0 && len(plan.Conflicts) == 0 && len(plan.Stale) == 0 {
		fmt.Fprintln(out, styled(successStyle, "no changes"))
	}
	if opts.DryRun {
		fmt.Fprintln(out, "dry-run: no destination files or state were changed")
		if len(plan.Conflicts) > 0 && !opts.Force {
			fmt.Fprintln(out, "next: preview conflict resolution with cfgraft sync --dry-run --force; apply with cfgraft sync --force")
		} else if opts.Force {
			fmt.Fprintln(out, "next: apply with cfgraft sync --force")
		} else {
			fmt.Fprintln(out, "next: apply with cfgraft sync")
		}
	}
}

func conflictReason(op PlannedOp) string {
	if op.SourceAbs == "" {
		return op.Reason + "; use cfgraft sync --force to delete destination"
	}
	reason := op.Reason + "; use cfgraft sync --force to overwrite with source content"
	if op.Reason == unmanagedDestinationReason {
		reason += " and begin managing this file"
	}
	return reason
}

func planLabel(label string, style lipgloss.Style) string {
	return styled(style, fmt.Sprintf("%-8s", label))
}

func operationStyle(kind string) lipgloss.Style {
	switch kind {
	case "create":
		return successStyle
	case "update", "copy":
		return actionStyle
	case "delete":
		return warningStyle
	case "noop":
		return subtleStyle
	default:
		return actionStyle
	}
}
