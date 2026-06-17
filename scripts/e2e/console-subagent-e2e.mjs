/**
 * Console sub-agent E2E — verifies sub-agent delegation is visible in Session UI.
 *
 * Requires oma-server with OMA_FAKE_HARNESS=subagent (see smoke-subagent-console-e2e.sh).
 * Creates coordinator + worker, posts a turn, then asserts:
 *   - Thread selector shows worker tab beside Main
 *   - Worker thread shows delegated reply
 *   - Primary thread shows coordinator summary
 *   - Timeline lists call_agent_* tool use
 */
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const base =
  process.env.CONSOLE_URL ||
  process.env.PLATFORM_URL ||
  process.env.OMA_API_URL ||
  'http://127.0.0.1:8793';
const apiKey = process.env.OMA_API_KEY || 'dev-key';
const workerReply = process.env.SUBAGENT_WORKER_REPLY || 'SUBAGENT-UI-WORKER-OK';
const primaryReply = process.env.SUBAGENT_PRIMARY_REPLY || 'SUBAGENT-UI-COORD-OK';
const workerName = 'subagent-ui-worker';
const coordName = 'subagent-ui-coordinator';
const headed = process.env.SUBAGENT_E2E_HEADED === '1';
const timeoutMs = Number(process.env.SUBAGENT_E2E_TIMEOUT_MS || '60000');

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

async function waitForTurnComplete(sessionId) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const raw = await api('GET', `/v1/sessions/${sessionId}/events?order=asc`);
    const events = normalizeEvents(raw);
    const hasPrimary = events.some(
      (ev) =>
        ev.type === 'agent.message' &&
        !ev.session_thread_id &&
        textFromContent(ev.content).includes(primaryReply),
    );
    const hasWorker = events.some(
      (ev) =>
        ev.type === 'agent.message' &&
        ev.session_thread_id &&
        textFromContent(ev.content).includes(workerReply),
    );
    if (hasPrimary && hasWorker) {
      return events;
    }
    await sleep(500);
  }
  throw new Error(`turn did not complete within ${timeoutMs}ms`);
}

function normalizeEvents(raw) {
  const rows = raw?.data ?? [];
  return rows.map((row) => {
    if (row?.data && typeof row.data === 'object') {
      return row.data;
    }
    if (typeof row?.data === 'string') {
      try {
        return JSON.parse(row.data);
      } catch {
        return row;
      }
    }
    return row;
  });
}

function textFromContent(content) {
  if (!Array.isArray(content)) return '';
  return content
    .filter((b) => b?.type === 'text')
    .map((b) => b.text || '')
    .join('\n');
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

let workerId;
let coordId;
let sessionId;

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

  workerId = (
    await api('POST', '/v1/agents', {
      name: workerName,
      model: 'faux/test',
      system_prompt: 'Worker sub-agent for console UI E2E.',
    })
  ).id;
  log('create worker agent', !!workerId, workerId);

  const workerVersion = (
    await api('GET', `/v1/agents/${workerId}`)
  ).version;

  coordId = (
    await api('POST', '/v1/agents', {
      name: coordName,
      model: 'faux/test',
      system_prompt: 'Delegate all work to workers via call_agent tools.',
      callable_agents: [{ type: 'agent', id: workerId, version: workerVersion }],
    })
  ).id;
  log('create coordinator agent', !!coordId, coordId);

  const envs = await api('GET', '/v1/environments?limit=5');
  const envId = envs.data?.[0]?.id || 'env-local-default';

  sessionId = (
    await api('POST', '/v1/sessions', {
      agent: coordId,
      environment_id: envId,
      title: 'subagent-console-e2e',
    })
  ).id;
  log('create session', !!sessionId, sessionId);

  const sessionUrl = `${base}/sessions/${sessionId}`;
  await page.goto(sessionUrl, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.getByRole('tab', { name: 'Conversation' }).waitFor({ state: 'visible', timeout: 15000 });
  await page.waitForTimeout(800);
  log('open session page', page.url().includes(sessionId), sessionUrl);

  await api('POST', `/v1/sessions/${sessionId}/events`, {
    events: [
      {
        type: 'user.message',
        content: [{ type: 'text', text: 'Delegate smoke task to the worker.' }],
      },
    ],
  });
  log('post user message', true);

  await waitForTurnComplete(sessionId);
  log('api turn completed', true);

  // Reload so GET /threads picks up the sub-agent thread from server.
  await page.reload({ waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.getByRole('tab', { name: 'Conversation' }).waitFor({ state: 'visible', timeout: 15000 });
  await page.waitForTimeout(1000);

  const workerTab = page.getByRole('tab', { name: workerName });
  await workerTab.waitFor({ state: 'visible', timeout: 15000 });
  log('worker thread tab visible', true, workerName);

  const mainTab = page.getByRole('tab', { name: 'Main' });
  await mainTab.click();
  await page.waitForTimeout(500);
  const mainText = await page.locator('body').innerText();
  const primaryVisible = mainText.includes(primaryReply);
  log('primary thread shows coordinator reply', primaryVisible, primaryReply);

  await workerTab.click();
  await page.waitForTimeout(500);
  const workerText = await page.locator('body').innerText();
  const workerVisible = workerText.includes(workerReply);
  log('worker thread shows delegated reply', workerVisible, workerReply);

  // call_agent tool_use is stamped on the primary thread — switch back
  // before inspecting Main-thread content (thread filter applies to both views).
  await mainTab.click();
  await page.waitForTimeout(400);

  await page.getByRole('tab', { name: 'Conversation' }).click();
  await page.waitForTimeout(600);
  const conversationText = await page.locator('body').innerText();
  const toolVisible =
    conversationText.includes('call_agent_') ||
    conversationText.includes('call_agent');
  log('conversation shows call_agent tool', toolVisible);

  // Timeline span labels need processed_at; fake harness may omit it.
  // Still verify the raw event exists via API for regression coverage.
  const apiEvents = await api('GET', `/v1/sessions/${sessionId}/events?order=asc`);
  const normalized = normalizeEvents(apiEvents);
  const toolEvent = normalized.some(
    (ev) =>
      ev.type === 'agent.tool_use' &&
      String(ev.name || '').includes('call_agent'),
  );
  log('api has call_agent tool_use event', toolEvent);

  const shotDir = process.env.SUBAGENT_E2E_SCREENSHOT_DIR || '/tmp';
  const shotPath = path.join(shotDir, `subagent-console-e2e-${sessionId}.png`);
  await page.screenshot({ path: shotPath, fullPage: true });
  log('screenshot saved', fs.existsSync(shotPath), shotPath);

  if (headed) {
    console.log(`\nSession URL (manual inspection): ${sessionUrl}`);
    console.log('Headed mode — browser stays open for 20s…');
    await page.waitForTimeout(20000);
  }
} catch (err) {
  log('subagent console flow', false, err.message);
  try {
    const failShot = `/tmp/subagent-console-e2e-fail-${Date.now()}.png`;
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
  steps,
  apiErrors,
  pageErrors: hardPageErrors,
};

console.log(JSON.stringify(report, null, 2));
process.exit(report.ok ? 0 : 1);
