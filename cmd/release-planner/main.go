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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fabricahq/release-planner/internal/buildinfo"
	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/draft"
	"github.com/fabricahq/release-planner/internal/generate"
	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/publish"
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
  inventory   List changes since the previous release and the candidate versions
  draft       Create the release notes file for a version

Run in the Release workflow:
  plan        Validate a release request and print what to publish
  publish     Tag the approved commit and publish the approved notes

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
		"guide": cmdGuide, "inventory": cmdInventory, "draft": cmdDraft,
		"plan": cmdPlan, "publish": cmdPublish, "version": cmdVersion,
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
	fs, dir := flags("inventory", "inventory [--head <ref>]")
	head := fs.String("head", "HEAD", "commit the release would tag")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(*dir)
	if err != nil {
		return err
	}
	inv, err := plan.Take(ctx, gitrepo.Repo{Dir: *dir}, plan.Options{NotesDir: c.NotesDir, FirstVersion: c.FirstVersion}, *head)
	if err != nil {
		return err
	}
	return printJSON(out, inv)
}

func cmdDraft(ctx context.Context, args []string, out io.Writer) error {
	fs, dir := flags("draft", "draft [--repository owner/name] <version>")
	head := fs.String("head", "HEAD", "commit the release would tag")
	repository := fs.String("repository", "", "GitHub repository for links, as owner/name (default: from the origin remote)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("give exactly one version, such as v1.2.0")
	}
	c, err := config.Load(*dir)
	if err != nil {
		return err
	}
	repo := gitrepo.Repo{Dir: *dir}
	if *repository == "" {
		if *repository, err = draft.Repository(ctx, repo); err != nil {
			return err
		}
	}
	name, err := draft.Write(ctx, repo, c, *repository, fs.Arg(0), *head)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "created %s\nWrite the notes, delete empty headings, then run release-planner plan.\n", name)
	return nil
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func cmdPlan(ctx context.Context, args []string, out io.Writer) error {
	fs, dir := flags("plan", "plan --base <ref> [--head <ref>] [--out <file>] [--ci --event <name>]")
	base := fs.String("base", "", "commit before the release request")
	head := fs.String("head", "HEAD", "commit containing the approved notes")
	outFile := fs.String("out", "", "write the plan to this file instead of standard output")
	ci := fs.Bool("ci", false, "running in the Release workflow: write step outputs and enforce retry rules")
	event := fs.String("event", "", "GitHub event name, with --ci")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *base == "" {
		fs.Usage()
		return fmt.Errorf("--base is required")
	}
	c, err := config.Load(*dir)
	if err != nil {
		return err
	}
	repo := gitrepo.Repo{Dir: *dir}
	retry := *ci && *event == "workflow_dispatch"
	if retry {
		// A retry must name the original approved range exactly, never the latest commit.
		if !fullSHA.MatchString(*base) || !fullSHA.MatchString(*head) {
			return fmt.Errorf("a retry needs the full Base SHA and Approved head SHA from the failed run's summary")
		}
		if !repo.IsAncestor(ctx, *head, "origin/"+c.Branch) {
			return fmt.Errorf("the approved head %s is not on %s", *head, c.Branch)
		}
	}
	if *ci && *event == "pull_request" {
		// The pull request's base SHA is the base branch's current tip, which the head
		// doesn't contain once the branch has moved on. Check what the pull request adds.
		mergeBase, err := repo.MergeBase(ctx, *base, *head)
		if err != nil {
			return fmt.Errorf("finding where the pull request branched from %s: %w", *base, err)
		}
		*base = mergeBase
	}
	if *ci {
		if err := appendEnvFile("GITHUB_STEP_SUMMARY", fmt.Sprintf("Release request range\n\nBase SHA: `%s`\n\nApproved head SHA: `%s`\n", *base, *head)); err != nil {
			return err
		}
	}
	p, err := plan.Read(ctx, repo, plan.Options{NotesDir: c.NotesDir, FirstVersion: c.FirstVersion}, *base, *head)
	if err != nil {
		return err
	}
	if retry && p.Tag == "" {
		return fmt.Errorf("the retry range contains no release request")
	}
	if *ci {
		if err := appendEnvFile("GITHUB_OUTPUT", fmt.Sprintf("tag=%s\ncommit=%s\n", p.Tag, p.Commit)); err != nil {
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
	if *outFile != "" && p.Tag != "" {
		fmt.Fprintf(out, "Planned %s at %s.\n", p.Tag, p.Commit)
	} else if *outFile != "" {
		fmt.Fprintln(out, "No release requested.")
	}
	return nil
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

func cmdPublish(ctx context.Context, args []string, out io.Writer) error {
	fs, _ := flags("publish", "publish --plan <file> --commit <sha> --branch <name> [--assets <dir>] [--repository owner/name]")
	planFile := fs.String("plan", "", "plan written by release-planner plan")
	commit := fs.String("commit", "", "approved commit from the plan job")
	branch := fs.String("branch", "", "release branch; the commit must be a pull request merged into it")
	repository := fs.String("repository", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository, as owner/name")
	assetsDir := fs.String("assets", "", "directory of files to attach to the release, staged on a draft and verified before publishing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *planFile == "" || *commit == "" || *branch == "" || *repository == "" {
		fs.Usage()
		return fmt.Errorf("--plan, --commit, --branch, and --repository (or GITHUB_REPOSITORY) are required")
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("set GITHUB_TOKEN to a token that can write to %s", *repository)
	}
	data, err := os.ReadFile(*planFile)
	if err != nil {
		return err
	}
	var p plan.Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("read %s: %w", *planFile, err)
	}
	var assets []publish.File
	if *assetsDir != "" {
		if assets, err = publish.ReadAssets(*assetsDir); err != nil {
			return err
		}
	}
	api := os.Getenv("GITHUB_API_URL")
	if api == "" {
		api = "https://api.github.com"
	}
	res, err := publish.Publish(ctx, &publish.GitHub{BaseURL: api, Token: token, Repository: *repository}, p, *commit, *branch, assets)
	if err != nil {
		return err
	}
	if res.AlreadyPublished {
		fmt.Fprintf(out, "%s is already published: %s\n", p.Tag, res.URL)
	} else {
		fmt.Fprintf(out, "Published %s: %s\n", p.Tag, res.URL)
	}
	return appendEnvFile("GITHUB_STEP_SUMMARY", fmt.Sprintf("\nPublished %s\n", res.URL))
}

func cmdVersion(_ context.Context, args []string, out io.Writer) error {
	fmt.Fprintln(out, buildinfo.Version())
	return nil
}
