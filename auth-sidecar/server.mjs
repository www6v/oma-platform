import { createServer } from "node:http";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { randomBytes } from "node:crypto";
import Database from "better-sqlite3";
import { betterAuth } from "better-auth";

const listenAddr = process.env.AUTH_LISTEN_ADDR ?? "127.0.0.1:8788";
const authDbPath = process.env.AUTH_DATABASE_PATH ?? "./data/auth.db";
const omaDbPath = process.env.OMA_DATABASE_PATH ?? "./data/oma.db";
const secret =
  process.env.BETTER_AUTH_SECRET ??
  randomBytes(32).toString("hex");
const baseURL = process.env.PUBLIC_BASE_URL ?? "http://127.0.0.1:8787";
const googleClientId = process.env.GOOGLE_CLIENT_ID ?? "";
const googleClientSecret = process.env.GOOGLE_CLIENT_SECRET ?? "";

mkdirSync(dirname(authDbPath), { recursive: true });
mkdirSync(dirname(omaDbPath), { recursive: true });

const authDb = new Database(authDbPath);
applyBetterAuthSchema(authDb);

const omaDb = new Database(omaDbPath);
ensureOmaTenantSchema(omaDb);

const socialProviders = {};
if (googleClientId && googleClientSecret) {
  socialProviders.google = {
    clientId: googleClientId,
    clientSecret: googleClientSecret,
  };
}

const auth = betterAuth({
  basePath: "/auth",
  secret,
  baseURL,
  database: authDb,
  emailAndPassword: { enabled: true },
  socialProviders,
  trustedOrigins: [baseURL],
  user: {
    additionalFields: {
      tenantId: { type: "string", required: false },
      role: { type: "string", required: false, defaultValue: "member" },
    },
  },
  databaseHooks: {
    user: {
      create: {
        after: async (user) => {
          try {
            ensureTenant(omaDb, user.id, user.name, user.email);
          } catch (err) {
            console.error("[auth-sidecar] ensureTenant failed:", err);
          }
        },
      },
    },
  },
});

const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url ?? "/", `http://${req.headers.host ?? "localhost"}`);
    if (!url.pathname.startsWith("/auth")) {
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "not found" }));
      return;
    }
    const headers = new Headers();
    for (const [key, value] of Object.entries(req.headers)) {
      if (value === undefined) continue;
      if (Array.isArray(value)) {
        for (const part of value) headers.append(key, part);
      } else {
        headers.set(key, value);
      }
    }
    const body =
      req.method === "GET" || req.method === "HEAD"
        ? undefined
        : await readBody(req);
    const response = await auth.handler(
      new Request(url.toString(), {
        method: req.method,
        headers,
        body,
      }),
    );
    res.writeHead(response.status, Object.fromEntries(response.headers.entries()));
    const text = await response.text();
    res.end(text);
  } catch (err) {
    console.error("[auth-sidecar] handler error:", err);
    res.writeHead(500, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "internal error" }));
  }
});

server.listen(
  Number(listenAddr.split(":").pop()),
  listenAddr.split(":")[0],
  () => {
    console.log(`[auth-sidecar] listening on http://${listenAddr}`);
    console.log(`[auth-sidecar] auth db: ${authDbPath}`);
    console.log(`[auth-sidecar] oma db: ${omaDbPath}`);
    if (!process.env.BETTER_AUTH_SECRET) {
      console.warn(
        "[auth-sidecar] BETTER_AUTH_SECRET unset — using ephemeral secret",
      );
    }
  },
);

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (chunk) => chunks.push(chunk));
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}

function applyBetterAuthSchema(db) {
  db.exec(`
    CREATE TABLE IF NOT EXISTS "user" (
      "id" TEXT PRIMARY KEY NOT NULL,
      "email" TEXT NOT NULL UNIQUE,
      "emailVerified" INTEGER NOT NULL DEFAULT 0,
      "name" TEXT NOT NULL,
      "image" TEXT,
      "tenantId" TEXT,
      "role" TEXT,
      "createdAt" INTEGER NOT NULL,
      "updatedAt" INTEGER NOT NULL
    );
    CREATE TABLE IF NOT EXISTS "session" (
      "id" TEXT PRIMARY KEY NOT NULL,
      "userId" TEXT NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
      "token" TEXT NOT NULL UNIQUE,
      "expiresAt" INTEGER NOT NULL,
      "ipAddress" TEXT,
      "userAgent" TEXT,
      "createdAt" INTEGER NOT NULL,
      "updatedAt" INTEGER NOT NULL
    );
    CREATE TABLE IF NOT EXISTS "account" (
      "id" TEXT PRIMARY KEY NOT NULL,
      "userId" TEXT NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
      "accountId" TEXT NOT NULL,
      "providerId" TEXT NOT NULL,
      "accessToken" TEXT,
      "refreshToken" TEXT,
      "idToken" TEXT,
      "accessTokenExpiresAt" INTEGER,
      "refreshTokenExpiresAt" INTEGER,
      "scope" TEXT,
      "password" TEXT,
      "createdAt" INTEGER NOT NULL,
      "updatedAt" INTEGER NOT NULL
    );
    CREATE TABLE IF NOT EXISTS "verification" (
      "id" TEXT PRIMARY KEY NOT NULL,
      "identifier" TEXT NOT NULL,
      "value" TEXT NOT NULL,
      "expiresAt" INTEGER NOT NULL,
      "createdAt" INTEGER,
      "updatedAt" INTEGER
    );
  `);
}

function ensureOmaTenantSchema(db) {
  db.exec(`
    CREATE TABLE IF NOT EXISTS tenant (
      id TEXT PRIMARY KEY NOT NULL,
      name TEXT NOT NULL,
      "createdAt" INTEGER NOT NULL,
      "updatedAt" INTEGER NOT NULL
    );
    CREATE TABLE IF NOT EXISTS membership (
      user_id TEXT NOT NULL,
      tenant_id TEXT NOT NULL,
      role TEXT NOT NULL DEFAULT 'member',
      created_at INTEGER NOT NULL,
      PRIMARY KEY (user_id, tenant_id)
    );
    CREATE INDEX IF NOT EXISTS idx_membership_user ON membership (user_id);
    INSERT OR IGNORE INTO tenant (id, name, "createdAt", "updatedAt")
    VALUES ('default', 'Default', 0, 0);
  `);
}

function ensureTenant(db, userId, userName, userEmail) {
  const existing = db
    .prepare(
      `SELECT tenant_id FROM membership
       WHERE user_id = ?
       ORDER BY created_at ASC, tenant_id ASC
       LIMIT 1`,
    )
    .get(userId);
  if (existing?.tenant_id) return existing.tenant_id;

  const tenantId = `tn_${randomHex(16)}`;
  const now = Date.now();
  const trimmedName = (userName ?? "").trim();
  const emailPrefix = (userEmail ?? "").split("@")[0]?.trim() ?? "";
  const display = trimmedName || emailPrefix || "User";
  const tenantName = `${display}'s workspace`;

  db.prepare(
    `INSERT INTO tenant (id, name, "createdAt", "updatedAt") VALUES (?, ?, ?, ?)`,
  ).run(tenantId, tenantName, now, now);
  db.prepare(
    `INSERT INTO membership (user_id, tenant_id, role, created_at)
     VALUES (?, ?, 'owner', ?)
     ON CONFLICT (user_id, tenant_id) DO NOTHING`,
  ).run(userId, tenantId, now);

  const final = db
    .prepare(
      `SELECT tenant_id FROM membership
       WHERE user_id = ?
       ORDER BY created_at ASC, tenant_id ASC
       LIMIT 1`,
    )
    .get(userId);
  return final?.tenant_id ?? tenantId;
}

function randomHex(bytes) {
  return randomBytes(bytes).toString("hex");
}
