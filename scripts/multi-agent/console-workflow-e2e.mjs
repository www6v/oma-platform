/**
 * Console workflow Quickstart E2E — describe → generate → execute → trace.
 *
 * Requires oma-server + harness with pipy-dynamic-workflows mounted.
 * See ../workflows/smoke-workflow-console-e2e.sh for stack bootstrap.
 */
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const base =
  process.env.CONSOLE_URL ||
  process.env.PLATFORM_URL ||
  'http://127.0.0.1:8796';
const headed = process.env.WORKFLOW_E2E_HEADED === '1';
const timeoutMs = Number(process.env.WORKFLOW_E2E_TIMEOUT_MS || '90000');

const steps = [];
const pageErrors = [];

function log(step, ok, detail = '') {
  steps.push({ step, ok, detail });
  const mark = ok ? 'OK' : 'FAIL';
  console.log(`[${mark}] ${step}${detail ? `: ${detail}` : ''}`);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForHealth(url) {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${url}/health`);
      if (res.ok) {
        return;
      }
    } catch {
      // retry
    }
    await sleep(500);
  }
  throw new Error(`health check failed for ${url}`);
}

const browser = await chromium.launch({
  headless: !headed,
  slowMo: headed ? 80 : 0,
});
const context = await browser.newContext();
const page = await context.newPage();

page.on('pageerror', (e) => pageErrors.push(`pageerror:${e.message}`));
page.on('console', (msg) => {
  if (msg.type() === 'error') {
    pageErrors.push(`console:${msg.text()}`);
  }
});

try {
  log('preflight platform health', true, base);
  await waitForHealth(base);

  const proxyHealth = await fetch(`${base}/api/workflows/health`);
  log(
    'workflow proxy health',
    proxyHealth.ok,
    `${proxyHealth.status}`,
  );
  if (!proxyHealth.ok) {
    throw new Error(`workflow proxy health ${proxyHealth.status}`);
  }

  await page.goto(`${base}/workflows`, {
    waitUntil: 'domcontentloaded',
    timeout: 60000,
  });
  await page.getByRole('heading', { name: 'Workflow Quickstart' }).waitFor({
    state: 'visible',
    timeout: 15000,
  });
  log('open quickstart page', page.url().includes('/workflows'), page.url());

  await page.getByText('deep_research', { exact: false }).first().waitFor({
    state: 'visible',
    timeout: 15000,
  });
  log('templates loaded', true);

  const prompt =
    process.env.WORKFLOW_E2E_PROMPT ||
    'Search for LLM papers and summarize the top findings';
  await page.getByPlaceholder(/Describe your workflow/i).fill(prompt);
  await page.getByRole('button', { name: 'Generate YAML' }).click();

  await page.locator('.quickstart-yaml').waitFor({ state: 'visible', timeout: 20000 });
  const yamlText = await page.locator('.quickstart-yaml').inputValue();
  const hasYaml = yamlText.includes('name:') && yamlText.includes('steps:');
  log('generate YAML preview', hasYaml, yamlText.slice(0, 80));

  await page.getByRole('button', { name: 'Run Workflow' }).click();

  await page.waitForURL(/\/workflows\/[^/]+\/traces\/[^/]+/, {
    timeout: timeoutMs,
  });
  log('navigated to trace viewer', true, page.url());

  await page.getByRole('heading', { name: 'Execution Trace' }).waitFor({
    state: 'visible',
    timeout: 20000,
  }).catch(async () => {
    await page.locator('.trace-viewer-page, .traces-timeline').first().waitFor({
      state: 'visible',
      timeout: 20000,
    });
  });
  log('trace viewer rendered', true);

  const shotDir = process.env.WORKFLOW_E2E_SCREENSHOT_DIR || '/tmp';
  const shotPath = path.join(shotDir, `workflow-quickstart-e2e-${Date.now()}.png`);
  await page.screenshot({ path: shotPath, fullPage: true });
  log('screenshot saved', fs.existsSync(shotPath), shotPath);

  if (headed) {
    console.log(`\nQuickstart URL: ${base}/workflows`);
    console.log('Headed mode — browser stays open for 15s…');
    await page.waitForTimeout(15000);
  }
} catch (err) {
  log('workflow quickstart flow', false, err.message);
  try {
    const failShot = `/tmp/workflow-quickstart-e2e-fail-${Date.now()}.png`;
    await page.screenshot({ path: failShot, fullPage: true });
    log('failure screenshot', true, failShot);
  } catch {
    // ignore
  }
}

await browser.close();

const hardPageErrors = pageErrors.filter(
  (e) =>
    !e.includes('favicon') &&
    !e.includes('ResizeObserver') &&
    !e.includes('Failed to fetch'),
);
const failedSteps = steps.filter((s) => !s.ok);

const report = {
  ok: failedSteps.length === 0 && hardPageErrors.length === 0,
  steps,
  pageErrors: hardPageErrors,
};

console.log(JSON.stringify(report, null, 2));
process.exit(report.ok ? 0 : 1);
