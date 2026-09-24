/** @fileoverview Configures the Release Planner documentation site, navigation, and Markdown rendering. */

import { defineConfig } from 'astro/config';
import { unified } from '@astrojs/markdown-remark';
import starlight from '@astrojs/starlight';
import tailwindcss from '@tailwindcss/vite';
import accessibleAsideTitles from './src/plugins/accessible-aside-titles.mjs';

// The look, theme menu, and callout handling follow the Code Rules documentation site.
export default defineConfig({
  devToolbar: { enabled: false },
  // Keep native bindings outside the SSR bundle: https://vite.dev/config/ssr-options.html#ssr-external
  vite: { plugins: [tailwindcss()], ssr: { external: ['satteri'] } },
  markdown: { processor: unified({ rehypePlugins: [accessibleAsideTitles] }) },
  integrations: [starlight({
    title: 'Release Planner',
    description: 'Agent-drafted, maintainer-approved GitHub releases',
    favicon: '/favicon.svg',
    customCss: ['./src/styles/tailwind.css', './src/styles/custom.css'],
    components: {
      SiteTitle: './src/components/SiteTitle.astro',
      SocialIcons: './src/components/NavLinks.astro',
      ThemeSelect: './src/components/ThemeSelect.astro',
      Footer: './src/components/Footer.astro',
    },
    sidebar: [
      { label: 'Start here', items: [
        { label: 'What is Release Planner?', slug: 'index' },
        { label: 'Set up a repository', slug: 'start-here/set-up' },
        { label: 'Make a release', slug: 'start-here/release' },
      ] },
      { label: 'Customize', items: [
        { label: 'Release policy', slug: 'customize/policy' },
        { label: 'Release notes style', slug: 'customize/release-notes-style' },
        { label: 'Release checks', slug: 'customize/release-checks' },
        { label: 'Configuration', slug: 'customize/configuration' },
      ] },
      { label: 'For agents', items: [{ label: 'How Release Planner works', slug: 'for-agents' }] },
    ],
    tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
  })],
});
