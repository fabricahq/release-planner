// Command release-planner prepares, validates, and publishes agent-drafted, maintainer-approved releases.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/fabricahq/release-planner/internal/buildinfo"
	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/contributors"
	"github.com/fabricahq/release-planner/internal/generate"
	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/publish"
	"github.com/fabricahq/release-planner/internal/report"
	"github.com/fabricahq/release-planner/internal/semver"
)

const usage = `release-planner prepares, validates, and publishes releases that an agent drafts and a maintainer approves by merging.

Set up a repository:
  init        Create .release-planner/config.yml and a starter release policy
  install     Write or update the generated workflow, agent skill, and AGENTS.md section
  check       Verify that the generated files match .release-planner/config.yml
  uninstall   Remove the generated files and the AGENTS.md section

Prepare a release (agents):
  guide       Print the release procedure to follow
  inventory   List changes since the previous release, with the lines that list them in the notes
  validate    Check a release pull request and its notes, and print what merging publishes

Run in the Release workflow:
  publish     Tag the release commit and publish the approved notes, or edit published notes
  report      Write the release status blocks of the release pull request's description
  downstream  Run workflows in other repositories for a new release

Other:
  version     Print this program's version

Run release-planner <command> -h for a command's options.
`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	commands := map[string]func(context.Context, []string, io.Writer) error{
		"init": cmdInit, "install": cmdInstall, "check": cmdCheck, "uninstall": cmdUninstall,
		"guide": cmdGuide, "inventory": cmdInventory, "validate": cmdValidate,
		"publish": cmdPublish, "report": cmdReport, "downstream": cmdDownstream, "version": cmdVersion,
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err := cmd(ctx, args[1:], stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var problems generate.Problems
		if errors.As(err, &problems) {
			for _, p := range problems {
				fmt.Fprintf(stderr, "%s: %s\n", p.Path, p.Reason)
			}
		} else {
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
		return 1
	}
	return 0
}

func flags(name, synopsis string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: release-planner %s\n\n", synopsis)
		fs.PrintDefaults()
	}
	dir := fs.String("dir", ".", "repository root")
	return fs, dir
}

// loadPinned reads the config and refuses to render files for a different version than it pins.
func loadPinned(dir string) (config.Config, error) {
	c, err := config.Load(dir)
	if err != nil {
		return c, err
	}
	// A build from a checkout is development work on Release Planner itself.
	if running := buildinfo.Version(); !buildinfo.Local() && !buildinfo.Matches(c.Version, running) {
		return c, fmt.Errorf("%s pins %s, but this is %s; install the pinned version:\n  %s", config.File, c.Version, running, generate.InstallCommand(c.Version))
	}
	return c, nil
}

func cmdInit(_ context.Context, args []string, out io.Writer) error {
	fs, dir := flags("init", "init [--version <tag>] [--first-version <version>]")
	version := fs.String("version", "", "Release Planner version to pin (default: the running version)")
	first := fs.String("first-version", "v0.1.0", "the version the repository's first release must use")
	if err := fs.Parse(args); err != nil {
		return err
	}
	file := filepath.Join(*dir, filepath.FromSlash(config.File))
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s already exists; edit it, then run release-planner install", config.File)
	}
	if *version == "" {
		*version = buildinfo.Version()
		if _, ok := semver.Parse(*version); !ok || buildinfo.Local() {
			return fmt.Errorf("this build is %s; pass --version with a Release Planner release tag or full commit SHA", *version)
		}
	}
	content := generate.InitConfig(*version, *first)
	c, err := config.Parse([]byte(content), "")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "created   %s\n", config.File)
	policy := filepath.Join(*dir, filepath.FromSlash(config.Policy))
	if _, err := os.Stat(policy); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(policy, []byte(generate.Policy(c)), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "created   %s\n", config.Policy)
	}
	fmt.Fprintf(out, "Next: fill in %s, review the settings in %s, then run release-planner install.\n", config.Policy, config.File)
	return nil
}

func cmdInstall(_ context.Context, args []string, out io.Writer) error {
	fs, dir := flags("install", "install [--force]")
	force := fs.Bool("force", false, "replace generated files and sections even if they were edited by hand or written by someone else")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := loadPinned(*dir)
	if err != nil {
		return err
	}
	changes, err := generate.Install(*dir, c, *force)
	if err != nil {
		return err
	}
	written := false
	for _, ch := range changes {
		detail := ch.Detail
		if ch.From != "" && ch.From != c.Version {
			detail = strings.TrimPrefix(detail+", ", ", ") + ch.From + " → " + c.Version
		}
		if detail != "" {
			detail = "  (" + detail + ")"
		}
		fmt.Fprintf(out, "%-10s%s%s\n", ch.Action, ch.Path, detail)
		written = written || ch.Action != "unchanged"
	}
	if written {
		fmt.Fprintln(out, "Commit these files. The Release workflow runs release-planner check on every release pull request.")
	} else {
		fmt.Fprintln(out, "Nothing to do.")
	}
	return nil
}

func cmdCheck(_ context.Context, args []string, out io.Writer) error {
	fs, dir := flags("check", "check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := loadPinned(*dir)
	if err != nil {
		return err
	}
	if err := generate.Check(*dir, c); err != nil {
		return err
	}
	fmt.Fprintf(out, "Generated files match %s (release-planner %s).\n", config.File, c.Version)
	return nil
}

func cmdUninstall(_ context.Context, args []string, out io.Writer) error {
	fs, dir := flags("uninstall", "uninstall [--force]")
	force := fs.Bool("force", false, "delete generated files and sections even if they were edited by hand")
	if err := fs.Parse(args); err != nil {
		return err
	}
	changes, err := generate.Uninstall(*dir, *force)
	if err != nil {
		return err
	}
	for _, ch := range changes {
		fmt.Fprintf(out, "%-10s%s\n", ch.Action, ch.Path)
	}
	fmt.Fprintf(out, "Left %s and your release notes in place.\n", config.File)
	return nil
}

func cmdGuide(_ context.Context, args []string, out io.Writer) error {
	fs, dir := flags("guide", "guide [--default-style]")
	defaultStyle := fs.Bool("default-style", false, "print only Release Planner's default release notes style, to start "+config.StyleFile+" from")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *defaultStyle {
		fmt.Fprint(out, generate.DefaultStyle())
		return nil
	}
	c, err := config.Load(*dir)
	if err != nil {
		return err
	}
	fmt.Fprint(out, generate.Guide(c))
	return nil
}

func printJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func cmdInventory(ctx context.Context, args []string, out io.Writer) error {
	fs, dir := flags("inventory", "inventory [--head <ref>] [--repository owner/name] [--offline]")
	head := fs.String("head", "HEAD", "commit the release would tag")
	repository := fs.String("repository", "", "GitHub repository for links and pull request authors, as owner/name (default: from the origin remote)")
	offline := fs.Bool("offline", false, "don't look up pull request authors on GitHub")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*dir)
	if err != nil {
		return err
	}
	repo := gitrepo.Repo{Dir: *dir}
	if *repository != "" && !gitrepo.ValidRepository(*repository) {
		return fmt.Errorf("--repository must be owner/name, not %q", *repository)
	}
	inv, err := plan.Take(ctx, repo, plan.Options{NotesDir: c.NotesDir, FirstVersion: c.FirstVersion}, *head)
	if err != nil {
		return err
	}
	// Without a repository, the history is still useful: the links name a placeholder to
	// replace, and pull request authors are left out.
	known := true
	if *repository == "" {
		if *repository, err = repo.GitHubRepository(ctx); err != nil {
			inv.Warnings = append(inv.Warnings, fmt.Sprintf("no GitHub repository (%v): links name %s, and pull request authors are missing; pass --repository owner/name", err, repositoryPlaceholder))
			*repository, known = repositoryPlaceholder, false
		}
	}
	if known && !*offline && len(inv.PullRequests) > 0 {
		contributors.Add(ctx, api(githubToken(ctx), *repository), &inv)
	}
	inv.AddEntries(*repository)
	return printJSON(out, inv)
}

// repositoryPlaceholder stands in for owner/name in inventory's links when the repository is unknown.
const repositoryPlaceholder = "<owner>/<name>"

// githubToken finds a token for GitHub API reads: GITHUB_TOKEN, GH_TOKEN, or the GitHub CLI's
// login. With none, requests are unauthenticated, which works for public repositories.
func githubToken(ctx context.Context) string {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if token := os.Getenv(name); token != "" {
			return token
		}
	}
	if out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// api returns a GitHub API client for repository, at GITHUB_API_URL or github.com.
func api(token, repository string) *publish.GitHub {
	base := os.Getenv("GITHUB_API_URL")
	if base == "" {
		base = "https://api.github.com"
	}
	return &publish.GitHub{BaseURL: base, Token: token, Repository: repository}
}

func cmdValidate(ctx context.Context, args []string, out io.Writer) error {
	fs, dir := flags("validate", "validate --base <ref> [--head <ref>] [--repository owner/name] [--out <file>] [--ci [--head-repository owner/name]] | validate --ci --merged <sha> [--out <file>] | validate --rules")
	base := fs.String("base", "", "the release branch, such as origin/main; the release commit is the newest commit head shares with it")
	head := fs.String("head", "HEAD", "the release pull request's head")
	merged := fs.String("merged", "", "with --ci, after the merge: the full SHA of the commit the release pull request merged as on the release branch")
	outFile := fs.String("out", "", "write the release plan to this file instead of standard output")
	ci := fs.Bool("ci", false, "running in the Release workflow: write step outputs, check the repository's settings, and report release notes rules as warnings")
	headRepository := fs.String("head-repository", "", "with --ci, the owner/name the pull request comes from; a fork's pull request builds nothing")
	listRules := fs.Bool("rules", false, "list the release notes rules")
	repository := fs.String("repository", "", "GitHub repository the closing link must name, as owner/name (default: GITHUB_REPOSITORY with --ci, otherwise from the origin remote)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repository != "" && !gitrepo.ValidRepository(*repository) {
		return fmt.Errorf("--repository must be owner/name, not %q", *repository)
	}
	if *listRules {
		for _, r := range notes.Rules {
			fmt.Fprintf(out, "%-28s%s\n", r.ID, r.Description)
		}
		return nil
	}
	if (*base == "") == (*merged == "") {
		fs.Usage()
		return fmt.Errorf("pass --base, or --merged with --ci")
	}
	if *merged != "" && !*ci {
		return fmt.Errorf("--merged plans a publication in the Release workflow; pass --ci")
	}
	c, err := config.Load(*dir)
	if err != nil {
		return err
	}
	repo := gitrepo.Repo{Dir: *dir}
	// The closing link must point at this repository: the one given; in the workflow, the one it
	// runs in; locally, origin's, which a fork's clone overrides with --repository. Without any,
	// the link's repository isn't checked.
	switch {
	case *repository != "":
	case *ci:
		*repository = os.Getenv("GITHUB_REPOSITORY")
	default:
		*repository, _ = repo.GitHubRepository(ctx)
	}
	opts := plan.Options{NotesDir: c.NotesDir, FirstVersion: c.FirstVersion, RulesOff: c.RulesOff(), Repository: *repository}
	var p plan.Plan
	if *merged != "" {
		p, err = readMerged(ctx, repo, c, opts, *merged)
		// The report says why the run stopped, so it doesn't read as a failure to fix.
		if w := (plan.WithdrawnError{}); errors.As(err, &w) {
			if err := appendEnvFile("GITHUB_OUTPUT", "withdrawn="+w.Commit+"\n"); err != nil {
				return err
			}
		}
	} else {
		p, err = plan.Read(ctx, repo, opts, *base, *head)
	}
	if err != nil {
		return err
	}
	// Locally, findings fail, so the agent fixes them. In the workflow they're warnings: the
	// maintainer may break a rule on purpose, and merging approves the notes as written.
	if !*ci {
		var lines []string
		for _, f := range allFindings(p) {
			lines = append(lines, fmt.Sprintf("%s: %s", f.file, f))
		}
		if len(lines) > 0 {
			return fmt.Errorf("the notes break release notes rules:\n  %s\nEdit each file in place to fix each one, then rerun release-planner validate", strings.Join(lines, "\n  "))
		}
	}
	if *ci {
		if err := inWorkflow(ctx, out, repo, c, &p, *base, *headRepository, *outFile != ""); err != nil {
			return err
		}
	}
	w := out
	if *outFile != "" {
		f, err := os.Create(*outFile)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	if err := printJSON(w, p); err != nil {
		return err
	}
	if *outFile != "" {
		switch {
		case p.Tag != "":
			fmt.Fprintf(out, "Validated %s at the release commit %s.\n", p.Tag, p.Commit)
		case len(p.Edits) > 0:
			fmt.Fprintf(out, "Validated edits to published notes.\n")
		default:
			fmt.Fprintln(out, "No release requested and no notes edited.")
		}
	}
	return nil
}

type fileFinding struct {
	file string
	notes.Finding
}

// allFindings lists the rules broken by the requested notes and by every edit.
func allFindings(p plan.Plan) []fileFinding {
	var all []fileFinding
	for _, f := range p.Findings {
		all = append(all, fileFinding{p.File, f})
	}
	for _, e := range p.Edits {
		for _, f := range e.Findings {
			all = append(all, fileFinding{e.File, f})
		}
	}
	return all
}

// readMerged plans the publication a release pull request's merge approved. merged is the
// commit it merged as on the release branch, by a merge commit, squash, or rebase. The
// release commit is the newest commit the pull request's head shares with the branch
// before the merge, and the notes merged must be the notes at that head.
func readMerged(ctx context.Context, repo gitrepo.Repo, c config.Config, opts plan.Options, merged string) (plan.Plan, error) {
	empty := plan.Plan{Tags: []string{}}
	if !fullSHA.MatchString(merged) {
		return empty, fmt.Errorf("--merged needs the full SHA of the commit the release pull request merged as, not %q", merged)
	}
	if !repo.IsAncestor(ctx, merged, "origin/"+c.Branch) {
		return empty, fmt.Errorf("%s is not on %s", merged, c.Branch)
	}
	token := os.Getenv("GITHUB_TOKEN")
	pr, err := api(token, opts.Repository).MergedPullRequest(ctx, merged, c.Branch)
	if err != nil {
		return empty, err
	}
	if pr == nil {
		return empty, fmt.Errorf("%s is not the merge of a pull request into %s; a release is approved by merging its pull request, so a direct push publishes nothing", merged, c.Branch)
	}
	// A squashed or rebased pull request's head is on no branch once its branch is deleted.
	if !repo.HasCommit(ctx, pr.Head) {
		if err := repo.FetchCommit(ctx, pr.Head, token); err != nil {
			return empty, fmt.Errorf("fetching pull request #%d's head: %v", pr.Number, err)
		}
	}
	// Plan with the notes directory the pull request merged under: a manual retry runs from the
	// release branch's current checkout, which may have moved the notes since.
	data, _ := repo.Run(ctx, "show", merged+":"+config.File)
	opts.NotesDir = config.NotesDirIn([]byte(data))
	p, err := plan.Read(ctx, repo, opts, merged+"^1", pr.Head)
	if err != nil {
		return empty, fmt.Errorf("pull request #%d: %v", pr.Number, err)
	}
	approved := map[string]string{}
	if p.Tag != "" {
		approved[p.File] = p.Notes
	}
	for _, e := range p.Edits {
		approved[e.File] = e.Notes
	}
	for file, text := range approved {
		if landed, err := repo.Run(ctx, "show", merged+":"+file); err != nil || landed != text {
			return empty, fmt.Errorf("%s on %s differs from %s at pull request #%d's head %s; publish only what the pull request approved by opening a new one", file, c.Branch, file, pr.Number, pr.Head)
		}
	}
	p.PullRequest, p.Merged, p.MergedBy = pr.Number, merged, pr.MergedBy
	tip, err := branchTip(ctx, repo, api(token, opts.Repository), c.Branch, token)
	if err != nil {
		return empty, err
	}
	if err := plan.StillApproved(ctx, repo, c.Branch, tip, p); err != nil {
		return empty, err
	}
	return p, nil
}

// branchTip returns the release branch's current commit, read from GitHub and fetched if the
// checkout lacks it. origin/<branch> won't do: in a re-run, or a run that started before the
// branch moved on, actions/checkout points it at the run's own commit.
func branchTip(ctx context.Context, repo gitrepo.Repo, gh *publish.GitHub, branch, token string) (string, error) {
	tip, err := gh.BranchCommit(ctx, branch)
	if err != nil {
		return "", err
	}
	if !fullSHA.MatchString(tip) {
		return "", fmt.Errorf("%s's commit is %q, not a full commit SHA", branch, tip)
	}
	if !repo.HasCommit(ctx, tip) {
		if err := repo.FetchCommit(ctx, tip, token); err != nil {
			return "", fmt.Errorf("fetching %s's current commit: %v", branch, err)
		}
	}
	return tip, nil
}

// inWorkflow completes the plan in the Release workflow: it decides which run's release
// checks and assets the release uses, checks the repository's settings, flags notes edited on
// GitHub, and writes step outputs, warnings, and the step summary. base is the pull
// request's base commit, or "" after the merge.
func inWorkflow(ctx context.Context, out io.Writer, repo gitrepo.Repo, c config.Config, p *plan.Plan, base, headRepository string, annotate bool) error {
	token, repository := os.Getenv("GITHUB_TOKEN"), os.Getenv("GITHUB_REPOSITORY")
	var gh *publish.GitHub
	if token != "" && repository != "" {
		gh = api(token, repository)
	}
	warn := func(format string, args ...any) { p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...)) }
	runID, _ := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	build := false
	var reused buildRun
	if p.Tag != "" {
		p.BuildRun, build = runID, true
		switch {
		case base != "":
			// A fork's pull request gets a read-only token, so it can't attest what it builds.
			build = headRepository == "" || headRepository == repository
		case gh != nil:
			if run, err := reusableRun(ctx, gh, c, p.Head, repository); err != nil {
				warn("Couldn't look for the pull request's run to reuse its checks and assets, so this run repeats them: %v", err)
			} else if run.id != 0 {
				p.BuildRun, p.Reused, build = run.id, true, false
				reused = run
			}
		}
	}
	if gh != nil && base != "" {
		for i, e := range p.Edits {
			release, err := gh.ReleaseByTag(ctx, e.Tag)
			switch {
			case err != nil:
				warn("Couldn't read the %s release to compare its notes: %v", e.Tag, err)
			case release == nil || release.Draft:
				warn("%s has no published release, so merging can't edit its notes.", e.Tag)
			default:
				previous, err := repo.Run(ctx, "show", base+":"+e.File)
				p.Edits[i].HandEdited = err == nil && normalize(previous) != normalize(release.Body)
			}
		}
	}
	if gh != nil && !p.Empty() {
		environment(ctx, gh, config.ReleaseEnvironment, c.Branch, publish.EnvironmentDocs, warn)
		if p.Tag != "" && len(c.Downstream) > 0 {
			environment(ctx, gh, config.DownstreamEnvironment, c.Branch, publish.DownstreamEnvironmentDocs, warn)
		}
	}
	if gh != nil && p.Tag != "" && c.PrePublish.Enabled() {
		if err := beforePrePublish(ctx, repo, gh, c, *p, base, warn); err != nil {
			return err
		}
	}

	publishes := base == "" && !p.Empty()
	outputs := fmt.Sprintf("tag=%s\nversion=%s\ncommit=%s\npublish=%t\nbuild=%t\nbuild-run=%d\nbuilt-plan-artifact=%s\nbuilt-assets-artifact=%s\n",
		p.Tag, p.Version, p.Commit, publishes, build, p.BuildRun, artifactID(reused.plan), artifactID(reused.assets))
	if err := appendEnvFile("GITHUB_OUTPUT", outputs); err != nil {
		return err
	}
	var summary strings.Builder
	if p.Tag != "" {
		fmt.Fprintf(&summary, "Release %s\n\nRelease commit: `%s`\n\nPull request head: `%s`\n", p.Tag, p.Commit, p.Head)
		if p.Merged != "" {
			fmt.Fprintf(&summary, "\nMerged as: `%s`\n", p.Merged)
		}
		if p.Reused {
			fmt.Fprintf(&summary, "\nReleasing the checks and assets of the pull request's run %d.\n", p.BuildRun)
		}
	}
	for _, e := range p.Edits {
		fmt.Fprintf(&summary, "\nEdits the notes of %s.\n", e.Tag)
	}
	if found := allFindings(*p); len(found) > 0 {
		summary.WriteString("\nRelease notes rules\n\nThe notes break these rules. They don't block the release; fix them if they're mistakes.\n\n")
		for _, f := range found {
			if annotate {
				line := ""
				if f.Line > 0 {
					line = fmt.Sprintf(",line=%d", f.Line)
				}
				fmt.Fprintf(out, "::warning file=%s%s,title=%s::%s\n", escapeProperty(f.file), line, escapeProperty(f.Rule), escapeData(f.Message))
			}
			fmt.Fprintf(&summary, "- %s: %s\n", f.file, f.Finding)
		}
	}
	if len(p.Warnings) > 0 {
		summary.WriteString("\nSettings\n\n")
		for _, w := range p.Warnings {
			if annotate {
				fmt.Fprintf(out, "::warning title=Release settings::%s\n", escapeData(w))
			}
			summary.WriteString("- " + w + "\n")
		}
	}
	return appendEnvFile("GITHUB_STEP_SUMMARY", summary.String())
}

// buildRun is a pull request's run whose release checks and assets the release reuses, with
// the IDs of the plan and assets artifacts it built.
type buildRun struct{ id, plan, assets int64 }

func artifactID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// reusableRun returns the pull request's successful run of this workflow at head, from the
// same repository, whose release plan and any release assets haven't expired: it already
// ran the release checks on the release commit, and built and attested the assets. It
// returns a zero buildRun if there is none.
//
// Any job can upload an artifact under any name, so the run's artifacts must identify what
// it built. Without assets, that's its one release-plan. With assets, its attest job names
// the plan and assets it used in one release-binding-<plan>-<assets> artifact, after every job
// that runs the repository's code; a run with any other binding isn't reused.
func reusableRun(ctx context.Context, gh *publish.GitHub, c config.Config, head, repository string) (buildRun, error) {
	// GITHUB_WORKFLOW_REF is owner/name/.github/workflows/<file>@<ref>.
	ref, _, _ := strings.Cut(os.Getenv("GITHUB_WORKFLOW_REF"), "@")
	if ref == "" {
		return buildRun{}, nil
	}
	runs, err := gh.SuccessfulPullRequestRuns(ctx, path.Base(ref), head)
	if err != nil {
		return buildRun{}, err
	}
	for _, r := range runs {
		// A fork's run built nothing; only this repository's own branches do.
		if r.HeadSHA != head || r.Conclusion != "success" || r.HeadRepository.FullName != repository {
			continue
		}
		artifacts, err := gh.Artifacts(ctx, r.ID)
		if err != nil {
			return buildRun{}, err
		}
		if built, ok := boundArtifacts(artifacts, c.ReleaseAssets.Workflow != ""); ok {
			built.id = r.ID
			return built, nil
		}
	}
	return buildRun{}, nil
}

// boundArtifacts finds the plan and any assets a run built, as reusableRun describes.
func boundArtifacts(artifacts []publish.Artifact, assets bool) (buildRun, bool) {
	named := func(id int64, name string) bool {
		return slices.ContainsFunc(artifacts, func(a publish.Artifact) bool { return a.ID == id && a.Name == name })
	}
	var plans, bindings []publish.Artifact
	for _, a := range artifacts {
		switch {
		case a.Name == "release-plan":
			plans = append(plans, a)
		case strings.HasPrefix(a.Name, "release-binding"):
			bindings = append(bindings, a)
		}
	}
	if !assets {
		if len(plans) != 1 {
			return buildRun{}, false
		}
		return buildRun{plan: plans[0].ID}, true
	}
	if len(bindings) != 1 {
		return buildRun{}, false
	}
	var built buildRun
	if n, _ := fmt.Sscanf(bindings[0].Name, "release-binding-%d-%d", &built.plan, &built.assets); n != 2 ||
		bindings[0].Name != fmt.Sprintf("release-binding-%d-%d", built.plan, built.assets) ||
		!named(built.plan, "release-plan") || !named(built.assets, "release-assets") {
		return buildRun{}, false
	}
	return built, true
}

// normalize ignores the line endings and trailing whitespace GitHub may change in a release body.
func normalize(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), " \t\n")
}

// appendEnvFile writes to a GitHub Actions file such as GITHUB_OUTPUT, if the variable is set.
func appendEnvFile(name, content string) error {
	file := os.Getenv(name)
	if file == "" {
		return nil
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// readPlan reads a release plan that validate --out wrote.
func readPlan(file string) (plan.Plan, error) {
	var p plan.Plan
	data, err := os.ReadFile(file)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("read %s: %v", file, err)
	}
	return p, nil
}

func cmdPublish(ctx context.Context, args []string, out io.Writer) error {
	fs, dir := flags("publish", "publish --plan <file> --branch <name> [--dir <checkout>] [--built-plan <file>] [--assets <dir> [--signer-workflow <path>]] [--repository owner/name]")
	planFile := fs.String("plan", "", "release plan written by release-planner validate --ci --merged --out")
	builtPlan := fs.String("built-plan", "", "release plan of the run that ran the release checks and built the assets, which must plan the same release")
	branch := fs.String("branch", "", "release branch; the plan's merged commit must be a pull request merged into it")
	repository := fs.String("repository", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository, as owner/name")
	assetsDir := fs.String("assets", "", "directory of files to attach to the release, staged on a draft and verified before publishing")
	signer := fs.String("signer-workflow", "", "with --assets, require each file's build attestation from this workflow in the repository, such as .github/workflows/release-planner.yml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *planFile == "" || *branch == "" || *repository == "" {
		fs.Usage()
		return fmt.Errorf("--plan, --branch, and --repository (or GITHUB_REPOSITORY) are required")
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("set GITHUB_TOKEN to a token that can write to %s", *repository)
	}
	p, err := readPlan(*planFile)
	if err != nil {
		return err
	}
	var assets []publish.File
	if p.Tag != "" {
		if *builtPlan != "" {
			built, err := readPlan(*builtPlan)
			if err != nil {
				return err
			}
			if built.Tag != p.Tag || built.Commit != p.Commit {
				return fmt.Errorf("the run that checked and built the release planned %q at %s, not %s at %s", built.Tag, built.Commit, p.Tag, p.Commit)
			}
		}
		if *assetsDir != "" {
			if assets, err = publish.ReadAssets(*assetsDir); err != nil {
				return err
			}
			if *signer != "" {
				if err := publish.VerifyAttestations(ctx, *assetsDir, assets, *repository, *signer); err != nil {
					return err
				}
			}
		}
	}
	gh := api(token, *repository)
	// A release GitHub App's token, when the release environment configures one, makes the writes.
	gh.WriteToken = os.Getenv("RELEASE_TOKEN")
	// Re-run failed jobs reuses validate's plan, so check that no later commit on the release
	// branch has withdrawn or replaced what the merge approved: now, and again immediately
	// before each write, since staging assets takes a while.
	repo := gitrepo.Repo{Dir: *dir}
	approved := func(ctx context.Context) error {
		tip, err := branchTip(ctx, repo, gh, *branch, token)
		if err != nil {
			return err
		}
		return plan.StillApproved(ctx, repo, *branch, tip, p)
	}
	err = approved(ctx)
	var res publish.Result
	if err == nil {
		res, err = publish.Publish(ctx, gh, p, *branch, assets, approved)
	}
	if errors.As(err, new(plan.WithdrawnError)) {
		if err := appendEnvFile("GITHUB_OUTPUT", "release=withdrawn\n"); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if err := appendEnvFile("GITHUB_OUTPUT", publishOutputs(p, res)); err != nil {
		return err
	}
	var summary strings.Builder
	switch {
	case p.Tag == "":
	case res.AlreadyPublished && res.NotesChanged:
		fmt.Fprintf(&summary, "%s is already published, and its notes have been edited since this release was approved; kept them: %s\n", p.Tag, res.URL)
	case res.AlreadyPublished:
		fmt.Fprintf(&summary, "%s is already published: %s\n", p.Tag, res.URL)
	default:
		fmt.Fprintf(&summary, "Published %s: %s\n", p.Tag, res.URL)
	}
	for _, e := range res.Edited {
		if e.Changed {
			fmt.Fprintf(&summary, "Updated the notes of %s: %s\n", e.Tag, e.URL)
		} else {
			fmt.Fprintf(&summary, "The notes of %s were already up to date: %s\n", e.Tag, e.URL)
		}
	}
	fmt.Fprint(out, summary.String())
	return appendEnvFile("GITHUB_STEP_SUMMARY", "\n"+summary.String())
}

// publishOutputs are the publish step's outputs, which the report reads: release, whether the
// requested release was published now or already, and notes, whether each edit changed the
// release's notes, as a JSON list of report.EditedNotes.
func publishOutputs(p plan.Plan, res publish.Result) string {
	release := ""
	switch {
	case p.Tag == "":
	case res.AlreadyPublished:
		release = "already-published"
	default:
		release = "published"
	}
	notes := []report.EditedNotes{}
	for _, e := range res.Edited {
		notes = append(notes, report.EditedNotes{Tag: e.Tag, Changed: e.Changed})
	}
	data, _ := json.Marshal(notes)
	return fmt.Sprintf("release=%s\nnotes=%s\n", release, data)
}

// escapeData and escapeProperty encode text for a GitHub Actions workflow command.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// beforePrePublish checks what must hold before the pre-publish workflow runs for a release.
// Its environment must keep its credentials to the release branch, and every earlier release
// must be published, so no earlier release's workflow can still run after this one's on
// purpose: the previous release, and any untagged lower version whose notes are on the release
// branch. On the pull request, at base, these are warnings, since they may change before the
// merge; after the merge, against the release branch's current tip, they stop the run, and
// Re-run failed jobs checks again.
func beforePrePublish(ctx context.Context, repo gitrepo.Repo, gh *publish.GitHub, c config.Config, p plan.Plan, base string, warn func(string, ...any)) error {
	merged := base == ""
	retry := ""
	if merged {
		retry = " Then use Re-run failed jobs on this run."
	}
	name, workflow := c.PrePublish.Environment, c.PrePublish.Workflow
	env, err := gh.Environment(ctx, name, c.Branch)
	if err != nil {
		if merged {
			return fmt.Errorf("couldn't check the %s environment before running %s: %v", name, workflow, err)
		}
		warn("Couldn't check the %s environment's settings: %v", name, err)
	}
	if err == nil {
		refusal, warnings := publish.PrePublishEnvironment(name, c.Branch, workflow, env)
		for _, w := range warnings {
			warn("%s", w)
		}
		switch {
		case refusal != "" && merged:
			return fmt.Errorf("%s%s", refusal, retry)
		case refusal != "":
			warn("%s", refusal)
		}
	}

	tag, file, err := waitingFor(ctx, repo, gh, c, p, base)
	switch {
	case err != nil && merged:
		return fmt.Errorf("couldn't check that the releases before %s are published: %v", p.Tag, err)
	case err != nil:
		warn("Couldn't check that the releases before %s are published: %v", p.Tag, err)
	case tag == "":
	case !merged:
		warn("%s merged before this release and isn't published yet. After you merge, this release waits until %s is published or withdrawn.", tag, tag)
	default:
		if err := appendEnvFile("GITHUB_OUTPUT", fmt.Sprintf("waiting-for=%s\nwaiting-for-file=%s\n", tag, file)); err != nil {
			return err
		}
		if file == "" {
			return fmt.Errorf("%s is tagged but has no published release, so %s waits for it. Publish %s by retrying its release, then use Re-run failed jobs on this run", tag, p.Tag, tag)
		}
		return fmt.Errorf("%s is on %s, but %s isn't published, so %s waits for it. Publish %s, or withdraw it by deleting %s in a pull request.%s", file, c.Branch, tag, p.Tag, tag, file, retry)
	}
	return nil
}

// waitingFor returns the earliest release the plan's release must wait for: the lowest
// untagged version, lower than the plan's, whose notes file is on the release branch at base,
// or after the merge at its current tip, with that file; or else the previous release, if it
// isn't published. It returns "" when there's none.
func waitingFor(ctx context.Context, repo gitrepo.Repo, gh *publish.GitHub, c config.Config, p plan.Plan, base string) (tag, file string, err error) {
	ref := base
	if ref == "" {
		if ref, err = branchTip(ctx, repo, gh, c.Branch, os.Getenv("GITHUB_TOKEN")); err != nil {
			return "", "", err
		}
	}
	tags, err := repo.Tags(ctx)
	if err != nil {
		return "", "", err
	}
	data, _ := repo.Run(ctx, "show", ref+":"+config.File)
	dir := config.NotesDirIn([]byte(data))
	files, err := plan.NotesFiles(ctx, repo, dir, ref)
	if err != nil {
		return "", "", err
	}
	current := semver.MustParse(p.Tag)
	var lowest semver.Version
	for _, name := range files {
		pending := plan.NotesTag(dir, name)
		v, ok := semver.Parse(pending)
		if !ok || slices.Contains(tags, pending) || semver.Compare(v, current) >= 0 {
			continue
		}
		if tag == "" || semver.Compare(v, lowest) < 0 {
			tag, file, lowest = pending, name, v
		}
	}
	if tag != "" || p.Previous == "" {
		return tag, file, nil
	}
	published, err := publish.Published(ctx, gh, p.Previous)
	if err != nil || published {
		return "", "", err
	}
	return p.Previous, "", nil
}

// environment warns, through warn, how an environment differs from the recommended setup.
func environment(ctx context.Context, gh *publish.GitHub, name, branch, docs string, warn func(string, ...any)) {
	env, err := gh.Environment(ctx, name, branch)
	if err != nil {
		warn("Couldn't check the %s environment's settings: %v. Give the validate job actions: read to check them.", name, err)
		return
	}
	for _, w := range publish.EnvironmentWarnings(name, branch, docs, env) {
		warn("%s", w)
	}
}

func cmdVersion(_ context.Context, args []string, out io.Writer) error {
	fmt.Fprintln(out, buildinfo.Version())
	return nil
}
