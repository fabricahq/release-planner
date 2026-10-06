/** @fileoverview Builds the head script that loads Cloudflare Web Analytics on the production site only. */

/**
 * Return a Starlight head entry that adds Cloudflare's cookieless beacon only when the page is served
 * from `host`, so local development, previews, and tests never report visits or contact Cloudflare.
 * Analytics is work no visitor waits for, so the beacon loads only after the page has loaded and the
 * next frame is painted, when the browser is idle or two seconds later at most. The beacon reads
 * buffered performance entries, so loading it late loses no measurements.
 * @param {{ host: string, token: string }} site the production hostname and its public Web Analytics site token
 * @returns {{ tag: 'script', content: string }} the head entry
 */
export function cloudflareWebAnalytics({ host, token }) {
  return {
    tag: 'script',
    content: `if (location.hostname === ${JSON.stringify(host)}) {
  const loadBeacon = () => {
    const beacon = document.createElement('script');
    beacon.src = 'https://static.cloudflareinsights.com/beacon.min.js';
    beacon.dataset.cfBeacon = ${JSON.stringify(JSON.stringify({ token }))};
    document.head.append(beacon);
  };
  const whenIdle = () =>
    typeof requestIdleCallback === 'function' ? requestIdleCallback(loadBeacon, { timeout: 2000 }) : setTimeout(loadBeacon, 1);
  // An idle period that starts inside a frame callback follows that frame's paint.
  const afterNextFrame = () => requestAnimationFrame(whenIdle);
  if (document.readyState === 'complete') afterNextFrame();
  else addEventListener('load', afterNextFrame, { once: true });
}`,
  };
}
