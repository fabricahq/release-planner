/**
 * @fileoverview Deploys documentation previews to Cloudflare Pages and deletes them.
 * The Cloudflare token can change every Pages project in the account, so nothing here runs
 * code from a pull request: wrangler comes from main's lockfile, and the site is data.
 */

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';

const api = 'https://api.cloudflare.com/client/v4';
const wranglerScript = join(dirname(fileURLToPath(import.meta.url)), 'node_modules/wrangler/bin/wrangler.js');
// wrangler waits for the deployment, but exits successfully even when it can't tell whether
// the deployment finished, so poll until Cloudflare reports it live.
const statusChecks = 10;
const statusInterval = 3_000;

/**
 * Uploads a validated site as a preview deployment on the given branch, then confirms
 * Cloudflare recorded it as a live static preview. Returns the deployment's URLs.
 *
 * @param {{site: string, branch: string, sha: string, pr: number, accountID: string,
 *   project: string, token: string}} options
 * @param {{fetch?: typeof fetch, wrangler?: (args: string[], env: object, cwd: string) => void,
 *   wait?: (ms: number) => Promise<unknown>}} [deps]
 */
export async function deployPreview(options, { fetch = globalThis.fetch, wrangler = runWrangler, wait = sleep } = {}) {
  const { site, branch, sha, pr, accountID, project, token } = options;
  if (!/^[a-f0-9]{40}$/.test(sha)) throw new Error('Expected a full commit SHA.');
  // An empty working directory: wrangler builds ./functions into a Worker and reads any
  // Wrangler config it finds in the directory it runs from.
  const cwd = mkdtempSync(join(tmpdir(), 'docs-preview-'));
  let deploymentID;
  try {
    const outputFile = join(cwd, 'wrangler-output.ndjson');
    wrangler([
      'pages', 'deploy', site,
      '--project-name', project,
      '--branch', branch,
      '--commit-hash', sha,
      '--commit-message', `Pull request #${pr}`,
      '--commit-dirty=true',
    ], {
      CLOUDFLARE_API_TOKEN: token,
      CLOUDFLARE_ACCOUNT_ID: accountID,
      WRANGLER_OUTPUT_FILE_PATH: outputFile,
      WRANGLER_SEND_METRICS: 'false',
    }, cwd);
    deploymentID = readFileSync(outputFile, 'utf8').split('\n').filter(Boolean)
      .map((line) => JSON.parse(line))
      .find((entry) => entry.type === 'pages-deploy-detailed')?.deployment_id;
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
  if (!/^[a-z0-9-]+$/.test(deploymentID ?? '')) throw new Error('wrangler reported no deployment.');

  const url = `${api}/accounts/${accountID}/pages/projects/${project}/deployments/${deploymentID}`;
  try {
    for (let check = 1; ; check++) {
      const deployment = await request(fetch, token, url);
      const stage = deployment.latest_stage;
      if (stage?.name === 'deploy' && stage.status === 'success') return checkDeployment(deployment, { branch, project });
      if (stage?.status === 'failure' || stage?.status === 'canceled' || check === statusChecks) {
        throw new Error(`Deployment ${deploymentID} didn't go live: ${stage?.name} ${stage?.status}.`);
      }
      await wait(statusInterval);
    }
  } catch (error) {
    await deleteDeployment(fetch, token, accountID, project, deploymentID);
    throw error;
  }
}

/**
 * Throws unless Cloudflare recorded a static preview of the branch on the project's
 * pages.dev host. Returns its URL and branch alias.
 */
export function checkDeployment(deployment, { branch, project }) {
  if (deployment.environment !== 'preview' || deployment.deployment_trigger?.metadata?.branch !== branch) {
    throw new Error(`Deployment ${deployment.id} isn't a preview of ${branch}.`);
  }
  if (deployment.uses_functions) {
    throw new Error(`Deployment ${deployment.id} runs Functions code.`);
  }
  // Cloudflare adds a suffix to the subdomain when the project name is taken elsewhere.
  const host = new RegExp(`^https://([a-z0-9-]+)\\.${project}(-[a-z0-9]+)?\\.pages\\.dev$`);
  const alias = (deployment.aliases ?? []).find((candidate) => host.exec(candidate)?.[1] === branch);
  if (!host.test(deployment.url) || !alias) {
    throw new Error(`Deployment ${deployment.id} has unexpected URLs.`);
  }
  return { url: deployment.url, alias };
}

/**
 * Deletes preview deployments that the keep predicate rejects. Returns how many were deleted.
 *
 * @param {{accountID: string, project: string, token: string}} target
 * @param {(deployment: any) => boolean} keep
 */
export async function deletePreviews({ accountID, project, token }, keep, { fetch = globalThis.fetch } = {}) {
  const base = `${api}/accounts/${accountID}/pages/projects/${project}/deployments`;
  const doomed = [];
  for (let page = 1; ; page++) {
    const { result, result_info: info } = await request(fetch, token, `${base}?env=preview&page=${page}&per_page=25`, {}, true);
    doomed.push(...result.filter((deployment) => !keep(deployment)));
    if (result.length === 0 || page >= (info?.total_pages ?? page)) break;
  }
  for (const deployment of doomed) {
    await deleteDeployment(fetch, token, accountID, project, deployment.id);
  }
  return doomed.length;
}

/** Keeps every deployment except those of the given branch. */
export function keepOtherBranches(branch) {
  return (deployment) => deployment.deployment_trigger?.metadata?.branch !== branch;
}

/**
 * Keeps deployments of open pull requests, and anything created at or after the cutoff,
 * which a pull request opened after the open list was taken may own.
 */
export function keepOpenPullRequests(openPRs, cutoff) {
  const open = new Set(openPRs.map((pr) => `pr-${pr}`));
  return (deployment) => open.has(deployment.deployment_trigger?.metadata?.branch) ||
    !(Date.parse(deployment.created_on) < Date.parse(cutoff));
}

async function deleteDeployment(fetch, token, accountID, project, id) {
  // force deletes a deployment that a branch alias still points at.
  await request(fetch, token,
    `${api}/accounts/${accountID}/pages/projects/${project}/deployments/${id}?force=true`, { method: 'DELETE' });
}

async function request(fetch, token, url, init = {}, envelope = false) {
  const response = await fetch(url, { ...init, headers: { Authorization: `Bearer ${token}` } });
  const body = await response.json().catch(() => null);
  if (!response.ok || !body?.success) {
    const errors = body?.errors?.map((error) => `${error.code}: ${error.message}`).join('; ');
    throw new Error(`Cloudflare API ${init.method ?? 'GET'} failed with ${response.status}${errors ? ` (${errors})` : ''}.`);
  }
  return envelope ? body : body.result;
}

function runWrangler(args, env, cwd) {
  execFileSync(process.execPath, [wranglerScript, ...args], {
    cwd,
    env: { PATH: process.env.PATH, HOME: process.env.HOME, CI: 'true', ...env },
    stdio: 'inherit',
  });
}
