import "@testing-library/jest-dom/vitest";
import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import LessonLive from "./LessonLive";
import { ME_KEY, type Me } from "../lib/auth";
import { effectiveMetricsKey } from "../lib/component_metrics";

const owner: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const metric = (name: string) => ({ metric_type_name: name, is_sampled: true, value: 1 });

// The threshold preview (#2450) keeps the metric the learner sees selected even
// when a metric sampled for the first time later sorts ahead of it: the draft
// must not change under them, which would clear the preview they just ran.
describe("LessonLive rule-preview", () => {
  it("latches the first metric shown as the choice", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], owner);
    qc.setQueryData([...effectiveMetricsKey("c-1")], [metric("temperature")]);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="*" component={() => <LessonLive kind="rule-preview" componentId="c-1" componentName="amp-1" />} />
        </Router>
      </QueryClientProvider>
    ));
    const select = (await screen.findByLabelText("Metric")) as HTMLSelectElement;
    expect(select.value).toBe("temperature");
    qc.setQueryData([...effectiveMetricsKey("c-1")], [metric("fan-speed"), metric("temperature")]);
    await waitFor(() => expect((screen.getByLabelText("Metric") as HTMLSelectElement).options.length).toBe(2));
    expect((screen.getByLabelText("Metric") as HTMLSelectElement).value).toBe("temperature");
  });
});
