/** @fileoverview Adapts Starlight callout titles for accessible labeling and copied HTML. */

/**
 * Return a transformer that mutates callouts to expose their titles in copied HTML and accessible names.
 * Preserves existing title IDs and assigns collision-free IDs where missing; hides decorative title icons.
 */
export default function accessibleAsideTitles() {
  return (tree) => {
    const ids = new Set();
    const asides = [];
    /** Collect all existing IDs before generating labels so later elements cannot collide. */
    function visit(node) {
      if (node.type === 'element') {
        if (node.properties?.id) ids.add(node.properties.id);
        if (node.tagName === 'aside') asides.push(node);
      }
      for (const child of node.children ?? []) visit(child);
    }
    visit(tree);

    let sequence = 0;
    for (const aside of asides) {
      const title = aside.children.find((node) =>
        node.type === 'element' &&
        [].concat(node.properties?.className ?? []).includes('starlight-aside__title')
      );
      if (!title) continue;

      if (!title.properties.id) {
        let id;
        do { id = `cr-aside-title-${++sequence}`; } while (ids.has(id));
        title.properties.id = id;
        ids.add(id);
      }
      delete title.properties.ariaHidden;
      delete aside.properties.ariaLabel;
      aside.properties.ariaLabelledBy = [title.properties.id];
      for (const child of title.children) {
        if (child.type === 'element' && child.tagName === 'svg') {
          child.properties.ariaHidden = 'true';
        }
      }
    }
  };
}
