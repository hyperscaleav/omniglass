import { api, sendRaw } from "../api/client";
import { describeError } from "./format";

// The omnibot panel's data layer (#1167). A run is the server's: the console
// posts a prompt (or the answer to an ask) to a conversation's :run and reads
// back the AG-UI events it narrates (#2121). readEvents parses that stream;
// reduce folds each event into what the panel shows, a pure function the
// server's own fixtures drive in the tests.

// AguiEvent is one AG-UI event as the server writes it: a type and the fields
// that type declares (web/src/lib/agui.test.ts holds them to the protocol).
export type AguiEvent = { type: string; [field: string]: unknown };

// A step is one tool call: what omnibot asked to run, its arguments, and its
// result once it ran (or was declined). decision is the person's answer to an
// ask, set only for a call that waited on one.
export type Step = {
  kind: "step";
  id: string;
  tool: string;
  args: Record<string, unknown>;
  result?: string;
  decision?: "approved" | "declined";
};

export type Item = { kind: "user"; text: string } | { kind: "text"; id: string; text: string } | Step;

// The call a run paused on, waiting for the person.
export type Ask = {
  id: string;
  tool: string;
  title: string;
  arguments: Record<string, unknown>;
  destructive: boolean;
};

// idle: nothing asked yet; working: a run streams; pending: a call waits on
// the person; done: answered; stopped: the step limit ended it unanswered;
// refused: this person may not run omnibot here; error: it failed.
export type RunStatus = "idle" | "working" | "pending" | "done" | "stopped" | "refused" | "error";

export type RunState = {
  items: Item[];
  status: RunStatus;
  interrupt?: Ask;
  steps?: number;
  error?: string;
};

export const initialRun: RunState = { items: [], status: "idle" };

// ask adds the person's prompt and starts working on it.
export function ask(s: RunState, text: string): RunState {
  return { items: [...s.items, { kind: "user", text }], status: "working" };
}

// answer records the person's answer to the waiting call and starts the
// resume that carries it.
export function answer(s: RunState, approved: boolean): RunState {
  const id = s.interrupt?.id;
  const decision = approved ? "approved" : "declined";
  return {
    items: s.items.map((i) => (i.kind === "step" && i.id === id ? { ...i, decision } : i)),
    status: "working",
  };
}

// fail ends the run on a failure to start or to read it.
export function fail(s: RunState, message: string, refused = false): RunState {
  return { ...s, status: refused ? "refused" : "error", error: message, interrupt: undefined };
}

function objectOf(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}

function parseArgs(delta: unknown): Record<string, unknown> {
  try {
    return objectOf(JSON.parse(String(delta ?? "{}")));
  } catch {
    return {};
  }
}

function updateStep(items: Item[], id: unknown, f: (s: Step) => Step): Item[] {
  return items.map((i) => (i.kind === "step" && i.id === id ? f(i) : i));
}

// reduce folds one event into the panel's state.
export function reduce(s: RunState, ev: AguiEvent): RunState {
  switch (ev.type) {
    case "RUN_STARTED":
      return { ...s, status: "working", interrupt: undefined, error: undefined, steps: undefined };
    case "TEXT_MESSAGE_START":
      return { ...s, items: [...s.items, { kind: "text", id: String(ev.messageId), text: "" }] };
    case "TEXT_MESSAGE_CONTENT":
      return {
        ...s,
        items: s.items.map((i) => (i.kind === "text" && i.id === ev.messageId ? { ...i, text: i.text + String(ev.delta ?? "") } : i)),
      };
    case "TOOL_CALL_START":
      return { ...s, items: [...s.items, { kind: "step", id: String(ev.toolCallId), tool: String(ev.toolCallName), args: {} }] };
    case "TOOL_CALL_ARGS":
      return { ...s, items: updateStep(s.items, ev.toolCallId, (st) => ({ ...st, args: parseArgs(ev.delta) })) };
    case "TOOL_CALL_RESULT":
      return { ...s, items: updateStep(s.items, ev.toolCallId, (st) => ({ ...st, result: String(ev.content ?? "") })) };
    case "RUN_ERROR":
      return fail(s, String(ev.message ?? "The run failed."));
    case "RUN_FINISHED": {
      const outcome = objectOf(ev.outcome);
      if (outcome.type === "interrupt") {
        const first = objectOf((outcome.interrupts as unknown[] | undefined)?.[0]);
        const meta = objectOf(first.metadata);
        return {
          ...s,
          status: "pending",
          interrupt: {
            id: String(first.id),
            tool: String(meta.tool ?? ""),
            title: String(meta.title || meta.tool || ""),
            arguments: objectOf(meta.arguments),
            destructive: meta.destructive === true,
          },
        };
      }
      const result = objectOf(ev.result);
      if (result.stopped === "step_limit") return { ...s, status: "stopped", steps: Number(result.steps) };
      return { ...s, status: "done" };
    }
  }
  return s;
}

// readEvents parses an SSE stream into its events: each `data:` line is one
// JSON event, events end on a blank line, and a chunk may split anywhere.
export async function* readEvents(stream: ReadableStream<Uint8Array>): AsyncGenerator<AguiEvent> {
  const reader = stream.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    buf += done ? dec.decode() : dec.decode(value, { stream: true });
    let end: number;
    while ((end = buf.indexOf("\n\n")) >= 0) {
      const block = buf.slice(0, end);
      buf = buf.slice(end + 2);
      const data = block
        .split("\n")
        .filter((l) => l.startsWith("data:"))
        .map((l) => l.slice(5).trimStart())
        .join("\n");
      if (data) yield JSON.parse(data) as AguiEvent;
    }
    if (done) return;
  }
}

// RunRefused is a :run this person may not make (a 403): an agent, or an
// impersonation session.
export class RunRefused extends Error {}

export type RunInput = { prompt: string } | { resume: { id: string; approved: boolean } };

// runOmnibot runs omnibot on the named conversation and hands each event to
// onEvent as it arrives. A run the server refuses to start throws: RunRefused
// for a 403, an Error naming the server's reason otherwise.
export async function runOmnibot(name: string, input: RunInput, onEvent: (ev: AguiEvent) => void, signal?: AbortSignal): Promise<void> {
  const body =
    "prompt" in input
      ? { messages: [{ id: crypto.randomUUID(), role: "user", content: input.prompt }] }
      : {
          messages: [],
          resume: [
            {
              interruptId: input.resume.id,
              status: input.resume.approved ? "resolved" : "cancelled",
              payload: { approved: input.resume.approved },
            },
          ],
        };
  const response = await sendRaw(`/assistant/conversations/${encodeURIComponent(name)}:run`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
    body: JSON.stringify(body),
    signal,
  });
  if (!response.ok || !response.body) {
    const text = await response.text();
    let err: unknown = text;
    try {
      err = JSON.parse(text);
    } catch {
      // Not JSON: the reason is the body's text.
    }
    const message = typeof err === "string" && err ? err : describeError(err);
    throw response.status === 403 ? new RunRefused(message) : new Error(message);
  }
  for await (const ev of readEvents(response.body)) onEvent(ev);
}

// startConversation makes the person a new conversation, named for them, the
// one a panel's first prompt runs on.
export async function startConversation(): Promise<string> {
  const { data, error } = await api.POST("/assistant/conversations", { body: {} });
  if (error) throw new Error(describeError(error));
  return data.name;
}
