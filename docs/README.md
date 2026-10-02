# Release Planner documentation

The Release Planner documentation site uses Astro and Starlight. Its look, theme menu, and callout handling follow the [Code Rules documentation](https://github.com/fabricahq/code-rules/tree/main/docs), without the homepage: the site opens on "What is Release Planner?"

## Run locally

From this directory:

```sh
bun install --frozen-lockfile
bun run dev
```

Before you push, check and build the site:

```sh
bun run check
bun run build
```

The output is `dist/`. These commands do not publish the site.

## Publishing

The Documentation workflow publishes the site to GitHub Pages at <https://release-planner.fabricahq.com> after each push to `main`. Pull requests only check and build it. The site is served at the domain root, so page links are root-relative. The Pages settings, custom domain, and DNS record are managed in Fabrica's infrastructure repository, not here.

### Pull request previews

When a pull request into `main` changes anything under `docs/`, its build is published as a public preview on Cloudflare Pages, at `pr-<number>.release-planner-docs-previews.pages.dev`. A pull request comment links the preview and the changed pages. Every push updates the preview, and closing the pull request deletes its previews. A pull request retargeted away from `main` gets no new preview, and loses its previews at its next deploy attempt or the daily run. That daily run also deletes any previews of closed pull requests that were left behind. Previews aren't indexed by search engines.

The repository is public, so anyone can open a pull request, and a build runs the pull request's code. The design keeps that code away from the Cloudflare token, which can change every Pages project in Fabrica's account:

1. The Documentation workflow builds and checks the site on a GitHub-hosted runner with no secrets, then uploads `docs/dist` as the `docs-site` artifact.
2. The Documentation previews workflow (`.github/workflows/docs-preview.yml`) runs main's copy of itself and its scripts in `.github/docs-preview/`. It never checks out pull request code, and treats the artifact as data.
3. Before deploying, it rejects symlinks and the top-level `_worker.js`, `functions`, `_routes.json`, `_headers`, and `_redirects` entries. Pages would run the first two as server code and use the others to redirect visitors or change headers. It adds its own `_headers`, with a Content Security Policy that keeps pages from loading anything from other sites or submitting forms. It runs wrangler, pinned by main's lockfile, from an empty directory, then confirms Cloudflare recorded a live static preview.
4. A build is matched to its pull request by repository, branch, and commit. Previews of branches in this repository opened by authors with write access deploy automatically. Others wait in the `docs-preview-approval` environment until a maintainer reviews the commit and approves the deployment from the workflow run that the comment links. A newer push cancels a pending approval, and a deploy first checks that its commit is still the pull request's head, so an approval never publishes any other commit.
5. Only the `docs-preview` environment holds the `DOCS_PREVIEW_CLOUDFLARE_API_TOKEN` secret, and it admits deployments from `main` only.

The workflow fails rather than deploy if `docs-preview` admits any branch but `main`, or `docs-preview-approval` has no required reviewers. GitHub creates a missing environment without protection the first time a job uses it.

The Pages project is managed in Fabrica's infrastructure repository. To set up the repository side once:

```sh
repo=fabricahq/release-planner
for environment in docs-preview docs-preview-approval; do
  gh api -X PUT "repos/$repo/environments/$environment" --input - <<'EOF'
{"deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}
EOF
  gh api -X POST "repos/$repo/environments/$environment/deployment-branch-policies" -f name=main -f type=branch
done
gh api -X PUT "repos/$repo/environments/docs-preview-approval" --input - <<'EOF'
{"reviewers": [{"type": "User", "id": 4295964}],
 "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}
EOF
gh secret set DOCS_PREVIEW_CLOUDFLARE_API_TOKEN --env docs-preview --repo "$repo"
```

Use a Cloudflare API token limited to the Fabrica account, with only **Cloudflare Pages: Edit** permission and an expiry date, kept in 1Password. User `4295964` is `josh-padnick`; add other maintainers as reviewers.

## Maintain the docs

Pages live under `src/content/docs/`, in folders matching the sidebar: `start-here/`, `customize/`, and `for-agents.md`. The sidebar is defined in `astro.config.mjs`.

Keep the human pages short and focused on setting up and customizing releases. Put command details, file formats, and workflow internals in "For agents". When a command, config key, or generated file changes, update the page that describes it in the same pull request.
