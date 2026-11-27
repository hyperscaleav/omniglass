import { describe, expect, it } from "vitest";
import { systemAddress, systemWithAddress } from "./system_name";

// A system is known by two things (#2757): the name a company gave its room
// ("Curie") and where the room is (its location, "Hq Tower 12 01"). The
// address rides beside the name wherever a system is named, except where it is
// the name already: a system called for its room says it once.
describe("a system's name and its address", () => {
  const curie = { name: "hq-conf-1", label: "Curie" };
  const room = { name: "hq-tower-12-01", label: "Hq Tower 12 01" };

  it("reads the location's label as the address", () => {
    expect(systemAddress(curie, room)).toBe("Hq Tower 12 01");
    expect(systemWithAddress(curie, room)).toBe("Curie · Hq Tower 12 01");
  });

  it("falls back to the location's name when it has no label", () => {
    expect(systemAddress(curie, { name: "hq-tower-12-01" })).toBe("hq-tower-12-01");
  });

  it("has no address for a system with no location", () => {
    expect(systemAddress(curie, undefined)).toBeUndefined();
    expect(systemWithAddress(curie, null)).toBe("Curie");
  });

  it("says it once when the system is named for its room", () => {
    expect(systemAddress({ name: "dev-board", label: "Boardroom A" }, { name: "boardroom-a", label: "Boardroom A" })).toBeUndefined();
    expect(systemWithAddress({ name: "dev-board", label: "Boardroom A" }, { name: "boardroom-a", label: "Boardroom A" })).toBe("Boardroom A");
  });

  it("reads a system's name when it has no label", () => {
    expect(systemWithAddress({ name: "hq-conf-1" }, room)).toBe("hq-conf-1 · Hq Tower 12 01");
  });
});
