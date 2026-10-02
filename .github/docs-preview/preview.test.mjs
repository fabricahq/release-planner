import { describe, expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkDeployment, deletePreviews, deployPreview, keepOpenPullRequests, keepOtherBranches } from './cloudflare.mjs';
import { isNewer, renderComment, reportRetargeted, writeComment } from './comment.mjs';
import { commentMarker, pagePath, previewBranch } from './config.mjs';
import { recheck, resolve } from './resolve.mjs';
import { addPreviewHeaders, validateSite } from './site.mjs';

const repositoryID = 100;
const forkID = 200;
const sha = 'a'.repeat(40);
const project = 'release-planner-docs-previews';

describe('pagePath', () => {
  test('maps content files to site paths', () => {
    expect(pagePath('docs/src/content/docs/index.mdx')).toBe('/');
    expect(pagePath('docs/src/content/docs/for-agents.md')).toBe('/for-agents/');
    expect(pagePath('docs/src/content/docs/customize/configuration.md')).toBe('/customize/configuration/');
    expect(pagePath('docs/src/content/docs/customize/index.md')).toBe('/customize/');
  });

  test('ignores other files and names that could break out of a link', () => {
    expect(pagePath('docs/astro.config.mjs')).toBeNull();
    expect(pagePath('docs/src/content/docs/a](https://evil.example).md')).toBeNull();
    expect(pagePath('docs/src/content/docs/Upper.md')).toBeNull();
    expect(pagePath('docs/src/content/docs/a//b.md')).toBeNull();
    expect(pagePath('README.md')).toBeNull();
  });
});

describe('previewBranch', () => {
  test('accepts only positive pull request numbers', () => {
    expect(previewBranch(32)).toBe('pr-32');
    expect(() => previewBranch(Number('main'))).toThrow();
    expect(() => previewBranch(0)).toThrow();
  });
});

describe('validateSite', () => {
  const site = (files = {}) => {
    const root = mkdtempSync(join(tmpdir(), 'site-'));
    writeFileSync(join(root, 'index.html'), '<h1>Docs</h1>');
    for (const [path, contents] of Object.entries(files)) {
      mkdirSync(join(root, path, '..'), { recursive: true });
      writeFileSync(join(root, path), contents);
    }
    return root;
  };

  test('accepts a plain site, including underscore directories', () => {
    expect(() => validateSite(site({ '_astro/app.js': 'x', 'start-here/index.html': 'x' }))).not.toThrow();
  });

  test('rejects symlinks, which wrangler would follow and upload', () => {
    const root = site();
    symlinkSync('/etc/hosts', join(root, 'hosts'));
    expect(() => validateSite(root)).toThrow('only files and directories');
  });

  test.each(['_worker.js', '_routes.json', '_headers', '_redirects', 'functions/index.js'])(
    'rejects Pages configuration and code: %s', (path) => {
      expect(() => validateSite(site({ [path]: 'x' }))).toThrow('treats as configuration or code');
    });

  test('allows reserved names below the root, where Pages ignores them', () => {
    expect(() => validateSite(site({ 'guide/_headers': 'x' }))).not.toThrow();
  });

  test('quotes file names in errors, so a name cannot forge a workflow command', () => {
    const root = site();
    symlinkSync('/etc/hosts', join(root, 'x\n::error::forged'));
    expect(() => validateSite(root)).toThrow('"x\\n::error::forged"');
  });

  test('adds the preview headers', () => {
    const root = site();
    addPreviewHeaders(root);
    const headers = readFileSync(join(root, '_headers'), 'utf8');
    expect(headers).toContain("Content-Security-Policy: default-src 'self';");
    expect(headers).toContain('X-Robots-Tag: noindex');
  });

  test('rejects a site without an index page', () => {
    const root = mkdtempSync(join(tmpdir(), 'site-'));
    expect(() => validateSite(root)).toThrow('index.html');
  });
});

// A GitHub API double holding one repository, its pull requests, and comments.
function fakeGitHub({ pulls = [], files = [], artifacts = [{ name: 'docs-site', expired: false, size_in_bytes: 10 }],
  permissions = {}, comments = [], approvalReviewers = 1, deployBranches = ['main'] } = {}) {
  const calls = [];
  const rest = {
    actions: { listWorkflowRunArtifacts: () => artifacts },
    repos: {
      getCollaboratorPermissionLevel: async ({ username }) => {
        if (!(username in permissions)) throw Object.assign(new Error('Not Found'), { status: 404 });
        return { data: { permission: permissions[username] } };
      },
      getEnvironment: async ({ environment_name }) => {
        if (environment_name === 'docs-preview') {
          return { data: { deployment_branch_policy: deployBranches && { protected_branches: false, custom_branch_policies: true } } };
        }
        if (approvalReviewers === null) throw Object.assign(new Error('Not Found'), { status: 404 });
        return { data: { protection_rules: [{ type: 'required_reviewers', reviewers: Array(approvalReviewers).fill({}) }] } };
      },
      listDeploymentBranchPolicies: () => (deployBranches ?? []).map((name) => ({ name, type: 'branch' })),
    },
    pulls: {
      get: async ({ pull_number }) => ({ data: pulls.find((pr) => pr.number === pull_number) }),
      // Ignores the head filter, so the code under test must check each match itself.
      list: ({ state }) => pulls.filter((pr) => pr.state === state),
      listFiles: () => files,
    },
    issues: {
      listComments: () => comments,
      createComment: async (args) => calls.push(['create', args]),
      updateComment: async (args) => calls.push(['update', args]),
    },
  };
  return { calls, github: { rest, paginate: async (method, args) => method(args) } };
}

function fakeCore() {
  const outputs = {};
  return { outputs, core: { setOutput: (name, value) => { outputs[name] = value; }, notice: () => {} } };
}

const ownRepo = { id: repositoryID, owner: { login: 'fabricahq' } };
const fork = { id: forkID, owner: { login: 'mallory' } };
const pullRequest = (overrides = {}) => ({
  number: 32, state: 'open', user: { login: 'writer' },
  base: { ref: 'main', repo: { id: repositoryID } }, head: { sha, ref: 'docs', repo: ownRepo }, ...overrides,
});
const forkPullRequest = (overrides = {}) => pullRequest({ number: 33, user: { login: 'mallory' }, head: { sha, ref: 'docs', repo: fork }, ...overrides });

const runContext = (run = {}) => ({
  eventName: 'workflow_run', ref: 'refs/heads/main', repo: { owner: 'fabricahq', repo: 'release-planner' },
  payload: {
    repository: { id: repositoryID, default_branch: 'main' },
    workflow_run: { id: 7, path: '.github/workflows/docs.yml', event: 'pull_request', status: 'completed',
      conclusion: 'success', head_sha: sha, head_branch: 'docs', head_repository: ownRepo,
      repository: { id: repositoryID }, triggering_actor: { login: 'writer' }, ...run },
  },
});

const docsFiles = [
  { filename: 'docs/src/content/docs/customize/configuration.md', status: 'modified' },
  { filename: 'docs/src/content/docs/old.md', status: 'removed' },
  { filename: 'internal/config/config.go', status: 'modified' },
];

describe('resolve', () => {
  const forkRun = { head_repository: fork, triggering_actor: { login: 'mallory' } };
  const resolved = async (options, run) => {
    const { core, outputs } = fakeCore();
    await resolve({ github: fakeGitHub(options).github, context: runContext(run), core });
    return outputs;
  };

  test('deploys a writer\'s same-repository pull request without approval', async () => {
    expect(await resolved({ pulls: [pullRequest()], files: docsFiles, permissions: { writer: 'write' } })).toEqual({
      action: 'deploy', pr: '32', sha, run_id: '7', trusted: 'true', pages: '["/customize/configuration/"]', pages_total: '1',
    });
  });

  test('requires approval for a fork, even from a writer', async () => {
    const outputs = await resolved({ pulls: [forkPullRequest({ user: { login: 'writer' } })], files: docsFiles, permissions: { writer: 'write' } }, forkRun);
    expect(outputs).toMatchObject({ action: 'deploy', pr: '33', trusted: 'false' });
  });

  test('requires approval for a same-repository author without write access', async () => {
    const outputs = await resolved({ pulls: [pullRequest({ user: { login: 'bot[bot]' } })], files: docsFiles });
    expect(outputs.trusted).toBe('false');
  });

  test('binds a fork run to the fork pull request when a trusted one shares its commit', async () => {
    const pulls = [pullRequest(), forkPullRequest()];
    const outputs = await resolved({ pulls, files: docsFiles, permissions: { writer: 'write' } }, forkRun);
    expect(outputs).toMatchObject({ action: 'deploy', pr: '33', trusted: 'false' });
  });

  test('binds a trusted run to its own pull request when a fork shares its commit', async () => {
    const pulls = [forkPullRequest(), pullRequest()];
    const outputs = await resolved({ pulls, files: docsFiles, permissions: { writer: 'write' } });
    expect(outputs).toMatchObject({ action: 'deploy', pr: '32', trusted: 'true' });
  });

  test('skips a pull request into another branch', async () => {
    const pr = pullRequest({ base: { ref: 'release', repo: { id: repositoryID } } });
    expect(await resolved({ pulls: [pr], files: docsFiles, permissions: { writer: 'write' } })).toEqual({ action: 'skip' });
  });

  test.each([[0], [null]])('fails closed when the approval environment has %p reviewers', async (approvalReviewers) => {
    const { github } = fakeGitHub({ pulls: [forkPullRequest()], files: docsFiles, approvalReviewers });
    await expect(resolve({ github, context: runContext(forkRun), core: fakeCore().core })).rejects.toThrow('required reviewers');
  });

  test.each([[null], [['main', 'docs']], [['feature']]])('fails closed when the token environment admits %p', async (deployBranches) => {
    const { github } = fakeGitHub({ pulls: [pullRequest()], files: docsFiles, permissions: { writer: 'write' }, deployBranches });
    await expect(resolve({ github, context: runContext(), core: fakeCore().core })).rejects.toThrow('admit only the main branch');
  });

  test.each(['getEnvironment', 'listDeploymentBranchPolicies'])('fails closed when %s is forbidden', async (method) => {
    const { github } = fakeGitHub({ pulls: [forkPullRequest()], files: docsFiles });
    github.rest.repos[method] = async () => { throw Object.assign(new Error('Resource not accessible by integration'), { status: 403 }); };
    const { core, outputs } = fakeCore();
    await expect(resolve({ github, context: runContext(forkRun), core })).rejects.toThrow('not accessible');
    expect(outputs.action).toBeUndefined();
  });

  test('skips pull requests that do not change the documentation', async () => {
    expect(await resolved({ pulls: [pullRequest()], files: [docsFiles[2]], permissions: { writer: 'write' } })).toEqual({ action: 'skip' });
  });

  test('counts a moved-out documentation file as a change', async () => {
    const files = [{ filename: 'notes/page.md', previous_filename: 'docs/page.md', status: 'renamed' }];
    const outputs = await resolved({ pulls: [pullRequest()], files, permissions: { writer: 'write' } });
    expect(outputs).toMatchObject({ action: 'deploy', pages: '[]', pages_total: '0' });
  });

  test('lists at most 25 changed pages', async () => {
    const files = Array.from({ length: 30 }, (_, i) => ({ filename: `docs/src/content/docs/p${i}.md`, status: 'added' }));
    const outputs = await resolved({ pulls: [pullRequest()], files, permissions: { writer: 'write' } });
    expect(JSON.parse(outputs.pages)).toHaveLength(25);
    expect(outputs.pages_total).toBe('30');
  });

  test('skips a run whose commit is no longer the head', async () => {
    const pr = pullRequest({ head: { sha: 'b'.repeat(40), ref: 'docs', repo: ownRepo } });
    expect(await resolved({ pulls: [pr], files: docsFiles, permissions: { writer: 'write' } })).toEqual({ action: 'skip' });
  });

  test.each([
    ['a failed build', { conclusion: 'failure' }],
    ['another workflow', { path: '.github/workflows/test.yml' }],
    ['a push to main', { event: 'push' }],
    ['a Dependabot run', { triggering_actor: { login: 'dependabot[bot]' } }],
  ])('skips %s', async (_, run) => {
    expect(await resolved({ pulls: [pullRequest()], files: docsFiles, permissions: { writer: 'write' } }, run)).toEqual({ action: 'skip' });
  });

  test('skips a run without exactly one live site artifact', async () => {
    expect(await resolved({ pulls: [pullRequest()], files: docsFiles, artifacts: [] })).toEqual({ action: 'skip' });
  });

  test('refuses to run a workflow from another branch', async () => {
    const context = { ...runContext(), ref: 'refs/heads/feature' };
    await expect(resolve({ github: fakeGitHub().github, context, core: fakeCore().core })).rejects.toThrow('default-branch');
  });

  test('lists open pull requests into main for a scheduled cleanup', async () => {
    const { core, outputs } = fakeCore();
    const context = { ...runContext(), eventName: 'schedule' };
    const retargeted = pullRequest({ number: 34, base: { ref: 'release', repo: { id: repositoryID } } });
    await resolve({ github: fakeGitHub({ pulls: [pullRequest(), forkPullRequest({ state: 'closed' }), retargeted] }).github, context, core });
    expect(outputs).toMatchObject({ action: 'reconcile', open_prs: '[32]', retargeted_prs: '[34]' });
    expect(Date.parse(outputs.cutoff)).toBeLessThanOrEqual(Date.now());
  });

  const closedContext = {
    eventName: 'pull_request_target', ref: 'refs/heads/main', repo: { owner: 'fabricahq', repo: 'release-planner' },
    payload: { action: 'closed', repository: { id: repositoryID, default_branch: 'main' }, pull_request: pullRequest() },
  };

  test('removes the preview of a closed pull request that has one', async () => {
    const comments = [{ id: 1, user: { login: 'github-actions[bot]' }, body: `${commentMarker}\n...` }];
    const { core, outputs } = fakeCore();
    await resolve({ github: fakeGitHub({ comments }).github, context: closedContext, core });
    expect(outputs).toEqual({ action: 'stop', pr: '32' });
  });

  test('removes previews of a closed pull request that changes the documentation', async () => {
    const { core, outputs } = fakeCore();
    await resolve({ github: fakeGitHub({ files: docsFiles }).github, context: closedContext, core });
    expect(outputs).toEqual({ action: 'stop', pr: '32' });
  });

  test('ignores a marker comment written by someone else', async () => {
    const comments = [{ id: 1, user: { login: 'mallory' }, body: `${commentMarker}\n...` }];
    const { core, outputs } = fakeCore();
    await resolve({ github: fakeGitHub({ comments }).github, context: closedContext, core });
    expect(outputs).toEqual({ action: 'skip' });
  });

  test('leaves pull requests into other branches to the scheduled cleanup', async () => {
    const { core, outputs } = fakeCore();
    await resolve({ github: fakeGitHub({ files: docsFiles }).github, context: { ...closedContext, ref: 'refs/heads/release' }, core });
    expect(outputs).toEqual({ action: 'skip' });
  });
});

describe('recheck', () => {
  test.each([
    ['current', pullRequest()],
    ['superseded', pullRequest({ head: { sha: 'b'.repeat(40) } })],
    ['closed', pullRequest({ state: 'closed' })],
    ['retargeted', pullRequest({ base: { ref: 'release', repo: { id: repositoryID } } })],
  ])('reports %s', async (state, pr) => {
    const { core, outputs } = fakeCore();
    await recheck({ github: fakeGitHub({ pulls: [pr] }).github, context: runContext(), core }, { pr: 32, sha });
    expect(outputs.state).toBe(state);
  });
});

describe('comments', () => {
  const links = { runURL: 'https://github.test/run', commitURL: (s) => `https://github.test/commit/${s}`, stamp: '<!-- run:9 attempt:1 -->' };

  test('links the preview and each changed page', () => {
    const body = renderComment({
      pr: 32, state: 'deployed', sha,
      url: `https://abc123.${project}.pages.dev`, alias: `https://pr-32.${project}.pages.dev`,
      pages: ['/', '/customize/configuration/'],
    }, links);
    expect(body.startsWith(`${commentMarker}\n<!-- run:9 attempt:1 -->\n### Documentation preview`)).toBe(true);
    expect(body).toContain(`**[Open preview](https://pr-32.${project}.pages.dev/)**`);
    expect(body).toContain(`- [\`/customize/configuration/\`](https://pr-32.${project}.pages.dev/customize/configuration/)`);
  });

  test('tells maintainers how to approve a pending preview', () => {
    expect(renderComment({ pr: 32, state: 'pending', sha }, links)).toContain('approve the `docs-preview-approval` deployment');
  });

  test('explains why a preview was removed', () => {
    expect(renderComment({ pr: 32, state: 'removed' }, links)).toContain('because this pull request closed');
    // A retargeted pull request may never have had a preview: one could have been pending.
    const retargeted = renderComment({ pr: 32, state: 'removed', reason: 'retargeted' }, links);
    expect(retargeted).toContain('no longer targets the default branch, so it has no preview, and any earlier one was removed');
    expect(retargeted).not.toContain('Preview removed');
    expect(retargeted).toContain('its next push publishes a new preview');
  });

  test('orders runs and attempts', () => {
    expect(isNewer('<!-- run:10 attempt:1 -->', 9, 3)).toBe(true);
    expect(isNewer('<!-- run:9 attempt:2 -->', 9, 1)).toBe(true);
    expect(isNewer('<!-- run:9 attempt:1 -->', 9, 1)).toBe(false);
    expect(isNewer(undefined, 9, 1)).toBe(false);
  });

  const context = { repo: { owner: 'fabricahq', repo: 'release-planner' }, serverUrl: 'https://github.test', runId: 9, runAttempt: 1 };

  // Jobs of one run share a stamp, so only job order keeps a pending comment from
  // replacing that run's result, or two jobs from each creating a comment.
  test('reports a result only after the pending comment is written', async () => {
    const { jobs } = Bun.YAML.parse(await Bun.file(new URL('../workflows/docs-preview.yml', import.meta.url)).text());
    const needs = (job) => [jobs[job].needs ?? []].flat().flatMap((need) => [need, ...needs(need)]);
    expect(needs('publish')).toContain('announce');
  });

  test('never replaces a newer run\'s comment', async () => {
    const comments = [{ id: 1, user: { login: 'github-actions[bot]' }, body: `${commentMarker}\n<!-- run:10 attempt:1 -->` }];
    const { github, calls } = fakeGitHub({ comments });
    await writeComment({ github, context, core: fakeCore().core }, { pr: 32, state: 'failed', sha });
    expect(calls).toEqual([]);
  });

  test('updates its own comment and leaves others alone', async () => {
    const comments = [
      { id: 1, user: { login: 'mallory' }, body: `${commentMarker}\n<!-- run:99 attempt:1 -->` },
      { id: 2, user: { login: 'github-actions[bot]' }, body: `${commentMarker}\n<!-- run:8 attempt:1 -->` },
    ];
    const { github, calls } = fakeGitHub({ comments });
    await writeComment({ github, context, core: fakeCore().core }, { pr: 32, state: 'failed', sha });
    expect(calls.map(([kind, args]) => [kind, args.comment_id])).toEqual([['update', 2]]);
  });

  test('posts nothing on removal when there was no preview', async () => {
    const { github, calls } = fakeGitHub();
    await writeComment({ github, context, core: fakeCore().core }, { pr: 32, state: 'removed' });
    expect(calls).toEqual([]);
  });

  test('reports a closed pull request\'s removal over a newer run\'s comment', async () => {
    const comments = [{ id: 1, user: { login: 'github-actions[bot]' }, body: `${commentMarker}\n<!-- run:10 attempt:1 state:deployed -->` }];
    const { github, calls } = fakeGitHub({ comments });
    await writeComment({ github, context, core: fakeCore().core }, { pr: 32, state: 'removed' });
    expect(calls.map(([kind]) => kind)).toEqual(['update']);
  });

  describe('after the reconciler removes retargeted previews', () => {
    const reconcileContext = { ...context, payload: { repository: { id: repositoryID, default_branch: 'main' } } };
    const retargeted = pullRequest({ base: { ref: 'release', repo: { id: repositoryID } } });
    const comment = (stamp) => [{ id: 1, user: { login: 'github-actions[bot]' }, body: `${commentMarker}\n${stamp}` }];
    const report = async (options) => {
      const { github, calls } = fakeGitHub(options);
      await reportRetargeted({ github, context: reconcileContext, core: fakeCore().core }, [32]);
      return calls;
    };

    test('marks the comment of a pull request that still targets another branch', async () => {
      const calls = await report({ pulls: [retargeted], comments: comment('<!-- run:8 attempt:1 state:deployed -->') });
      expect(calls).toHaveLength(1);
      expect(calls[0][1].body).toContain('<!-- run:9 attempt:1 state:retargeted -->');
      expect(calls[0][1].body).toContain('no longer targets the default branch');
    });

    test('replaces the closed notice of a pull request reopened against another branch', async () => {
      const calls = await report({ pulls: [retargeted], comments: comment('<!-- run:8 attempt:1 state:removed -->') });
      expect(calls.map(([kind]) => kind)).toEqual(['update']);
    });

    test.each([
      ['targets main again', { pulls: [pullRequest()], comments: comment('<!-- run:8 attempt:1 state:deployed -->') }],
      ['closed since', { pulls: [{ ...retargeted, state: 'closed' }], comments: comment('<!-- run:8 attempt:1 state:deployed -->') }],
      ['has no preview comment', { pulls: [retargeted] }],
      ['is already marked retargeted', { pulls: [retargeted], comments: comment('<!-- run:8 attempt:1 state:retargeted -->') }],
      ['has a newer run\'s comment', { pulls: [retargeted], comments: comment('<!-- run:10 attempt:1 state:deployed -->') }],
    ])('leaves the comment of a pull request that %s', async (_, options) => {
      expect(await report(options)).toEqual([]);
    });
  });
});

// A Cloudflare API double that records requests and serves deployments. A deployment's
// stages list the latest_stage values that successive reads return.
function fakeCloudflare(deployments) {
  const requests = [];
  const reads = {};
  const fetch = async (url, init) => {
    requests.push([init.method ?? 'GET', url]);
    const { pathname, searchParams } = new URL(url);
    if (init.method === 'DELETE') return Response.json({ success: true, result: null });
    if (pathname.endsWith('/deployments')) {
      const page = Number(searchParams.get('page'));
      const result = deployments.slice((page - 1) * 25, page * 25);
      return Response.json({ success: true, result, result_info: { page, total_pages: Math.ceil(deployments.length / 25) } });
    }
    const found = deployments.find((d) => pathname.endsWith(`/${d.id}`));
    if (!found) return Response.json({ success: false }, { status: 404 });
    const { stages, ...result } = found;
    const read = reads[found.id] = (reads[found.id] ?? 0) + 1;
    if (stages) result.latest_stage = stages[Math.min(read, stages.length) - 1];
    return Response.json({ success: true, result });
  };
  return { fetch, requests };
}

const live = { name: 'deploy', status: 'success' };
const deployment = (id, branch, overrides = {}) => ({
  id, environment: 'preview', uses_functions: false, deployment_trigger: { metadata: { branch } },
  created_on: '2026-10-01T00:00:00Z', latest_stage: live,
  url: `https://${id}.${project}.pages.dev`, aliases: [`https://${branch}.${project}.pages.dev`], ...overrides,
});

describe('Cloudflare', () => {
  const options = { site: '/site', branch: 'pr-32', sha, pr: 32, accountID: 'account', project, token: 'token' };
  const wrangler = (deploymentID) => (args, env, cwd) => {
    expect(readdirSync(cwd)).toEqual([]);
    expect(args).toContain('--project-name');
    expect(env.CLOUDFLARE_API_TOKEN).toBe('token');
    writeFileSync(env.WRANGLER_OUTPUT_FILE_PATH, `${JSON.stringify({ type: 'pages-deploy-detailed', deployment_id: deploymentID })}\n`);
  };
  const deps = (fetch, deploymentID = 'abc123') => ({ fetch, wrangler: wrangler(deploymentID), wait: async () => {} });

  test('deploys from an empty directory and returns the preview URLs', async () => {
    const { fetch } = fakeCloudflare([deployment('abc123', 'pr-32')]);
    expect(await deployPreview(options, deps(fetch))).toEqual({
      url: `https://abc123.${project}.pages.dev`, alias: `https://pr-32.${project}.pages.dev`,
    });
  });

  test('waits until the deployment is live', async () => {
    const stages = [{ name: 'deploy', status: 'active' }, { name: 'deploy', status: 'active' }, live];
    const { fetch, requests } = fakeCloudflare([deployment('abc123', 'pr-32', { stages })]);
    await deployPreview(options, deps(fetch));
    expect(requests.filter(([method]) => method === 'GET')).toHaveLength(3);
  });

  test.each([
    ['a failed deployment', { stages: [{ name: 'deploy', status: 'failure' }] }, "didn't go live"],
    ['a deployment that never goes live', { stages: [{ name: 'deploy', status: 'active' }] }, "didn't go live"],
    ['a deployment that runs Functions', { uses_functions: true }, 'Functions'],
  ])('deletes and rejects %s', async (_, overrides, message) => {
    const { fetch, requests } = fakeCloudflare([deployment('abc123', 'pr-32', overrides)]);
    await expect(deployPreview(options, deps(fetch))).rejects.toThrow(message);
    expect(requests.at(-1)).toEqual(['DELETE', expect.stringContaining('/deployments/abc123?force=true')]);
  });

  test('rejects wrangler output without a deployment', async () => {
    const { fetch } = fakeCloudflare([]);
    await expect(deployPreview(options, deps(fetch, '../x'))).rejects.toThrow('no deployment');
  });

  test.each([
    ['a production deployment', { environment: 'production' }],
    ['another branch', { deployment_trigger: { metadata: { branch: 'pr-33' } } }],
    ['a URL outside the project', { url: 'https://abc123.example.com' }],
    ['an alias outside the project', { aliases: ['https://pr-32.example.com'] }],
    ['another branch\'s alias', { aliases: [`https://pr-320.${project}.pages.dev`] }],
  ])('rejects %s', (_, overrides) => {
    expect(() => checkDeployment(deployment('abc123', 'pr-32', overrides), { branch: 'pr-32', project })).toThrow();
  });

  test('accepts the suffixed subdomain Cloudflare assigns when a name is taken', () => {
    const suffixed = deployment('abc123', 'pr-32', {
      url: `https://abc123.${project}-x7q.pages.dev`, aliases: [`https://pr-32.${project}-x7q.pages.dev`],
    });
    expect(checkDeployment(suffixed, { branch: 'pr-32', project }).alias).toBe(`https://pr-32.${project}-x7q.pages.dev`);
  });

  test('deletes every deployment of a branch across pages', async () => {
    const deployments = Array.from({ length: 30 }, (_, i) => deployment(`d${i}`, i % 2 ? 'pr-32' : 'pr-3'));
    const { fetch, requests } = fakeCloudflare(deployments);
    expect(await deletePreviews(options, keepOtherBranches('pr-32'), { fetch })).toBe(15);
    const deleted = requests.filter(([method]) => method === 'DELETE').map(([, url]) => url);
    expect(deleted).toHaveLength(15);
    expect(deleted.every((url) => url.endsWith('?force=true'))).toBe(true);
  });

  test('reconciles only older previews of pull requests that are not open', () => {
    const keep = keepOpenPullRequests([32], '2026-10-01T12:00:00Z');
    expect(keep(deployment('a', 'pr-32'))).toBe(true);
    expect(keep(deployment('b', 'pr-3'))).toBe(false);
    expect(keep(deployment('c', 'unexpected'))).toBe(false);
    expect(keep(deployment('d', 'pr-40', { created_on: '2026-10-01T12:00:01Z' }))).toBe(true);
  });

  test('reports Cloudflare errors', async () => {
    const fetch = async () => Response.json({ success: false, errors: [{ code: 8000007, message: 'Project not found' }] }, { status: 404 });
    await expect(deletePreviews(options, () => true, { fetch })).rejects.toThrow('8000007: Project not found');
  });
});
