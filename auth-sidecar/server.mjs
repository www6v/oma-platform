import { createServer } from "node:http";
import { randomBytes } from "node:crypto";
import { createPool } from "mysql2/promise";
import { betterAuth } from "better-auth";

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const listenAddr = process.env.AUTH_LISTEN_ADDR ?? "127.0.0.1:8788";
const secret =
  process.env.BETTER_AUTH_SECRET ??
  randomBytes(32).toString("hex");
const baseURL = process.env.PUBLIC_BASE_URL ?? "http://127.0.0.1:8787";

// Browser Origin may use either 127.0.0.1 or localhost interchangeably,
// but Better Auth matches origins as exact strings. Trust both variants
// (and any explicit PUBLIC_BASE_URL) so login works regardless of which
// hostname the user typed in the address bar.
const trustedOrigins = Array.from(
  new Set([
    baseURL,
    "http://127.0.0.1:8787",
    "http://localhost:8787",
    ...(process.env.TRUSTED_ORIGINS
      ? process.env.TRUSTED_ORIGINS.split(",").map((s) => s.trim()).filter(Boolean)
      : []),
  ]),
);

const googleClientId = process.env.GOOGLE_CLIENT_ID ?? "";
const googleClientSecret = process.env.GOOGLE_CLIENT_SECRET ?? "";

// ---------------------------------------------------------------------------
// MySQL — single pool shared by Better Auth and the tenant helper.
// ---------------------------------------------------------------------------
//
// DATABASE_URL accepts either a Go-style DSN or a URL:
//   mysql+aiomysql://user:pass@host:port/db
//   mysql://user:pass@host:port/db
//   user:pass@tcp(host:port)/db
//
const databaseUrl =
  process.env.DATABASE_URL ??
  "mysql+aiomysql://managed:managedAgent123@124.221.28.203:3306/managed_agent";

function parseMySQLUrl(raw) {
  // Already a Go-style DSN: user:pass@tcp(host:port)/db
  if (/@tcp\(/.test(raw)) {
    const m = raw.match(
      /^([^:]+):([^@]*)@tcp\(([^):]+):(\d+)\)\/([^?]+)(\?.*)?$/,
    );
    if (!m) throw new Error(`cannot parse mysql DSN: ${raw}`);
    return {
      host: m[3],
      port: Number(m[4]),
      user: m[1],
      password: m[2],
      database: m[5],
    };
  }
  // URL form: mysql[+driver]://user:pass@host:port/db
  let s = raw;
  for (const prefix of [
    "mysql+aiomysql://",
    "mysql+mysqlconnector://",
    "mysql://",
  ]) {
    if (s.startsWith(prefix)) {
      s = "mysql://" + s.slice(prefix.length);
      break;
    }
  }
  const u = new URL(s);
  return {
    host: u.hostname,
    port: Number(u.port) || 3306,
    user: decodeURIComponent(u.username),
    password: decodeURIComponent(u.password),
    database: u.pathname.replace(/^\/+/, ""),
  };
}

const mysqlConfig = {
  ...parseMySQLUrl(databaseUrl),
  waitForConnections: true,
  charset: "utf8mb4",
};
const pool = createPool(mysqlConfig);

console.log(
  `[auth-sidecar] mysql: ${mysqlConfig.user}@${mysqlConfig.host}:${mysqlConfig.port}/${mysqlConfig.database}`,
);

// ---------------------------------------------------------------------------
// Better Auth — pass the mysql2 pool directly; better-auth auto-detects it
// and runs its own migrations for user/session/account/verification.
// ---------------------------------------------------------------------------

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
  database: pool,
  emailAndPassword: { enabled: true },
  socialProviders,
  trustedOrigins,
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
            await ensureTenant(user.id, user.name, user.email);
          } catch (err) {
            console.error("[auth-sidecar] ensureTenant failed:", err);
          }
        },
      },
    },
  },
});

// ---------------------------------------------------------------------------
// HTTP handler — unchanged from before.
// ---------------------------------------------------------------------------

const server = createServer(async (req, res) => {
  try {
    const url = new URL(
      req.url ?? "/",
      `http://${req.headers.host ?? "localhost"}`,
    );
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
    res.writeHead(
      response.status,
      Object.fromEntries(response.headers.entries()),
    );
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

// ---------------------------------------------------------------------------
// Tenant helper — runs against the same MySQL pool. tenant/membership are
// owned by the platform schema (see scripts/sql/platform_mysql.sql) so we
// do NOT create them here; just read/write.
// ---------------------------------------------------------------------------

async function ensureTenant(userId, userName, userEmail) {
  const [existing] = await pool.execute(
    `SELECT tenant_id FROM membership
     WHERE user_id = ?
     ORDER BY created_at ASC, tenant_id ASC
     LIMIT 1`,
    [userId],
  );
  if (existing?.length && existing[0].tenant_id) {
    return existing[0].tenant_id;
  }

  const tenantId = `tn_${randomHex(16)}`;
  const now = Date.now();
  const trimmedName = (userName ?? "").trim();
  const emailPrefix = (userEmail ?? "").split("@")[0]?.trim() ?? "";
  const display = trimmedName || emailPrefix || "User";
  const tenantName = `${display}'s workspace`;

  // tenant table is owned by the platform; INSERT IGNORE tolerates a race
  // where the platform already created the same tenant id.
  await pool.execute(
    `INSERT IGNORE INTO tenant (id, name, createdAt, updatedAt)
     VALUES (?, ?, ?, ?)`,
    [tenantId, tenantName, now, now],
  );
  // membership has a composite PK; INSERT IGNORE tolerates the same race.
  await pool.execute(
    `INSERT IGNORE INTO membership (user_id, tenant_id, \`role\`, created_at)
     VALUES (?, ?, 'owner', ?)`,
    [userId, tenantId, now],
  );

  const [final] = await pool.execute(
    `SELECT tenant_id FROM membership
     WHERE user_id = ?
     ORDER BY created_at ASC, tenant_id ASC
     LIMIT 1`,
    [userId],
  );
  return final?.[0]?.tenant_id ?? tenantId;
}

function randomHex(bytes) {
  return randomBytes(bytes).toString("hex");
}
