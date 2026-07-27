import { useMemo, useState } from "react";
import { useParams, Link, useNavigate } from "react-router";
import { useApi } from "../lib/api";
import { useApiQuery } from "../lib/useApiQuery";
import { GitHubIcon, LinearIcon, SlackIcon } from "../components/icons";
import { Page } from "../components/Page";
import { PageHeader } from "../components/PageHeader";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { AgentRecord as Agent } from "../types/agent";
import { AgentSessionsPanel } from "./agents/AgentSessionsPanel";

/** Shared publication shape across Linear / GitHub / Slack — they all
 *  expose the same id / status / mode / persona / workspace_name fields. */
interface Pub {
  id: string;
  status: string;
  mode: string;
  persona: { name: string; avatarUrl: string | null };
  workspace_name: string | null;
}

type TabKey = "configuration" | "sessions";

function formatRelativeTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  if (Number.isNaN(ms)) return "—";
  const sec = Math.round(ms / 1000);
  if (sec < 60) return "just now";
  const min = Math.round(sec / 60);
  if (min < 60) return `${min} minute${min === 1 ? "" : "s"} ago`;
  const hr = Math.round(min / 60);
  if (hr < 48) return `${hr} hour${hr === 1 ? "" : "s"} ago`;
  const day = Math.round(hr / 24);
  if (day < 30) return `${day} day${day === 1 ? "" : "s"} ago`;
  return new Date(iso).toLocaleDateString();
}

export function AgentDetail() {
  const { id } = useParams();
  const { api } = useApi();
  const nav = useNavigate();
  const [tab, setTab] = useState<TabKey>("configuration");

  // Single-resource fetches via TQ. `enabled: !!id` defers until the route
  // param is available; the publication queries inherit the same gate.
  // Each query runs independently — failures on the publication endpoints
  // (404 / not-installed) don't block the agent detail render, same as
  // the previous behavior where each had its own .catch.
  const enabled = !!id;
  const { data: agent, error: agentError } = useApiQuery<Agent>(
    id ? `/v1/agents/${id}` : null,
    undefined,
    { enabled },
  );
  const { data: versionsRes } = useApiQuery<{ data: Agent[] }>(
    id ? `/v1/agents/${id}/versions` : null,
    undefined,
    { enabled },
  );
  // Reverse-lookup publications per provider. Each endpoint exists thanks
  // to the /linear/agents/:id/publications + /slack/agents/:id/publications
  // + /github/agents/:id/publications routes added on the main worker.
  const { data: linearRes } = useApiQuery<{ data: Pub[] }>(
    id ? `/v1/integrations/linear/agents/${id}/publications` : null,
    undefined,
    { enabled },
  );
  const { data: githubRes } = useApiQuery<{ data: Pub[] }>(
    id ? `/v1/integrations/github/agents/${id}/publications` : null,
    undefined,
    { enabled },
  );
  const { data: slackRes } = useApiQuery<{ data: Pub[] }>(
    id ? `/v1/integrations/slack/agents/${id}/publications` : null,
    undefined,
    { enabled },
  );

  const versions = versionsRes?.data ?? [];
  const versionNumbers = useMemo(
    () => versions.map((v) => v.version).sort((a, b) => b - a),
    [versions],
  );
  // Filter to live publications only — same predicate the old useEffect ran.
  const linearPubs = useMemo(
    () => (linearRes?.data ?? []).filter((p) => p.status === "live"),
    [linearRes],
  );
  const githubPubs = useMemo(
    () => (githubRes?.data ?? []).filter((p) => p.status === "live"),
    [githubRes],
  );
  const slackPubs = useMemo(
    () => (slackRes?.data ?? []).filter((p) => p.status === "live"),
    [slackRes],
  );

  const error = agentError instanceof Error ? agentError.message : agentError ? String(agentError) : "";

  const modelStr = (m: Agent["model"]) => typeof m === "string" ? m : `${m?.id} (${m?.speed || "standard"})`;

  const archive = async () => {
    if (!confirm("Archive this agent?")) return;
    await api(`/v1/agents/${id}/archive`, { method: "POST", body: "{}" });
    nav("/agents");
  };

  const del = async () => {
    if (!confirm("Delete this agent? This cannot be undone.")) return;
    await api(`/v1/agents/${id}`, { method: "DELETE" });
    nav("/agents");
  };

  if (error) return <div className="p-10 text-danger">Error: {error}</div>;
  if (!agent) return <div className="p-10 text-fg-subtle">Loading...</div>;

  const archived = !!agent.archived_at;
  const updatedAt = agent.updated_at || agent.created_at;

  return (
    <Page
      header={
        <PageHeader
          title={
            <span className="inline-flex items-center gap-2.5 min-w-0">
              <span className="truncate">{agent.name}</span>
              <span
                className={`shrink-0 inline-flex items-center text-[11px] px-2 py-0.5 rounded-full font-medium ${
                  archived
                    ? "bg-bg-surface text-fg-subtle"
                    : "bg-success-subtle text-success"
                }`}
              >
                {archived ? "Archived" : "Active"}
              </span>
            </span>
          }
          subtitle={
            <span className="flex flex-col gap-1">
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-subtle">
                <span className="font-mono">{agent.id}</span>
                <span>Last updated {formatRelativeTime(updatedAt)}</span>
              </span>
              {agent.description && (
                <span className="text-sm text-fg-muted max-w-3xl">
                  {agent.description}
                </span>
              )}
            </span>
          }
          actions={
            <>
              {!archived && (
                <Button variant="outline" size="sm" onClick={archive}>
                  Archive
                </Button>
              )}
              <Button variant="destructive" size="sm" onClick={del}>
                Delete
              </Button>
            </>
          }
        />
      }
    >
      <Tabs
        value={tab}
        onValueChange={(v) => setTab(v as TabKey)}
        aria-label="Agent sections"
        className="mt-2 flex-col"
      >
        <TabsList
          variant="line"
          className="mb-6 h-auto w-full justify-start gap-5 rounded-none border-b border-border bg-transparent p-0"
        >
          <TabsTrigger
            value="configuration"
            className="flex-none rounded-none border-b-2 border-transparent px-0.5 pb-2.5 pt-1 text-fg-muted shadow-none after:hidden hover:text-fg data-[state=active]:border-fg data-[state=active]:bg-transparent data-[state=active]:font-medium data-[state=active]:text-fg data-[state=active]:shadow-none"
          >
            Configuration
          </TabsTrigger>
          <TabsTrigger
            value="sessions"
            className="flex-none rounded-none border-b-2 border-transparent px-0.5 pb-2.5 pt-1 text-fg-muted shadow-none after:hidden hover:text-fg data-[state=active]:border-fg data-[state=active]:bg-transparent data-[state=active]:font-medium data-[state=active]:text-fg data-[state=active]:shadow-none"
          >
            Sessions
          </TabsTrigger>
        </TabsList>

        <TabsContent value="configuration">
          <div className="space-y-6">
            {/* Properties grid */}
            <div className="grid grid-cols-[140px_1fr] gap-x-4 gap-y-2 max-w-2xl text-sm">
              <span className="text-fg-muted">ID</span><span className="font-mono text-xs">{agent.id}</span>
              <span className="text-fg-muted">Model</span><span>{modelStr(agent.model)}</span>
              <span className="text-fg-muted">Harness</span>
              <span>{agent._oma?.harness || "default"}</span>
              {agent._oma?.runtime_binding &&
                "runtime_id" in agent._oma.runtime_binding && (
                  <>
                    <span className="text-fg-muted">Local Runtime</span>
                    <span className="text-xs">
                      <span className="font-mono">{agent._oma.runtime_binding.runtime_id.slice(0, 8)}…</span>
                      <span className="text-fg-subtle"> · ACP agent: </span>
                      <span className="font-mono">{agent._oma.runtime_binding.acp_agent_id}</span>
                    </span>
                  </>
                )}
              {agent._oma?.runtime_binding && "agent" in agent._oma.runtime_binding && (
                <>
                  <span className="text-fg-muted">Managed agent</span>
                  <span className="font-mono text-xs">{agent._oma.runtime_binding.agent}</span>
                </>
              )}
              <span className="text-fg-muted">Version</span><span>v{agent.version}</span>
              <span className="text-fg-muted">Tools</span>
              <span>{(agent.tools || []).map((t: any) => t.type === "custom" ? `Custom: ${t.name}` : t.type).join(", ") || "None"}</span>
              <span className="text-fg-muted">Created</span><span>{new Date(agent.created_at).toLocaleString()}</span>
              <span className="text-fg-muted">Updated</span><span>{new Date(agent.updated_at || agent.created_at).toLocaleString()}</span>
              {agent.archived_at && <><span className="text-fg-muted">Archived</span><span className="text-warning">{new Date(agent.archived_at).toLocaleString()}</span></>}
            </div>

            {/* Integrations — one fold per provider so adding a 4th / 5th doesn't
                push the rest of the page below the viewport. Default-open when
                there's at least one live publication so the user sees what's wired
                up at a glance; otherwise default-closed. */}
            <div className="mt-6 max-w-2xl">
              <h2 className="font-display text-base font-semibold mb-2">Integrations</h2>
              <div className="space-y-2">
                <IntegrationFold
                  kind="linear"
                  label="Linear"
                  icon={<LinearIcon className="w-4 h-4" />}
                  pubs={linearPubs}
                  agentId={agent.id}
                />
                <IntegrationFold
                  kind="github"
                  label="GitHub"
                  icon={<GitHubIcon className="w-4 h-4" />}
                  pubs={githubPubs}
                  agentId={agent.id}
                />
                <IntegrationFold
                  kind="slack"
                  label="Slack"
                  icon={<SlackIcon className="w-4 h-4" />}
                  pubs={slackPubs}
                  agentId={agent.id}
                />
              </div>
            </div>

            {/* System prompt */}
            {agent.system && (
              <div className="mt-8 max-w-2xl">
                <h2 className="font-display text-base font-semibold mb-2">System Prompt</h2>
                <pre className="bg-bg-surface border border-border rounded-lg p-4 text-sm whitespace-pre-wrap max-h-64 overflow-y-auto font-mono text-fg-muted leading-relaxed">
                  {agent.system}
                </pre>
              </div>
            )}

            {/* Version history */}
            {versions.length > 0 && (
              <div className="mt-8 max-w-2xl">
                <h2 className="font-display text-base font-semibold mb-2">Version History</h2>
                <div className="border border-border rounded-lg overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="bg-bg-surface/60 text-fg-muted text-xs uppercase tracking-wider">
                        <th className="text-left px-4 py-2">Version</th>
                        <th className="text-left px-4 py-2">Model</th>
                        <th className="text-left px-4 py-2">System Prompt</th>
                      </tr>
                    </thead>
                    <tbody>
                      {versions.map((v) => (
                        <tr key={v.version} className="border-t border-border">
                          <td className="px-4 py-2">v{v.version}</td>
                          <td className="px-4 py-2 text-fg-muted">{modelStr(v.model)}</td>
                          <td className="px-4 py-2 text-fg-muted max-w-xs truncate">{v.system || "—"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </div>
        </TabsContent>

        <TabsContent value="sessions">
          <AgentSessionsPanel
            agentId={agent.id}
            versions={versionNumbers.length > 0 ? versionNumbers : [agent.version]}
          />
        </TabsContent>
      </Tabs>
    </Page>
  );
}

/**
 * One foldable provider section. Default-open when there's a live
 * publication, default-closed otherwise — opening an empty section
 * just to find the "Publish to X" link is wasteful.
 */
function IntegrationFold({
  kind,
  label,
  icon,
  pubs,
  agentId,
}: {
  kind: "linear" | "github" | "slack";
  label: string;
  icon: React.ReactNode;
  pubs: Pub[];
  agentId: string;
}) {
  return (
    <details
      open={pubs.length > 0}
      className="border border-border rounded-lg bg-bg-surface/30 [&_summary::-webkit-details-marker]:hidden"
    >
      <summary className="px-4 py-2.5 min-h-11 sm:min-h-0 flex items-center gap-3 text-sm cursor-pointer hover:bg-bg-surface/60 list-none">
        <span className="text-fg-muted shrink-0">{icon}</span>
        <span className="font-medium text-fg">{label}</span>
        <span className="ml-auto text-xs text-fg-subtle">
          {pubs.length === 0 ? "Not published" : `${pubs.length} live`}
        </span>
      </summary>
      <div className="px-4 pb-3 pt-2 border-t border-border/40 space-y-1.5 text-sm">
        {pubs.length === 0 ? (
          <Link
            to={`/integrations/${kind}/publish?agent_id=${agentId}`}
            className="inline-flex items-center gap-1.5 min-h-11 sm:min-h-0 text-brand hover:underline"
          >
            Publish to {label} →
          </Link>
        ) : (
          <>
            {pubs.map((p) => (
              <Link
                key={p.id}
                to={`/integrations/${kind}`}
                className="flex items-center gap-2 min-h-11 sm:min-h-0 text-fg-muted hover:text-fg"
              >
                <span className="inline-flex items-center gap-1 text-[11px] px-1.5 py-0.5 rounded bg-success-subtle text-success">
                  Live
                </span>
                <span>
                  as <strong>{p.persona.name}</strong> in {p.workspace_name ?? `${label} workspace`}
                </span>
                {p.mode === "full" && (
                  <span className="text-xs text-fg-subtle">(full identity)</span>
                )}
              </Link>
            ))}
            <Link
              to={`/integrations/${kind}/publish?agent_id=${agentId}`}
              className="inline-flex items-center min-h-11 sm:min-h-0 text-xs text-brand hover:underline pt-1"
            >
              + Publish to another workspace
            </Link>
          </>
        )}
      </div>
    </details>
  );
}
