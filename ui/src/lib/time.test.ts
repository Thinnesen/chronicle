import { describe, expect, it } from "vitest";

import { duration } from "./time";

describe("duration", () => {
  it("formats seconds, minutes and hours", () => {
    expect(duration(12_400)).toBe("12s");
    expect(duration(64_000)).toBe("1m 4s");
    expect(duration(3_840_000)).toBe("1h 4m");
    expect(duration(-5)).toBe("0s");
  });
});
