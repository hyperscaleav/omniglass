// @ts-check
// Terminal demos (#1132): the pure half of the terminal recorder
// (web/e2e/tapes.sh). A demo of CLI or API work is a VHS tape,
// scripts/demo/<id>.tape, typed into a real terminal against a lab
// (omniglass lab run) and rendered as a GIF (what plays inline on the PR) and an MP4 (the
// scrubbable video). This module reads a tape's title and description, holds
// it to the house rules, wraps it in the house settings the recorder plays,
// and judges the recording. Plain ESM with no dependencies, so the recorder
// runs it in the Playwright container with no install, and its types are the
// JSDoc below, checked by tsc (web/tsconfig.json); unit-tested in
// web/src/tape.test.ts.
//
// A tape is VHS's own format (one command per line), opened by a comment block:
//
//   # HTTP logins keep a session                 <- the title (the first line)
//   #
//   # A simulated touch panel logs in ...        <- the description (the rest)
//
//   Type `omniglass component get 'hq.west.lobby.$comp.display-1' | jq -c .`
//   Enter
//   Wait                                         <- every Enter is waited on
//
//   Hide                                         <- unseen: a wait or a lookup
//   Type `demo_until 120 'omniglass ... | jq -e ...'`
//   Enter
//   Wait@150s
//   Show
//
// The house rules, each a refusal naming the line:
// - only the commands a demo types and waits on: Type, Enter, Wait (Wait@<t>,
//   Wait+Screen /re/, Wait+Line /re/), Hide, Show and the editing keys;
//   a Set, Output, Require, Source, Env, Screenshot, Copy or Paste is the
//   recorder's, since the house settings below fix the size, font, theme,
//   typing speed and outputs (a Type@<speed> is refused for the same reason);
// - every Enter is followed by a prompt Wait (a plain Wait or Wait@<t>, not a
//   pattern, which could match the command's own typed text) before the next
//   Type or the end, so every command finishes on screen, and a failing one is
//   caught (the prelude lists each command line that exits non-zero, and any
//   such fails the demo); nothing rides after a Hide, Show or Wait on its line;
// - no uuid in typed text: a demo addresses a resource by name or dotted
//   address, and looks an id up by name inside the tape;
// - Hide and Show alternate, ending shown, and something is typed on screen;
// - the pacing rule (PACE, below) is the prelude's alone: no Sleep (a hand-added
//   hold would stack on the rule's or stand in for it), and no typed text that
//   reaches its hooks (the prompt variables, its DEMO_ tables, the prelude's
//   demo__ names, an exec, a trap, or a new interactive shell, which would run
//   without the prelude).
//
// The pacing rule (#1247): the GIF plays inline on the PR, where a viewer
// cannot pause it, so after every command typed on screen the shell holds
// before its prompt returns (and so before the tape's Wait lets the next
// command type): a base read time, more per row its output filled, capped;
// a typed `# caption` holds by its words the same way. A tape writes no
// Sleep; it reads at the rule's pace.
//
// Inside the terminal (web/e2e/tape/prelude.sh, sourced unseen first): the
// lab's server and an owner token are in the CLI's env; the lab's container
// logs are files under /stack/logs/ (server.log, and node.log, which carries
// the node's in-process simulator too); and
// demo_hush '<code>' runs code unseen, demo_until <seconds> '<code>' retries it
// until it succeeds, each erasing its own command line, so a hidden block
// leaves the screen as it found it.

import { ID } from './walkthroughcore.mjs';

// The house settings every tape is played in (#1132): one size, font, theme and
// typing speed, so every demo reads alike and a re-recording differs only where
// the product did. The font and theme ship in the recorder's pinned image.
export const HOUSE = Object.freeze({
  width: 1280,
  height: 720,
  fontFamily: 'JetBrains Mono',
  fontSize: 15,
  padding: 20,
  theme: 'Catppuccin Mocha',
  typingSpeed: '20ms',
  framerate: 20,
  // A plain Wait's limit; a long wait says its own (Wait@150s).
  waitTimeout: '60s',
  // The last frame's hold before the GIF loops.
  hold: '3s',
});

// The pacing rule (#1247), in milliseconds: an answer holds baseMs plus perMs a
// row its output filled, a caption baseMs plus perMs a word, each up to maxMs.
// wrapTape hands the prelude these as tables (holdTable), which
// web/e2e/tape/pace.sh reads; it never restates the numbers.
export const PACE = Object.freeze({
  output: Object.freeze({ baseMs: 2500, perMs: 500, maxMs: 7000 }),
  caption: Object.freeze({ baseMs: 1500, perMs: 200, maxMs: 4500 }),
});

/** @param {{ baseMs: number, perMs: number, maxMs: number }} rule @param {number} n @returns {number} */
function hold(rule, n) {
  if (!Number.isInteger(n) || n < 0) throw new Error(`a hold is by a count of rows or words, not ${n}`);
  return Math.min(rule.maxMs, rule.baseMs + n * rule.perMs);
}

// The hold after a command whose output filled `rows` terminal rows.
/** @param {number} rows @returns {number} */
export const outputHoldMs = (rows) => hold(PACE.output, rows);

// The hold after a typed caption of `words` words.
/** @param {number} words @returns {number} */
export const captionHoldMs = (words) => hold(PACE.caption, words);

// A hold as the table the prelude reads: entry n is the hold for a count of n,
// from 0 to the first count at the cap, which the shell uses past the end.
/** @param {(n: number) => number} holdMs @returns {number[]} */
export function holdTable(holdMs) {
  const t = [holdMs(0)];
  while (holdMs(t.length) > t[t.length - 1]) {
    if (t.length > 10_000) throw new Error('a hold that never reaches its cap');
    t.push(holdMs(t.length));
  }
  return t;
}

// A typed line that is a caption: bash runs nothing for it.
/** @param {string} typed */
const caption = (typed) => typed.trimStart().startsWith('#');

// A line the prelude holds nothing for: an unseen helper, which erases its own
// line, or a clear (web/e2e/tape/pace.sh's demo__hold says the same).
/** @param {string} typed */
const unheld = (typed) => /^(?:demo_hush|demo_until) /.test(typed) || typed === 'clear';

/** @param {string} d a VHS duration (20ms, 3s, 1.5m) @returns {number} milliseconds */
function durationMs(d) {
  const m = /^(\d+(?:\.\d+)?)(ms|s|m)$/.exec(d);
  if (!m) throw new Error(`not a duration: ${d}`);
  const unit = m[2] === 'ms' ? 1 : m[2] === 's' ? 1000 : 60_000;
  return Number(m[1]) * unit;
}

// The least a tape can run, in seconds, before a command takes any time of its
// own (#1247): each line it types on screen, at the house typing speed, and
// the pacing rule's hold after it (a caption's by its words; a command's as if
// its output filled no row), and the last frame's hold. What is hidden is not
// recorded, so it costs nothing. A tape whose least is over the budget can
// never pass, so make test-web refuses it before a recording is tried.
/** @param {Tape} tape @returns {number} */
export function tapeMinSeconds(tape) {
  const typeMs = durationMs(HOUSE.typingSpeed);
  let hidden = false;
  let ms = durationMs(HOUSE.hold);
  let line = '';
  for (const raw of tape.body) {
    const cmd = raw.trim();
    if (cmd === 'Hide' || cmd === 'Show') hidden = cmd === 'Hide';
    if (cmd.startsWith('Type ')) {
      const typed = quoted(cmd.slice('Type '.length).trim()) ?? '';
      line += typed;
      if (!hidden) ms += typed.length * typeMs;
    } else if (/^Enter\b/.test(cmd)) {
      // The hold follows the line entered, once (a repeated Enter enters blanks).
      if (!hidden && line !== '') {
        if (caption(line)) ms += captionHoldMs(line.trimStart().slice(1).split(/\s+/).filter(Boolean).length);
        else if (!unheld(line)) ms += outputHoldMs(0);
      }
      line = '';
    }
  }
  return ms / 1000;
}

// The budget: the GIF plays inline on the PR page, so it is held to the
// walkthrough preview's ceiling; the length keeps a demo watchable (its waits
// are hidden, so what plays is typing and answers).
export const MAX_GIF_BYTES = 5_000_000;
// 180s, raised from 120s by the pacing rule (#1247, ADR-1247): every answer now
// holds a few seconds on screen, which a tape of fifteen to twenty commands pays
// once each; the GIF stays far under its byte limit, since a held frame costs
// almost nothing to encode.
export const MAX_SECONDS = 180;

const MAX_TITLE = 100;
const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
const DURATION = /^\d+(?:\.\d+)?(?:ms|s|m)$/;
// The recorder's commands: a tape that names one is refused.
const RECORDERS = new Set(['Set', 'Output', 'Require', 'Source', 'Env', 'Screenshot', 'Copy', 'Paste']);
// Typed text that reaches the pacing rule's hooks: the prompt variables, the
// DEMO_ variables the recorder and the prelude set, the prelude's demo__ names,
// the shell's history (the prelude reads each line's number from it to pace and
// to check it, so a line history skips is neither), and, in a command's place
// (behind env, command, builtin or a path, too), an exec or a trap (either
// replaces what the prelude installed; an exec that only opens or closes a
// file descriptor replaces nothing) or an interactive shell (it would run
// without the prelude); a shell given -c only runs its command, and is paced as
// one. A typed `# caption` runs nothing, so it is never held to this.
const CMD_AT = String.raw`(?:^|[;&|(){}!]|\b(?:then|do|else|time))\s*(?:(?:[^\s;&|()]*/)?(?:env|command|builtin|nohup)(?:\s+-\S+)*\s+)*(?:[^\s;&|()]*/)?`;
const HOOKS = new RegExp(
  String.raw`\b(?:PROMPT_COMMAND|PS0|PS1|PS2)\b|\bDEMO_\w|\bdemo__\w|` +
    String.raw`\bHIST\w*\s*[+]?=|\bunset\s[^;&|]*\bHIST|\bset\s+[-+]o\s+history\b|\bset\s+\+o\b|\bhistory\s+-[a-z]*[cdrwns]|` +
    `${CMD_AT}exec(?=\\s|$)(?!\\s+(?:\\d+|\\{\\w+\\})?[<>])|` +
    `${CMD_AT}trap(?=\\s|$)|` +
    `${CMD_AT}(?:bash|sh|zsh|dash)(?!\\s+-c\\b)(?=\\s|$)`,
);
const KEYS = new Set(['Enter', 'Backspace', 'Delete', 'Tab', 'Space', 'Up', 'Down', 'Left', 'Right', 'Escape']);

/** @typedef {{ id: string, title: string, description: string, body: string[] }} Tape */
/**
 * What tapes.sh writes as demo.json once a recording passes; `budget` is the
 * mark only a pass carries.
 * @typedef {{ id: string, title: string, description: string, gif: string, mp4: string,
 *   budget?: { seconds: number, gifBytes: number, mp4Bytes: number } }} TapeManifest
 */

/** @param {unknown} id @returns {string} */
export function checkTapeId(id) {
  if (typeof id !== 'string' || !ID.test(id)) {
    throw new Error(`tape id "${id}" must be lowercase letters, digits and dashes (it names a directory)`);
  }
  return id;
}

// The one quoted string a Type carries ("...", '...' or `...`), or null.
/** @param {string} rest @returns {string | null} */
function quoted(rest) {
  const q = rest[0];
  if (!q || !'"\'`'.includes(q) || rest.length < 2 || rest.at(-1) !== q) return null;
  const inner = rest.slice(1, -1);
  return inner.includes(q) ? null : inner;
}

// Parses tape `text` (file `file`, demo `id`) and holds it to the house rules;
// throws naming the file and line of the first problem.
/** @param {string} id @param {string} text @param {string} file @returns {Tape} */
export function parseTape(id, text, file) {
  checkTapeId(id);
  const lines = text.replace(/\r\n?/g, '\n').replace(/\n$/, '').split('\n');
  /** @type {(n: number, msg: string) => never} */
  const fail = (n, msg) => {
    throw new Error(`${file}:${n}: ${msg}`);
  };
  const comment = (/** @type {string} */ l) => l.startsWith('#');
  let head = 0;
  while (head < lines.length && comment(lines[head])) head++;
  const block = lines.slice(0, head).map((l) => l.replace(/^#\s?/, '').trim());
  const title = block[0] ?? '';
  if (!title) throw new Error(`${file}: a tape opens with its title as a comment (# <title>)`);
  if (title.length > MAX_TITLE) throw new Error(`${file}: the title is over ${MAX_TITLE} characters`);
  const description = block.slice(1).filter(Boolean).join(' ');

  let hidden = false;
  let hiddenAt = 0;
  let pendingEnter = 0;
  let typedOnScreen = false;
  for (let i = head; i < lines.length; i++) {
    const n = i + 1;
    const line = lines[i].trim();
    if (line === '' || comment(line)) continue;
    const sp = line.search(/\s/);
    const word = sp < 0 ? line : line.slice(0, sp);
    const rest = sp < 0 ? '' : line.slice(sp + 1).trim();
    const [name, speed] = word.split('@');
    if (RECORDERS.has(name)) {
      fail(n, `${name} is the recorder's to set, not a tape's (the house settings fix the size, font, theme, typing speed and outputs)`);
    }
    if (name === 'Type') {
      if (speed !== undefined) fail(n, `${word} changes the typing speed, which the house settings fix`);
      const s = quoted(rest);
      if (s === null) fail(n, 'Type takes one quoted string ("...", \'...\' or `...`)');
      if (UUID.test(/** @type {string} */ (s))) fail(n, 'types a uuid; address it by name or dotted address, or look its id up by name inside the tape');
      if (!caption(/** @type {string} */ (s)) && HOOKS.test(/** @type {string} */ (s))) {
        fail(n, "types into the pacing rule's hooks (the prompt, its hold tables, the prelude's own names, the shell's history, a new shell or a trap); a tape's commands are paced, never the pacing");
      }
      if (pendingEnter) fail(pendingEnter, 'the command entered here is not waited on (a Wait) before the next Type');
      if (!hidden) typedOnScreen = true;
    } else if (name === 'Enter') {
      if (rest !== '' && !/^\d+$/.test(rest)) fail(n, 'Enter takes an optional repeat count');
      pendingEnter = n;
    } else if (/^Wait(\+Screen|\+Line)?$/.test(name)) {
      if ((speed !== undefined && !DURATION.test(speed)) || (rest !== '' && !/^\/[^/]*\/$/.test(rest))) {
        fail(n, `malformed Wait (Wait, Wait@<duration>, Wait+Screen /regex/): ${line}`);
      }
      // Only a wait for the prompt finishes a command: a pattern can match the
      // command's own typed text, and let the next step run before it ends.
      if (name === 'Wait' && rest === '') pendingEnter = 0;
    } else if (name === 'Sleep') {
      fail(n, "Sleep is the pacing rule's: the prelude holds each command's output and each caption on screen (web/e2e/tape/pace.sh), so a tape adds no hold of its own");
    } else if (name === 'Hide' || name === 'Show') {
      // VHS reads several commands to a line: nothing may ride after these.
      if (rest !== '' || speed !== undefined) fail(n, `${name} takes nothing after it`);
      if (name === 'Hide' && hidden) fail(n, 'Hide while already hidden');
      if (name === 'Show' && !hidden) fail(n, 'Show with no Hide before it');
      hidden = name === 'Hide';
      if (hidden) hiddenAt = n;
    } else if (KEYS.has(name) || /^Ctrl\+[A-Za-z@[\]\\^_]$/.test(name)) {
      if (rest !== '' && !/^\d+$/.test(rest)) fail(n, `${name} takes an optional repeat count`);
    } else {
      fail(n, `unknown command "${name}"`);
    }
  }
  if (pendingEnter) fail(pendingEnter, 'the command entered here is not waited on (a Wait) before the tape ends');
  if (hidden) fail(hiddenAt, 'Hide is never followed by a Show, so the end of the tape would not be recorded');
  if (!typedOnScreen) throw new Error(`${file}: the tape types nothing on screen`);
  return { id, title, description, body: lines.slice(head) };
}

// The tape the recorder plays: the outputs, the house settings, the prelude
// sourced unseen with the pacing rule's tables (folding the tape's own opening Hide into it, so no frame
// shows before its setup), the tape, and a held last frame. VHS reads settings
// only before the first command, so they lead.
/** @param {Tape} tape @returns {string} */
export function wrapTape(tape) {
  const id = checkTapeId(tape.id);
  const body = [...tape.body];
  const first = body.findIndex((l) => l.trim() !== '' && !l.trim().startsWith('#'));
  const opensHidden = first >= 0 && body[first].trim() === 'Hide';
  if (opensHidden) body.splice(first, 1);
  return [
    `# Generated by web/e2e/tapelib.mjs from the tape "${id}": the outputs and house settings, then the tape.`,
    `Output "/out/${id}.gif"`,
    `Output "/out/${id}.mp4"`,
    'Require omniglass',
    'Require jq',
    'Set Shell "bash"',
    `Set FontFamily "${HOUSE.fontFamily}"`,
    `Set FontSize ${HOUSE.fontSize}`,
    `Set Width ${HOUSE.width}`,
    `Set Height ${HOUSE.height}`,
    `Set Padding ${HOUSE.padding}`,
    `Set Theme "${HOUSE.theme}"`,
    `Set TypingSpeed ${HOUSE.typingSpeed}`,
    `Set Framerate ${HOUSE.framerate}`,
    'Set CursorBlink false',
    `Set WaitTimeout ${HOUSE.waitTimeout}`,
    // A plain Wait is for the prompt alone (the trimmed line is just "> "),
    // never an output line that happens to end in ">".
    'Set WaitPattern /^>$/',
    'Hide',
    `Type "export DEMO_OUTPUT_HOLDS='${holdTable(outputHoldMs).join(' ')}' DEMO_CAPTION_HOLDS='${holdTable(captionHoldMs).join(' ')}' && source /demo/prelude.sh"`,
    'Enter',
    'Wait',
    'Type "clear"',
    'Enter',
    'Wait',
    ...(opensHidden ? [] : ['Show']),
    ...body,
    `Sleep ${HOUSE.hold}`,
  ].join('\n');
}

// The limits a recording is judged by: the house budget, lowered (never
// raised) by OG_TAPE_MAX_GIF_BYTES and OG_TAPE_MAX_SECONDS, which the
// recorder's own test sets to prove the over-budget path.
/** @param {Record<string, string | undefined>} env @returns {{ maxGifBytes: number, maxSeconds: number }} */
export function tapeLimits(env) {
  /** @type {(v: string | undefined, max: number) => number} */
  const lower = (v, max) => {
    const n = v === undefined || v.trim() === '' ? NaN : Number(v);
    return Number.isFinite(n) && n > 0 ? Math.min(n, max) : max;
  };
  return { maxGifBytes: lower(env.OG_TAPE_MAX_GIF_BYTES, MAX_GIF_BYTES), maxSeconds: lower(env.OG_TAPE_MAX_SECONDS, MAX_SECONDS) };
}

// The verdict on one recording: empty when it passes, else one line per
// problem, naming the tape. `failures` is the prelude's list of commands the
// tape typed that exited non-zero (one "exit <n>: <command line>" per line);
// gifBytes and mp4Bytes are 0 for a file never rendered; seconds is the MP4's
// length, NaN when it could not be read.
/**
 * @param {string} id
 * @param {{ failures: string, gifBytes: number, mp4Bytes: number, seconds: number }} rec
 * @param {{ maxGifBytes: number, maxSeconds: number }} limits
 * @returns {string[]}
 */
export function tapeProblems(id, rec, limits) {
  const out = rec.failures
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => `tape ${id}: a command it typed failed (${l})`);
  if (!(rec.gifBytes > 0)) out.push(`tape ${id}: no GIF was rendered`);
  if (!(rec.mp4Bytes > 0)) out.push(`tape ${id}: no MP4 was rendered`);
  if (!(rec.seconds > 0)) out.push(`tape ${id}: the MP4's length could not be read`);
  if (rec.gifBytes >= limits.maxGifBytes) {
    out.push(`tape ${id}: over budget: the GIF is ${rec.gifBytes} bytes, and the limit is under ${limits.maxGifBytes} bytes (it plays inline on the PR page)`);
  }
  if (rec.seconds >= limits.maxSeconds) {
    out.push(`tape ${id}: over budget: it runs ${rec.seconds}s, and the limit is under ${limits.maxSeconds}s (hide the waits; split a tape that will not fit)`);
  }
  return out;
}

// The manifest a passing recording carries (demo.json), with the budget mark.
/**
 * @param {Tape} tape
 * @param {{ gifBytes: number, mp4Bytes: number, seconds: number }} rec
 * @returns {TapeManifest}
 */
export function tapeManifest(tape, rec) {
  const id = checkTapeId(tape.id);
  return {
    id,
    title: tape.title,
    description: tape.description,
    gif: `${id}.gif`,
    mp4: `${id}.mp4`,
    budget: { seconds: rec.seconds, gifBytes: rec.gifBytes, mp4Bytes: rec.mp4Bytes },
  };
}
