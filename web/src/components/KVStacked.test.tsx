import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import KVStacked from "./KVStacked";

describe("KVStacked", () => {
  it("renders an eyebrow label above the value", () => {
    const { getByText } = render(() => <KVStacked label="Type" value={<span>mic</span>} />);
    const label = getByText("Type");
    expect(label.className).toContain("eyebrow"); // the small-caps label
    expect(getByText("mic")).toBeTruthy(); // the value
  });

  it("wraps the value in a plain text-sm box (no mono) by default", () => {
    const { container } = render(() => <KVStacked label="Type" value="mic" />);
    const box = container.querySelector(".text-sm")!;
    expect(box.className).toContain("text-sm");
    expect(box.className).not.toContain("font-data"); // matches the legacy fact markup
  });

  it("renders the value font-data when mono is set", () => {
    const { container } = render(() => <KVStacked label="Name" value="og-1" mono />);
    expect(container.querySelector(".text-sm")!.className).toContain("font-data");
  });

  it("renders the label even when no value is given", () => {
    const { getByText } = render(() => <KVStacked label="Parent" />);
    expect(getByText("Parent")).toBeTruthy();
  });
});
