import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { CalcRuleLink } from "./RuleLink";

// A calculated value names the calc rule that derived it (#999), linked by
// the rule's id, so a rename since does not break the link.
describe("CalcRuleLink", () => {
  it("links a calculated value to its calc rule by id", () => {
    render(() => (
      <Router>
        <Route path="*" component={() => <CalcRuleLink name="room-mean" id="11111111-1111-7111-8111-111111111111" />} />
      </Router>
    ));
    const a = screen.getByRole("link", { name: "room-mean" });
    expect(a.getAttribute("href")).toBe("/calc-rules?rule=11111111-1111-7111-8111-111111111111");
    expect(screen.getByText(/calc rule/)).toBeTruthy();
  });
});
