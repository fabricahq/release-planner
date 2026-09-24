// Package semver parses and orders release tags of the form vMAJOR.MINOR.PATCH[-PRERELEASE].
package semver

import (
	"cmp"
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

// MaxNumber bounds every numeric part of a version, so parts always fit in an int
// and incrementing one can't overflow.
const MaxNumber = 999_999_999

// Parse accepts a tag such as v1.2.3 or v1.2.3-rc.1. It rejects a numeric part larger
// than MaxNumber, including a numeric prerelease identifier.
func Parse(tag string) (Version, bool) {
	m := pattern.FindStringSubmatch(tag)
	if m == nil {
		return Version{}, false
	}
	var v Version
	for i, part := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, ok := number(m[i+1])
		if !ok {
			return Version{}, false
		}
		*part = n
	}
	if m[4] != "" {
		v.Prerelease = strings.Split(m[4], ".")
		for _, id := range v.Prerelease {
			if isNumeric(id) {
				if _, ok := number(id); !ok {
					return Version{}, false
				}
			}
		}
	}
	return v, true
}

// number parses a decimal part no larger than MaxNumber.
func number(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n <= MaxNumber
}

func isNumeric(id string) bool {
	return id != "" && strings.Trim(id, "0123456789") == ""
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
	for _, c := range []int{cmp.Compare(a.Major, b.Major), cmp.Compare(a.Minor, b.Minor), cmp.Compare(a.Patch, b.Patch)} {
		if c != 0 {
			return c
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
	return cmp.Compare(len(a.Prerelease), len(b.Prerelease))
}

// Numeric identifiers sort before alphanumeric ones and compare numerically. Parse
// bounds numeric identifiers, so they always convert.
func compareIdentifier(a, b string) int {
	an, bn := isNumeric(a), isNumeric(b)
	switch {
	case an && bn:
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return cmp.Compare(x, y)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
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
