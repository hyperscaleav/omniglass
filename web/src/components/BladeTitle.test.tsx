import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import BladeTitle from "./BladeTitle";
import { type Labelled } from "../lib/entities";

describe("BladeTitle", () => {
  it("shows the display name of the row", () => {
    const { getByText } = render(() => (
      <BladeTitle row={() => ({ name: "acme-av", display_name: "Acme AV" })} fallback="acme-av" />
    ));
    expect(getByText("Acme AV")).toBeTruthy();
  });

  it("falls back to the identifier, in the data face, when there is no display name", () => {
    const { getByText } = render(() => (
      <BladeTitle row={() => ({ name: "acme-av", display_name: "" })} fallback="acme-av" />
    ));
    expect(getByText("acme-av").className).toContain("font-data");
  });

  it("shows the fallback before the row resolves", () => {
    const { getByText } = render(() => <BladeTitle row={() => undefined} fallback="acme-av" />);
    expect(getByText("acme-av")).toBeTruthy();
  });

  // #579. The heading read its accessor once in the component body, which in
  // Solid subscribes to nothing, so a rename reached the row and the body and
  // never the heading. This is that bug as a unit test.
  it("follows its row when the display name changes", () => {
    const [row, setRow] = createSignal<Labelled>({ name: "acme-av", display_name: "Acme AV" });
    const { getByText, queryByText } = render(() => <BladeTitle row={row} fallback="acme-av" />);
    expect(getByText("Acme AV")).toBeTruthy();

    setRow({ name: "acme-av", display_name: "Acme Audio Visual" });
    expect(getByText("Acme Audio Visual")).toBeTruthy();
    expect(queryByText("Acme AV")).toBeNull();
  });

  it("follows its row from absent to resolved", () => {
    const [row, setRow] = createSignal<Labelled | undefined>(undefined);
    const { getByText } = render(() => <BladeTitle row={row} fallback="acme-av" />);
    expect(getByText("acme-av").className).toContain("font-data");

    setRow({ name: "acme-av", display_name: "Acme AV" });
    const resolved = getByText("Acme AV");
    expect(resolved.className).not.toContain("font-data");
  });
});
