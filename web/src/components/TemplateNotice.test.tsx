import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import TemplateNotice from "./TemplateNotice";
import { TEMPLATES_KEY } from "../lib/templates";

// The notice a created row's page shows after a fork or an instantiate (#1329):
// named by ?from=, saying the row is the operator's own and nothing follows the
// template, and dismissed by dropping the param.

let search = "";
function Host() {
  const loc = useLocation();
  return (
    <>
      <TemplateNotice />
      <span data-testid="search">{(search = loc.search)}</span>
    </>
  );
}

function mount(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TEMPLATES_KEY], [{ kind: "system_template", name: "mr55-system", label: "Meeting Room 55, staffed" }]);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/systems/:id" component={Host} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("TemplateNotice", () => {
  it("names the template and says the row is the operator's own, nothing following it", () => {
    mount("/systems/abc?from=mr55-system&tab=overview");
    const n = screen.getByTestId("template-notice");
    expect(n).toHaveTextContent("Started from the template Meeting Room 55, staffed.");
    expect(n).toHaveTextContent("It is yours now: nothing points back at the template");
  });

  it("names an unknown template by its name", () => {
    mount("/systems/abc?from=gone-template");
    expect(screen.getByTestId("template-notice")).toHaveTextContent("gone-template");
  });

  it("renders nothing without ?from=", () => {
    mount("/systems/abc");
    expect(screen.queryByTestId("template-notice")).toBeNull();
  });

  it("says what the start forked and what is still to attach", () => {
    mount("/systems/abc?from=mr55-system&forked=meeting-room,mr55&unattached=3");
    const n = screen.getByTestId("template-notice");
    expect(n).toHaveTextContent("It forked the standards meeting-room, mr55 to build to");
    expect(n).toHaveTextContent("3 components are still to attach.");
  });

  it("dismisses by dropping the param, keeping the rest of the address", async () => {
    mount("/systems/abc?from=mr55-system&tab=overview");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    await waitFor(() => expect(screen.queryByTestId("template-notice")).toBeNull());
    expect(search).toBe("?tab=overview");
  });
});
