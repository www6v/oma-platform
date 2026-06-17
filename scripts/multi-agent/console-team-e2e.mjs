/**
 * Console team E2E — verifies Team tab (members, mailbox, shutdown).
 *
 * Requires oma-server with console dist (see smoke-team-console-e2e.sh).
 * Seeds team rows via seed-team-console-fixture.py, then asserts UI + API.
 */
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { spawnSync } from 'child_process';
import { fileURLToPath } from 'url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));

const base =
  process.env.CONSOLE_URL ||
  process.env.PLATFORM_URL ||
  process.env.OMA_API_URL ||
  'http://127.0.0.1:8794';
const apiKey = process.env.OMA_API_KEY || 'dev-key';
const dbPath =
  process.env.DATABASE_PATH ||
  process.env.OMA_DATABASE_PATH ||
  path.resolve(process.cwd(), '../../data/team-console-e2e.db');
const teamName = process.env.TEAM_E2E_TEAM_NAME || 'console-e2e-alpha';
const mailboxBody = process.env.TEAM_E2E_MAILBOX_BODY || 'TEAM-UI-MAILBOX-OK';
const workerName = process.env.TEAM_E2E_WORKER_NAME || 'coder';
const leadName = process.env.TEAM_E2E_LEAD_NAME || 'lead';
const headed = process.env.TEAM_E2E_HEADED === '1';
const timeoutMs = Number(process.env.TEAM_E2E_TIMEOUT_MS || '60000');
const rootDir = path.resolve(scriptDir, '../..');

const steps = [];
const pageErrors = [];
const apiErrors = [];

function log(step, ok, detail = '') {
  steps.push({ step, ok, detail });
  const mark = ok ? 'OK' : 'FAIL';
  console.log(`[${mark}] ${step}${detail ? `: ${detail}` : ''}`);
}

async function api(method, urlPath, body) {
  const res = await fetch(`${base}${urlPath}`, {
    method,
    headers: {
      authorization: `Bearer ${apiKey}`,
      'x-api-key': apiKey,
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
    throw new Error(`${method} ${urlPath} -> ${res.status} ${text.slice(0, 300)}`);
  }
  return parsed;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function seedTeamFixture(sessionId) {
  const env = {
    ...process.env,
    TEAM_E2E_SESSION_ID: sessionId,
    DATABASE_PATH: dbPath,
    OMA_DATABASE_PATH: dbPath,
  };
  const uv = spawnSync('uv', ['run', 'python', '../scripts/multi-agent/seed-team-console-fixture.py'], {
    cwd: path.join(rootDir, 'harness'),
    env,
    encoding: 'utf8',
  });
  if (uv.status === 0 && uv.stdout.trim()) {
    return JSON.parse(uv.stdout.trim());
  }
  const py = spawnSync('python3', [path.join(rootDir, 'scripts/multi-agent/seed-team-console-fixture.py')], {
    cwd: path.join(rootDir, 'harness'),
    env,
    encoding: 'utf8',
  });
  if (py.status !== 0) {
    const err = (uv.stderr || uv.stdout || py.stderr || py.stdout || '').trim();
    throw new Error(`seed-team-console-fixture failed: ${err}`);
  }
  return JSON.parse(py.stdout.trim());
}

let sessionId;
let fixture;

const headlessShell =
  process.env.PW_EXECUTABLE_PATH ||
  `${process.env.HOME}/Library/Caches/ms-playwright/chromium_headless_shell-1208/chrome-headless-shell-mac-x64/chrome-headless-shell`;

const browser = await chromium.launch({
  headless: !headed,
  slowMo: headed ? 80 : 0,
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
page.on('response', async (res) => {
  const url = res.url();
  if (!url.includes('/v1/')) return;
  if (res.status() >= 400) {
    let body = '';
    try {
      body = (await res.text()).slice(0, 200);
    } catch {
      body = '';
    }
    apiErrors.push(`${res.status()} ${url} ${body}`);
  }
});

try {
  log('preflight health', true, base);
  await api('GET', '/health');

  const agentId = (
    await api('POST', '/v1/agents', {
      name: 'team-console-e2e-lead',
      model: 'faux/test',
      system_prompt: 'Team lead for console E2E.',
      metadata: { enable_team_tools: true },
    })
  ).id;
  log('create agent', !!agentId, agentId);

  const envs = await api('GET', '/v1/environments?limit=5');
  const envId = envs.data?.[0]?.id || 'env-local-default';

  sessionId = (
    await api('POST', '/v1/sessions', {
      agent: agentId,
      environment_id: envId,
      title: 'team-console-e2e',
    })
  ).id;
  log('create session', !!sessionId, sessionId);

  fixture = seedTeamFixture(sessionId);
  log('seed team fixture', !!fixture?.team_id, fixture?.team_id);

  const teamsApi = await api('GET', `/v1/sessions/${sessionId}/teams`);
  const teamCount = teamsApi.data?.length ?? 0;
  log('api lists seeded team', teamCount === 1, String(teamCount));

  const sessionUrl = `${base}/sessions/${sessionId}`;
  await page.goto(sessionUrl, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.getByRole('tab', { name: 'Conversation' }).waitFor({
    state: 'visible',
    timeout: 15000,
  });
  await page.waitForTimeout(600);
  log('open session page', page.url().includes(sessionId), sessionUrl);

  const teamTab = page.getByRole('tab', { name: /^Team\b/ });
  await teamTab.click();
  await page.waitForTimeout(800);

  const bodyText = await page.locator('body').innerText();
  const teamVisible = bodyText.includes(teamName);
  log('team tab shows team name', teamVisible, teamName);

  const leadVisible = bodyText.includes(leadName);
  log('team tab shows lead member', leadVisible, leadName);

  const workerVisible = bodyText.includes(workerName);
  log('team tab shows worker member', workerVisible, workerName);

  const mailboxVisible = bodyText.includes(mailboxBody);
  log('team tab shows mailbox message', mailboxVisible, mailboxBody);

  const shutdownBtn = page.getByRole('button', { name: 'Shutdown' }).first();
  await shutdownBtn.waitFor({ state: 'visible', timeout: 10000 });
  await shutdownBtn.click();
  await page.waitForTimeout(1200);

  const afterShutdownText = await page.locator('body').innerText();
  const shutdownMsgVisible =
    afterShutdownText.includes('shutdown request') ||
    afterShutdownText.includes('Shutdown requested from Console');
  log('mailbox shows shutdown request', shutdownMsgVisible);

  const msgs = await api(
    'GET',
    `/v1/sessions/${sessionId}/teams/${fixture.team_id}/messages?limit=50`,
  );
  const shutdownRow = (msgs.data ?? []).some(
    (m) => m.message_type === 'shutdown_request',
  );
  log('api has shutdown_request message', shutdownRow);

  const shotDir = process.env.TEAM_E2E_SCREENSHOT_DIR || '/tmp';
  const shotPath = path.join(shotDir, `team-console-e2e-${sessionId}.png`);
  await page.screenshot({ path: shotPath, fullPage: true });
  log('screenshot saved', fs.existsSync(shotPath), shotPath);

  if (headed) {
    console.log(`\nSession URL (manual inspection): ${sessionUrl}`);
    console.log('Headed mode — browser stays open for 20s…');
    await page.waitForTimeout(Math.min(timeoutMs, 20000));
  }
} catch (err) {
  log('team console flow', false, err.message);
  try {
    const failShot = `/tmp/team-console-e2e-fail-${Date.now()}.png`;
    await page.screenshot({ path: failShot, fullPage: true });
    log('failure screenshot', true, failShot);
  } catch {
    // ignore screenshot errors
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
  sessionId,
  sessionUrl: sessionId ? `${base}/sessions/${sessionId}` : null,
  fixture,
  steps,
  apiErrors,
  pageErrors: hardPageErrors,
};

console.log(JSON.stringify(report, null, 2));
process.exit(report.ok ? 0 : 1);
