/** @fileoverview Builds the head script that loads Cloudflare Web Analytics on the production site only. */

/**
 * Return a Starlight head entry that adds Cloudflare's cookieless beacon only when the page is served
 * from `host`, so local development, previews, and tests never report visits or contact Cloudflare.
 * @param {{ host: string, token: string }} site the production hostname and its public Web Analytics site token
 * @returns {{ tag: 'script', content: string }} the head entry
 */
export function cloudflareWebAnalytics({ host, token }) {
  return {
    tag: 'script',
    content: `if (location.hostname === ${JSON.stringify(host)}) {
  const beacon = document.createElement('script');
  beacon.src = 'https://static.cloudflareinsights.com/beacon.min.js';
  beacon.dataset.cfBeacon = ${JSON.stringify(JSON.stringify({ token }))};
  document.head.append(beacon);
}`,
  };
}
