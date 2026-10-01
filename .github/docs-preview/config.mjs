/** @fileoverview Names shared by the documentation preview workflow and its scripts. */

// The Cloudflare Pages project is managed in Fabrica's infrastructure repository
// (infra-live, cloudflare/fabricahq/release-planner-docs-previews). Neither value is secret.
export const cloudflareAccountID = '5171b397327ee7e50cc9abcb85a2be50';
export const pagesProject = 'release-planner-docs-previews';

// The deploy job reads the Cloudflare token from this environment, which admits only main.
export const deployEnvironment = 'docs-preview';
// Previews of pull requests from forks or from authors without write access wait in this
// environment until a maintainer approves them. It must have required reviewers.
export const approvalEnvironment = 'docs-preview-approval';

// Starts every preview comment, so the workflow can find and update its own comment.
export const commentMarker = '<!-- release-planner-docs-preview -->';

/** Returns the Pages branch, and so the alias hostname label, for a pull request. */
export function previewBranch(pr) {
  if (!Number.isSafeInteger(pr) || pr <= 0) throw new Error(`Invalid pull request number: ${pr}`);
  return `pr-${pr}`;
}

const contentPage = /^docs\/src\/content\/docs\/([a-z0-9][a-z0-9/-]*)\.mdx?$/;

/**
 * Returns the site path of a documentation page changed by a pull request, or null when
 * the file isn't a page. File names come from the pull request, so anything outside a
 * strict character set is ignored rather than placed in a link.
 */
export function pagePath(filename) {
  const match = contentPage.exec(filename);
  if (!match || match[1].includes('//') || match[1].endsWith('/')) return null;
  const slug = match[1].replace(/(^|\/)index$/, '');
  return slug === '' ? '/' : `/${slug}/`;
}

/** Reports whether a changed file is part of the documentation site. */
export function isDocsFile(filename) {
  return filename.startsWith('docs/');
}
