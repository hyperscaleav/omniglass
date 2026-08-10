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

  // The face follows the label, not the pen (#683): a label a rule rendered is
  // prose, so it must not sit in the identifier face just because no operator
  // typed it. The heading asked "is display_name blank" by hand, which was the
  // same question as "is this an identifier" only while every label was typed.
  it("shows a platform-rendered label in the prose face", () => {
    const { getByText } = render(() => (
      <BladeTitle row={() => ({ name: "display-1", display_name: "Display 1", display_name_generated: true })} fallback="display-1" />
    ));
    expect(getByText("Display 1").className).not.toContain("font-data");
  });

  // The heading used to ask "is display_name blank", which said prose face for a
  // label that merely repeats the name. IdentityCell has always called that an
  // identifier and rendered it once, in the data face; the two surfaces now agree
  // because they ask the same primitive.
  it("keeps the data face when the label merely repeats the name", () => {
    const { getByText } = render(() => (
      <BladeTitle row={() => ({ name: "codec-1", display_name: "codec-1" })} fallback="codec-1" />
    ));
    expect(getByText("codec-1").className).toContain("font-data");
  });

  // A rule with nothing to say about a row keeps the pen and stores no label
  // (ADR-0098), so the heading is the name and belongs in the data face.
  it("keeps the data face when the platform holds the pen but rendered nothing", () => {
    const { getByText } = render(() => (
      <BladeTitle row={() => ({ name: "codec-1", display_name: "", display_name_generated: true })} fallback="codec-1" />
    ));
    expect(getByText("codec-1").className).toContain("font-data");
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
