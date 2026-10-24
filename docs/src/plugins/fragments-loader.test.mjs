// Unit tests for withFragments (fragments-loader.mjs): a host page's digest
// follows its fragment directory, and in dev a fragment change re-announces the
// host page, including after Astro's full re-sync drops every listener.
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { join, sep } from 'node:path';
import { describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';
import { withFragments } from './fragments-loader.mjs';

const srcDir = new URL('../', import.meta.url);
const buildLogDir = join(fileURLToPath(srcDir), 'content', 'build-log');

class FakeWatcher extends EventEmitter {
  add() {}
}

const load = (loader, watcher) => loader.load({ config: { srcDir }, generateDigest: (d) => d, watcher });

describe('withFragments', () => {
  it('folds the fragment directory into a host page digest only', async () => {
    let digest;
    const inner = { name: 'inner', load: async (ctx) => { digest = ctx.generateDigest; } };
    await withFragments(inner).load({ config: { srcDir }, generateDigest: (d) => d });
    assert.equal(digest('plain page'), 'plain page');
    assert.match(digest('Intro.\n\n::build-log\n'), /\0fragments:[0-9a-f]{64}$/);
  });

  it('re-announces the host page on a fragment change, after a full re-sync too', async () => {
    const watcher = new FakeWatcher();
    const loader = withFragments({ name: 'inner', load: async () => {} });
    await load(loader, watcher);
    // Astro's full content re-sync removes the listeners a loader added, then loads again.
    watcher.removeAllListeners('all');
    await load(loader, watcher);
    assert.equal(watcher.listenerCount('all'), 1);
    const announced = [];
    watcher.on('change', (path) => announced.push(path));
    watcher.emit('all', 'add', join(buildLogDir, '2026-10-25-new.md'));
    assert.deepEqual(
      announced.map((p) => p.split(sep).slice(-2).join('/')),
      ['architecture/build-log.md'],
    );
  });
});
