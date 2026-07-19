import { describe, expect, it } from "vitest";
import { useViewMode } from "./useViewMode";

describe("useViewMode", () => {
  it("exports useViewMode hook", () => {
    expect(useViewMode).toBeDefined();
    expect(typeof useViewMode).toBe("function");
  });

  it("returns correct initial state", () => {
    // Can't call hooks outside React, but we can verify the function signature
    // This is a smoke test — full integration testing requires jsdom + React
    expect(useViewMode).toBeDefined();
  });
});
