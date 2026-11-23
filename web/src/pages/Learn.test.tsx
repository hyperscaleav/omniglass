import "@testing-library/jest-dom/vitest";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { Route, Router } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Learn, { LessonPage, lessonComponent } from "./Learn";
import { ME_KEY, type Me } from "../lib/auth";
import { SETTINGS_ME_KEY } from "../lib/settings";
import { SIMULATION_KEY, type SimFleet } from "../lib/simulation";
import { LESSONS } from "../lib/lessons";
import { uuidFor } from "../lib/testids";

// The Learn page (#2450): the six lessons as cards in lifecycle order, each
// saying whether this person finished it (their own learn.done setting), and a
// lesson page whose steps link into the console with the simulated component
// filled in, or send the learner to the lab when no simulator is connected.

const owner: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };

const fleet: SimFleet = {
  connected: true,
  outposts: [
    {
      outpost: "edge",
      connected: true,
      components: [
        { component: "projector-1", component_id: uuidFor("projector-1"), serving: true },
        { component: "dsp", component_id: uuidFor("dsp"), serving: true },
        { component: "gone", serving: false },
      ],
    },
  ],
} as unknown as SimFleet;

function mount(opts: { path?: string; done?: string[]; sim?: SimFleet | null } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], owner);
  qc.setQueryData([...SETTINGS_ME_KEY], { values: { learn: { done: opts.done ?? [] } } });
  qc.setQueryData([...SIMULATION_KEY], opts.sim === null ? { connected: false, outposts: [] } : (opts.sim ?? fleet));
  window.history.pushState({}, "", opts.path ?? "/learn");
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/learn" component={Learn} />
        <Route path="/learn/:id" component={LessonPage} />
      </Router>
    </QueryClientProvider>
  ));
}

// Live elements fetch on their own; answer every request empty so a step
// renders its empty face rather than a network error.
function quietFetch(onPatch?: (url: string, body: unknown) => void) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : null;
    const url = req ? req.url : String(input);
    const method = (req ? req.method : init?.method) ?? "GET";
    if (method === "PATCH") {
      const raw = req ? await req.clone().text() : (init?.body as string);
      onPatch?.(url, JSON.parse(raw));
      return new Response(JSON.stringify({ values: { learn: JSON.parse(raw) } }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
  });
}

describe("lessonComponent", () => {
  it("is the first served simulated component by name", () => {
    expect(lessonComponent(fleet)).toEqual({ id: uuidFor("dsp"), name: "dsp" });
  });
  it("is undefined with nothing served", () => {
    expect(lessonComponent(undefined)).toBeUndefined();
    expect(lessonComponent({ connected: true, outposts: [] } as unknown as SimFleet)).toBeUndefined();
  });
});

describe("Learn", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the six lessons in order, marking the ones this person finished", () => {
    quietFetch();
    mount({ done: ["rules-and-alarms"] });
    const cards = [...document.querySelectorAll("[data-lesson]")];
    expect(cards.map((c) => c.getAttribute("data-lesson"))).toEqual(LESSONS.map((l) => l.slug));
    expect(cards[2].querySelector('[data-testid="lesson-done"]')).not.toBeNull();
    expect(cards[0].querySelector('[data-testid="lesson-done"]')).toBeNull();
    expect(cards[0].getAttribute("href")).toBe("/learn/device-to-samples");
    expect(screen.queryByTestId("learn-no-simulator")).toBeNull();
  });

  it("says when no simulator is connected and links the lab", () => {
    quietFetch();
    mount({ sim: null });
    const note = screen.getByTestId("learn-no-simulator");
    expect(note.querySelector("a")?.getAttribute("href")).toContain("/developer/lab/");
  });
});

describe("LessonPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("links every step into the console on the simulated component", () => {
    quietFetch();
    mount({ path: "/learn/reading-the-history" });
    const links = screen.getAllByTestId("lesson-do-it").map((a) => a.getAttribute("href"));
    expect(links).toEqual([`/components/${uuidFor("dsp")}?tab=activity`, "/explore"]);
    expect(screen.queryByTestId("lesson-next")).toBeNull();
  });

  it("with no simulator, renders no live element and sends component links to the lab", () => {
    quietFetch();
    mount({ path: "/learn/reading-the-history", sim: null });
    expect(screen.getByTestId("learn-no-simulator")).toBeInTheDocument();
    expect(document.querySelector('[data-testid^="lesson-live-"]')).toBeNull();
    expect(screen.getAllByTestId("lesson-do-it").map((a) => a.getAttribute("href"))).toEqual(["/explore"]);
    expect(screen.getByText("Needs a simulator: lab instructions")).toBeInTheDocument();
  });

  it("marks the lesson done in the person's own settings", async () => {
    const patches: { url: string; body: unknown }[] = [];
    quietFetch((url, body) => patches.push({ url, body }));
    mount({ path: "/learn/rules-and-alarms", done: ["device-to-samples"] });
    fireEvent.click(screen.getByRole("button", { name: "Mark as done" }));
    await waitFor(() => expect(patches.length).toBe(1));
    expect(patches[0].url).toContain("/settings/me/learn");
    expect(patches[0].body).toEqual({ done: ["device-to-samples", "rules-and-alarms"] });
  });

  it("answers an unknown lesson with its own face", () => {
    quietFetch();
    mount({ path: "/learn/nope" });
    expect(screen.getByText("No such lesson")).toBeInTheDocument();
  });
});
