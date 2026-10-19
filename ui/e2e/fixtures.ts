import { expect, test as base } from "@playwright/test";

// test fails each test in which the strict Content Security Policy blocks a part of the page, for example a style
// that a component injects. WebKit for Linux also reports a native <select>, so the check runs in Chromium only.
export const test = base.extend<{ csp: undefined }>({
  csp: [
    async ({ page, browserName }, use) => {
      const violations: string[] = [];
      page.on("console", (m) => m.text().includes("Content Security Policy") && violations.push(m.text().slice(0, 160)));
      await use(undefined);
      if (browserName === "chromium") expect(violations, "CSP violations").toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };
