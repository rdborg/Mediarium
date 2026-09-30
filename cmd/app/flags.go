package main

import (
	"fmt"
	"io"
	"runtime"

	"github.com/rdborg/mediarium/internal/selfupdate"
	semverpkg "github.com/rdborg/mediarium/internal/semver"
)

// handleProbeFlags answers the two flags the container's entrypoint (and the
// app's own update check) use to look at a program file, and reports whether
// it did. They run before anything else and have no side effects: no config
// is read, no folder is created, nothing is opened.
//
//	--version-check          prints "mediarium <version> <os>/<arch>" and exits 0
//	--accepts-update <v>     exits 0 when version v is not older than this program
//	                         (used by the entrypoint to decide whether an installed
//	                         update may replace the one in the image), 1 when it is
//	                         older, 2 when either version is not a version number
func handleProbeFlags(args []string, stdout, stderr io.Writer) (handled bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	switch args[0] {
	case "--version-check":
		fmt.Fprintln(stdout, selfupdate.VersionLine(version))
		return true, 0
	case "--accepts-update":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: app --accepts-update <version>")
			return true, 2
		}
		c, err := semverpkg.Compare(args[1], version)
		if err != nil {
			fmt.Fprintf(stderr, "cannot compare %q with %q: %v\n", args[1], version, err)
			return true, 2
		}
		if c < 0 {
			fmt.Fprintf(stderr, "%s is older than %s (%s/%s)\n", args[1], version, runtime.GOOS, runtime.GOARCH)
			return true, 1
		}
		return true, 0
	}
	return false, 0
}
