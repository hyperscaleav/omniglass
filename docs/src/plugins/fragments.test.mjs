// Unit tests for the fragment-directory primitive (fragments.mjs): the parse,
// the three renders and the directive expansion the build log and the decision
// log are assembled by. Run by `pnpm test` and first in `pnpm build`.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import {
  adrAnchor,
  directivesIn,
  expandFragments,
  missingLogIds,
  parseFragment,
  renderBuildLog,
  renderDecisionIndex,
  renderDecisionLog,
} from './fragments.mjs';

const frag = (name, text) => ({ name, text });

describe('parseFragment', () => {
  it('reads the frontmatter value forms and trims the body', () => {
    const f = parseFragment(
      '2026-10-24-x.md',
      '---\ntitle: "A `code` title: with a colon"\ndate: 2026-10-24\nrefs: [12, 34]\nsummary: \'It\'\'s quoted\'\n---\n\n\nBody line.\n\n',
    );
    assert.deepEqual(f.data, {
      title: 'A `code` title: with a colon',
      date: '2026-10-24',
      refs: [12, 34],
      summary: "It's quoted",
    });
    assert.equal(f.body, 'Body line.');
  });

  it('accepts CRLF line endings', () => {
    const f = parseFragment('a.md', '---\r\ntitle: T\r\n---\r\nBody\r\n');
    assert.equal(f.data.title, 'T');
    assert.equal(f.body, 'Body');
  });

  for (const [why, text, msg] of [
    ['no opening fence', 'title: T\n---\nBody', /must open with a --- frontmatter fence/],
    ['an unclosed fence', '---\ntitle: T\nBody', /never closes/],
    ['a line that is not key: value', '---\ntitle T\n---\nBody', /line 2 .*not a "key: value" line/],
    ['a duplicate key', '---\ntitle: A\ntitle: B\n---\nBody', /duplicate key "title"/],
    ['a list of non-integers', '---\nrefs: ["a"]\n---\nBody', /refs .*integers/],
    ['a list with a decimal', '---\nrefs: [1.0]\n---\nBody', /refs .*integers/],
    ['a list with an exponent', '---\nrefs: [1e3]\n---\nBody', /refs .*integers/],
    ['a list with a leading zero', '---\nrefs: [01]\n---\nBody', /refs .*integers/],
    ['a number too long to be an issue', '---\nrefs: [1234567890]\n---\nBody', /refs .*integers/],
    ['a line separator in a value', '---\ntitle: A\u2028B\n---\nBody', /line 2 .*control character/],
    ['a lone carriage return in a value', '---\ntitle: A\rB\n---\nBody', /line 2 .*control character/],
    ['a newline escaped into a string', '---\ntitle: "First\\nline two"\n---\nBody', /title .*control character/],
    ['an empty body', '---\ntitle: T\n---\n\n', /empty body/],
  ]) {
    it(`refuses ${why}, naming the file`, () => {
      assert.throws(() => parseFragment('bad.md', text), (err) => {
        assert.match(err.message, /bad\.md/);
        assert.match(err.message, msg);
        return true;
      });
    });
  }
});

describe('renderBuildLog', () => {
  const section = (name, title, date, body = 'Body of ' + title + '.') =>
    frag(name, `---\ntitle: ${JSON.stringify(title)}\ndate: ${date}\nrefs: [1]\n---\n\n${body}\n`);
  const bullet = (name, title, date) =>
    frag(name, `---\ntitle: ${JSON.stringify(title)}\ndate: ${date}\nrefs: []\nform: bullet\n---\n\n- **${title}.** Text.\n  More.\n`);

  it('lets a body show a directive inside a fenced code block', () => {
    const body = 'A page embeds a shot with:\n\n```markdown\n::screenshot{#console-map}\n```';
    assert.equal(renderBuildLog([section('2026-10-25-x.md', 'T (#1)', '2026-10-25', body)]), `### T (#1)\n\n${body}`);
  });

  it('orders by file name, the historical sequence first, and joins with one blank line', () => {
    const out = renderBuildLog([
      section('2026-10-25-b.md', 'Later (#2)', '2026-10-25'),
      section('2026-10-25-a.md', 'Same day (#3)', '2026-10-25'),
      bullet('0001-first.md', 'First', '2026-06-27'),
      section('0002-second.md', 'Second (#1)', '2026-06-28'),
    ]);
    assert.equal(
      out,
      [
        '- **First.** Text.\n  More.',
        '### Second (#1)\n\nBody of Second (#1).',
        '### Same day (#3)\n\nBody of Same day (#3).',
        '### Later (#2)\n\nBody of Later (#2).',
      ].join('\n\n'),
    );
  });

  for (const [why, f, msg] of [
    ['a file name with no date or sequence prefix', section('later.md', 'T', '2026-10-25'), /later\.md.*<YYYY-MM-DD>-<slug>\.md/],
    ['a date prefix that disagrees with the date', section('2026-10-25-x.md', 'T', '2026-10-26'), /names 2026-10-25 but its date is 2026-10-26/],
    ['a missing title', frag('2026-10-25-x.md', '---\ndate: 2026-10-25\nrefs: []\n---\nB'), /needs a title/],
    ['a malformed date', frag('2026-10-25-x.md', '---\ntitle: T\ndate: Oct 25\nrefs: []\n---\nB'), /date "Oct 25" is not YYYY-MM-DD/],
    ['missing refs', frag('2026-10-25-x.md', '---\ntitle: T\ndate: 2026-10-25\n---\nB'), /needs refs/],
    ['an unknown key', frag('2026-10-25-x.md', '---\ntitle: T\ndate: 2026-10-25\nrefs: []\nauthor: me\n---\nB'), /unknown key "author"/],
    ['a bullet whose body is not a list item', frag('0001-x.md', '---\ntitle: T\ndate: 2026-06-27\nrefs: []\nform: bullet\n---\nNot a bullet.'), /bullet.*must start with "- "/],
    ['a new dated entry in the bullet form', frag('2026-10-26-x.md', '---\ntitle: T\ndate: 2026-10-26\nrefs: []\nform: bullet\n---\n- **T.** Text.'), /form: bullet is the sequence-numbered history's/],
    ['a directive in a body', section('2026-10-25-x.md', 'T (#1)', '2026-10-25', 'Text.\n\n::screenshot{#console-map}'), /2026-10-25-x\.md.*directive/],
    ['a design fence in a body', section('2026-10-25-x.md', 'T (#1)', '2026-10-25', ':::design\nUnowned.\n:::'), /directive/],
  ]) {
    it(`refuses ${why}`, () => {
      assert.throws(() => renderBuildLog([f]), msg);
    });
  }
});

describe('the decision log', () => {
  const adr = (n, title, extra = {}) =>
    frag(
      `adr-${n}-${extra.slug ?? 'x'}.md`,
      `---\ntitle: ${JSON.stringify(title)}\ndate: ${extra.date ?? '2026-06-27'}\nstatus: ${JSON.stringify(extra.status ?? 'Accepted')}\nsummary: ${JSON.stringify(extra.summary ?? 'One line about ' + n)}\n---\n\n- **Date:** ${extra.date ?? '2026-06-27'} | **Status:** Accepted | **Pages:** none\n- **Decision:** ${n}.\n`,
    );

  it('computes the anchor the heading renders with', () => {
    assert.equal(
      adrAnchor('0001', 'AI acts as a user; the `agent` principal is deferred'),
      'adr-0001-ai-acts-as-a-user-the-agent-principal-is-deferred',
    );
    assert.equal(
      adrAnchor('0023', 'The IAM directory reads (`principal`, `role`, `principal_group`) are admin-tier'),
      'adr-0023-the-iam-directory-reads-principal-role-principal_group-are-admin-tier',
    );
    assert.equal(adrAnchor('0137', "Explore is the fleet's door"), 'adr-0137-explore-is-the-fleets-door');
    assert.equal(adrAnchor('0200', 'A [linked](/x/) **bold** word'), 'adr-0200-a-linked-bold-word');
  });

  it('renders the index table in number order, one row per file, a bar in a cell escaped', () => {
    const out = renderDecisionIndex([
      adr('0002', 'Second', { status: 'Superseded by [ADR-0001](#adr-0001-first)', summary: 'Two | with a bar' }),
      adr('0003', 'Third', { summary: 'Already \\| escaped' }),
      adr('0001', 'First'),
    ]);
    assert.equal(
      out,
      [
        '| ID | Date | Status | Decision |',
        '|---|---|---|---|',
        '| [ADR-0001](#adr-0001-first) | 2026-06-27 | Accepted | One line about 0001 |',
        '| [ADR-0002](#adr-0002-second) | 2026-06-27 | Superseded by [ADR-0001](#adr-0001-first) | Two \\| with a bar |',
        '| [ADR-0003](#adr-0003-third) | 2026-06-27 | Accepted | Already \\| escaped |',
      ].join('\n'),
    );
  });

  it('renders each entry under its numbered heading', () => {
    const out = renderDecisionLog([adr('0002', 'Second'), adr('0001', 'First')]);
    assert.match(out, /^### ADR-0001: First\n\n- \*\*Date:\*\* 2026-06-27/);
    assert.match(out, /\.\n\n### ADR-0002: Second\n\n- \*\*Date:\*\*/);
  });

  it('refuses two files that take the same number, naming both', () => {
    assert.throws(
      () => renderDecisionLog([adr('0002', 'Mine', { slug: 'mine' }), adr('0002', 'Theirs', { slug: 'theirs' })]),
      (err) => {
        assert.match(err.message, /ADR-0002/);
        assert.match(err.message, /adr-0002-mine\.md/);
        assert.match(err.message, /adr-0002-theirs\.md/);
        assert.match(err.message, /make adr-renumber/);
        return true;
      },
    );
  });

  for (const [why, f, msg] of [
    ['a file name that is not adr-NNNN-<slug>.md', frag('adr-12-x.md', '---\ntitle: T\ndate: 2026-06-27\nstatus: A\nsummary: S\n---\nB'), /adr-NNNN-<slug>\.md/],
    ['a missing summary', frag('adr-0001-x.md', '---\ntitle: T\ndate: 2026-06-27\nstatus: A\n---\nB'), /needs a summary/],
    ['an unknown key', frag('adr-0001-x.md', '---\ntitle: T\ndate: 2026-06-27\nstatus: A\nsummary: S\nnumber: 1\n---\nB'), /unknown key "number"/],
    ['a directive in a body', frag('adr-0001-x.md', '---\ntitle: T\ndate: 2026-06-27\nstatus: A\nsummary: S\n---\n- **Date:** 2026-06-27\n\n::walkthrough{#x}'), /adr-0001-x\.md.*directive/],
  ]) {
    it(`refuses ${why}`, () => {
      assert.throws(() => renderDecisionIndex([f]), msg);
    });
  }
});

describe('missingLogIds', () => {
  const sources = [
    frag('adr-0001-first.md', '---\ntitle: First\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-27\n'),
    frag('adr-0002-a-move-recomputes.md', '---\ntitle: "A move -- recomputes"\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-27\n'),
  ];
  it('names each decision whose index link has no heading id on the built page', () => {
    const html = '<h3 id="adr-0001-first">ADR-0001: First</h3><h3 id="adr-0002-a-move--recomputes">';
    assert.deepEqual(missingLogIds(html, sources), [
      'adr-0002-a-move-recomputes.md: the index links #adr-0002-a-move----recomputes, but the built page has no such heading',
    ]);
  });
  it('names every decision when the page rendered empty', () => {
    assert.equal(missingLogIds('<main></main>', sources).length, 2);
  });
});

describe('expandFragments', () => {
  const dirs = {
    'build-log': [frag('2026-10-24-x.md', '---\ntitle: "X (#1)"\ndate: 2026-10-24\nrefs: [1]\n---\n\nThe entry.\n')],
    decisions: [frag('adr-0001-first.md', '---\ntitle: First\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-27\n')],
  };
  const load = (dir) => dirs[dir];

  it('splices each directive line with its render and leaves the rest alone', () => {
    const page = 'Intro.\n\n## Index\n\n::decision-index\n\n## Entries\n\n::decision-log\n';
    assert.deepEqual(directivesIn(page), ['decision-index', 'decision-log']);
    assert.equal(
      expandFragments(page, load),
      'Intro.\n\n## Index\n\n| ID | Date | Status | Decision |\n|---|---|---|---|\n' +
        '| [ADR-0001](#adr-0001-first) | 2026-06-27 | Accepted | S |\n\n## Entries\n\n' +
        '### ADR-0001: First\n\n- **Date:** 2026-06-27\n',
    );
  });

  it('expands the build log', () => {
    assert.equal(expandFragments('Intro.\n\n::build-log\n', load), 'Intro.\n\n### X (#1)\n\nThe entry.\n');
  });

  it('leaves a directive shown inside a fenced code block alone', () => {
    const page = 'A host page looks like:\n\n```markdown\n::build-log\n```\n\n~~~\n::decision-log\n~~~\n';
    assert.deepEqual(directivesIn(page), []);
    assert.equal(expandFragments(page, load), page);
  });

  it('leaves a page with no directive untouched, and an inline mention alone', () => {
    const page = 'Write `::build-log` on its own line.\n';
    assert.deepEqual(directivesIn(page), []);
    assert.equal(expandFragments(page, load), page);
  });
});
