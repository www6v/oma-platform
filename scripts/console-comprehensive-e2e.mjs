/**
 * Comprehensive Console E2E — visits every OSS route and exercises core CRUD flows.
 * Requires oma-server (+ harness) on CONSOLE_URL (default http://127.0.0.1:8787).
 */
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));

const base = process.env.CONSOLE_URL || 'http://127.0.0.1:8787';
const apiKey = process.env.OMA_API_KEY || 'dev-key';
const reportDir =
  process.env.QA_REPORT_DIR ||
  path.join(process.cwd(), '..', '.gstack', 'qa-reports');
const screenshotDir = path.join(reportDir, 'screenshots');
const suffix = `qa-${Date.now()}`;

fs.mkdirSync(screenshotDir, { recursive: true });

const apiErrors = [];
const pageErrors = [];
const issues = [];
const steps = [];
let issueSeq = 0;

function log(step, ok, detail = '') {
  steps.push({ step, ok, detail });
  const mark = ok ? 'OK' : 'FAIL';
  console.log(`[${mark}] ${step}${detail ? `: ${detail}` : ''}`);
}

function recordIssue({ title, severity, category, route, detail }) {
  issueSeq += 1;
  issues.push({
    id: `ISSUE-${String(issueSeq).padStart(3, '0')}`,
    title,
    severity,
    category,
    route,
    detail,
  });
}

async function api(method, apiPath, body) {
  const res = await fetch(`${base}${apiPath}`, {
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
    throw new Error(`${method} ${apiPath} -> ${res.status} ${text.slice(0, 200)}`);
  }
  return parsed;
}

async function visitRoute(page, route, label) {
  const shot = path.join(
    screenshotDir,
    `${route.replace(/\//g, '_').replace(/^_/, '') || 'root'}.png`,
  );
  try {
    await page.goto(`${base}${route}`, {
      waitUntil: 'networkidle',
      timeout: 60000,
    });
    await page.waitForTimeout(1200);
    await page.screenshot({ path: shot, fullPage: true });
    const bodyText = await page.locator('body').innerText();
    const fatal = /Error:\s|Failed to load|Network error|Something went wrong/i.test(
      bodyText,
    );
    if (fatal) {
      recordIssue({
        title: `Fatal error on ${label}`,
        severity: 'high',
        category: 'functional',
        route,
        detail: bodyText.replace(/\s+/g, ' ').slice(0, 200),
      });
    }
    log(`visit ${route}`, !fatal, fatal ? bodyText.slice(0, 120) : '');
    return !fatal;
  } catch (err) {
    recordIssue({
      title: `Navigation failed: ${label}`,
      severity: 'critical',
      category: 'functional',
      route,
      detail: err.message,
    });
    log(`visit ${route}`, false, err.message);
    return false;
  }
}

const listRoutes = [
  ['/agents', 'Agents list'],
  ['/sessions', 'Sessions list'],
  ['/files', 'Files list'],
  ['/evals', 'Evals list'],
  ['/environments', 'Environments list'],
  ['/skills', 'Skills list'],
  ['/vaults', 'Vaults list'],
  ['/memory', 'Memory stores list'],
  ['/model-cards', 'Model cards list'],
  ['/api-keys', 'API keys list'],
  ['/runtimes', 'Runtimes list'],
  ['/integrations/linear/publish', 'Linear publish'],
  ['/integrations/linear/install-pat', 'Linear PAT install'],
  ['/integrations/github/bind', 'GitHub bind'],
  ['/integrations/slack/publish', 'Slack publish'],
];

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

let agentId;
let sessionId;
let evalId;
let fileId;
let skillId;
let envId;
const agentName = `qa-agent-${suffix}`;
const qaFileName = `qa-upload-${suffix}.txt`;
const qaSkillZip = path.join(scriptDir, 'fixtures', 'qa-skill.zip');

try {
  for (const [route, label] of listRoutes) {
    await visitRoute(page, route, label);
  }

  await page.goto(`${base}/agents`, { waitUntil: 'networkidle', timeout: 60000 });
  await page.getByRole('button', { name: '+ New agent' }).click();
  await page.getByRole('button', { name: 'Blank agent config' }).click();
  await page.locator('#agent-name').fill(agentName);
  await page.locator('#agent-description').fill('comprehensive qa agent');

  const modelCombo = page.getByRole('combobox').first();
  await modelCombo.click();
  await page.getByRole('option').first().click({ timeout: 15000 });

  await page.getByRole('button', { name: 'Create Agent' }).click();
  await page.waitForURL(/\/agents\//, { timeout: 30000 });
  agentId = page.url().split('/agents/')[1]?.split(/[?#]/)[0];
  await page.screenshot({
    path: path.join(screenshotDir, 'agent-detail.png'),
    fullPage: true,
  });
  log('create agent', !!agentId, agentId || 'no redirect');

  if (agentId) {
    await visitRoute(page, `/agents/${agentId}`, 'Agent detail');
  }

  await page.goto(`${base}/sessions`, { waitUntil: 'networkidle', timeout: 60000 });
  await page.getByRole('button', { name: '+ New session' }).click();
  await page.waitForSelector('text=New Session');

  const agentCombo = page.getByRole('combobox').first();
  await agentCombo.click();
  await page
    .getByRole('option', { name: new RegExp(agentName) })
    .click({ timeout: 15000 });

  await page.getByRole('button', { name: 'Create' }).click();
  await page.waitForURL(/\/sessions\//, { timeout: 30000 });
  sessionId = page.url().split('/sessions/')[1]?.split(/[?#]/)[0];
  await page.screenshot({
    path: path.join(screenshotDir, 'session-detail.png'),
    fullPage: true,
  });
  log('create session', !!sessionId, sessionId || 'no redirect');

  if (sessionId) {
    const textarea = page.getByPlaceholder('Send a message');
    await textarea.fill('Reply with exactly: qa-pong');
    await page
      .locator('form')
      .filter({ has: textarea })
      .locator('button[type="submit"]')
      .click();
    await page.waitForTimeout(5000);
    const chatText = await page.locator('body').innerText();
    const messageOk =
      chatText.includes('qa-pong') ||
      chatText.includes('queued') ||
      chatText.includes('Reply with exactly');
    if (!messageOk) {
      recordIssue({
        title: 'Session message not visible after send',
        severity: 'high',
        category: 'functional',
        route: `/sessions/${sessionId}`,
        detail: chatText.replace(/\s+/g, ' ').slice(0, 200),
      });
    }
    log('send session message', messageOk);

    const timelineTab = page.getByRole('tab', { name: 'Timeline' });
    if (await timelineTab.count()) {
      await timelineTab.click();
      await page.waitForTimeout(800);
      log('timeline tab', true);
    }

    const threadsTab = page.getByRole('tab', { name: /Threads/i });
    if (await threadsTab.count()) {
      await threadsTab.click();
      await page.waitForTimeout(800);
      log('threads tab', true);
    }

    const resourcesTab = page.getByRole('tab', { name: /Resources/i });
    if (await resourcesTab.count()) {
      await resourcesTab.click();
      await page.waitForTimeout(800);
      log('resources tab', true);
    }
  }

  try {
    const envPayload = await api('GET', '/v1/environments?limit=5');
    envId = envPayload?.data?.[0]?.id;
    log('resolve environment', !!envId, envId || 'none');
  } catch (err) {
    log('resolve environment', false, err.message);
  }

  if (agentId && envId) {
    try {
      const evalCreated = await api('POST', '/v1/evals/runs', {
        agent_id: agentId,
        environment_id: envId,
        tasks: [{ id: 'qa-task', messages: ['Reply with exactly: qa-eval'] }],
      });
      evalId = evalCreated?.run_id;
      log('create eval run (API)', !!evalId, evalId || 'no run_id');

      if (evalId) {
        await page.goto(`${base}/evals`, {
          waitUntil: 'networkidle',
          timeout: 60000,
        });
        await page.waitForTimeout(1200);
        const evalListText = await page.locator('body').innerText();
        const evalListed = evalListText.includes(evalId);
        if (!evalListed) {
          recordIssue({
            title: 'Eval run not visible on /evals list',
            severity: 'high',
            category: 'functional',
            route: '/evals',
            detail: `missing ${evalId}`,
          });
        }
        log('eval visible on list', evalListed, evalId);
        await visitRoute(page, `/evals/${evalId}`, 'Eval detail (created)');
      }
    } catch (err) {
      log('create eval run (API)', false, err.message);
    }
  } else {
    log('create eval run (API)', true, 'skipped (missing agent or environment)');
  }

  if (sessionId) {
    try {
      const uploaded = await api('POST', '/v1/files', {
        filename: qaFileName,
        content: 'console comprehensive qa upload',
        media_type: 'text/plain',
        scope_id: sessionId,
        downloadable: true,
      });
      fileId = uploaded?.id;
      log('upload file (API)', !!fileId, fileId || 'no id');

      if (fileId) {
        await page.goto(`${base}/files`, {
          waitUntil: 'networkidle',
          timeout: 60000,
        });
        await page.waitForTimeout(1200);
        const filesText = await page.locator('body').innerText();
        const fileListed = filesText.includes(qaFileName);
        if (!fileListed) {
          recordIssue({
            title: 'Uploaded file not visible on /files',
            severity: 'high',
            category: 'functional',
            route: '/files',
            detail: `missing ${qaFileName}`,
          });
        }
        log('file visible on list', fileListed, qaFileName);
        await page.screenshot({
          path: path.join(screenshotDir, 'files-after-upload.png'),
          fullPage: true,
        });
      }
    } catch (err) {
      log('upload file (API)', false, err.message);
    }
  }

  if (fs.existsSync(qaSkillZip)) {
    try {
      await page.goto(`${base}/skills`, {
        waitUntil: 'networkidle',
        timeout: 60000,
      });
      await page.getByRole('button', { name: '+ New skill' }).click();
      await page.waitForSelector('text=Upload Custom Skill');
      const fileInput = page.locator('input[type="file"]').first();
      await fileInput.setInputFiles(qaSkillZip);
      await page.getByRole('button', { name: 'Upload' }).click();
      await page.waitForTimeout(2500);

      const skillsPayload = await api('GET', '/v1/skills');
      const customSkill = (skillsPayload?.data || []).find(
        (s) =>
          s.source === 'custom' &&
          (s.name === 'qa-test-skill' ||
            s.display_title?.toLowerCase().includes('qa')),
      );
      skillId = customSkill?.id;
      const skillsBody = await page.locator('body').innerText();
      const skillListed =
        !!skillId ||
        skillsBody.toLowerCase().includes('qa-test-skill') ||
        skillsBody.toLowerCase().includes('qa test skill');
      if (!skillListed) {
        recordIssue({
          title: 'Uploaded skill not visible on /skills',
          severity: 'high',
          category: 'functional',
          route: '/skills',
          detail: 'custom skill missing after upload',
        });
      }
      log('upload skill (UI)', skillListed, skillId || 'name match');

      if (skillId) {
        await page.getByText(customSkill.display_title || customSkill.name).first().click();
        await page.waitForTimeout(800);
        const detailText = await page.locator('body').innerText();
        const detailOk = detailText.includes('SKILL.md') || detailText.includes('qa-test-skill');
        log('skill detail dialog', detailOk);
        await page.keyboard.press('Escape');
      }

      await page.screenshot({
        path: path.join(screenshotDir, 'skills-after-upload.png'),
        fullPage: true,
      });
    } catch (err) {
      log('upload skill (UI)', false, err.message);
    }
  } else {
    log('upload skill (UI)', true, `missing fixture ${qaSkillZip}`);
  }

  await page.goto(`${base}/evals`, { waitUntil: 'networkidle', timeout: 60000 });
  const newEvalBtn = page.getByRole('button', { name: /New eval|\+ New/i });
  if (await newEvalBtn.count()) {
    await newEvalBtn.first().click();
    await page.waitForTimeout(800);
    log('evals new modal', true);
    await page.keyboard.press('Escape');
  } else {
    log('evals new modal', true, 'no create button (API-only flow)');
  }

  if (!evalId) {
    try {
      const evalPayload = await api('GET', '/v1/evals/runs?limit=5');
      const firstEval = evalPayload?.data?.[0];
      if (firstEval?.id) {
        evalId = firstEval.id;
        await visitRoute(page, `/evals/${evalId}`, 'Eval detail (existing)');
      } else {
        log('eval detail fallback', true, 'no existing eval runs');
      }
    } catch (err) {
      log('eval detail fallback', false, err.message);
    }
  }

  try {
    const envPayload = await api('GET', '/v1/environments?limit=5');
    const firstEnv = envPayload?.data?.[0];
    if (firstEnv?.id) {
      await visitRoute(
        page,
        `/environments/${firstEnv.id}`,
        'Environment detail',
      );
    } else {
      log('environment detail', true, 'no environments');
    }
  } catch (err) {
    log('environment detail', false, err.message);
  }

  try {
    const vaultPayload = await api('GET', '/v1/vaults?limit=5');
    const firstVault = vaultPayload?.data?.[0];
    if (firstVault?.id) {
      await visitRoute(page, `/vaults/${firstVault.id}`, 'Vault detail');
    } else {
      log('vault detail', true, 'no vaults');
    }
  } catch (err) {
    log('vault detail', false, err.message);
  }

  try {
    const memPayload = await api('GET', '/v1/memory_stores?limit=5');
    const firstMem = memPayload?.data?.[0];
    if (firstMem?.id) {
      await visitRoute(page, `/memory/${firstMem.id}`, 'Memory store detail');
    } else {
      log('memory detail', true, 'no memory stores');
    }
  } catch (err) {
    log('memory detail', false, err.message);
  }
} catch (err) {
  log('comprehensive flow', false, err.message);
  recordIssue({
    title: 'Unhandled E2E flow error',
    severity: 'critical',
    category: 'functional',
    route: page.url(),
    detail: err.message,
  });
}

try {
  if (evalId) {
    await api('DELETE', `/v1/evals/runs/${evalId}`);
    log('cleanup eval', true);
  }
} catch (err) {
  log('cleanup eval', false, err.message);
}

try {
  if (fileId) {
    await api('DELETE', `/v1/files/${fileId}`);
    log('cleanup file', true);
  }
} catch (err) {
  log('cleanup file', false, err.message);
}

try {
  if (skillId) {
    await api('DELETE', `/v1/skills/${skillId}`);
    log('cleanup skill', true);
  }
} catch (err) {
  log('cleanup skill', false, err.message);
}

try {
  if (sessionId) {
    await api('DELETE', `/v1/sessions/${sessionId}`);
    log('cleanup session', true);
  }
} catch (err) {
  log('cleanup session', false, err.message);
}

try {
  if (agentId) {
    const purgeAgent = async () => {
      await api('DELETE', `/v1/agents/${agentId}`);
    };
    try {
      await purgeAgent();
      log('cleanup agent', true);
    } catch (err) {
      try {
        const sessList = await api('GET', `/v1/sessions?limit=100`);
        for (const s of sessList?.data || []) {
          if (s.agent?.id === agentId || s.agent_id === agentId) {
            await api('DELETE', `/v1/sessions/${s.id}`).catch(() => {});
          }
        }
        await purgeAgent();
        log('cleanup agent', true, 'after session purge');
      } catch (retryErr) {
        log('cleanup agent', false, retryErr.message);
      }
    }
  }
} catch (err) {
  log('cleanup agent', false, err.message);
}

await browser.close();

const hardPageErrors = pageErrors.filter(
  (e) =>
    !e.includes('favicon') &&
    !e.includes('ResizeObserver') &&
    !e.includes('Failed to fetch') &&
    !e.includes('ERR_PROXY_CONNECTION_FAILED'),
);
const failedSteps = steps.filter((s) => !s.ok);
const hardApiErrors = apiErrors.filter(
  (e) =>
    !e.includes('/integrations/') &&
    !e.includes('/404') &&
    !e.includes('/auth'),
);

const visitedOk = listRoutes.length - issues.filter((i) =>
  listRoutes.some(([r]) => i.route === r),
).length;
const healthScore = Math.max(
  0,
  Math.min(
    100,
    100 -
      issues.filter((i) => i.severity === 'critical').length * 25 -
      issues.filter((i) => i.severity === 'high').length * 15 -
      hardPageErrors.length * 5 -
      hardApiErrors.length * 3,
  ),
);

const report = {
  ok:
    failedSteps.length === 0 &&
    issues.length === 0 &&
    hardPageErrors.length === 0 &&
    hardApiErrors.length === 0,
  healthScore,
  steps,
  issues,
  apiErrors: hardApiErrors,
  pageErrors: hardPageErrors,
  routesVisited: listRoutes.length,
  routesOk: visitedOk,
};

const reportPath = path.join(
  reportDir,
  `qa-report-console-${new Date().toISOString().slice(0, 10)}.json`,
);
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));
console.log(`\nReport: ${reportPath}`);
console.log(JSON.stringify(report, null, 2));
process.exit(report.ok ? 0 : 1);
