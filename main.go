package main

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

var Version = "dev" // This will be set by the build systems to the release version

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

func normalizedVersion(version string) string {
	if semverRe.MatchString(version) && !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func versionString(version string) string {
	return fmt.Sprintf("ghorgsync version %s (%s, %s/%s)", normalizedVersion(version), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// main is the entry point for the ghorgsync command-line tool.
func main() {
	// Set the build version from the build info if not set by the build system
	if Version == "dev" || Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				Version = bi.Main.Version
			}
		}
	}

	// TODO Implement everything
}
