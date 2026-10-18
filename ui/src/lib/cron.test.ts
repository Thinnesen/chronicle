import { describe, expect, it } from "vitest";

import { readable, relative } from "./cron";

describe("cron", () => {
  it("makes schedules readable", () => {
    expect(readable("0 3 * * *")).toBe("At 03:00");
    expect(readable("not a schedule")).toBe("not a schedule");
  });

  it("formats relative times", () => {
    const now = Date.parse("2026-10-18T12:00:00Z");
    expect(relative(new Date(now + 3 * 3600_000 + 4 * 60_000), now)).toBe("in 3h 4m");
    expect(relative(new Date(now - 5 * 60_000), now)).toBe("5m ago");
    expect(relative(new Date(now + 26 * 3600_000), now)).toBe("in 1d 2h");
    expect(relative(new Date(now + 10_000), now)).toBe("in less than a minute");
  });
});
