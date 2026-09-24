// Package buildinfo reports which Release Planner version is running.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is the module version `go run module@version` built, or "(devel)" for a local build.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// Local reports whether this binary was built from a checkout, where Go stamps version
// control details, rather than by go run or go install at a module version.
func Local() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return true
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return true
		}
	}
	return info.Main.Version == "" || info.Main.Version == "(devel)"
}

// Matches reports whether the running build is the version a config pins. A commit pin
// matches the pseudo-version Go builds for it.
func Matches(pin, running string) bool {
	switch {
	case running == pin:
		return true
	case len(pin) == 40:
		return strings.HasSuffix(running, "-"+pin[:12])
	}
	return false
}
