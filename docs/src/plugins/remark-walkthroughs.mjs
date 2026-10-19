// remarkWalkthroughs renders a `::walkthrough{#id}` leaf directive from the
// page's `walkthroughs` frontmatter (#926), exactly as `::screenshot` resolves
// against `screenshots` (the shared frontmatterDirective). The content comes from
// the recorder's manifest, copied with the poster and stills into
// public/walkthroughs/<id>/ by `make docs-walkthroughs`: the page never restates
// a caption, so a re-recorded walkthrough changes the docs by a copy, not an edit.
//
// The video is not committed (size), so the embed renders it, and links it, only
// when public/walkthroughs/<id>/<id>.mp4 is present when the page renders; a
// build without it shows the poster and the captioned stills. Rendered pages are
// cached between builds, so scripts/check-walkthrough-links.mjs re-checks the
// built HTML after `astro build`: a video link to a file the build did not ship
// fails the build rather than publishing a dead link.
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
