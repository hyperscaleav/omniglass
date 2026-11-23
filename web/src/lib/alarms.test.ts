import { describe, expect, it } from "vitest";
import { alarmDuration, alarmDurationText } from "./alarms";

describe("how long an alarm was raised (#2426)", () => {
  const raised = "2026-11-23T10:00:00Z";
  it("is raised to cleared for a cleared alarm, however late it is read", () => {
    const a = { raised_at: raised, cleared_at: "2026-11-23T10:12:00Z" };
    expect(alarmDurationText(a, Date.parse("2026-11-30T00:00:00Z"))).toBe("12m");
    expect(alarmDuration(a)).toBe(12 * 60_000);
  });
  it("is raised to now for an open alarm, so it grows", () => {
    const a = { raised_at: raised };
    expect(alarmDurationText(a, Date.parse("2026-11-23T10:00:45Z"))).toBe("45s");
    expect(alarmDurationText(a, Date.parse("2026-11-23T13:12:00Z"))).toBe("3h 12m");
  });
  it("never goes negative on a skewed clock", () => {
    expect(alarmDuration({ raised_at: raised }, Date.parse("2026-11-23T09:00:00Z"))).toBe(0);
  });
});
