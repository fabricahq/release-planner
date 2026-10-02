/**
 * @fileoverview Writes the sticky documentation preview comment on a pull request. Runs in
 * actions/github-script from main. Every value placed in the comment is a number, a commit
 * SHA, a Cloudflare URL, or a site path that config.pagePath has already restricted.
 */

import { approvalEnvironment, commentMarker } from './config.mjs';
import { findPreviewComment } from './resolve.mjs';

const heading = '### Documentation preview';

/**
 * Creates or updates the preview comment. Each body records the run that wrote it, and a
 * comment from a newer run is never replaced by an older one. Removal always applies.
 *
 * @param {{github: any, context: any, core: any}} script
 * @param {{pr: number, state: 'pending' | 'deployed' | 'failed' | 'removed', sha?: string,
 *   url?: string, alias?: string, pages?: string[], pagesTotal?: number,
 *   reason?: 'closed' | 'retargeted'}} preview
 */
export async function writeComment({ github, context, core }, preview) {
  const { owner, repo } = context.repo;
  const issue_number = preview.pr;
  const previous = await findPreviewComment(github, { owner, repo, issue_number });
  if (preview.state === 'removed' && !previous) return;
  if (preview.state !== 'removed' && isNewer(previous?.body, context.runId, context.runAttempt)) {
    core.notice('A newer run has already updated the preview comment.');
    return;
  }
  const body = renderComment(preview, {
    runURL: `${context.serverUrl}/${owner}/${repo}/actions/runs/${context.runId}`,
    commitURL: (sha) => `${context.serverUrl}/${owner}/${repo}/commit/${sha}`,
    stamp: `<!-- run:${context.runId} attempt:${context.runAttempt} -->`,
  });
  if (previous) {
    await github.rest.issues.updateComment({ owner, repo, comment_id: previous.id, body });
  } else {
    await github.rest.issues.createComment({ owner, repo, issue_number, body });
  }
}

/** Reports whether a comment body was written by a later run, or a later attempt of this one. */
export function isNewer(body, runId, runAttempt) {
  const stamp = body?.match(/<!-- run:(\d+) attempt:(\d+) -->/);
  if (!stamp) return false;
  const [stampRun, stampAttempt] = [Number(stamp[1]), Number(stamp[2])];
  return stampRun > runId || (stampRun === runId && stampAttempt > runAttempt);
}

/** Renders a preview comment body. */
export function renderComment(preview, { runURL, commitURL, stamp }) {
  const commit = preview.sha && `[\`${preview.sha.slice(0, 7)}\`](${commitURL(preview.sha)})`;
  let status;
  switch (preview.state) {
    case 'pending':
      status = [
        `The preview of ${commit} is waiting for a maintainer. This pull request comes from a fork or from an author without write access, so its build is published only after review.`,
        '',
        `Maintainers: review the commit, then approve the \`${approvalEnvironment}\` deployment in [the workflow run](${runURL}).`,
      ];
      break;
    case 'deployed': {
      const base = (preview.alias ?? preview.url).replace(/\/$/, '');
      status = [`**[Open preview](${base}/)** · built from ${commit} · [this commit's preview](${preview.url}) · updated on every push`];
      if (preview.pages?.length) {
        status.push('', 'Changed pages:', '', ...preview.pages.map((path) => `- [\`${path}\`](${base}${path})`));
        const more = (preview.pagesTotal ?? 0) - preview.pages.length;
        if (more > 0) status.push(`- and ${more} more`);
      }
      status.push('', 'Public, and not indexed by search engines. Removed when this pull request closes.');
      break;
    }
    case 'failed':
      status = [`⚠️ The preview of ${commit} failed to deploy. See [the workflow run](${runURL}).`];
      break;
    case 'removed':
      status = [preview.reason === 'retargeted'
        ? 'Preview removed because this pull request no longer targets the default branch.'
        : 'Preview removed because this pull request closed.'];
      break;
    default:
      throw new Error(`Unknown preview state: ${preview.state}`);
  }
  return [commentMarker, stamp, heading, '', ...status].join('\n');
}
