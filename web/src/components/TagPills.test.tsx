import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import TagPills from "./TagPills";
import { tagHue } from "../lib/tagcolor";

describe("TagPills", () => {
  it("renders one chip per key, sorted, as key=value", () => {
    const { container } = render(() => <TagPills tags={{ environment: "prod", asset_id: "A-42" }} />);
    const chips = container.querySelectorAll(".tag-pill");
    expect(chips.length).toBe(2);
    // sorted: asset_id before environment
    expect(chips[0].textContent).toBe("asset_id=A-42");
    expect(chips[1].textContent).toBe("environment=prod");
  });

  it("colors each chip by its key via the --tag-h custom property", () => {
    const { container } = render(() => <TagPills tags={{ environment: "prod" }} />);
    const chip = container.querySelector(".tag-pill") as HTMLElement;
    expect(chip.style.getPropertyValue("--tag-h")).toBe(String(tagHue("environment")));
  });

  it("renders a muted dash when there are no tags", () => {
    const { container } = render(() => <TagPills tags={{}} />);
    expect(container.querySelector(".tag-pill")).toBeNull();
    expect(container.textContent).toContain("—");
  });

  it("treats an undefined tag set as empty", () => {
    const { container } = render(() => <TagPills />);
    expect(container.querySelector(".tag-pill")).toBeNull();
    expect(container.textContent).toContain("—");
  });

  it("wrap mode lays every chip out inline without the one-line tooltip trigger", () => {
    const { container } = render(() => <TagPills wrap tags={{ a: "1", b: "2", c: "3" }} />);
    expect(container.querySelectorAll(".tag-pill").length).toBe(3);
    // no clipped one-line row in wrap mode
    expect(container.querySelector(".tag-fade")).toBeNull();
  });
});
