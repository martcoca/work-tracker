import axe from "axe-core";
import { expect, it } from "vitest";
import { fakeAPI, fakeAuth, render, syntheticUser } from "./test-fixtures";

it("has no automated WCAG 2.2 A or AA violations on list and packet views", async () => {
  const checked: string[] = [];
  for (const path of ["/", "/initiatives/0004/epics/E02/packets/0004-E02-T01", "/initiatives/0004/epics/E02/new"]) {
    const { wrapper } = await render(path, fakeAuth(syntheticUser()).auth, fakeAPI());
    const report = await axe.run(wrapper.element, {
      runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"] },
      rules: { "color-contrast": { enabled: false } },
    });
    checked.push(`${path}: ${report.violations.length} violations, ${report.incomplete.length} incomplete`);
    expect(report.violations, JSON.stringify(report.violations, null, 2)).toEqual([]);
    wrapper.unmount();
  }
  console.info(`axe-core 4.13.0: ${checked.join("; ")}`);
});

it("meets AA contrast for the tracked foreground and action color pairs", () => {
  const pairs: Array<[string, string]> = [
    ["#102a43", "#f6f8fb"],
    ["#486581", "#ffffff"],
    ["#075985", "#ffffff"],
    ["#bcccdc", "#102a43"],
  ];
  const results: string[] = [];
  for (const [foreground, background] of pairs) {
    const ratio = contrast(foreground, background);
    results.push(`${foreground} on ${background} = ${ratio.toFixed(2)}:1`);
    expect(ratio, `${foreground} on ${background}`).toBeGreaterThanOrEqual(4.5);
  }
  console.info(`contrast: ${results.join("; ")}`);
});

function contrast(left: string, right: string): number {
  const [bright, dark] = [luminance(left), luminance(right)].sort((a, b) => b - a);
  return ((bright ?? 0) + 0.05) / ((dark ?? 0) + 0.05);
}

function luminance(hex: string): number {
  const channels = [hex.slice(1, 3), hex.slice(3, 5), hex.slice(5, 7)].map((value) => Number.parseInt(value, 16) / 255);
  const linear = channels.map((channel) => (channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4));
  return 0.2126 * (linear[0] ?? 0) + 0.7152 * (linear[1] ?? 0) + 0.0722 * (linear[2] ?? 0);
}
