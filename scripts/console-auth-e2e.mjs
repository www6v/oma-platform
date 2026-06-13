/**
 * Console auth E2E — signup + login via /login when AUTH_DISABLED=0.
 * Run via scripts/run-console-qa-auth.sh (ephemeral server on :8789).
 */
import { chromium } from 'playwright';
import fs from 'fs';

const base = process.env.CONSOLE_URL || 'http://127.0.0.1:8789';
const suffix = Date.now();
const email = process.env.QA_AUTH_EMAIL || `qa-auth-${suffix}@example.com`;
const password = process.env.QA_AUTH_PASSWORD || 'Qa-Test-Password-9!';
const name = process.env.QA_AUTH_NAME || 'QA Auth User';

const steps = [];
const pageErrors = [];

function log(step, ok, detail = '') {
  steps.push({ step, ok, detail });
  const mark = ok ? 'OK' : 'FAIL';
  console.log(`[${mark}] ${step}${detail ? `: ${detail}` : ''}`);
}

const headlessShell =
  process.env.PW_EXECUTABLE_PATH ||
  `${process.env.HOME}/Library/Caches/ms-playwright/chromium_headless_shell-1208/chrome-headless-shell-mac-x64/chrome-headless-shell`;

const browser = await chromium.launch({
  headless: true,
  executablePath: fs.existsSync(headlessShell) ? headlessShell : undefined,
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
  const authInfoRes = await fetch(`${base}/auth-info`);
  const authInfo = await authInfoRes.json();
  log('auth-info reachable', authInfoRes.ok, JSON.stringify(authInfo));

  await page.goto(`${base}/login`, { waitUntil: 'networkidle', timeout: 60000 });
  const loginBody = await page.locator('body').innerText();
  const hasLoginForm = loginBody.includes('Sign in to your workspace');
  log('login page renders', hasLoginForm);

  await page.getByRole('button', { name: 'Sign up' }).click();
  await page.waitForTimeout(400);

  await page.locator('#auth-name').fill(name);
  await page.locator('#auth-email').fill(email);
  await page.locator('#auth-password').fill(password);
  await page.locator('form button[type="submit"]').click();

  await page.waitForURL(
    (url) => !url.pathname.includes('/login'),
    { timeout: 30000 },
  );
  const authedUrl = page.url();
  const authed = !authedUrl.includes('/login');
  log('signup redirects away from login', authed, authedUrl);

  await context.clearCookies();
  await page.goto(`${base}/login`, { waitUntil: 'networkidle', timeout: 60000 });

  await page.locator('#auth-email').fill(email);
  await page.locator('#auth-password').fill(password);
  await page.locator('form button[type="submit"]').click();

  await page.waitForURL(
    (url) => !url.pathname.includes('/login'),
    { timeout: 30000 },
  );
  const reloginOk = !page.url().includes('/login');
  log('login with credentials', reloginOk, page.url());

  await page.goto(`${base}/agents`, { waitUntil: 'networkidle', timeout: 60000 });
  const agentsBody = await page.locator('body').innerText();
  const agentsOk = !/Error:\s|Failed to load|Network error/i.test(agentsBody);
  log('authenticated /agents', agentsOk);
} catch (err) {
  log('auth flow', false, err.message);
}

await browser.close();

const hardPageErrors = pageErrors.filter(
  (e) =>
    !e.includes('favicon') &&
    !e.includes('ResizeObserver') &&
    !e.includes('ERR_PROXY_CONNECTION_FAILED'),
);
const failedSteps = steps.filter((s) => !s.ok);
const report = {
  ok: failedSteps.length === 0 && hardPageErrors.length === 0,
  steps,
  pageErrors: hardPageErrors,
  email,
};

console.log(JSON.stringify(report, null, 2));
process.exit(report.ok ? 0 : 1);
