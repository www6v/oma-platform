import {
  createAgent,
  createSession,
  deleteAgent,
  getOrCreateEnvironment,
  sendAndWait,
} from "../../test/eval/client.js";

async function main() {
  const envId = await getOrCreateEnvironment();
  console.log("env:", envId);

  const agentId = await createAgent({
    name: `web-search-smoke-${Date.now()}`,
    model: process.env.SMOKE_MODEL || "claude-sonnet-4-6",
    system:
      "Call web_search for every lookup. Do not answer from memory. " +
      "After results return, reply in one short sentence.",
    tools: [
      {
        type: "agent_toolset_20260401",
        default_config: { enabled: false },
        configs: [
          { name: "web_search", enabled: true },
          { name: "read", enabled: true },
        ],
      },
    ],
  });
  console.log("agent:", agentId);

  const sessionId = await createSession(agentId, envId);
  console.log("session:", sessionId);
const consoleUrl =
  process.env.CONSOLE_URL ||
  process.env.PLATFORM_URL ||
  process.env.OMA_API_URL ||
  "http://localhost:5173";
  console.log(`console: ${consoleUrl}/sessions/${sessionId}`);

  console.log("sending...");
  const events = await sendAndWait(
    sessionId,
    "Use web_search to find the official Python programming language website. " +
      "Reply with only the domain name.",
    240_000,
  );

  const searchUses = events.filter(
    (e: { type?: string; name?: string }) =>
      (e.type === "agent.tool_use" || e.type === "agent.custom_tool_use") &&
      e.name === "web_search",
  );
  const results = events.filter(
    (e: { type?: string }) => e.type === "agent.tool_result",
  );
  const idle = events.some((e: { type?: string }) => e.type === "session.status_idle");
  const errored = events.some((e: { type?: string }) => e.type === "session.error");

  console.log("");
  console.log("=== RESULTS ===");
  console.log("web_search calls:", searchUses.length);
  console.log("tool_result events:", results.length);
  console.log("session.status_idle:", idle);
  console.log("session.error:", errored);

  if (searchUses.length === 0 || !idle || errored) {
    console.error("web_search smoke FAILED");
    process.exit(1);
  }

  for (const e of results) {
    const c = (e as { content?: unknown }).content;
    const text =
      typeof c === "string"
        ? c
        : Array.isArray(c)
          ? c.map((b: { text?: string }) => b.text || "").join("")
          : "";
    if (text.includes("python.org") || text.includes("url")) {
      console.log("tool result snippet:", text.slice(0, 400));
    }
  }

  console.log("(keeping session for inspection)");
  await deleteAgent(agentId).catch(() => {});
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
