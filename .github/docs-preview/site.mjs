/** @fileoverview Checks a pull request's built documentation site before it's uploaded. */

import { lstatSync, readdirSync, writeFileSync } from 'node:fs';
import { join, relative } from 'node:path';

// The built site is about 2 MB in 60 files; these bounds leave ample headroom.
// Pages also rejects any file over 25 MiB.
const maxSiteBytes = 100 * 1024 * 1024;
const maxSiteFiles = 5_000;
const maxFileBytes = 25 * 1024 * 1024;

// Pages treats these top-level entries as configuration or server code rather than files to
// serve: _worker.js would run as a Worker, and _headers or _redirects could remove the
// preview's noindex header or redirect visitors elsewhere. The documentation build makes none.
const reservedNames = new Set(['_worker.js', '_routes.json', '_headers', '_redirects', 'functions']);

// File names come from the pull request; quoting keeps a name from forging a workflow command.
function show(root, path) {
  return JSON.stringify(relative(root, path));
}

/**
 * Throws unless the site is only regular files and directories within the size bounds, with
 * an index page and no Pages configuration. Wrangler follows symlinks and would upload their
 * targets, so a symlink anywhere fails the check.
 */
export function validateSite(root) {
  if (!lstatSync(join(root, 'index.html'), { throwIfNoEntry: false })?.isFile()) {
    throw new Error('The site has no index.html.');
  }
  let bytes = 0;
  let files = 0;
  const pending = [root];
  while (pending.length > 0) {
    const directory = pending.pop();
    for (const name of readdirSync(directory)) {
      const path = join(directory, name);
      if (directory === root && reservedNames.has(name)) {
        throw new Error(`The site may not contain ${JSON.stringify(name)}, which Pages treats as configuration or code.`);
      }
      const stats = lstatSync(path);
      if (stats.isDirectory()) {
        pending.push(path);
        continue;
      }
      if (!stats.isFile()) {
        throw new Error(`The site may contain only files and directories: ${show(root, path)}`);
      }
      if (stats.size > maxFileBytes) {
        throw new Error(`${show(root, path)} is larger than 25 MiB.`);
      }
      bytes += stats.size;
      files += 1;
      if (bytes > maxSiteBytes || files > maxSiteFiles) {
        throw new Error('The site exceeds the preview size or file-count limit.');
      }
    }
  }
}

// Every preview response gets these headers. The policy allows only the site's own scripts,
// styles, fonts, images, and requests, so a page can't load code or content from elsewhere
// after review, and it can't submit a form anywhere. Starlight and its Pagefind search work
// within it. Previews are noindex by default; this keeps them so.
const previewHeaders = `/*
  Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'none'; frame-ancestors 'none'
  X-Robots-Tag: noindex
  X-Content-Type-Options: nosniff
  Referrer-Policy: no-referrer
`;

/** Adds the preview's own _headers file to a site that validateSite accepted. */
export function addPreviewHeaders(root) {
  writeFileSync(join(root, '_headers'), previewHeaders, { flag: 'wx' });
}
