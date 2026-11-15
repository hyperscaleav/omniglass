import "@testing-library/jest-dom/vitest";
import { afterEach, describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { AuthGuard } from "./AuthGuard";
import { ME_KEY, type Me } from "../lib/auth";

// AuthGuard gates the console. When the caller's principal carries
// must_change_password (an admin reset), the guard replaces the whole app shell
// with the forced change-password screen; otherwise it renders its children. The
// principal is seeded into the query cache; /auth/status is answered by a fetch
// stub, a plain server unless a test holds it.
afterEach(() => vi.unstubAllGlobals());

function answerStatus(status: () => Promise<Response>) {
  vi.stubGlobal("fetch", vi.fn((req: Request) =>
    new URL(req.url).pathname.endsWith("/auth/status") ? status() : Promise.resolve(new Response("{}", { status: 404 })),
  ));
}

function mount(me: Me, status: () => Promise<Response> = () => Promise.resolve(Response.json({ lab: false }))) {
  answerStatus(status);
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route
          path="*"
          component={() => (
            <AuthGuard>
              <div>the app shell</div>
            </AuthGuard>
          )}
        />
      </Router>
    </QueryClientProvider>
  ));
}

describe("AuthGuard force-change gate", () => {
  const base: Me = { principal: { id: "u", kind: "human" }, human: { username: "alice" }, permissions: [], grants: [] };

  it("renders the app shell for an ordinary authenticated principal", async () => {
    mount(base);
    expect(await screen.findByText("the app shell")).toBeInTheDocument();
    expect(screen.queryByText("Set a new password")).not.toBeInTheDocument();
  });

  it("replaces the shell with the forced change-password screen when flagged", async () => {
    mount({ ...base, human: { username: "alice", must_change_password: true } });
    expect(await screen.findByText("Set a new password")).toBeInTheDocument();
    expect(screen.queryByText("the app shell")).not.toBeInTheDocument();
  });
});

// On a lab the shell carries a banner whose presence only /auth/status knows.
// Painting the shell before that answer lands and then inserting the banner
// shifts every screen down (#2003), so the guard holds its loader until the
// status has settled; a failed status read still lets the console in.
describe("AuthGuard waits for the server status", () => {
  const base: Me = { principal: { id: "u", kind: "human" }, human: { username: "alice" }, permissions: [], grants: [] };

  it("holds the shell until /auth/status answers", async () => {
    let answer!: (r: Response) => void;
    mount(base, () => new Promise((r) => (answer = r)));
    expect(screen.queryByText("the app shell")).not.toBeInTheDocument();
    await vi.waitFor(() => expect(answer).toBeDefined());
    expect(screen.queryByText("the app shell")).not.toBeInTheDocument();
    answer(Response.json({ lab: true }));
    expect(await screen.findByText("the app shell")).toBeInTheDocument();
  });

  it("lets the shell in when /auth/status fails", async () => {
    mount(base, () => Promise.resolve(new Response("{}", { status: 500 })));
    expect(await screen.findByText("the app shell")).toBeInTheDocument();
  });
});
