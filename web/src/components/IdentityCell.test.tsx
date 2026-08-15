import { render, screen } from "@solidjs/testing-library";
import { describe, expect, it } from "vitest";
import { IdentityCell, identityColumn } from "./IdentityCell";

// The display rule, pinned once. Before this primitive it was restated in sixteen
// FlatList column arrays across four idioms, which is why the same estate read
// four different ways depending on the page, and why nothing could assert it.
describe("IdentityCell", () => {
  it("shows the label on top and the name beneath it", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-dsp", label: "HQ Boardroom DSP" }} />);
    expect(screen.getByText("HQ Boardroom DSP")).toBeTruthy();
    expect(screen.getByText("hq-boardroom-dsp")).toBeTruthy();
  });

  // The anti-duplication rule. Showing a name that is identical to the display
  // name renders the same string twice and reads as a bug.
  it("shows the name exactly once when there is no label", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-nvx-tx" }} />);
    expect(screen.getAllByText("hq-boardroom-nvx-tx")).toHaveLength(1);
  });

  it("treats a blank label as absent", () => {
    for (const blank of ["", "   ", "\t"]) {
      const { unmount } = render(() => <IdentityCell entity={{ name: "codec-1", label: blank }} />);
      expect(screen.getAllByText("codec-1")).toHaveLength(1);
      unmount();
    }
  });

  // A label that happens to equal the name is the same case as no display
  // name: one line, not two identical ones.
  it("collapses when the label equals the name", () => {
    render(() => <IdentityCell entity={{ name: "codec-1", label: "codec-1" }} />);
    expect(screen.getAllByText("codec-1")).toHaveLength(1);
  });

  // The name is not re-cased into a pseudo label: this domain is acronyms,
  // and "Hq boardroom dsp" is worse than the honest identifier.
  it("never derives a label from the name", () => {
    render(() => <IdentityCell entity={{ name: "hq-boardroom-dsp" }} />);
    expect(screen.queryByText("Hq boardroom dsp")).toBeNull();
    expect(screen.queryByText("Hq Boardroom Dsp")).toBeNull();
  });

  // The pen (#683). A label a rule rendered differs from the name exactly as an
  // operator's does, so the old string comparison would have put a second
  // identifier line under every row in the estate.
  it("does not repeat the name beneath a label the platform rendered", () => {
    render(() => <IdentityCell entity={{ name: "display-1", label: "Display 1", label_generated: true }} />);
    expect(screen.getByText("Display 1")).toBeTruthy();
    expect(screen.queryByText("display-1")).toBeNull();
  });

  // The same row with the pen the other way round: the operator typed it, so the
  // identifier is theirs to read and copy.
  it("still shows the name beneath a label the operator typed", () => {
    render(() => <IdentityCell entity={{ name: "display-1", label: "Display 1", label_generated: false }} />);
    expect(screen.getByText("display-1")).toBeTruthy();
  });

  // The pen's mark is NOT here (#693). It was, as a full-text "Generated" chip,
  // and it cost the Name column the width of the word on every row of all 18
  // pages this cell serves while telling an operator something they could not act
  // on from a list. The fact moved to the field it belongs to, the label
  // on the edit blade, where the same lock the create form carries states it and
  // offers the act (components/LabelPenField.tsx). The whole-estate question the
  // chip half-answered ("which rows would a rule edit rewrite") is answered whole
  // by `<entity> previewLabels`, which a chip could only ever answer one row at a
  // time.
  it("renders no pen chip for a label the platform rendered", () => {
    render(() => <IdentityCell entity={{ name: "display-1", label: "Display 1", label_generated: true }} />);
    expect(screen.getByText("Display 1")).toBeTruthy();
    expect(screen.queryByText("Generated")).toBeNull();
    expect(screen.queryByTitle(/platform/i)).toBeNull();
  });

  it("renders no pen chip for a label the operator typed either", () => {
    render(() => <IdentityCell entity={{ name: "display-1", label: "Display 1" }} />);
    expect(screen.queryByText("Generated")).toBeNull();
    expect(screen.queryByTitle(/platform/i)).toBeNull();
  });

  // Holding the pen over a rule that rendered nothing was never a generated
  // label: what shows IS the name. The row still reads as one line, which is the
  // half of this case that outlived the chip.
  it("shows a row whose rule rendered nothing as its name, once", () => {
    render(() => <IdentityCell entity={{ name: "codec-1", label: "", label_generated: true }} />);
    expect(screen.queryByTitle(/platform/i)).toBeNull();
    expect(screen.getAllByText("codec-1")).toHaveLength(1);
  });

  // The face is the label's, not the pen's. A rendered label is prose ("Display
  // 1"), so it must not fall into the data face just because no second line
  // follows it; an unlabelled row is an identifier and must.
  it("renders a generated label in the prose face and a bare name in the data face", () => {
    const { unmount } = render(() => (
      <IdentityCell entity={{ name: "display-1", label: "Display 1", label_generated: true }} />
    ));
    expect(screen.getByText("Display 1").className).not.toContain("font-data");
    unmount();
    render(() => <IdentityCell entity={{ name: "codec-1" }} />);
    expect(screen.getByText("codec-1").className).toContain("font-data");
  });
});

describe("identityColumn", () => {
  it("sorts on the label, not the name", () => {
    const col = identityColumn<{ name: string; label?: string | null }>();
    expect(col.sortVal({ name: "zzz-1", label: "Alpha" })).toBe("Alpha");
    // No label means the name IS the label, so that is what sorts.
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
