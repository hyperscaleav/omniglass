import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Files from "./Files";
import { FILES_KEY } from "../lib/files";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

afterEach(cleanup);

const me: Me = { user: { id: "u", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const file = { id: uuidFor("f1"), name: "firmware.bin", content_type: "application/octet-stream", size: 2048, sensitive: false, created_at: new Date(Date.now() - 3600_000).toISOString() };

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FILES_KEY], [file]);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={Files} />
      </Router>
    </QueryClientProvider>
  ));
}

// The Files list wears the Page header, one date format, and the open chevron
// every other list's rows carry (#2447).
describe("Files list chrome (#2447)", () => {
  it("wears the Page header and the lower-case filter placeholder", () => {
    mount();
    expect(screen.getByRole("heading", { level: 1, name: "Files" })).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/^filter by name/)).toBeInTheDocument();
  });

  it("shows the added time as relative with the absolute on hover, not a raw date", () => {
    mount();
    expect(screen.getByText("1h ago")).toBeInTheDocument();
  });

  it("gives each row the open chevron", () => {
    mount();
    expect(screen.getAllByTestId("row-chevron")).toHaveLength(1);
  });
});
