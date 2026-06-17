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
		printHelp(stdout)
		return nil
	}
	switch args[0] {
	case "tui":
		if wantsHelp(args[1:]) {
			printTUIHelp(stdout)
			return nil
		}
		return runTUI()
	case "sync":
		if wantsHelp(args[1:]) {
			printSyncHelp(stdout)
			return nil
		}
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
		if wantsHelp(args[1:]) {
			printDiffHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("diff", flag.ContinueOnError)
		fs.SetOutput(stderr)
		verbose := fs.Bool("verbose", false, "show safety information")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return diffCommand(*verbose, stdout)
	case "version":
		if wantsHelp(args[1:]) {
			printVersionHelp(stdout)
			return nil
		}
		fmt.Fprintln(stdout, VersionString(version))
		return nil
	case "--version", "-v":
		fmt.Fprintln(stdout, VersionString(version))
		return nil
	case "help", "--help", "-h":
		printHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, `cfgraft safely synchronizes files from Git repositories.

Usage:
  cfgraft                 show this help
  cfgraft tui             launch the interactive TUI
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
  cfgraft tui
  cfgraft sync --dry-run
  cfgraft sync --interactive
  cfgraft diff --verbose`)
}

func printTUIHelp(w io.Writer) {
	fmt.Fprintln(w, `Launch the interactive cfgraft terminal UI.

Usage:
  cfgraft tui

The TUI manages config.yaml sources and mappings, and can run sync or diff
operations for all sources or a selected source.`)
}

func printSyncHelp(w io.Writer) {
	fmt.Fprintln(w, `Refresh repositories and synchronize configured mappings.

Usage:
  cfgraft sync [flags]

Flags:
  --force        overwrite conflicts with repository content
  --interactive  prompt for each conflict
  --dry-run      plan changes without writing destinations or state
  --verbose      show no-op and extra detail

Examples:
  cfgraft sync --dry-run
  cfgraft sync --interactive
  cfgraft sync --force --verbose`)
}

func printDiffHelp(w io.Writer) {
	fmt.Fprintln(w, `Compare cached source content with local destinations.

Usage:
  cfgraft diff [flags]

Flags:
  --verbose      show safety information for conflicts

Examples:
  cfgraft diff
  cfgraft diff --verbose`)
}

func printVersionHelp(w io.Writer) {
	fmt.Fprintln(w, `Print cfgraft version information.

Usage:
  cfgraft version
  cfgraft --version
  cfgraft -v`)
}
