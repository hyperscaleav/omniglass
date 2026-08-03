import { render, screen } from "@solidjs/testing-library";
import { describe, expect, it } from "vitest";
import { IdentityCell, identityColumn } from "./IdentityCell";

// The display rule, pinned once. Before this primitive it was restated in sixteen
// FlatList column arrays across four idioms, which is why the same estate read
// four different ways depending on the page, and why nothing could assert it.
describe("IdentityCell", () => {
  it("shows the label on top and the segment beneath it", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-dsp", display_name: "HQ Boardroom DSP" }} />);
    expect(screen.getByText("HQ Boardroom DSP")).toBeTruthy();
    expect(screen.getByText("hq-boardroom-dsp")).toBeTruthy();
  });

  // The anti-duplication rule. Showing a segment that is identical to the label
  // renders the same string twice and reads as a bug.
  it("shows the segment exactly once when there is no label", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-nvx-tx" }} />);
    expect(screen.getAllByText("hq-boardroom-nvx-tx")).toHaveLength(1);
  });

  it("treats a blank label as absent", () => {
    for (const blank of ["", "   ", "\t"]) {
      const { unmount } = render(() => <IdentityCell entity={{ name: "codec-1", display_name: blank }} />);
      expect(screen.getAllByText("codec-1")).toHaveLength(1);
      unmount();
    }
  });

  // A label that happens to equal the segment is the same case as no label: one
  // line, not two identical ones.
  it("collapses when the label equals the segment", () => {
    render(() => <IdentityCell entity={{ name: "codec-1", display_name: "codec-1" }} />);
    expect(screen.getAllByText("codec-1")).toHaveLength(1);
  });

  // The segment is not re-cased into a pseudo-label: this domain is acronyms, and
  // "Hq boardroom dsp" is worse than the honest identifier.
  it("never derives a label from the segment", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-dsp" }} />);
    expect(screen.queryByText("Hq boardroom dsp")).toBeNull();
    expect(screen.queryByText("Hq Boardroom Dsp")).toBeNull();
  });
});

describe("identityColumn", () => {
  it("sorts on the label, not the segment", () => {
    const col = identityColumn<{ name: string; display_name?: string | null }>();
    expect(col.sortVal({ name: "zzz-1", display_name: "Alpha" })).toBe("Alpha");
    // No label means the segment IS the label, so that is what sorts.
    expect(col.sortVal({ name: "aaa-1" })).toBe("aaa-1");
  });

  it("uses one header word by default so every page agrees", () => {
    expect(identityColumn().label).toBe("Name");
    expect(identityColumn().key).toBe("name");
  });
});
