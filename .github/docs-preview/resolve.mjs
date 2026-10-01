/**
 * @fileoverview Decides what the documentation preview workflow does for an event, before
 * any job can read the Cloudflare token. Runs in actions/github-script from main.
 *
 * Outputs: action (deploy, stop, reconcile, or skip). For deploy and stop: pr. For deploy:
 * sha, run_id, trusted, pages (a JSON list of the first changed pages' site paths), and
 * pages_total. For reconcile: open_prs (a JSON list) and cutoff (an ISO time).
 */

import { approvalEnvironment, commentMarker, deployEnvironment, isDocsFile, pagePath } from './config.mjs';

const docsWorkflowPath = '.github/workflows/docs.yml';
const maxListedPages = 25;

/** @param {{github: any, context: any, core: any}} script */
export async function resolve({ github, context, core }) {
  const { owner, repo } = context.repo;
  const repositoryID = context.payload.repository.id;
  const defaultBranch = context.payload.repository.default_branch;
  const skip = (reason) => {
    core.notice(reason);
    core.setOutput('action', 'skip');
  };
  if (context.ref !== `refs/heads/${defaultBranch}`) {
    // A pull request into another branch runs that branch's copy of this workflow on close.
    // Its previews, if any, are left to the reconciler on main.
    if (context.eventName === 'pull_request_target') {
      return skip(`Previews are managed only for pull requests into ${defaultBranch}.`);
    }
    throw new Error('Documentation previews must run the default-branch workflow.');
  }

  if (context.eventName === 'schedule' || context.eventName === 'workflow_dispatch') {
    // Taken before listing, so a deployment made after it is never mistaken for an orphan.
    const cutoff = new Date().toISOString();
    const open = await github.paginate(github.rest.pulls.list, { owner, repo, state: 'open', per_page: 100 });
    core.setOutput('action', 'reconcile');
    core.setOutput('open_prs', JSON.stringify(open.map((pr) => pr.number)));
    core.setOutput('cutoff', cutoff);
    return;
  }

  if (context.eventName === 'pull_request_target') {
    const pr = context.payload.pull_request;
    if (context.payload.action !== 'closed' || pr.base.repo.id !== repositoryID) {
      return skip('Only closed pull requests in this repository remove a preview.');
    }
    // A deploy can still be running without having commented yet, so a documentation
    // change is enough to look for previews to remove.
    if (!(await changesDocs(github, owner, repo, pr.number)) &&
        !(await findPreviewComment(github, { owner, repo, issue_number: pr.number }))) {
      return skip(`Pull request #${pr.number} has no documentation preview to remove.`);
    }
    core.setOutput('action', 'stop');
    core.setOutput('pr', String(pr.number));
    return;
  }

  if (context.eventName !== 'workflow_run') {
    throw new Error(`Unsupported event: ${context.eventName}`);
  }
  const run = context.payload.workflow_run;
  if (run.path !== docsWorkflowPath || run.event !== 'pull_request' || run.status !== 'completed' ||
      run.conclusion !== 'success' || run.repository.id !== repositoryID) {
    return skip('Only successful pull request Documentation runs in this repository are previewed.');
  }
  // GitHub withholds secrets from runs that Dependabot triggers, so its previews can't deploy.
  if (run.triggering_actor?.login === 'dependabot[bot]') {
    return skip('Dependabot pull requests are not previewed.');
  }

  const artifacts = await github.paginate(github.rest.actions.listWorkflowRunArtifacts, {
    owner, repo, run_id: run.id, per_page: 100,
  });
  const sites = artifacts.filter((artifact) => artifact.name === 'docs-site');
  if (sites.length !== 1 || sites[0].expired || sites[0].size_in_bytes <= 0) {
    return skip('The run has a missing, duplicate, or expired docs-site artifact.');
  }

  // Match the pull request by the run's own repository, branch, and commit, not by commit
  // alone: a fork can open a pull request whose head is the same commit as another's.
  const head = run.head_repository;
  const candidates = await github.paginate(github.rest.pulls.list, {
    owner, repo, state: 'open', head: `${head.owner.login}:${run.head_branch}`, base: defaultBranch, per_page: 100,
  });
  const matches = candidates.filter((pr) => pr.base.repo.id === repositoryID && pr.base.ref === defaultBranch &&
    pr.head.repo?.id === head.id && pr.head.ref === run.head_branch && pr.head.sha === run.head_sha);
  if (matches.length !== 1) {
    return skip(`Expected one open pull request into ${defaultBranch} with this run's branch at its head; found ${matches.length}.`);
  }
  const [pr] = matches;

  const files = await listFiles(github, owner, repo, pr.number);
  if (!files.some(touchesDocs)) {
    return skip(`Pull request #${pr.number} doesn't change the documentation.`);
  }
  const pages = [...new Set(files
    .filter((file) => file.status !== 'removed')
    .map((file) => pagePath(file.filename))
    .filter(Boolean))].sort();

  // The build ran the pull request's code, so its pages are untrusted. Only builds of branches
  // in this repository, which only writers can push to, opened by a writer, deploy unreviewed.
  const trusted = head.id === repositoryID && await canWrite(github, owner, repo, pr.user.login);
  await requireEnvironments(github, owner, repo, defaultBranch, !trusted);

  core.setOutput('action', 'deploy');
  core.setOutput('pr', String(pr.number));
  core.setOutput('sha', run.head_sha);
  core.setOutput('run_id', String(run.id));
  core.setOutput('trusted', String(trusted));
  core.setOutput('pages', JSON.stringify(pages.slice(0, maxListedPages)));
  core.setOutput('pages_total', String(pages.length));
}

/**
 * Rechecks a pull request right before deploying, since approval can take a while. Sets
 * the state output: current, superseded by a newer commit, or closed.
 *
 * @param {{github: any, context: any, core: any}} script
 * @param {{pr: number, sha: string}} preview
 */
export async function recheck({ github, context, core }, { pr, sha }) {
  const { data } = await github.rest.pulls.get({ ...context.repo, pull_number: pr });
  const state = data.state !== 'open' ? 'closed' : data.head.sha !== sha ? 'superseded' : 'current';
  if (state !== 'current') core.notice(`Not deploying ${sha}: pull request #${pr} is ${state}.`);
  core.setOutput('state', state);
}

/** Finds the workflow's own preview comment on a pull request. */
export async function findPreviewComment(github, { owner, repo, issue_number }) {
  const comments = await github.paginate(github.rest.issues.listComments, {
    owner, repo, issue_number, per_page: 100,
  });
  return comments.find((comment) =>
    comment.user?.login === 'github-actions[bot]' && comment.body?.startsWith(commentMarker));
}

function listFiles(github, owner, repo, pull_number) {
  return github.paginate(github.rest.pulls.listFiles, { owner, repo, pull_number, per_page: 100 });
}

function touchesDocs(file) {
  return isDocsFile(file.filename) || (file.previous_filename !== undefined && isDocsFile(file.previous_filename));
}

async function changesDocs(github, owner, repo, pull_number) {
  return (await listFiles(github, owner, repo, pull_number)).some(touchesDocs);
}

async function canWrite(github, owner, repo, username) {
  try {
    const { data } = await github.rest.repos.getCollaboratorPermissionLevel({ owner, repo, username });
    return ['admin', 'write'].includes(data.permission);
  } catch (error) {
    if (error.status === 404) return false;
    throw error;
  }
}

/**
 * Fails unless the environments still protect the token. GitHub creates a missing
 * environment, without protection, the first time a job uses it, which would deploy
 * untrusted pull requests unreviewed or let other branches read the token.
 */
async function requireEnvironments(github, owner, repo, defaultBranch, needsApproval) {
  const setup = 'See docs/README.md.';
  const deploy = await getEnvironment(github, owner, repo, deployEnvironment);
  const policies = deploy?.deployment_branch_policy?.custom_branch_policies
    ? (await github.paginate(github.rest.repos.listDeploymentBranchPolicies, {
      owner, repo, environment_name: deployEnvironment, per_page: 100,
    }))
    : [];
  if (policies.length === 0 || policies.some((policy) => policy.name !== defaultBranch || policy.type !== 'branch')) {
    throw new Error(`The ${deployEnvironment} environment must admit only the ${defaultBranch} branch. ${setup}`);
  }
  if (!needsApproval) return;
  const approval = await getEnvironment(github, owner, repo, approvalEnvironment);
  const reviewers = approval?.protection_rules
    ?.filter((rule) => rule.type === 'required_reviewers')
    .flatMap((rule) => rule.reviewers ?? []) ?? [];
  if (reviewers.length === 0) {
    throw new Error(`The ${approvalEnvironment} environment must exist with required reviewers. ${setup}`);
  }
}

async function getEnvironment(github, owner, repo, environment_name) {
  try {
    return (await github.rest.repos.getEnvironment({ owner, repo, environment_name })).data;
  } catch (error) {
    if (error.status === 404) return undefined;
    throw error;
  }
}
