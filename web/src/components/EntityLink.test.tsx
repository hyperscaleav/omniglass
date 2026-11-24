import "@testing-library/jest-dom/vitest";
import { describe, expect, it } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { MemoryRouter, Route } from "@solidjs/router";
import EntityLink from "./EntityLink";
import { drawerHref } from "../lib/drawer_address";

describe("EntityLink", () => {
  it("links a bare address through drawerHref and reads it as the handle", () => {
    render(() => <EntityLink kind="vendor" address="boreal" />);
    const a = screen.getByRole("link", { name: "boreal" });
    expect(a).toHaveAttribute("href", drawerHref("vendor", "boreal"));
    expect(a).toHaveAttribute("data-entity-link", "vendor");
    expect(a.className).toContain("font-data");
  });

  it("reads a catalog row as its label and opens it by name", () => {
    render(() => <EntityLink kind="driver" entity={{ name: "boreal-beam-snmp", label: "Boreal Beam SNMP" }} />);
    const a = screen.getByRole("link", { name: "Boreal Beam SNMP" });
    expect(a).toHaveAttribute("href", "/drivers?open=boreal-beam-snmp");
    expect(a.className).not.toContain("font-data");
  });

  it("a row with no label reads as its name, in the data face", () => {
    render(() => <EntityLink kind="severity_level" entity={{ name: "high" }} />);
    expect(screen.getByRole("link", { name: "high" }).className).toContain("font-data");
  });

  it("carries extra params and keeps the router's base inside a Router", async () => {
    render(() => (
      <MemoryRouter>
        <Route path="/" component={() => <EntityLink kind="user" address="u-1" extra={{ edit: "1" }} />} />
      </MemoryRouter>
    ));
    expect(await screen.findByRole("link", { name: "u-1" })).toHaveAttribute("href", "/users?open=u-1&edit=1");
  });
});
