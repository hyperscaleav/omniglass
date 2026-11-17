import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { answer, ask, initialRun, readEvents, reduce, runOmnibot, RunRefused, type RunState, type Step } from "./assistant";

// The panel's data layer (#1167): the reader turns a :run stream into AG-UI
// events, and the reducer folds them into what the panel shows. The fixtures
// are the server's own narrations (internal/assistant/agui), so the console
// reads exactly what the server writes.

const here = dirname(fileURLToPath(import.meta.url));
const testdata = join(here, "../../../internal/assistant/agui/testdata");
const fixture = (name: string) => readFileSync(join(testdata, name), "utf8");

function streamOf(...chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(c) {
      for (const ch of chunks) c.enqueue(enc.encode(ch));
      c.close();
    },
  });
}

async function eventsOf(stream: ReadableStream<Uint8Array>) {
  const out = [];
  for await (const ev of readEvents(stream)) out.push(ev);
  return out;
}

async function play(state: RunState, name: string): Promise<RunState> {
  for (const ev of await eventsOf(streamOf(fixture(name)))) state = reduce(state, ev);
  return state;
}

describe("reading a run's stream", () => {
  it("parses every event, however the bytes are split", async () => {
    const text = fixture("run.sse");
    const whole = await eventsOf(streamOf(text));
    const cut = await eventsOf(streamOf(...text.match(/[\s\S]{1,7}/g)!));
    expect(whole.map((e) => e.type)[0]).toBe("RUN_STARTED");
    expect(whole.at(-1)?.type).toBe("RUN_FINISHED");
    expect(cut).toEqual(whole);
  });
});

describe("folding a run into the panel", () => {
  it("shows the prompt, the text, each call as a step, and waits on the ask", async () => {
    const s = await play(ask(initialRun, "add the lobby display"), "run.sse");
    expect(s.items.map((i) => i.kind)).toEqual(["user", "text", "step", "step"]);
    expect(s.items[0]).toMatchObject({ kind: "user", text: "add the lobby display" });
    expect(s.items[1]).toMatchObject({ kind: "text", text: "Let me look." });
    expect(s.items[2]).toMatchObject({ kind: "step", id: "call-1", tool: "list-components", args: {}, result: '{"components":[]}' });
    expect(s.items[3]).toMatchObject({ kind: "step", id: "call-2", tool: "create-component" });
    expect((s.items[3] as Step).args).toEqual({ name: "lobby-display" });
    expect(s.status).toBe("pending");
    expect(s.interrupt).toMatchObject({ id: "call-2", tool: "create-component", title: "Create a component" });
    expect(s.interrupt?.arguments).toEqual({ name: "lobby-display" });
  });

  it("runs the call once it is confirmed, and finishes", async () => {
    let s = await play(ask(initialRun, "add the lobby display"), "run.sse");
    s = answer(s, true);
    expect(s.status).toBe("working");
    expect(s.interrupt).toBeUndefined();
    s = await play(s, "resume-approved.sse");
    expect(s.items.find((i) => i.kind === "step" && i.id === "call-2")).toMatchObject({ decision: "approved", result: '{"name":"lobby-display"}' });
    expect(s.items.at(-1)).toMatchObject({ kind: "text", text: "Created lobby-display." });
    expect(s.status).toBe("done");
  });

  it("marks a declined call, and the run goes on to its end", async () => {
    let s = await play(ask(initialRun, "add the lobby display"), "run.sse");
    s = await play(answer(s, false), "resume-declined.sse");
    expect(s.items.find((i) => i.kind === "step" && i.id === "call-2")).toMatchObject({ decision: "declined" });
    expect(s.items.at(-1)).toMatchObject({ kind: "text", text: "Understood, I left it alone." });
    expect(s.status).toBe("done");
  });

  it("says when the run stopped at its step limit", async () => {
    const s = await play(ask(initialRun, "look around"), "step-limit.sse");
    expect(s.status).toBe("stopped");
    expect(s.steps).toBe(10);
  });

  it("keeps the error a run ended on", () => {
    let s = reduce(ask(initialRun, "x"), { type: "RUN_STARTED", threadId: "t", runId: "r" });
    s = reduce(s, { type: "RUN_ERROR", message: "the run stopped: the model", code: "run" });
    expect(s.status).toBe("error");
    expect(s.error).toBe("the run stopped: the model");
  });

  it("joins text deltas of one message", () => {
    let s = reduce(initialRun, { type: "TEXT_MESSAGE_START", messageId: "m", role: "assistant" });
    s = reduce(s, { type: "TEXT_MESSAGE_CONTENT", messageId: "m", delta: "Hel" });
    s = reduce(s, { type: "TEXT_MESSAGE_CONTENT", messageId: "m", delta: "lo" });
    expect(s.items).toEqual([{ kind: "text", id: "m", text: "Hello" }]);
  });
});

describe("running omnibot", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("posts the prompt to the conversation's :run and yields its events", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(streamOf(fixture("step-limit.sse")), { headers: { "Content-Type": "text/event-stream" } }));
    const seen: string[] = [];
    await runOmnibot("lobby", { prompt: "look around" }, (ev) => seen.push(ev.type));
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/assistant/conversations/lobby:run");
    expect(req.method).toBe("POST");
    const body = await req.json();
    expect(body.messages).toEqual([{ role: "user", content: "look around" }]);
    expect(body.resume).toBeUndefined();
    expect(seen.at(-1)).toBe("RUN_FINISHED");
  });

  it("answers an interrupt by resume", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(streamOf(fixture("resume-declined.sse"))));
    await runOmnibot("lobby", { resume: { id: "call-2", approved: false } }, () => {});
    const body = await (fetchMock.mock.calls[0][0] as Request).json();
    expect(body.resume).toEqual([{ interruptId: "call-2", status: "cancelled", payload: { approved: false } }]);
    expect(body.messages).toEqual([]);
  });

  it("is refused for a person who may not run it", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ detail: "only a person, as themselves, runs omnibot" }), { status: 403 }),
    );
    await expect(runOmnibot("lobby", { prompt: "x" }, () => {})).rejects.toBeInstanceOf(RunRefused);
  });

  it("names the server's reason for any other refusal to start", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ detail: "omnibot has no model on this server" }), { status: 503 }));
    await expect(runOmnibot("lobby", { prompt: "x" }, () => {})).rejects.toThrow("omnibot has no model on this server");
  });
});
