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
		fmt.Fprintf(out, "%s %s  %s\n", planLabel("conflict", errorStyle), op.Target, styled(errorStyle, op.Reason))
	}
	for _, f := range plan.Stale {
		fmt.Fprintf(out, "%s %s  %s\n", planLabel("stale", warningStyle), f.Target, styled(subtleStyle, "no longer referenced by config"))
	}
	if len(plan.Ops) == 0 && len(plan.Conflicts) == 0 && len(plan.Stale) == 0 {
		fmt.Fprintln(out, styled(successStyle, "no changes"))
	}
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
