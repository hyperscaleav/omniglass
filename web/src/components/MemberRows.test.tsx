import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import MemberRows from "./MemberRows";
import type { MemberRow } from "../lib/detail";

const row = (over: Partial<MemberRow>): MemberRow => ({
  id: "c1", name: "service-1", label: "Fleet Recording Service", slot: "", product: "Shinra Recording Service",
  icon: "server", health: null, also: [], noSlot: true, ...over,
});

// A component shared by several systems carries a chip naming them. The chip
// once held its full width (flex-none) while the name truncated, so a long list
// squeezed the name to nothing: the row read as only "also in A, B, C". The
// chip now gives way first and the name keeps a floor, as the outline's does.
describe("MemberRows", () => {
  it("keeps a shared component's name readable beside a long also-in chip", () => {
    const also = ["Auditorium", "Boardroom B", "Briefing Room"];
    render(() => <MemberRows rows={[row({ also })]} groups={[]} onOpen={() => {}} />);
    const name = screen.getByTestId("member-c1").querySelector("[data-label]") as HTMLElement;
    expect(name.textContent).toBe("Fleet Recording Service");
    expect(name.className).toContain("min-w-[4rem]");
    const chip = screen.getByTestId("member-c1").querySelector("[data-also-in]") as HTMLElement;
    expect(chip.className).toContain("shrink-[999]");
    expect(chip.className).toContain("truncate");
    expect(chip.className).not.toContain("flex-none");
    expect(chip.getAttribute("title")).toBe("Also in Auditorium, Boardroom B, Briefing Room");
    expect(chip.textContent).toContain("also in Auditorium, Boardroom B, Briefing Room");
  });

  it("draws no chip for a component in one system", () => {
    render(() => <MemberRows rows={[row({})]} groups={[]} onOpen={() => {}} />);
    expect(screen.getByTestId("member-c1").querySelector("[data-also-in]")).toBeNull();
  });
});
