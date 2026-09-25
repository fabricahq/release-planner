// Package notes checks a release notes file against Release Planner's rules. Each rule is
// named for what it enforces, so adding one is a new entry in Rules, in rules.go, and a test.
package notes

import (
	"fmt"
	"slices"
	"strings"
)

// Change is one pull request or direct commit the release contains. A pull request is
// matched by its number, and a direct commit by its SHA, so titles can be edited.
type Change struct {
	PullRequest int
	SHA         string
}

// Release is what the notes of one release must match.
type Release struct {
	Version  string
	Previous string
	// Repository is the release's GitHub owner/name, for its closing link. When it's empty,
	// the closing link's repository isn't checked.
	Repository string
	// Changes lists every pull request and direct commit from Previous to the release.
	Changes []Change
}

// Finding is one rule the notes break, at a 1-based line, or 0 for the whole file.
type Finding struct {
	Rule    string
	Line    int
	Message string
}

func (f Finding) String() string {
	if f.Line == 0 {
		return fmt.Sprintf("%s: %s", f.Rule, f.Message)
	}
	return fmt.Sprintf("line %d: %s: %s", f.Line, f.Rule, f.Message)
}

// Check returns every finding of every rule not in off.
func Check(text string, r Release, off []string) []Finding {
	doc := parse(text)
	var findings []Finding
	for _, rule := range Rules {
		if slices.Contains(off, rule.ID) {
			continue
		}
		found := rule.check(doc, r)
		slices.SortStableFunc(found, func(a, b Finding) int { return a.Line - b.Line })
		for _, f := range found {
			f.Rule = rule.ID
			findings = append(findings, f)
		}
	}
	return findings
}

// Closing returns the line that ends the notes of version: the comparison with previous at
// github.com/<ownerName>, or for a first release, a link to its source.
func Closing(ownerName, previous, version string) string {
	base := "https://github.com/" + ownerName
	if previous == "" {
		return fmt.Sprintf("This is the first release. Browse the source at [%s](%s/tree/%s).", version, base, version)
	}
	return fmt.Sprintf("**Full Changelog**: %s/compare/%s...%s", base, previous, version)
}

type heading struct {
	line  int
	level int
	text  string
}

type document struct {
	lines    []string
	headings []heading
	// fenced marks the lines inside fenced code blocks, fences included, by 0-based index.
	fenced []bool
}

// parse reads the lines and headings of a notes file, skipping fenced code blocks.
func parse(text string) document {
	doc := document{lines: strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")}
	doc.fenced = make([]bool, len(doc.lines))
	fence := "" // the open fence's delimiter, such as ``` or ~~~~
	for i, line := range doc.lines {
		if fence != "" {
			doc.fenced[i] = true
			// A fence closes with at least as many of its character and nothing else.
			if t := strings.TrimSpace(line); strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = t[:len(t)-len(strings.TrimLeft(t, t[:1]))]
			doc.fenced[i] = true
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		if level >= 1 && level <= 6 && strings.HasPrefix(line[level:], " ") {
			doc.headings = append(doc.headings, heading{line: i + 1, level: level, text: strings.TrimSpace(line[level:])})
		}
	}
	return doc
}

// section returns the 1-based line range of heading i's content, up to the next heading at its
// level or above.
func (doc document) section(i int) (start, end int) {
	h := doc.headings[i]
	end = len(doc.lines)
	for _, next := range doc.headings[i+1:] {
		if next.level <= h.level {
			end = next.line - 1
			break
		}
	}
	return h.line + 1, end
}

func (h heading) String() string { return strings.Repeat("#", h.level) + " " + h.text }
