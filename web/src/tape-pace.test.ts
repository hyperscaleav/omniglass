import { describe, it, expect } from "vitest";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { captionHoldMs, holdTable, outputHoldMs } from "../e2e/tapelib.mjs";

// The pacing rule's shell half (#1247): web/e2e/tape/pace.sh, which the
// terminal demo prelude sources to decide each hold. Its numbers are
// tapelib.mjs's, handed over as tables; this holds the shell to them, and its
// row and word counts to what a viewer would read. make test-demos proves the
// prelude applies it in a real recording.
const PACE_SH = join(__dirname, "../e2e/tape/pace.sh");
const TABLES = {
  DEMO_OUTPUT_HOLDS: holdTable(outputHoldMs).join(" "),
  DEMO_CAPTION_HOLDS: holdTable(captionHoldMs).join(" "),
};

// Runs `script` in bash with pace.sh sourced and the rule's tables in the env,
// `input` on stdin.
const sh = (script: string, input = "") =>
  execFileSync("bash", ["-c", `set -euo pipefail; . "$PACE_SH"; ${script}`], {
    input,
    env: { PATH: process.env.PATH, PACE_SH, ...TABLES },
    encoding: "utf8",
  }).trimEnd();

describe("the rows a command's output fills", () => {
  it("counts a row per line, a blank line included", () => {
    expect(sh("demo__rows 80", "one\n\nthree\n")).toBe("3");
    expect(sh("demo__rows 80", "")).toBe("0");
  });

  it("counts a line with no newline at its end", () => {
    expect(sh("demo__rows 80", "one\ntwo")).toBe("2");
  });

  it("counts a line wider than the terminal as the rows it wraps to", () => {
    expect(sh("demo__rows 10", `${"x".repeat(10)}\n${"x".repeat(11)}\n${"x".repeat(25)}\n`)).toBe("6");
  });

  it("reads past colors, carriage returns, the bracketed-paste toggles and an OSC, which take no width", () => {
    const colored = "\x1b[1;39m{\x1b[0m\x1b[34;1m\"name\"\x1b[0m\x1b[1;39m:\x1b[0;32m\"panel-1\"\x1b[0m\x1b[1;39m}\x1b[0m\r\n";
    expect(sh("demo__rows 18", colored)).toBe("1");
    expect(sh("demo__rows 17", colored)).toBe("2");
    expect(sh("demo__rows 80", "\x1b[?2004l\r\x1b]7701;og-run\x07hi\r\n")).toBe("1");
  });

  it("measures a wide character as one column, not its bytes", () => {
    expect(sh("demo__rows 4", "·→·→\n")).toBe("1");
  });
});

describe("the hold after a line", () => {
  it("holds a command's output by its rows, on the rule's table", () => {
    expect(sh(`demo__hold "omniglass node list" 80`, "a\nb\nc\n")).toBe(`${outputHoldMs(3)} output 3`);
    expect(sh(`demo__hold "true" 80`, "")).toBe(`${outputHoldMs(0)} output 0`);
  });

  it("holds a long output at the cap", () => {
    expect(sh(`demo__hold "seq 500" 80`, "x\n".repeat(500))).toBe(`${outputHoldMs(500)} output 500`);
  });

  it("holds a typed caption by its words", () => {
    expect(sh(`demo__hold "# Each logs in, and collects" 80`)).toBe(`${captionHoldMs(5)} caption 5`);
    expect(sh(`demo__hold "${"# word".padEnd(200, " word")}" 80`)).toBe(`${captionHoldMs(40)} caption 40`);
  });

  it("holds nothing for the unseen helpers, which erase their own line, nor for a clear", () => {
    expect(sh(`demo__hold "demo_until 60 'true'" 80`, "")).toBe("");
    expect(sh(`demo__hold "demo_hush 'x=1'" 80`, "")).toBe("");
    expect(sh(`demo__hold "clear" 80`, "\x1b[H\x1b[2J")).toBe("");
  });

  it("agrees with the rule at every count it tables, and past the end", () => {
    const n = holdTable(outputHoldMs).length + 3;
    const shell = sh(`for i in $(seq 0 ${n}); do demo__hold_ms "$DEMO_OUTPUT_HOLDS" "$i"; done`).split("\n").map(Number);
    expect(shell).toEqual(Array.from({ length: n + 1 }, (_, i) => outputHoldMs(i)));
  });

  it("writes a hold as a sleep duration", () => {
    expect(sh("demo__seconds 2500; demo__seconds 7000; demo__seconds 40")).toBe("2.500\n7.000\n0.040");
  });
});
