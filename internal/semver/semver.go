// Package semver parses and orders release tags of the form vMAJOR.MINOR.PATCH[-PRERELEASE].
package semver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Build metadata is deliberately unsupported: a release tag names one version, and the
// approved commit is recorded by the tag itself.
var pattern = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?$`)

// Version is a parsed release tag.
type Version struct {
	Major, Minor, Patch int
	Prerelease          []string
}

// Parse accepts a tag such as v1.2.3 or v1.2.3-rc.1.
func Parse(tag string) (Version, bool) {
	m := pattern.FindStringSubmatch(tag)
	if m == nil {
		return Version{}, false
	}
	var v Version
	// The pattern guarantees these are small decimal integers without leading zeros.
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.Prerelease = strings.Split(m[4], ".")
	}
	return v, true
}

// MustParse is for literals in tests and templates.
func MustParse(tag string) Version {
	v, ok := Parse(tag)
	if !ok {
		panic(fmt.Sprintf("invalid version %q", tag))
	}
	return v
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Prerelease) > 0 {
		s += "-" + strings.Join(v.Prerelease, ".")
	}
	return s
}

// IsPrerelease reports whether the version has a prerelease suffix.
func (v Version) IsPrerelease() bool { return len(v.Prerelease) > 0 }

// Compare orders versions by SemVer 2.0.0 precedence and returns -1, 0, or 1.
func Compare(a, b Version) int {
	for _, d := range []int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d != 0 {
			return sign(d)
		}
	}
	// A release sorts after all of its prereleases.
	switch {
	case len(a.Prerelease) == 0 && len(b.Prerelease) == 0:
		return 0
	case len(a.Prerelease) == 0:
		return 1
	case len(b.Prerelease) == 0:
		return -1
	}
	for i := 0; i < len(a.Prerelease) && i < len(b.Prerelease); i++ {
		if c := compareIdentifier(a.Prerelease[i], b.Prerelease[i]); c != 0 {
			return c
		}
	}
	return sign(len(a.Prerelease) - len(b.Prerelease))
}

// Numeric identifiers sort before alphanumeric ones and compare numerically.
func compareIdentifier(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		return sign(an - bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func sign(d int) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}

// Candidates returns the next patch, minor, and major versions after a released version.
// A prerelease's own core version is the next stable release in its line.
func Candidates(prev Version) (patch, minor, major Version) {
	pre := prev.IsPrerelease()
	patch = Version{Major: prev.Major, Minor: prev.Minor, Patch: prev.Patch + 1}
	minor = Version{Major: prev.Major, Minor: prev.Minor + 1}
	major = Version{Major: prev.Major + 1}
	if pre {
		patch.Patch = prev.Patch
		if prev.Patch == 0 {
			minor.Minor = prev.Minor
			if prev.Minor == 0 {
				major.Major = prev.Major
			}
		}
	}
	return patch, minor, major
}
