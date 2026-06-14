import { chromium } from 'playwright';

const base = process.env.CONSOLE_URL || 'http://127.0.0.1:8787';
const apiKey = process.env.OMA_API_KEY || 'dev-key';
const errors = [];

async function apiPost(path, body) {
  const res = await fetch(`${base}${path}`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      authorization: `Bearer ${apiKey}`,
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`POST ${path} -> ${res.status}`);
  }
  return res.json();
}

async function apiDelete(path) {
  const res = await fetch(`${base}${path}`, {
    method: 'DELETE',
    headers: { authorization: `Bearer ${apiKey}` },
  });
  if (!res.ok) {
    throw new Error(`DELETE ${path} -> ${res.status}`);
  }
}

const headlessShell =
  process.env.PW_EXECUTABLE_PATH ||
  `${process.env.HOME}/Library/Caches/ms-playwright/chromium_headless_shell-1208/chrome-headless-shell-mac-x64/chrome-headless-shell`;
const browser = await chromium.launch({
  headless: true,
  executablePath: headlessShell,
});
const page = await browser.newPage();
page.on('pageerror', (e) => errors.push(`pageerror:${e.message}`));
page.on('console', (msg) => {
  if (msg.type() === 'error') {
    errors.push(`console:${msg.text()}`);
  }
});

const routes = ['/agents', '/sessions', '/skills'];
const results = {};

for (const route of routes) {
  await page.goto(`${base}${route}`, {
    waitUntil: 'networkidle',
    timeout: 45000,
  });
  await page.waitForTimeout(1500);
  const bodyText = await page.locator('body').innerText();
  results[route] = {
    title: await page.title(),
    hasFatalError: /Error:\s|Failed to load|Network error/i.test(bodyText),
    snippet: bodyText.replace(/\s+/g, ' ').slice(0, 200),
  };
}

let agentId;
try {
  const agent = await apiPost('/v1/agents', {
    name: 'ui-check-agent',
    model: 'claude-sonnet-4-6',
    system: 'ui check',
  });
  agentId = agent.id;
  await page.goto(`${base}/agents/${agentId}`, {
    waitUntil: 'networkidle',
    timeout: 45000,
  });
  await page.waitForTimeout(1500);
  const detailText = await page.locator('body').innerText();
  results['/agents/:id'] = {
    hasFatalError: /Error:\s|Failed to load|Network error/i.test(detailText),
    showsName: detailText.includes('ui-check-agent'),
    snippet: detailText.replace(/\s+/g, ' ').slice(0, 200),
  };
} finally {
  if (agentId) {
    await apiDelete(`/v1/agents/${agentId}`).catch(() => {});
  }
}

await browser.close();

const failed = Object.entries(results).filter(([, v]) => v.hasFatalError);
if (failed.length || errors.some((e) => !e.includes('favicon'))) {
  console.error(JSON.stringify({ results, errors }, null, 2));
  process.exit(1);
}
console.log(JSON.stringify({ ok: true, results, errors }, null, 2));
