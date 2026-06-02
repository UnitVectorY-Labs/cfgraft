package cfgraft

import (
	"flag"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"
)

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

func normalizedVersion(version string) string {
	if semverRe.MatchString(version) && !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func VersionString(version string) string {
	return fmt.Sprintf("cfgraft version %s (%s, %s/%s)", normalizedVersion(version), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func Run(args []string, stdout, stderr io.Writer, version string) error {
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
		fmt.Fprintln(stdout, VersionString(version))
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
  --verbose      show detailed planning output

Environment:
  NO_COLOR       disable colored output

Examples:
  cfgraft
  cfgraft sync --dry-run
  cfgraft sync --interactive
  cfgraft diff --verbose`)
}
