/**
 * Console dogfood — walks core operator flows and fails on API/UI errors.
 * Requires oma-server + harness on CONSOLE_URL (default http://127.0.0.1:8787).
 */
import { chromium } from 'playwright';

const base = process.env.CONSOLE_URL || 'http://127.0.0.1:8787';
const apiKey = process.env.OMA_API_KEY || 'dev-key';
const suffix = `dogfood-${Date.now()}`;
const agentName = `dogfood-agent-${suffix}`;

const apiErrors = [];
const pageErrors = [];
const steps = [];

function log(step, ok, detail = '') {
  steps.push({ step, ok, detail });
  const mark = ok ? 'OK' : 'FAIL';
  console.log(`[${mark}] ${step}${detail ? `: ${detail}` : ''}`);
}

async function api(method, path, body) {
  const res = await fetch(`${base}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${apiKey}`,
      ...(body !== undefined ? { 'content-type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let parsed;
  try {
    parsed = text ? JSON.parse(text) : {};
  } catch {
    parsed = text;
  }
  if (!res.ok) {
    throw new Error(`${method} ${path} -> ${res.status} ${text.slice(0, 200)}`);
  }
  return parsed;
}

let agentId;
let sessionId;

const headlessShell =
  process.env.PW_EXECUTABLE_PATH ||
  `${process.env.HOME}/Library/Caches/ms-playwright/chromium_headless_shell-1208/chrome-headless-shell-mac-x64/chrome-headless-shell`;
const browser = await chromium.launch({
  headless: true,
  executablePath: headlessShell,
});
const context = await browser.newContext();
const page = await context.newPage();

page.on('pageerror', (e) => pageErrors.push(`pageerror:${e.message}`));
page.on('console', (msg) => {
  if (msg.type() === 'error') {
    pageErrors.push(`console:${msg.text()}`);
  }
});
page.on('response', async (res) => {
  const url = res.url();
  if (!url.includes('/v1/') && !url.includes('/auth')) {
    return;
  }
  if (res.status() >= 400) {
    let body = '';
    try {
      body = (await res.text()).slice(0, 300);
    } catch {
      body = '';
    }
    apiErrors.push(`${res.status()} ${res.request().method()} ${url} ${body}`);
  }
});

try {
  await page.goto(`${base}/agents`, { waitUntil: 'networkidle', timeout: 60000 });
  await page.waitForTimeout(1000);
  const agentsBody = await page.locator('body').innerText();
  const agentsFatal = /Error:\s|Failed to load|Network error/i.test(agentsBody);
  log('load /agents', !agentsFatal, agentsFatal ? agentsBody.slice(0, 120) : '');

  await page.getByRole('button', { name: '+ New agent' }).click();
  await page.getByRole('button', { name: 'Blank agent config' }).click();
  await page.locator('#agent-name').fill(agentName);
  await page.locator('#agent-description').fill('dogfood description');

  const modelCombo = page.getByRole('combobox').first();
  await modelCombo.click();
  await page.getByRole('option').first().click({ timeout: 15000 });

  await page.getByRole('button', { name: 'Create Agent' }).click();
  await page.waitForURL(/\/agents\//, { timeout: 30000 });
  agentId = page.url().split('/agents/')[1]?.split(/[?#]/)[0];
  await page.getByRole('heading', { name: agentName }).waitFor({ timeout: 15000 });
  log('create agent via UI', !!agentId, agentId || 'no redirect');

  await page.goto(`${base}/sessions`, { waitUntil: 'networkidle', timeout: 60000 });
  await page.getByRole('button', { name: '+ New session' }).click();
  await page.waitForSelector('text=New Session');

  const agentCombo = page.getByRole('combobox').first();
  await agentCombo.click();
  await page.getByRole('option', { name: new RegExp(agentName) }).click({ timeout: 15000 });

  await page.getByRole('button', { name: 'Create' }).click();
  await page.waitForURL(/\/sessions\//, { timeout: 30000 });
  sessionId = page.url().split('/sessions/')[1]?.split(/[?#]/)[0];
  log('create session via UI', !!sessionId, sessionId || 'no redirect');

  const textarea = page.getByPlaceholder('Send a message');
  await textarea.fill('Reply with exactly: pong');
  await page.locator('form').filter({ has: textarea }).locator('button[type="submit"]').click();

  await page.waitForTimeout(4000);
  const chatText = await page.locator('body').innerText();
  const messageVisible =
    chatText.includes('Reply with exactly: pong') ||
    chatText.includes('pong') ||
    chatText.includes('queued');
  log('send message', messageVisible, chatText.replace(/\s+/g, ' ').slice(0, 160));

  await page.getByRole('tab', { name: 'Timeline' }).click();
  await page.waitForTimeout(1000);
  log('timeline tab', true);

  const trajBtn = page.getByRole('button', { name: /Trajectory|View trajectory/i }).first();
  if (await trajBtn.count()) {
    await trajBtn.click();
    await page.waitForTimeout(800);
    log('trajectory modal', true);
    await page.keyboard.press('Escape');
  } else {
    log('trajectory modal', true, 'trigger not found (optional)');
  }
} catch (err) {
  log('dogfood flow', false, err.message);
}

try {
  if (sessionId) {
    await api('DELETE', `/v1/sessions/${sessionId}`);
  }
} catch (err) {
  log('cleanup session', false, err.message);
}

try {
  if (agentId) {
    await api('DELETE', `/v1/agents/${agentId}`);
  }
} catch (err) {
  log('cleanup agent', false, err.message);
}

await browser.close();

const hardPageErrors = pageErrors.filter(
  (e) =>
    !e.includes('favicon') &&
    !e.includes('ResizeObserver') &&
    !e.includes('Failed to fetch'),
);
const failedSteps = steps.filter((s) => !s.ok);
const hardApiErrors = apiErrors.filter(
  (e) => !e.includes('/integrations/') && !e.includes('/404'),
);

const report = {
  ok: failedSteps.length === 0 && hardPageErrors.length === 0 && hardApiErrors.length === 0,
  steps,
  apiErrors: hardApiErrors,
  pageErrors: hardPageErrors,
};

console.log(JSON.stringify(report, null, 2));
process.exit(report.ok ? 0 : 1);
