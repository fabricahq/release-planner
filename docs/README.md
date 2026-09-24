# Release Planner documentation

The Release Planner documentation site uses Astro and Starlight. Its look, theme menu, and callout handling follow the [Code Rules documentation](https://github.com/fabricahq/code-rules/tree/main/docs), without the homepage: the site opens on "What is Release Planner?"

## Run locally

From this directory:

```sh
bun install --frozen-lockfile
bun run dev
```

Before you push, check and build the site:

```sh
bun run check
bun run build
```

The output is `dist/`. These commands do not publish the site.

## Publishing

The Documentation workflow publishes the site to GitHub Pages at <https://release-planner.fabricahq.com> after each push to `main`. Pull requests only check and build it. The site is served at the domain root, so page links are root-relative. The Pages settings, custom domain, and DNS record are managed in Fabrica's infrastructure repository, not here.

## Maintain the docs

Pages live under `src/content/docs/`, in folders matching the sidebar: `start-here/`, `customize/`, and `for-agents.md`. The sidebar is defined in `astro.config.mjs`.

Keep the human pages short and focused on setting up and customizing releases. Put command details, file formats, and workflow internals in "For agents". When a command, config key, or generated file changes, update the page that describes it in the same pull request.
