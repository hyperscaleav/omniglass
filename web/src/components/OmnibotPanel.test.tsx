import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import OmnibotPanel from "./OmnibotPanel";
import { uuidFor } from "../lib/testids";

// The omnibot panel (#1167): a prompt starts a conversation and runs it,
// omnibot's text and each tool call stream in, a call the person's policy
// asks about waits on a confirm card whose answer resumes the run, and every
// way a run can end reads as what it is. The :run responses are the server's
// own narrations (internal/assistant/agui/testdata).

const here = dirname(fileURLToPath(import.meta.url));
const fixture = (name: string) => readFileSync(join(here, "../../../internal/assistant/agui/testdata", name), "utf8");

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const sse = (text: string) => new Response(text, { headers: { "Content-Type": "text/event-stream" } });

type Call = { url: string; method: string; body: Record<string, unknown> };
let calls: Call[];
let runReply: (body: Record<string, unknown>) => Response;

beforeEach(() => {
  calls = [];
  runReply = () => sse(fixture("run.sse"));
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    const text = await req.text();
    const body = text ? JSON.parse(text) : {};
    calls.push({ url: req.url, method: req.method, body });
    if (req.url.endsWith("/assistant/conversations") && req.method === "POST") {
      return json({ id: uuidFor("conv-1"), name: "conv-1", message_count: 0, created_at: "", updated_at: "" }, 201);
    }
    if (req.url.includes(":run")) return runReply(body);
    if (req.url.endsWith("/auth/me")) return json({ principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] });
    if (req.url.endsWith("/secret-types")) {
      return json({ secret_types: [{ id: uuidFor("snmp-community"), name: "snmp-community", label: "SNMP community", fields: [{ name: "community", secret: true }] }] });
    }
    if (req.url.endsWith("/secrets") && req.method === "POST") {
      return json({ id: uuidFor("lobby-snmp"), name: body.name, secret_type: body.secret_type, owner_kind: body.owner_kind }, 201);
    }
    return json({ items: [] });
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function open(prompt?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <OmnibotPanel open onClose={() => {}} prompt={prompt} />} />
      </Router>
    </QueryClientProvider>
  ));
}

function send(text: string) {
  fireEvent.input(screen.getByLabelText("Ask omnibot"), { target: { value: text } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
}

describe("the omnibot panel", () => {
  it("teaches what omnibot is before the first prompt", () => {
    open();
    expect(screen.getByTestId("omnibot-empty").textContent).toContain("acts as you");
  });

  it("takes the palette's query as a draft, sent only when the person sends it", () => {
    open("why is amp-9 down");
    expect((screen.getByLabelText("Ask omnibot") as HTMLTextAreaElement).value).toBe("why is amp-9 down");
    expect(calls).toEqual([]);
  });

  it("starts a conversation, runs the prompt, and waits on the call it asks about", async () => {
    open();
    send("add the lobby display");
    await screen.findByTestId("omnibot-ask");
    expect(calls[0]).toMatchObject({ method: "POST" });
    expect(calls[0].url).toMatch(/\/assistant\/conversations$/);
    expect(calls[1].url).toContain("/assistant/conversations/conv-1:run");
    expect(calls[1].body.messages).toEqual([expect.objectContaining({ role: "user", content: "add the lobby display" })]);
    expect(screen.getByText("add the lobby display")).toBeTruthy();
    expect(screen.getByText("Let me look.")).toBeTruthy();
    expect(screen.getAllByTestId("omnibot-step")).toHaveLength(2);
    expect(screen.getByTestId("omnibot-ask").textContent).toContain("Create a component");
    expect((screen.getByLabelText("Ask omnibot") as HTMLTextAreaElement).disabled).toBe(true);
  });

  it("runs the call once it is confirmed", async () => {
    open();
    send("add the lobby display");
    await screen.findByTestId("omnibot-ask");
    runReply = () => sse(fixture("resume-approved.sse"));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await screen.findByText("Created lobby-display.");
    expect(calls.at(-1)?.body.resume).toEqual([{ interruptId: "call-2", status: "resolved", payload: { approved: true } }]);
    expect(screen.queryByTestId("omnibot-ask")).toBeNull();
    expect(screen.getByText("Approved")).toBeTruthy();
  });

  it("declines the call and the run goes on", async () => {
    open();
    send("add the lobby display");
    await screen.findByTestId("omnibot-ask");
    runReply = () => sse(fixture("resume-declined.sse"));
    fireEvent.click(screen.getByRole("button", { name: "Decline" }));
    await screen.findByText("Understood, I left it alone.");
    expect(calls.at(-1)?.body.resume).toEqual([{ interruptId: "call-2", status: "cancelled", payload: { approved: false } }]);
    expect(screen.getByText("Declined")).toBeTruthy();
  });

  it("keeps one conversation across prompts", async () => {
    runReply = () => sse(fixture("resume-declined.sse"));
    open();
    send("first");
    await screen.findByText("Understood, I left it alone.");
    await waitFor(() => expect((screen.getByLabelText("Ask omnibot") as HTMLTextAreaElement).disabled).toBe(false));
    send("second");
    await waitFor(() => expect(calls.filter((c) => c.url.includes(":run"))).toHaveLength(2));
    expect(calls.filter((c) => c.url.endsWith("/assistant/conversations"))).toHaveLength(1);
  });

  it("says when the run stopped at its step limit", async () => {
    runReply = () => sse(fixture("step-limit.sse"));
    open();
    send("look around");
    expect((await screen.findByTestId("omnibot-status")).textContent).toContain("Stopped after 10 steps");
  });

  it("says when this person may not run omnibot", async () => {
    runReply = () => json({ detail: "only a person, as themselves, runs omnibot" }, 403);
    open();
    send("x");
    const status = await screen.findByTestId("omnibot-status");
    expect(status.textContent).toContain("only a person, as themselves, runs omnibot");
    expect(status.dataset.status).toBe("refused");
  });

  it("names the reason a run failed", async () => {
    runReply = () => json({ detail: "omnibot has no model on this server" }, 503);
    open();
    send("x");
    const status = await screen.findByTestId("omnibot-status");
    expect(status.textContent).toContain("omnibot has no model on this server");
    expect(status.dataset.status).toBe("error");
  });

  it("hands omnibot a new secret's name, never its value", async () => {
    open();
    fireEvent.input(screen.getByLabelText("Ask omnibot"), { target: { value: "poll the lobby amp with" } });
    fireEvent.click(screen.getByRole("button", { name: /Add a credential/ }));
    fireEvent.input(await screen.findByPlaceholderText("poll-community"), { target: { value: "lobby-snmp" } });
    await screen.findByRole("option", { name: "SNMP community" });
    fireEvent.change(screen.getByDisplayValue("Choose a type…"), { target: { value: "snmp-community" } });
    fireEvent.input(await screen.findByLabelText("community"), { target: { value: "s3cret-value" } });
    fireEvent.click(screen.getByRole("button", { name: "Create secret" }));
    await waitFor(() => expect((screen.getByLabelText("Ask omnibot") as HTMLTextAreaElement).value).toBe("poll the lobby amp with secret lobby-snmp"));
    expect(calls.find((c) => c.url.endsWith("/secrets"))?.body.fields).toEqual({ community: "s3cret-value" });
    send((screen.getByLabelText("Ask omnibot") as HTMLTextAreaElement).value);
    await waitFor(() => expect(calls.some((c) => c.url.includes(":run"))).toBe(true));
    expect(JSON.stringify(calls.filter((c) => c.url.includes(":run")))).not.toContain("s3cret-value");
  });
});
