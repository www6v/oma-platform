import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Markdown, normalizeDisplayMath } from "./Markdown";

describe("normalizeDisplayMath", () => {
  it("promotes whole-line $$ formulas to display fences", () => {
    expect(normalizeDisplayMath("$$S = 1$$\n")).toBe("$$\nS = 1\n$$\n");
  });

  it("leaves inline $$ untouched mid-sentence", () => {
    expect(normalizeDisplayMath("see $$x$$ here")).toBe("see $$x$$ here");
  });
});

describe("Markdown math", () => {
  it("renders block LaTeX via KaTeX", () => {
    const { container } = render(
      <Markdown>{"$$S = \\frac{n(n+1)}{2}$$"}</Markdown>
    );
    expect(container.querySelector(".katex")).not.toBeNull();
    expect(container.querySelector(".katex-display")).not.toBeNull();
    expect(screen.queryByText(/\$\$S/)).toBeNull();
  });

  it("renders the moon-distance style formula from agent messages", () => {
    const md =
      "平均距离约：\n\n$$384{,}400 \\text{ 公里} = 384{,}400{,}000 \\text{ 米}$$\n\n更多";
    const { container } = render(<Markdown>{md}</Markdown>);
    expect(container.querySelector(".katex")).not.toBeNull();
    expect(container.textContent).not.toContain("$$");
  });

  it("does not treat bare dollar amounts as math", () => {
    render(<Markdown>{"The price is $100 today."}</Markdown>);
    expect(screen.getByText(/The price is \$100 today\./)).toBeTruthy();
  });
});
