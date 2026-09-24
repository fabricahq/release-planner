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

The output is `dist/`. Hosting is not configured, and these commands do not publish the site.

## Maintain the docs

Pages live under `src/content/docs/`, in folders matching the sidebar: `start-here/`, `customize/`, and `for-agents.md`. The sidebar is defined in `astro.config.mjs`.

Keep the human pages short and focused on setting up and customizing releases. Put command details, file formats, and workflow internals in "For agents". When a command, config key, or generated file changes, update the page that describes it in the same pull request.
