import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import NotFound from "./NotFound";

// The not-found face is its own page: it names the address that was asked for,
// offers the two doors a lost person wants (Home, Explore), points at the
// command palette, and wears no "soon" badge (#2445).
function mount(path: string) {
  window.history.pushState({}, "", `/web${path}`);
  return render(() => (
    <Router base="/web">
      <Route path="*" component={NotFound} />
    </Router>
  ));
}

describe("NotFound", () => {
  it("says what it is, names the address, and links Home and Explore", () => {
    mount("/dashboards?x=1");
    expect(screen.getByText("Not found")).toBeInTheDocument();
    expect(screen.getByText("/web/dashboards?x=1")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /home/i })).toHaveAttribute("href", "/web");
    expect(screen.getByRole("link", { name: /explore/i })).toHaveAttribute("href", "/web/explore");
  });

  it("points at the command palette", () => {
    mount("/nope");
    expect(screen.getByText(/to search and jump/i)).toBeInTheDocument();
  });

  it("wears no soon badge", () => {
    const { container } = mount("/nope");
    expect(container.textContent).not.toMatch(/\bsoon\b/i);
  });
});
