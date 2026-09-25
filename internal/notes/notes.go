// Package notes checks a release notes file against Release Planner's rules. Each rule is
// named for what it enforces, so adding one is a new entry in Rules and a test.
package notes

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxHeadingLength is the longest a ## or ### heading may be, in characters.
const MaxHeadingLength = 80

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

// Rule checks one property of a notes file.
type Rule struct {
	ID          string
	Description string
	check       func(doc document, r Release) []Finding
}

// Rules are every rule, in the order findings are reported.
var Rules = []Rule{
	{"no-empty-heading", "Every heading has content before the next heading at its level or above.", noEmptyHeading},
	{"no-duplicate-heading", "No ## heading appears twice, and no ### heading appears twice under the same ##.", noDuplicateHeading},
	{"no-long-heading", fmt.Sprintf("## and ### headings are at most %d characters.", MaxHeadingLength), noLongHeading},
	{"require-pull-requests-last", "## Pull Requests appears once and, with an optional ## New Contributors, is last, followed only by the closing line.", requirePullRequestsLast},
	{"list-every-change", "## Pull Requests lists every pull request and direct commit in the release exactly once, and nothing else.", listEveryChange},
	{"require-closing-link", "The notes end with one closing line: the Full Changelog link, or for a first release, the link to its source.", requireClosingLink},
}

// Known reports whether id names a rule.
func Known(id string) bool {
	return slices.ContainsFunc(Rules, func(r Rule) bool { return r.ID == id })
}

// Check returns every finding of every rule not in exclude.
func Check(text string, r Release, exclude []string) []Finding {
	doc := parse(text)
	var findings []Finding
	for _, rule := range Rules {
		if slices.Contains(exclude, rule.ID) {
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
}

// parse reads the lines and headings of a notes file, skipping fenced code blocks.
func parse(text string) document {
	doc := document{lines: strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")}
	fenced := false
	for i, line := range doc.lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
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

// isPullRequests reports whether h is, or was meant to be, the ## Pull Requests heading.
func isPullRequests(h heading) bool {
	return h.level == 2 && strings.HasPrefix(h.text, "Pull Requests")
}

var closingLine = regexp.MustCompile(`^(\*\*Full Changelog\*\*: |This is the first release\. Browse the source at )`)

func noEmptyHeading(doc document, _ Release) []Finding {
	var findings []Finding
	for i, h := range doc.headings {
		start, end := doc.section(i)
		empty := true
		for _, line := range doc.lines[start-1 : end] {
			if strings.TrimSpace(line) != "" {
				empty = false
				break
			}
		}
		if empty {
			findings = append(findings, Finding{Line: h.line, Message: fmt.Sprintf("\"%s\" has no content; fill it in or delete it", h.String())})
		}
	}
	return findings
}

func noDuplicateHeading(doc document, _ Release) []Finding {
	var findings []Finding
	seen := map[string]int{}
	parent := ""
	for _, h := range doc.headings {
		key := ""
		switch h.level {
		case 2:
			parent, key = h.text, h.text
		case 3:
			key = parent + "\x00" + h.text
		default:
			continue
		}
		if first, ok := seen[key]; ok {
			findings = append(findings, Finding{Line: h.line, Message: fmt.Sprintf("\"%s\" already appears on line %d; merge the two sections", h.String(), first)})
			continue
		}
		seen[key] = h.line
	}
	return findings
}

func noLongHeading(doc document, _ Release) []Finding {
	var findings []Finding
	for _, h := range doc.headings {
		if n := utf8.RuneCountInString(h.text); (h.level == 2 || h.level == 3) && n > MaxHeadingLength {
			findings = append(findings, Finding{Line: h.line, Message: fmt.Sprintf("heading is %d characters; keep headings to %d, and move detail into the text below", n, MaxHeadingLength)})
		}
	}
	return findings
}

func requirePullRequestsLast(doc document, _ Release) []Finding {
	var findings []Finding
	first := -1
	for i, h := range doc.headings {
		switch {
		case isPullRequests(h) && first < 0:
			first = i
			if h.text != "Pull Requests" {
				findings = append(findings, Finding{Line: h.line, Message: fmt.Sprintf("name this heading exactly \"## Pull Requests\", not \"%s\"", truncate(h.String()))})
			}
		case isPullRequests(h):
			findings = append(findings, Finding{Line: h.line, Message: fmt.Sprintf("\"%s\" is another Pull Requests section; keep one, after the rest of the notes", truncate(h.String()))})
		case first >= 0 && h.level == 2 && h.text != "New Contributors":
			findings = append(findings, Finding{Line: h.line, Message: fmt.Sprintf("\"%s\" follows ## Pull Requests; only ## New Contributors and the closing line may follow it", truncate(h.String()))})
		}
	}
	if first < 0 {
		return append(findings, Finding{Message: "## Pull Requests is missing; list the release's changes under it, after the rest of the notes"})
	}
	// Once ## New Contributors starts, ## Pull Requests is over.
	contributors := slices.IndexFunc(doc.headings, func(h heading) bool { return h.level == 2 && h.text == "New Contributors" })
	if contributors >= 0 && contributors < first {
		findings = append(findings, Finding{Line: doc.headings[contributors].line, Message: "## New Contributors goes after ## Pull Requests"})
	}
	// The closing line, if any, comes after these sections and is the last line with text.
	last := 0
	for i, line := range doc.lines {
		if strings.TrimSpace(line) != "" {
			last = i + 1
		}
	}
	for i, line := range doc.lines {
		if closingLine.MatchString(line) && (i+1 < doc.headings[first].line || i+1 != last) {
			findings = append(findings, Finding{Line: i + 1, Message: "the closing line must be the last line, after ## Pull Requests and ## New Contributors"})
		}
	}
	return findings
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= MaxHeadingLength {
		return s
	}
	return string([]rune(s)[:MaxHeadingLength]) + "…"
}

// reference matches what an entry links to: #N, …/pull/N, or …/commit/<sha>.
var reference = regexp.MustCompile(`(?:#|/pull/)(\d+)\b|/commit/([0-9a-f]{7,40})\b`)

func listEveryChange(doc document, r Release) []Finding {
	// Only entries under ## Pull Requests count: a (#7) in the prose above doesn't list #7.
	start, end := 0, 0
	for i, h := range doc.headings {
		if h.level == 2 && h.text == "Pull Requests" {
			start, _ = doc.section(i)
			end = len(doc.lines)
			for _, next := range doc.headings[i+1:] {
				if next.level <= 2 {
					end = next.line - 1
					break
				}
			}
			break
		}
	}
	if start == 0 {
		return nil // require-pull-requests-last reports the missing section.
	}
	listedPRs, listedCommits := map[int][]int{}, map[string][]int{}
	var findings []Finding
	for n := start; n <= end; n++ {
		line := strings.TrimSpace(doc.lines[n-1])
		if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		// An entry ends with what it links to; a title may mention other numbers.
		refs := reference.FindAllStringSubmatch(line, -1)
		if len(refs) == 0 {
			continue
		}
		ref := refs[len(refs)-1]
		if ref[1] != "" {
			number, _ := strconv.Atoi(ref[1])
			listedPRs[number] = append(listedPRs[number], n)
			continue
		}
		listedCommits[ref[2]] = append(listedCommits[ref[2]], n)
	}

	wantPRs := map[int]bool{}
	for _, c := range r.Changes {
		switch {
		case c.PullRequest != 0:
			wantPRs[c.PullRequest] = true
			switch lines := listedPRs[c.PullRequest]; len(lines) {
			case 0:
				findings = append(findings, Finding{Message: fmt.Sprintf("pull request #%d isn't listed; add its entry from release-planner inventory", c.PullRequest)})
			case 1:
			default:
				findings = append(findings, Finding{Line: lines[1], Message: fmt.Sprintf("pull request #%d is listed %d times; keep one entry", c.PullRequest, len(lines))})
			}
		default:
			var lines []int
			for sha, at := range listedCommits {
				if strings.HasPrefix(c.SHA, sha) {
					lines = append(lines, at...)
				}
			}
			slices.Sort(lines)
			switch len(lines) {
			case 0:
				findings = append(findings, Finding{Message: fmt.Sprintf("commit %s isn't listed; add its entry from release-planner inventory", short(c.SHA))})
			case 1:
			default:
				findings = append(findings, Finding{Line: lines[1], Message: fmt.Sprintf("commit %s is listed %d times; keep one entry", short(c.SHA), len(lines))})
			}
		}
	}
	for number, lines := range listedPRs {
		if !wantPRs[number] {
			findings = append(findings, Finding{Line: lines[0], Message: fmt.Sprintf("pull request #%d isn't in this release; remove it", number)})
		}
	}
	for sha, lines := range listedCommits {
		if !slices.ContainsFunc(r.Changes, func(c Change) bool { return c.PullRequest == 0 && strings.HasPrefix(c.SHA, sha) }) {
			findings = append(findings, Finding{Line: lines[0], Message: fmt.Sprintf("commit %s isn't a direct commit in this release; remove it", sha)})
		}
	}
	return findings
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func requireClosingLink(doc document, r Release) []Finding {
	want := "**Full Changelog**: "
	suffix := "/compare/" + r.Previous + "..." + r.Version
	if r.Previous == "" {
		want = "This is the first release. Browse the source at [" + r.Version + "]("
		suffix = "/tree/" + r.Version + ")."
	}
	var found, wrong []int
	for i, line := range doc.lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, want) && strings.HasSuffix(line, suffix):
			found = append(found, i+1)
		case closingLine.MatchString(line):
			wrong = append(wrong, i+1)
		}
	}
	var findings []Finding
	for _, n := range wrong {
		findings = append(findings, Finding{Line: n, Message: fmt.Sprintf("this closing line doesn't match %s; copy the closing line from release-planner inventory", r.Version)})
	}
	switch {
	case len(found) == 0 && len(wrong) == 0:
		findings = append(findings, Finding{Message: "the closing line is missing; copy it from release-planner inventory"})
	case len(found) > 1:
		findings = append(findings, Finding{Line: found[1], Message: fmt.Sprintf("the closing line appears %d times; keep one", len(found))})
	}
	return findings
}
