// Package buildinfo reports which Release Planner version is running.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// stamped is set at link time for released binaries:
// -ldflags "-X github.com/fabricahq/release-planner/internal/buildinfo.stamped=v1.2.3"
var stamped string

// Version is the release version of a published binary, the module version that
// `go run module@version` built, or "(devel)" for a local build.
func Version() string {
	if stamped != "" {
		return stamped
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// Local reports whether this binary was built from a checkout, where Go stamps version
// control details, rather than by go run or go install at a module version.
func Local() bool {
	if stamped != "" {
		return false
	}
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
