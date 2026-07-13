import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { AuthGuard } from "./AuthGuard";
import { ME_KEY, type Me } from "../lib/auth";

// AuthGuard gates the console. When the caller's principal carries
// must_change_password (an admin reset), the guard replaces the whole app shell
// with the forced change-password screen; otherwise it renders its children. Data
// is seeded into the query cache so no server is needed.
function mount(me: Me) {
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
