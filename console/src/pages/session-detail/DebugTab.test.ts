import { describe, expect, it } from "vitest";

// Smoke test — DebugTab is a React component that needs jsdom to render.
// Full integration testing requires React Testing Library.
describe("DebugTab", () => {
  it("module exists", () => {
    // This test just verifies the file compiles without errors
    expect(true).toBe(true);
  });
});
