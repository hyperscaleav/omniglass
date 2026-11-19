import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { EventSchema, InterruptSchema, RunAgentInputSchema } from "@ag-ui/core/schemas";

// The server narrates an omnibot run as AG-UI events (#2121). The Go package
// that writes them records runs to fixtures (internal/assistant/agui): one
// that pauses on an ask, each answer to it, and one stopped at its step limit.
// This test holds every fixture to the protocol's own schemas, so the server's
// events are what any AG-UI client, the console's included, reads. Each event
// must parse, and name only fields its type declares: the schemas let an
// unknown field through, so a misspelt one would otherwise pass unseen.

const here = dirname(fileURLToPath(import.meta.url));
const testdata = join(here, "../../../internal/assistant/agui/testdata");
const fixtures = ["run.sse", "resume-approved.sse", "resume-declined.sse", "step-limit.sse"];
const read = (name: string) => events(readFileSync(join(testdata, name), "utf8"));

// declared is the fields an event's type names in the protocol.
function declared(type: string): string[] {
  const schema = EventSchema.options.find((o) => o.shape.type.safeParse(type).success);
  if (!schema) throw new Error(`no AG-UI event of type ${type}`);
  return Object.keys(schema.shape);
}

function events(stream: string): unknown[] {
  return stream
    .split("\n")
    .filter((line) => line !== "")
    .map((line) => {
      expect(line.startsWith("data: ")).toBe(true);
      return JSON.parse(line.slice("data: ".length));
    });
}

describe("a server-narrated omnibot run", () => {
  it.each(fixtures)("%s is AG-UI events, naming only the fields each declares", (name) => {
    const evs = read(name);
    expect(evs.length).toBeGreaterThan(0);
    for (const ev of evs) {
      const parsed = EventSchema.safeParse(ev);
      expect(parsed.success, JSON.stringify(parsed.error?.issues ?? [])).toBe(true);
      const known = declared(parsed.data!.type);
      expect(Object.keys(ev as object).filter((k) => !known.includes(k))).toEqual([]);
    }
  });

  it.each(["resume-approved.sse", "resume-declined.sse"])("%s answers the call and finishes", (name) => {
    const evs = read(name).map((ev) => EventSchema.parse(ev));
    const result = evs.find((ev) => ev.type === "TOOL_CALL_RESULT");
    expect(result && result.type === "TOOL_CALL_RESULT" && result.toolCallId).toBe("call-2");
    const last = evs[evs.length - 1];
    expect(last.type === "RUN_FINISHED" && last.outcome?.type).toBe("success");
  });

  it("ends on an interrupt a client answers by resume", () => {
    const evs = read("run.sse");
    const last = EventSchema.parse(evs[evs.length - 1]);
    expect(last.type).toBe("RUN_FINISHED");
    if (last.type !== "RUN_FINISHED" || last.outcome?.type !== "interrupt") {
      throw new Error("the run did not end on an interrupt");
    }
    const [it0] = last.outcome.interrupts;
    const known = Object.keys(InterruptSchema.shape);
    expect(Object.keys(it0).filter((k) => !known.includes(k))).toEqual([]);
    const answer = RunAgentInputSchema.safeParse({
      threadId: "lobby",
      runId: "run-2",
      state: {},
      messages: [],
      tools: [],
      context: [],
      forwardedProps: {},
      resume: [{ interruptId: it0.id, status: "resolved", payload: { approved: true } }],
    });
    expect(answer.success, JSON.stringify(answer.error?.issues ?? [])).toBe(true);
    // The answer is {approved} and nothing else.
    expect(it0.responseSchema).toMatchObject({ required: ["approved"], additionalProperties: false });
  });
});
