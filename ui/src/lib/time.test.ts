import { describe, expect, it } from "vitest";

import { ago, duration } from "./time";

describe("duration", () => {
  it("formats seconds, minutes and hours", () => {
    expect(duration(12_400)).toBe("12s");
    expect(duration(64_000)).toBe("1m 4s");
    expect(duration(3_840_000)).toBe("1h 4m");
    expect(duration(-5)).toBe("0s");
  });
});

describe("ago", () => {
  it("formats recent times relative to now", () => {
    const now = new Date("2026-10-19T12:00:00Z");
    expect(ago(new Date("2026-10-19T11:59:59Z"), now)).toBe("just now");
    expect(ago(new Date("2026-10-19T11:59:55Z"), now)).toBe("5 s ago");
    expect(ago(new Date("2026-10-19T11:59:30Z"), now)).toBe("30 s ago");
    expect(ago(new Date("2026-10-19T11:56:00Z"), now)).toBe("4 min ago");
    expect(ago(new Date("2026-10-19T08:30:00Z"), now)).toBe("3 h ago");
  });
});
