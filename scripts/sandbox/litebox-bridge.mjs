#!/usr/bin/env node
/**
 * LiteBox (BoxLite) JSON-RPC bridge for meta-harness Go executor.
 * Mirrors open-managed-agents/packages/sandbox/src/adapters/litebox.ts.
 *
 * Protocol: one JSON object per line on stdin; one JSON response per line on stdout.
 *   {"id":1,"op":"init","image":"node:22-slim","volumes":[...]}
 *   {"id":2,"op":"exec","command":"echo hi","timeoutMs":5000}
 *   {"id":3,"op":"readFile","path":"/workspace/foo"}
 *   {"id":4,"op":"destroy"}
 */

import { createInterface } from "node:readline";
import { promises as fs, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { randomBytes } from "node:crypto";

let box = null;
let boxPromise = null;
let volumes = [];
let image = "node:22-slim";
let memoryMib;
let cpus;
let name;
const tmpRoot = join(tmpdir(), `oma-litebox-${randomBytes(6).toString("hex")}`);
mkdirSync(tmpRoot, { recursive: true });

const respond = (id, payload) => {
  process.stdout.write(JSON.stringify({ id, ...payload }) + "\n");
};

const normalise = (p) => {
  if (p.startsWith("/")) return p;
  return `/workspace/${p}`;
};

async function ensureBox() {
  if (boxPromise) return boxPromise;
  boxPromise = (async () => {
    const mod = await import("@boxlite-ai/boxlite");
    const SimpleBox = mod.SimpleBox;
    if (!SimpleBox) {
      throw new Error("SimpleBox export missing from @boxlite-ai/boxlite");
    }
    const instance = new SimpleBox({
      image,
      memoryMib,
      cpus,
      name,
      volumes,
    });
    box = instance;
    return instance;
  })();
  return boxPromise;
}

async function handleExec(id, msg) {
  const b = await ensureBox();
  const timeoutMs = msg.timeoutMs ?? 120000;
  const r = await b.exec("sh", ["-c", msg.command], {}, {
    timeoutSecs: Math.max(1, Math.ceil(timeoutMs / 1000)),
  });
  const combined =
    (r.stdout + (r.stderr ? `\n${r.stderr}` : "")).replace(/\s+$/, "") +
    (r.exitCode !== 0 ? `\n[exit ${r.exitCode}]` : "");
  respond(id, { ok: true, output: combined });
}

async function handleReadFile(id, msg) {
  const b = await ensureBox();
  const tmp = join(tmpRoot, `read-${randomBytes(6).toString("hex")}`);
  try {
    await b.copyOut(normalise(msg.path), tmp);
    const data = await fs.readFile(tmp);
    respond(id, {
      ok: true,
      data: data.toString("base64"),
    });
  } finally {
    await fs.rm(tmp, { force: true }).catch(() => {});
  }
}

async function handleInit(id, msg) {
  if (boxPromise) {
    respond(id, { ok: false, error: "box already initialized" });
    return;
  }
  image = msg.image ?? image;
  memoryMib = msg.memoryMib;
  cpus = msg.cpus;
  name = msg.name;
  volumes = Array.isArray(msg.volumes) ? msg.volumes : [];
  respond(id, { ok: true });
}

async function handleDestroy(id) {
  if (boxPromise) {
    try {
      const b = await boxPromise;
      await b.stop();
    } catch {
      // best-effort
    }
    box = null;
    boxPromise = null;
  }
  await fs.rm(tmpRoot, { recursive: true, force: true }).catch(() => {});
  respond(id, { ok: true });
}

const rl = createInterface({ input: process.stdin });
rl.on("line", async (line) => {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch (err) {
    respond(null, { ok: false, error: `invalid json: ${err.message}` });
    return;
  }
  const id = msg.id ?? null;
  try {
    switch (msg.op) {
      case "init":
        await handleInit(id, msg);
        break;
      case "exec":
        await handleExec(id, msg);
        break;
      case "readFile":
        await handleReadFile(id, msg);
        break;
      case "destroy":
        await handleDestroy(id);
        break;
      default:
        respond(id, { ok: false, error: `unknown op: ${msg.op}` });
    }
  } catch (err) {
    respond(id, { ok: false, error: String(err?.message ?? err) });
  }
});

process.on("SIGTERM", async () => {
  await handleDestroy(0);
  process.exit(0);
});
