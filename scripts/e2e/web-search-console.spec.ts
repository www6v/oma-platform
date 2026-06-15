import { test, expect, type Page } from "@playwright/test";

/**
 * Console E2E: web_search tool card visible in session timeline.
 *
 * Prerequisites:
 *   - oma-server on :8787 (or CONSOLE_URL single-stack)
 *   - vite on :5173 OR CONSOLE_URL pointing at integrated server
 *   - Real harness + LLM (OMA_FAKE_HARNESS=0)
 *
 * Run:
 *   cd scripts/e2e && npx playwright test web-search-console.spec.ts \
 *     --config=playwright-web-search.config.ts
 */

const API_BASE =
  process.env.OMA_API_URL ||
  process.env.PLATFORM_URL ||
  "http://localhost:8787";
const CONSOLE_BASE =
  process.env.CONSOLE_URL ||
  process.env.PLATFORM_URL ||
  process.env.OMA_API_URL ||
  "http://localhost:8787";
const TEST_EMAIL = `web-search-${Date.now()}@test.openma.dev`;
const TEST_PASSWORD = "e2e-websearch-pass-123";
const TEST_NAME = "Web Search E2E";
const MODEL = process.env.SMOKE_MODEL || "claude-sonnet-4-6";

async function signup(page: Page) {
  await page.goto("/");
  await page.waitForURL("**/login");
  await page.getByRole("button", { name: "Sign up" }).click();
  await expect(
    page.getByRole("heading", { name: "Create your account" }),
  ).toBeVisible();
  await page.getByPlaceholder("Your name").fill(TEST_NAME);
  await page.getByPlaceholder("you@example.com").fill(TEST_EMAIL);
  await page.getByPlaceholder("Min 8 characters").fill(TEST_PASSWORD);
  await page.getByRole("button", { name: "Create account" }).click();
  await page.waitForURL("**/", { timeout: 15_000 });
}

async function apiJson(
  page: Page,
  path: string,
  init?: { method?: string; body?: unknown },
) {
  const res = await page.request.fetch(`${API_BASE}${path}`, {
    method: init?.method ?? "GET",
    headers: { "content-type": "application/json" },
    data: init?.body,
  });
  const text = await res.text();
  if (!res.ok()) {
    throw new Error(`${init?.method ?? "GET"} ${path} → ${res.status}: ${text.slice(0, 300)}`);
  }
  return text ? JSON.parse(text) : {};
}

test.describe("web_search console E2E", () => {
  test("shows web_search tool card after agent search", async ({ page }) => {
    await signup(page);

    const agent = await apiJson(page, "/v1/agents", {
      method: "POST",
      body: {
        name: "Console Web Search Agent",
        model: MODEL,
        system:
          "For lookup questions you MUST use web_search. Never guess. " +
          "After results, answer briefly.",
        tools: [
          {
            type: "agent_toolset_20260401",
            default_config: { enabled: false },
            configs: [
              { name: "web_search", enabled: true },
              { name: "read", enabled: true },
            ],
          },
        ],
      },
    });

    const env = await apiJson(page, "/v1/environments", {
      method: "POST",
      body: {
        name: `web-search-ui-${Date.now()}`,
        config: { type: "cloud", networking: { type: "unrestricted" } },
      },
    });

    const session = await apiJson(page, "/v1/sessions", {
      method: "POST",
      body: {
        agent: agent.id,
        environment_id: env.id,
        title: "Web Search UI Demo",
      },
    });

    await page.goto(`/sessions/${session.id}`);
    await expect(page.getByText("Web Search UI Demo")).toBeVisible({
      timeout: 15_000,
    });

    const input = page.getByPlaceholder(/Send a message/);
    await input.fill(
      "Use web_search to search for Python programming language. Reply with only the domain name.",
    );
    await page.locator("form button[type='submit']").click();

    await expect(page.getByText("web_search", { exact: true }).first()).toBeVisible({
      timeout: 120_000,
    });

    const toolCard = page
      .locator("[data-slot='collapsible']")
      .filter({ hasText: "web_search" })
      .first();
    await expect(toolCard.getByText("Completed").or(toolCard.getByText("Running"))).toBeVisible();

    await toolCard.click();
    await expect(toolCard.getByText(/query/i)).toBeVisible();

    await expect(toolCard.getByText("Completed")).toBeVisible({ timeout: 120_000 });

    await apiJson(page, `/v1/agents/${agent.id}`, { method: "DELETE" });
    await apiJson(page, `/v1/environments/${env.id}`, { method: "DELETE" });
  });
});
