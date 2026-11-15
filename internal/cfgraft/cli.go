package cfgraft

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
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
	stdout = colorprofile.NewWriter(stdout, os.Environ())
	if len(args) == 0 {
		return printHelp(stdout)
	}
	switch args[0] {
	case "help", "--help", "-h":
		if len(args) == 1 {
			return printHelp(stdout)
		}
		if len(args) != 2 {
			return fmt.Errorf("help accepts one command")
		}
		return Run([]string{args[1], "--help"}, stdout, stderr, version)
	case "sync", "diff":
		name := args[0]
		if wantsHelp(args[1:]) {
			printCommandHelp(stdout, name)
			return nil
		}
		fs := flag.NewFlagSet(name, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		opts := SyncOptions{Refresh: true}
		type option struct {
			name, env   string
			dest        *bool
			description string
		}
		options := []option{{"verbose", "CFGRAFT_VERBOSE", &opts.Verbose, "show extra detail"}}
		if name == "sync" {
			options = append(options, option{"force", "CFGRAFT_FORCE", &opts.Force, "overwrite conflicts"}, option{"dry-run", "CFGRAFT_DRY_RUN", &opts.DryRun, "plan without writing destinations or state"})
		}
		// Explicit flags take precedence over environment defaults, including false.
		for _, o := range options {
			fs.BoolVar(o.dest, o.name, false, o.description)
		}
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("%s: unexpected arguments: %s", name, strings.Join(fs.Args(), " "))
		}
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		for _, o := range options {
			if !set[o.name] {
				if v, ok := os.LookupEnv(o.env); ok {
					b, err := strconv.ParseBool(v)
					if err != nil {
						return fmt.Errorf("%s: expected a boolean: %w", o.env, err)
					}
					*o.dest = b
				}
			}
		}
		if name == "sync" {
			return syncCommand(opts, stdout)
		}
		return diffCommand(opts.Verbose, stdout)
	case "config":
		if len(args) == 2 && wantsHelp(args[1:]) {
			printConfigHelp(stdout)
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("config: unexpected arguments")
		}
		printConfigHelp(stdout)
		return nil
	case "validate":
		if len(args) == 2 && wantsHelp(args[1:]) {
			fmt.Fprintln(stdout, "Validate the configuration and list mappings without accessing repositories or writing files.\n\nUsage:\n  cfgraft validate")
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("validate: unexpected arguments")
		}
		paths, err := cfgPaths()
		if err != nil {
			return err
		}
		cfg, err := readConfig(paths)
		if err != nil {
			return fmt.Errorf("validate %s: %w", paths.Config, err)
		}
		fmt.Fprintf(stdout, "%s %s\n", styled(successStyle, "valid   "), paths.Config)
		ids := make([]string, 0, len(cfg.Sources))
		for id := range cfg.Sources {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			src := cfg.Sources[id]
			fmt.Fprintf(stdout, "%s %s  %s  %s:%s\n", styled(actionStyle, "source  "), id, src.Repo, src.Ref.Type, src.Ref.Name)
			for _, mapping := range src.Mappings {
				target, err := resolveTarget(paths, mapping.Target)
				if err != nil {
					return err
				}
				fmt.Fprintf(stdout, "%s %s:%s -> %s\n", styled(actionStyle, "mapping "), id, mapping.Source, target)
			}
		}
		return nil
	case "version", "--version", "-v":
		if len(args) == 2 && wantsHelp(args[1:]) {
			fmt.Fprintln(stdout, "Print version information.\n\nUsage:\n  cfgraft version")
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("version: unexpected arguments")
		}
		fmt.Fprintln(stdout, VersionString(version))
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}
func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}
func heading(s string) string { return styled(actionStyle, s) }
func printHelp(w io.Writer) error {
	paths, err := cfgPaths()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "cfgraft safely synchronizes files from Git repositories.\n\n%s\n  cfgraft <command> [flags]\n\n%s\n  sync       refresh repositories and synchronize mappings\n  diff       compare cached sources with destinations\n  validate   validate configuration and list mappings\n  config     explain the configuration format\n  version    print version information\n  help       show help for a command\n\n%s\n  %s\n\nRun cfgraft help <command> for details. Set NO_COLOR to disable color.\n", heading("Usage:"), heading("Commands:"), heading("Config:"), paths.Config)
	return nil
}
func printCommandHelp(w io.Writer, name string) {
	if name == "sync" {
		fmt.Fprintf(w, "Refresh repositories and synchronize configured mappings.\n\n%s\n  cfgraft sync [flags]\n\n%s\n  --force      overwrite conflicts                  CFGRAFT_FORCE\n  --dry-run    plan without writing files or state  CFGRAFT_DRY_RUN\n  --verbose    show extra detail                    CFGRAFT_VERBOSE\n", heading("Usage:"), heading("Flags (environment variable):"))
	} else {
		fmt.Fprintf(w, "Compare cached sources with local destinations.\n\n%s\n  cfgraft diff [flags]\n\n%s\n  --verbose    show safety information             CFGRAFT_VERBOSE\n", heading("Usage:"), heading("Flags (environment variable):"))
	}
	fmt.Fprintln(w, "\nBoolean environment values accept true or false; flags take precedence.")
}
func printConfigHelp(w io.Writer) {
	fmt.Fprintf(w, "Configuration is hand-edited YAML. Run cfgraft validate to check it.\n\n%s\n  sources:                 # source ID -> settings\n    dotfiles:\n      repo: https://example.com/dotfiles.git\n      ref:\n        type: branch       # branch, tag, or commit\n        name: main\n      # local_id: dotfiles # optional cache path relative to repos directory\n      mappings:\n        - source: shell/zshrc # relative to repository root; . means root\n          target: ~/.zshrc    # absolute path or ~/ path\n\nTargets may also be \"~\" (quoted). ~/.config/cfgraft/home may contain an\nabsolute home override for targets. See docs/CONFIG.md for details.\n", heading("Format:"))
}
