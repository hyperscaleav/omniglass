import { render, screen } from "@solidjs/testing-library";
import { describe, expect, it } from "vitest";
import { IdentityCell, identityColumn } from "./IdentityCell";

// The display rule, pinned once. Before this primitive it was restated in sixteen
// FlatList column arrays across four idioms, which is why the same estate read
// four different ways depending on the page, and why nothing could assert it.
describe("IdentityCell", () => {
  it("shows the display name on top and the name beneath it", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-dsp", display_name: "HQ Boardroom DSP" }} />);
    expect(screen.getByText("HQ Boardroom DSP")).toBeTruthy();
    expect(screen.getByText("hq-boardroom-dsp")).toBeTruthy();
  });

  // The anti-duplication rule. Showing a name that is identical to the display
  // name renders the same string twice and reads as a bug.
  it("shows the name exactly once when there is no display name", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-nvx-tx" }} />);
    expect(screen.getAllByText("hq-boardroom-nvx-tx")).toHaveLength(1);
  });

  it("treats a blank display name as absent", () => {
    for (const blank of ["", "   ", "\t"]) {
      const { unmount } = render(() => <IdentityCell entity={{ name: "codec-1", display_name: blank }} />);
      expect(screen.getAllByText("codec-1")).toHaveLength(1);
      unmount();
    }
  });

  // A display name that happens to equal the name is the same case as no display
  // name: one line, not two identical ones.
  it("collapses when the display name equals the name", () => {
    render(() => <IdentityCell entity={{ name: "codec-1", display_name: "codec-1" }} />);
    expect(screen.getAllByText("codec-1")).toHaveLength(1);
  });

  // The name is not re-cased into a pseudo display name: this domain is acronyms,
  // and "Hq boardroom dsp" is worse than the honest identifier.
  it("never derives a display name from the name", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-dsp" }} />);
    expect(screen.queryByText("Hq boardroom dsp")).toBeNull();
    expect(screen.queryByText("Hq Boardroom Dsp")).toBeNull();
  });
});

describe("identityColumn", () => {
  it("sorts on the display name, not the name", () => {
    const col = identityColumn<{ name: string; display_name?: string | null }>();
    expect(col.sortVal({ name: "zzz-1", display_name: "Alpha" })).toBe("Alpha");
    // No display name means the name IS the label, so that is what sorts.
    expect(col.sortVal({ name: "aaa-1" })).toBe("aaa-1");
  });

  // One header word, and no seam through which a page could introduce a second.
  // identity-vocabulary-guard.test.ts holds the other half of that, a source scan
  // for anyone trying to pass a label option back in.
  it("uses one header word so every page agrees", () => {
    expect(identityColumn().label).toBe("Name");
    expect(identityColumn().key).toBe("name");
  });
});
