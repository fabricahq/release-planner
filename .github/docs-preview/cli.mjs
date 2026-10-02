/**
 * @fileoverview Command-line entry for the documentation preview workflow's Cloudflare steps.
 *
 * Usage: node .github/docs-preview/cli.mjs <deploy|teardown|reconcile>
 * Environment: CLOUDFLARE_API_TOKEN, plus
 *   deploy: PREVIEW_PR, PREVIEW_SHA, and PREVIEW_SITE, the downloaded docs-site artifact.
 *     Writes url and alias to GITHUB_OUTPUT.
 *   teardown: PREVIEW_PR. Deletes that pull request's previews.
 *   reconcile: PREVIEW_OPEN_PRS, a JSON list, and PREVIEW_CUTOFF, an ISO time. Deletes
 *     older previews of pull requests that aren't open into main.
 */

import { randomUUID } from 'node:crypto';
import { appendFileSync } from 'node:fs';
import { deletePreviews, deployPreview, keepOpenPullRequests, keepOtherBranches } from './cloudflare.mjs';
import { cloudflareAccountID, pagesProject, previewBranch } from './config.mjs';
import { addPreviewHeaders, validateSite } from './site.mjs';

const command = process.argv[2];
const target = { accountID: cloudflareAccountID, project: pagesProject, token: process.env.CLOUDFLARE_API_TOKEN };
if (!target.token) throw new Error('CLOUDFLARE_API_TOKEN is not set.');

// Output from here on can include text from the pull request's build, so stop the runner
// from reading workflow commands in it.
const resume = randomUUID();
console.log(`::stop-commands::${resume}`);

if (command === 'deploy') {
  const pr = Number(process.env.PREVIEW_PR);
  const site = process.env.PREVIEW_SITE;
  validateSite(site);
  addPreviewHeaders(site);
  const { url, alias } = await deployPreview({ ...target, site, branch: previewBranch(pr), sha: process.env.PREVIEW_SHA, pr });
  console.log(`Deployed ${url} (${alias})`);
  appendFileSync(process.env.GITHUB_OUTPUT, `url=${url}\nalias=${alias}\n`);
} else if (command === 'teardown') {
  const branch = previewBranch(Number(process.env.PREVIEW_PR));
  const count = await deletePreviews(target, keepOtherBranches(branch));
  console.log(`Deleted ${count} preview deployment(s) of ${branch}.`);
} else if (command === 'reconcile') {
  const open = JSON.parse(process.env.PREVIEW_OPEN_PRS);
  const cutoff = process.env.PREVIEW_CUTOFF;
  if (!Array.isArray(open) || !open.every(Number.isSafeInteger) || Number.isNaN(Date.parse(cutoff))) {
    throw new Error('PREVIEW_OPEN_PRS must be a list of pull request numbers, and PREVIEW_CUTOFF a time.');
  }
  const count = await deletePreviews(target, keepOpenPullRequests(open, cutoff));
  console.log(`Deleted ${count} preview deployment(s) of pull requests that aren't open into main.`);
} else {
  throw new Error('Usage: cli.mjs <deploy|teardown|reconcile>');
}

console.log(`::${resume}::`);
