package generate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/config"
	"go.yaml.in/yaml/v3"
)

// Paths of the generated files, relative to the repository root.
const (
	WorkflowPath    = ".github/workflows/release-planner.yml"
	AgentsSkillPath = ".agents/skills/release/SKILL.md"
	ClaudeSkillPath = ".claude/skills/release/SKILL.md"
	AgentsPath      = "AGENTS.md"
)

// Whole files the tool owns carry one marker line recording the version and a digest of
// everything else in the file. A matching digest proves nobody edited the file since.
var generatedMarker = regexp.MustCompile(`release-planner:generated (\S+) sha256:([0-9a-f]{16})`)

// A managed section inside a file the repository owns is fenced by these comments.
// The digest covers the lines between them.
var (
	beginMarker = regexp.MustCompile(`^<!-- release-planner:begin (\S+) sha256:([0-9a-f]{16}) -->$`)
	endMarker   = regexp.MustCompile(`^<!-- release-planner:end -->$`)
)

const placeholder = "\x00marker\x00"

type wholeFile struct {
	path    string
	comment func(string) string
	render  func(config.Config) string // contains the placeholder line
}

// The skill is written twice: .agents/skills is the cross-agent convention read by Codex,
// Gemini CLI, and VS Code; Claude Code reads .claude/skills.
var wholeFiles = []wholeFile{
	{WorkflowPath, func(s string) string { return "# " + s }, func(c config.Config) string {
		return placeholder + "\n" + render("workflow.yml.tmpl", c, "")
	}},
	{AgentsSkillPath, htmlComment, skill},
	{ClaudeSkillPath, htmlComment, skill},
}

func skill(c config.Config) string { return render("skill.md.tmpl", c, placeholder) }

func htmlComment(s string) string { return "<!-- " + s + " -->" }

func (w wholeFile) desired(c config.Config) string {
	body := w.render(c)
	withoutMarker := strings.Replace(body, placeholder+"\n", "", 1)
	marker := w.comment(fmt.Sprintf("release-planner:generated %s sha256:%s. Do not edit; change %s and run release-planner install.",
		c.Version, digest(withoutMarker), config.File))
	return strings.Replace(body, placeholder, marker, 1)
}

func section(c config.Config) string {
	inner := render("agents.md.tmpl", c, "")
	return fmt.Sprintf("<!-- release-planner:begin %s sha256:%s -->\n%s<!-- release-planner:end -->\n", c.Version, digest(inner), inner)
}

// wholeInfo describes an existing file at a whole-file path.
type wholeInfo struct {
	owned, edited bool
	version       string
}

func inspectWhole(existing string) wholeInfo {
	m := generatedMarker.FindStringSubmatch(existing)
	if m == nil {
		return wholeInfo{}
	}
	var rest strings.Builder
	for _, line := range strings.SplitAfter(existing, "\n") {
		if !generatedMarker.MatchString(line) {
			rest.WriteString(line)
		}
	}
	return wholeInfo{owned: true, edited: digest(rest.String()) != m[2], version: m[1]}
}

// sectionInfo locates the managed section in a file the repository owns.
type sectionInfo struct {
	lines      []string
	begin, end int // -1 when absent
	edited     bool
	version    string
	malformed  string
}

func inspectSection(existing string) sectionInfo {
	s := sectionInfo{lines: strings.SplitAfter(existing, "\n"), begin: -1, end: -1}
	for i, line := range s.lines {
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case beginMarker.MatchString(trimmed):
			if s.begin != -1 {
				s.malformed = "found more than one release-planner section; keep one and rerun"
				return s
			}
			s.begin = i
		case endMarker.MatchString(trimmed):
			if s.end != -1 {
				s.malformed = "found more than one release-planner:end marker; keep one section and rerun"
				return s
			}
			s.end = i
		}
	}
	if (s.begin == -1) != (s.end == -1) || s.end < s.begin {
		s.malformed = "found a release-planner:begin or :end marker without its pair; fix or remove it and rerun"
		return s
	}
	if s.begin != -1 {
		m := beginMarker.FindStringSubmatch(strings.TrimRight(s.lines[s.begin], "\r\n"))
		s.version = m[1]
		s.edited = digest(strings.Join(s.lines[s.begin+1:s.end], "")) != m[2]
	}
	return s
}

func (s sectionInfo) found() bool { return s.begin != -1 }

// replace swaps the section, markers included, keeping every line outside it.
func (s sectionInfo) replace(with string) string {
	return strings.Join(s.lines[:s.begin], "") + with + strings.Join(s.lines[s.end+1:], "")
}

func (s sectionInfo) current() string {
	c := strings.Join(s.lines[s.begin:s.end+1], "")
	if !strings.HasSuffix(c, "\n") {
		c += "\n"
	}
	return c
}

// appendSection adds the section after the repository's own text, separated by one blank line.
func appendSection(existing, with string) string {
	if existing == "" {
		return with
	}
	if !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	if !strings.HasSuffix(existing, "\n\n") {
		existing += "\n"
	}
	return existing + with
}

// Change is one file the tool wrote, or would write.
type Change struct {
	Path, Action, Detail string
	// From is the Release Planner version that generated the previous content, if any.
	From string
}

// Problem is a file the tool must not overwrite, or one that does not match the config.
type Problem struct {
	Path, Reason string
}

// Problems is returned when any file blocks an install or fails a check.
type Problems []Problem

func (p Problems) Error() string {
	lines := make([]string, len(p))
	for i, problem := range p {
		lines[i] = fmt.Sprintf("%s: %s", problem.Path, problem.Reason)
	}
	return strings.Join(lines, "\n")
}

type write struct {
	change  Change
	content string
	remove  bool
}

const (
	foreignFile = "not written by release-planner; move it aside, or rerun with --force to replace it"
	editedFile  = "edited after release-planner wrote it; change " + config.File + " instead, or rerun with --force to replace it"
	editedBlock = "the Releases section was edited after release-planner wrote it; move your text outside the markers, or rerun with --force to replace it"
)

func readFile(root, name string) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	return string(data), err == nil, err
}

// planInstall decides every write without touching the disk.
func planInstall(root string, c config.Config, force bool) ([]write, Problems, error) {
	var writes []write
	var problems Problems
	for _, w := range wholeFiles {
		want := w.desired(c)
		existing, ok, err := readFile(root, w.path)
		if err != nil {
			return nil, nil, err
		}
		change := Change{Path: w.path}
		info := inspectWhole(existing)
		switch {
		case !ok:
			change.Action = "created"
		case !info.owned || info.edited:
			reason := foreignFile
			if info.owned {
				reason = editedFile
			}
			if !force {
				problems = append(problems, Problem{w.path, reason})
				continue
			}
			change.Action, change.Detail = "replaced", "discarded its previous content"
		case existing == want:
			change.Action = "unchanged"
		default:
			change.Action, change.From = "updated", info.version
		}
		writes = append(writes, write{change: change, content: want})
	}

	want := section(c)
	existing, _, err := readFile(root, AgentsPath)
	if err != nil {
		return nil, nil, err
	}
	s := inspectSection(existing)
	change := Change{Path: AgentsPath, Detail: "Releases section"}
	var content string
	switch {
	case s.malformed != "":
		problems = append(problems, Problem{AgentsPath, s.malformed})
	case !s.found():
		change.Action, content = "added", appendSection(existing, want)
	case s.edited && !force:
		problems = append(problems, Problem{AgentsPath, editedBlock})
	case s.edited:
		change.Action, change.Detail, content = "replaced", "Releases section, discarding hand edits", s.replace(want)
	case s.current() == want:
		change.Action, content = "unchanged", existing
	default:
		change.Action, change.From, content = "updated", s.version, s.replace(want)
	}
	if change.Action != "" {
		writes = append(writes, write{change: change, content: content})
	}

	if c.ReleaseChecks.Workflow != "" {
		if problem, err := callableWorkflow(root, c.ReleaseChecks.Workflow); err != nil {
			return nil, nil, err
		} else if problem != "" {
			problems = append(problems, Problem{".github/workflows/" + c.ReleaseChecks.Workflow, problem})
		}
	}

	// The policy belongs to the repository: seed it once, never rewrite or check it.
	policy := config.Policy
	if _, ok, err := readFile(root, policy); err != nil {
		return nil, nil, err
	} else if !ok {
		writes = append(writes, write{change: Change{Path: policy, Action: "created", Detail: "fill in your release policy"}, content: Policy(c)})
	}
	return writes, problems, nil
}

// callableWorkflow checks that release-checks.workflow can be called with the commit to check.
func callableWorkflow(root, name string) (string, error) {
	data, ok, err := readFile(root, ".github/workflows/"+name)
	if err != nil || !ok {
		return "missing; release-checks.workflow in " + config.File + " names it", err
	}
	var wf struct {
		On any `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(data), &wf); err != nil {
		return "", fmt.Errorf(".github/workflows/%s: %v", name, err)
	}
	const need = "add a workflow_call trigger with a string input named ref, and check out that ref"
	on, _ := wf.On.(map[string]any)
	call, found := on["workflow_call"]
	if !found {
		return "cannot be called by the Release workflow; " + need, nil
	}
	callMap, _ := call.(map[string]any)
	inputs, _ := callMap["inputs"].(map[string]any)
	ref, ok := inputs["ref"]
	if !ok {
		return "has no ref input, so it cannot check the release commit; " + need, nil
	}
	if refMap, _ := ref.(map[string]any); refMap["type"] != "string" {
		return "declares ref without type: string, but the Release workflow passes a commit SHA; " + need, nil
	}
	var extra []string
	for name, input := range inputs {
		spec, _ := input.(map[string]any)
		if _, hasDefault := spec["default"]; name != "ref" && spec["required"] == true && !hasDefault {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		slices.Sort(extra)
		return fmt.Sprintf("requires inputs the Release workflow can't supply (%s); give them defaults or make them optional", strings.Join(extra, ", ")), nil
	}
	return "", nil
}

// Install writes or updates every generated file. It changes nothing if any file blocks it.
func Install(root string, c config.Config, force bool) ([]Change, error) {
	writes, problems, err := planInstall(root, c, force)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return apply(root, writes)
}

// Check reports every generated file that differs from what the config produces. It writes nothing.
func Check(root string, c config.Config) error {
	writes, problems, err := planInstall(root, c, false)
	if err != nil {
		return err
	}
	policy := config.Policy
	for _, w := range writes {
		ch := w.change
		switch {
		case ch.Action == "unchanged" || ch.Path == policy:
		case ch.Action == "created" || ch.Action == "added":
			problems = append(problems, Problem{ch.Path, "missing; run release-planner install"})
		case ch.From != "" && ch.From != c.Version:
			problems = append(problems, Problem{ch.Path, fmt.Sprintf("generated by %s, but %s pins %s; run release-planner install", ch.From, config.File, c.Version)})
		default:
			problems = append(problems, Problem{ch.Path, "differs from what " + config.File + " generates; run release-planner install"})
		}
	}
	if len(problems) > 0 {
		return problems
	}
	return nil
}

// Uninstall deletes the files the tool owns and removes its section from AGENTS.md.
func Uninstall(root string, force bool) ([]Change, error) {
	var writes []write
	var problems Problems
	for _, w := range wholeFiles {
		existing, ok, err := readFile(root, w.path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if info := inspectWhole(existing); (!info.owned || info.edited) && !force {
			reason := "not written by release-planner; left in place"
			if info.owned {
				reason = "edited after release-planner wrote it; rerun with --force to delete it anyway"
			}
			problems = append(problems, Problem{w.path, reason})
			continue
		}
		writes = append(writes, write{change: Change{Path: w.path, Action: "deleted"}, remove: true})
	}
	existing, ok, err := readFile(root, AgentsPath)
	if err != nil {
		return nil, err
	}
	if s := inspectSection(existing); ok && s.malformed != "" {
		problems = append(problems, Problem{AgentsPath, s.malformed})
	} else if ok && s.found() {
		if s.edited && !force {
			problems = append(problems, Problem{AgentsPath, editedBlock})
		} else {
			before := strings.Join(s.lines[:s.begin], "")
			after := strings.Join(s.lines[s.end+1:], "")
			// Undo the blank line appendSection added before a trailing section.
			if after == "" && strings.HasSuffix(before, "\n\n") {
				before = strings.TrimSuffix(before, "\n")
			}
			content := before + after
			writes = append(writes, write{change: Change{Path: AgentsPath, Action: "removed", Detail: "Releases section"},
				content: content, remove: strings.TrimSpace(content) == ""})
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return apply(root, writes)
}

func apply(root string, writes []write) ([]Change, error) {
	var changes []Change
	for _, w := range writes {
		file := filepath.Join(root, filepath.FromSlash(w.change.Path))
		switch {
		case w.change.Action == "unchanged":
		case w.remove:
			if err := os.Remove(file); err != nil {
				return changes, err
			}
			removeEmptyParents(root, filepath.Dir(file))
		default:
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				return changes, err
			}
			if err := os.WriteFile(file, []byte(w.content), 0o644); err != nil {
				return changes, err
			}
		}
		changes = append(changes, w.change)
	}
	return changes, nil
}

// removeEmptyParents deletes directories left empty by an uninstall, stopping at the root.
func removeEmptyParents(root, dir string) {
	root = filepath.Clean(root)
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
