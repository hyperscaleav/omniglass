import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  HOUSE,
  MAX_GIF_BYTES,
  MAX_SECONDS,
  parseTape,
  tapeLimits,
  tapeManifest,
  tapeProblems,
  wrapTape,
} from "../e2e/tapelib.mjs";

// Terminal demos (#1132): a demo for CLI or API work is a VHS tape under
// scripts/demo/, played in a real terminal against the walkthrough stack. The
// rules a tape is held to, the house settings it is wrapped in and the verdict
// on a recording are pure, so they are pinned here; make test-demos proves the
// recorder itself (a failing command, a stuck wait, an over-budget recording)
// against the real stack.
const F = "scripts/demo/x.tape";
const tape = (body: string, head = "# A title\n#\n# What it shows.\n\n") => head + body;
const ok = (body: string, head?: string) => parseTape("x", tape(body, head), F);
const bad = (body: string, head?: string) => {
  try {
    parseTape("x", tape(body, head), F);
  } catch (e) {
    return (e as Error).message;
  }
  throw new Error("expected the tape to be refused");
};
const CMD = 'Type "omniglass component list"\nEnter\nWait\n';

describe("a tape's title and description", () => {
  it("are its leading comment block: the first line the title, the rest the description", () => {
    const t = ok(CMD, "# Read the fleet\n#\n# Lists every component,\n# by name.\n\n");
    expect(t.id).toBe("x");
    expect(t.title).toBe("Read the fleet");
    expect(t.description).toBe("Lists every component, by name.");
  });

  it("allows no description", () => {
    expect(ok(CMD, "# Read the fleet\n").description).toBe("");
  });

  it("refuses a tape with no title comment first, naming the file", () => {
    expect(bad(CMD, "")).toBe(`${F}: a tape opens with its title as a comment (# <title>)`);
    expect(bad(CMD, "#\n")).toContain("opens with its title");
  });

  it("refuses a title too long to head a PR comment section", () => {
    expect(bad(CMD, `# ${"t".repeat(101)}\n`)).toContain("title is over 100 characters");
  });

  it("refuses an id that is not a safe path segment", () => {
    expect(() => parseTape("Bad Name", tape(CMD), F)).toThrow('tape id "Bad Name"');
  });
});

describe("the house rules a tape is held to", () => {
  it("accepts the commands a demo types and waits on, with comments and blank lines anywhere", () => {
    const t = ok(
      [
        "# set up, unseen",
        "Hide",
        "Type `demo_until 60 'true'`",
        "Enter",
        "Wait@90s",
        "Show",
        "",
        "Type 'omniglass node list | jq -c .'",
        "Enter",
        "Wait",
        "Sleep 2s",
        "Type \"x\"",
        "Backspace",
        "Ctrl+C",
        "Type 'echo done'",
        "Enter 1",
        "Wait+Screen /done/",
        "",
      ].join("\n"),
    );
    expect(t.body.filter((l) => l.trim() && !l.startsWith("#"))).toHaveLength(15);
  });

  it("refuses a setting, an output, a require, a source, an env, a screenshot or the clipboard: the recorder owns those", () => {
    for (const line of ["Set FontSize 30", "Set TypingSpeed 1ms", 'Output "x.gif"', "Require curl", 'Source "other.tape"', "Env VHS_PUBLISH true", 'Screenshot "x.png"', "Copy \"x\"", "Paste"]) {
      const word = line.split(/\s/)[0];
      expect(bad(`${line}\n${CMD}`)).toBe(`${F}:5: ${word} is the recorder's to set, not a tape's (the house settings fix the size, font, theme, typing speed and outputs)`);
    }
  });

  it("refuses a per-command typing speed, which the house settings fix", () => {
    expect(bad('Type@500ms "slow"\nEnter\nWait\n')).toBe(`${F}:5: Type@500ms changes the typing speed, which the house settings fix`);
  });

  it("refuses an unknown command, naming the line", () => {
    expect(bad(`Typo "x"\n${CMD}`)).toBe(`${F}:5: unknown command "Typo"`);
  });

  it("refuses a uuid in typed text: a demo addresses by name or dotted address, and looks an id up by name", () => {
    expect(bad('Type "omniglass secret update 01a1711e-fd2b-74c6-8707-3694e3612d89"\nEnter\nWait\n')).toBe(
      `${F}:5: types a uuid; address it by name or dotted address, or look its id up by name inside the tape`,
    );
  });

  it("refuses an Enter not followed by a Wait before the next command is typed, so every command finishes, and fails, on screen", () => {
    expect(bad('Type "a"\nEnter\nType "b"\nEnter\nWait\n')).toBe(`${F}:6: the command entered here is not waited on (a Wait) before the next Type`);
    expect(bad('Type "a"\nEnter\n')).toBe(`${F}:6: the command entered here is not waited on (a Wait) before the tape ends`);
    expect(bad('Type "a"\nEnter\nSleep 5s\n')).toContain("before the tape ends");
  });

  it("refuses a Hide with no Show after it, a Show with no Hide, and a second Hide", () => {
    expect(bad(`Hide\n${CMD}`)).toBe(`${F}:5: Hide is never followed by a Show, so the end of the tape would not be recorded`);
    expect(bad(`Show\n${CMD}`)).toBe(`${F}:5: Show with no Hide before it`);
    expect(bad(`Hide\nHide\n${CMD}Show\n`)).toBe(`${F}:6: Hide while already hidden`);
  });

  it("refuses a tape that types nothing on screen", () => {
    expect(bad(`Hide\n${CMD}Show\n`)).toBe(`${F}: the tape types nothing on screen`);
  });

  it("refuses a malformed Wait or Sleep", () => {
    expect(bad(`Wait soon\n${CMD}`)).toContain(`${F}:5: malformed Wait`);
    expect(bad(`Sleep forever\n${CMD}`)).toContain(`${F}:5: malformed Sleep`);
  });

  it("refuses a Type whose text is not one quoted string", () => {
    expect(bad("Type unquoted\nEnter\nWait\n")).toBe(`${F}:5: Type takes one quoted string ("...", '...' or \`...\`)`);
  });
});

describe("the tape the recorder plays", () => {
  const wrapped = (body: string) => wrapTape(ok(body)).split("\n");

  it("writes the GIF and the MP4 into the recorder's output directory, named by the id", () => {
    const w = wrapped(CMD);
    expect(w).toContain('Output "/out/x.gif"');
    expect(w).toContain('Output "/out/x.mp4"');
  });

  it("fixes the size, font, theme, typing speed and shell before the tape's first line", () => {
    const w = wrapped(CMD);
    for (const line of [
      `Set Width ${HOUSE.width}`,
      `Set Height ${HOUSE.height}`,
      `Set FontFamily "${HOUSE.fontFamily}"`,
      `Set FontSize ${HOUSE.fontSize}`,
      `Set Theme "${HOUSE.theme}"`,
      `Set TypingSpeed ${HOUSE.typingSpeed}`,
      'Set Shell "bash"',
      "Set CursorBlink false",
    ]) {
      expect(w.indexOf(line)).toBeGreaterThan(-1);
      expect(w.indexOf(line)).toBeLessThan(w.indexOf('Type "omniglass component list"'));
    }
  });

  it("requires the CLI and jq, so a recorder image without them fails before it records", () => {
    const w = wrapped(CMD);
    expect(w).toContain("Require omniglass");
    expect(w).toContain("Require jq");
  });

  it("sources the prelude unseen and clears the screen before the tape's first frame", () => {
    const w = wrapped(CMD);
    const hide = w.indexOf("Hide");
    expect(w[hide + 1]).toBe('Type "source /demo/prelude.sh && clear"');
    expect(w.slice(hide, w.indexOf('Type "omniglass component list"'))).toContain("Show");
  });

  it("folds a tape's own opening Hide into the prelude's, so no frame shows before its setup", () => {
    const w = wrapped(`Hide\nType "setup"\nEnter\nWait\nShow\n${CMD}`);
    expect(w.filter((l) => l === "Hide")).toHaveLength(1);
    expect(w.filter((l) => l === "Show")).toHaveLength(1);
    expect(w.indexOf('Type "setup"')).toBeLessThan(w.indexOf("Show"));
  });

  it("holds the last frame before the GIF loops", () => {
    expect(wrapped(CMD).at(-1)).toBe(`Sleep ${HOUSE.hold}`);
  });
});

describe("the verdict on a recording", () => {
  const limits = tapeLimits({});
  const good = { failures: "", gifBytes: 900_000, mp4Bytes: 700_000, seconds: 42 };

  it("passes a recording with no failed command, both files, and within budget", () => {
    expect(tapeProblems("x", good, limits)).toEqual([]);
  });

  it("fails on every command the tape typed that exited non-zero, naming each", () => {
    expect(tapeProblems("x", { ...good, failures: "exit 1: omniglass component get nowhere\nexit 4: jq .x\n" }, limits)).toEqual([
      "tape x: a command it typed failed (exit 1: omniglass component get nowhere)",
      "tape x: a command it typed failed (exit 4: jq .x)",
    ]);
  });

  it("fails when the GIF or the MP4 was never rendered, or its length was never read", () => {
    expect(tapeProblems("x", { ...good, gifBytes: 0 }, limits)).toEqual(["tape x: no GIF was rendered"]);
    expect(tapeProblems("x", { ...good, mp4Bytes: 0 }, limits)).toEqual(["tape x: no MP4 was rendered"]);
    expect(tapeProblems("x", { ...good, seconds: NaN }, limits)).toEqual(["tape x: the MP4's length could not be read"]);
  });

  it("fails a recording over budget, naming which limit", () => {
    expect(tapeProblems("x", { ...good, gifBytes: MAX_GIF_BYTES }, limits)).toEqual([
      `tape x: over budget: the GIF is ${MAX_GIF_BYTES} bytes, and the limit is under ${MAX_GIF_BYTES} bytes (it plays inline on the PR page)`,
    ]);
    expect(tapeProblems("x", { ...good, seconds: MAX_SECONDS + 1 }, limits)).toEqual([
      `tape x: over budget: it runs ${MAX_SECONDS + 1}s, and the limit is under ${MAX_SECONDS}s (hide the waits; split a tape that will not fit)`,
    ]);
  });

  it("takes lowered limits from the env, and never raised ones", () => {
    expect(tapeLimits({ OG_TAPE_MAX_GIF_BYTES: "1000", OG_TAPE_MAX_SECONDS: "1" })).toEqual({ maxGifBytes: 1000, maxSeconds: 1 });
    expect(tapeLimits({ OG_TAPE_MAX_GIF_BYTES: String(MAX_GIF_BYTES * 2), OG_TAPE_MAX_SECONDS: "junk" })).toEqual({
      maxGifBytes: MAX_GIF_BYTES,
      maxSeconds: MAX_SECONDS,
    });
  });
});

describe("the manifest a passing recording carries", () => {
  it("names the tape's title, description and files, with the budget mark only a pass writes", () => {
    const m = tapeManifest(ok(CMD), { gifBytes: 900_000, mp4Bytes: 700_000, seconds: 42.5 });
    expect(m).toEqual({
      id: "x",
      title: "A title",
      description: "What it shows.",
      gif: "x.gif",
      mp4: "x.mp4",
      budget: { seconds: 42.5, gifBytes: 900_000, mp4Bytes: 700_000 },
    });
  });
});

// Every tape in the tree is held to the rules here, so a malformed one fails in
// make test-web before a stack or a recorder starts.
describe("the tapes in the tree", () => {
  const dir = join(__dirname, "../../scripts/demo");
  const tapes = readdirSync(dir).filter((f) => f.endsWith(".tape"));

  it("has at least one tape", () => {
    expect(tapes.length).toBeGreaterThan(0);
  });

  it.each(tapes)("%s keeps the house rules", (f) => {
    expect(() => parseTape(f.slice(0, -".tape".length), readFileSync(join(dir, f), "utf8"), `scripts/demo/${f}`)).not.toThrow();
  });

  it("names no demo twice: a tape's id is never a walkthrough's", () => {
    const walkthroughs = new Set(readdirSync(join(__dirname, "../e2e/walkthroughs")).filter((f) => f.endsWith(".yaml")).map((f) => f.slice(0, -".yaml".length)));
    expect(tapes.map((f) => f.slice(0, -".tape".length)).filter((id) => walkthroughs.has(id))).toEqual([]);
  });
});
