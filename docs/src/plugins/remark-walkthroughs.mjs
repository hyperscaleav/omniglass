// remarkWalkthroughs renders a `::walkthrough{#id}` leaf directive from the
// page's `walkthroughs` frontmatter (#926), exactly as `::screenshot` resolves
// against `screenshots` (the shared frontmatterDirective). The content comes from
// the recorder's manifest, copied with the stills into public/walkthroughs/<id>/
// by `make docs-walkthroughs`: the page never restates a caption, so a
// re-recorded walkthrough reaches the docs by a copy, not an edit.
//
// The video is not committed (size), so the embed renders it, and links it, only
// when public/walkthroughs/<id>/<id>.mp4 is present when the page renders; a
// build without it shows the animated preview (committed with the stills, #937),
// or the poster step for a recording without one, and the captioned stills.
//
// What this plugin reads is invisible to Astro's content cache, which keys a
// rendered page on the page's own content alone: after a re-copy, or a video
// added or removed, an unforced build serves the old render. Astro offers a
// remark plugin no way to declare the dependency, so the docs build digests
// what this plugin reads (scripts/lib/render-stamp.mjs) and forces Astro when
// the digest changes (scripts/astro-build.mjs, #937);
// scripts/check-walkthrough-embeds.mjs then re-renders every built embed from
// the shipped manifest and fails the build on any difference.
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { frontmatterDirective } from './frontmatter-directive.mjs';
import { renderWalkthrough, videoName } from './walkthrough-embed.mjs';

/** @param {{ publicDir: string }} opts the docs site's public/ directory */
export function remarkWalkthroughs({ publicDir }) {
  return frontmatterDirective('walkthrough', 'walkthroughs', (spec, file, node) => {
    const dir = join(publicDir, 'walkthroughs', spec.id);
    const manifestPath = join(dir, 'walkthrough.json');
    if (!existsSync(manifestPath)) {
      file.fail(
        `::walkthrough{#${spec.id}} has no ${manifestPath}; record it (make walkthroughs ID=${spec.id}) and copy it (make docs-walkthroughs)`,
        node,
      );
    }
    const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
    if (manifest.id !== spec.id) {
      file.fail(`${manifestPath} is walkthrough "${manifest.id}", not "${spec.id}"; re-run make docs-walkthroughs`, node);
    }
    return renderWalkthrough(manifest, { video: existsSync(join(dir, videoName(spec.id))) });
  });
}
