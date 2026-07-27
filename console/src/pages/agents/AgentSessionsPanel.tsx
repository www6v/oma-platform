import { useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { ArchiveIcon, TrashIcon } from "lucide-react";

import { useApi } from "../../lib/api";
import { useInfiniteApiQuery } from "../../lib/useApiQuery";
import { FilterChip, CreatedFilterChip } from "../../components/FilterChip";
import { FacetedFilter } from "../../components/FacetedFilter";
import { RowActionsMenu } from "../../components/RowActionsMenu";
import { Button } from "@/components/ui/button";
import { PopoverContent } from "@/components/ui/popover";
import type { SessionRecord as Session } from "../../types/session";

type StatusValue = "any" | "idle" | "running" | "rescheduling" | "terminated";

const STATUS_OPTIONS: { value: StatusValue; label: string }[] = [
  { value: "any", label: "All" },
  { value: "idle", label: "Idle" },
  { value: "running", label: "Running" },
  { value: "rescheduling", label: "Rescheduling" },
  { value: "terminated", label: "Terminated" },
];

function statusCls(status?: string) {
  switch (status) {
    case "terminated":
      return "bg-danger-subtle text-danger";
    case "running":
      return "bg-info-subtle text-info";
    default:
      return "bg-bg-surface text-fg-muted";
  }
}

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

/**
 * Sessions tab on the agent detail page — lists sessions bound to this
 * agent (server-side `agent_id` filter). Mirrors the Claude Console
 * agent→sessions layout: Created / Version / Status chips + archived
 * toggle + a compact table. Version filtering is client-side because
 * the list endpoint has no `agent_version` param.
 */
export function AgentSessionsPanel({
  agentId,
  versions,
}: {
  agentId: string;
  versions: number[];
}) {
  const { api } = useApi();
  const nav = useNavigate();

  const [status, setStatus] = useState<StatusValue>("any");
  const [created, setCreated] = useState<{ after?: number; before?: number }>({});
  const [version, setVersion] = useState<number | "any">("any");
  const [showArchived, setShowArchived] = useState(true);

  const sessionsParams = useMemo(
    () => ({
      agent_id: agentId,
      ...(status !== "any" ? { status } : {}),
      ...(created.after !== undefined
        ? { created_after: new Date(created.after).toISOString() }
        : {}),
      ...(created.before !== undefined
        ? { created_before: new Date(created.before).toISOString() }
        : {}),
      ...(showArchived ? { include_archived: "true" } : {}),
    }),
    [agentId, status, created.after, created.before, showArchived],
  );

  const {
    items: sessions,
    isLoading: loading,
    hasMore,
    isLoadingMore,
    loadMore,
    refresh: refreshSessions,
  } = useInfiniteApiQuery<Session>("/v1/sessions", {
    limit: 20,
    params: sessionsParams,
  });

  const versionOptions = useMemo(() => {
    const fromAgent = versions.length > 0
      ? versions
      : Array.from(new Set(sessions.map((s) => s.agent.version))).sort(
          (a, b) => b - a,
        );
    return [
      { value: "any", label: "All" },
      ...fromAgent.map((v) => ({ value: String(v), label: `v${v}` })),
    ];
  }, [versions, sessions]);

  const displayed = useMemo(() => {
    if (version === "any") return sessions;
    return sessions.filter((s) => s.agent.version === version);
  }, [sessions, version]);

  const statusDisplay =
    status === "any"
      ? undefined
      : STATUS_OPTIONS.find((o) => o.value === status)?.label;
  const versionDisplay =
    version === "any" ? undefined : `v${version}`;

  const hasActiveFilter =
    status !== "any" ||
    version !== "any" ||
    created.after !== undefined ||
    created.before !== undefined;

  return (
    <div>
      <div className="flex items-center gap-2 mb-4 flex-wrap">
        <CreatedFilterChip value={created} onChange={setCreated} />

        <FilterChip
          label="Version"
          active={version !== "any"}
          display={versionDisplay}
          onClear={() => setVersion("any")}
        >
          <PopoverContent
            align="start"
            sideOffset={4}
            collisionPadding={8}
            className="w-40 p-0"
          >
            <FacetedFilter
              options={versionOptions}
              value={version === "any" ? "any" : String(version)}
              onValueChange={(v) =>
                setVersion(v === "any" ? "any" : Number(v))
              }
              searchPlaceholder="Version..."
            />
          </PopoverContent>
        </FilterChip>

        <FilterChip
          label="Status"
          active={status !== "any"}
          display={statusDisplay}
          onClear={() => setStatus("any")}
        >
          <PopoverContent
            align="start"
            sideOffset={4}
            collisionPadding={8}
            className="w-48 p-0"
          >
            <FacetedFilter
              options={STATUS_OPTIONS}
              value={status}
              onValueChange={(v) => setStatus(v as StatusValue)}
              searchPlaceholder="Status..."
            />
          </PopoverContent>
        </FilterChip>

        <label className="inline-flex items-center gap-2 ml-auto text-sm text-fg-muted cursor-pointer select-none">
          <span>Show archived</span>
          <button
            type="button"
            role="switch"
            aria-checked={showArchived}
            onClick={() => setShowArchived((v) => !v)}
            className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors ${
              showArchived ? "bg-brand" : "bg-bg-surface border border-border"
            }`}
          >
            <span
              className={`pointer-events-none absolute top-0.5 left-0.5 size-4 rounded-full bg-white shadow transition-transform ${
                showArchived ? "translate-x-4" : "translate-x-0"
              }`}
            />
          </button>
        </label>
      </div>

      {loading ? (
        <p className="text-fg-subtle text-sm py-4">Loading...</p>
      ) : (
        <div className="border border-border rounded-lg overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-bg-surface/60 text-fg-muted text-xs uppercase tracking-wider">
                <th className="text-left px-4 py-2.5">ID</th>
                <th className="text-left px-4 py-2.5">Name</th>
                <th className="text-left px-4 py-2.5">Status</th>
                <th className="text-left px-4 py-2.5">Version</th>
                <th className="text-left px-4 py-2.5">Created</th>
                <th className="text-right px-4 py-2.5 w-12" />
              </tr>
            </thead>
            <tbody>
              {displayed.map((s) => {
                const archived = !!s.archived_at;
                const label = s.title?.trim() || s.id;
                return (
                  <tr
                    key={s.id}
                    onClick={() => nav(`/sessions/${s.id}`)}
                    className="border-t border-border cursor-pointer hover:bg-bg-surface transition-colors duration-[var(--dur-quick)] ease-[var(--ease-soft)]"
                  >
                    <td className="px-4 py-3 font-mono text-xs text-fg-muted">
                      <span title={s.id}>{s.id}</span>
                    </td>
                    <td className="px-4 py-3 font-medium text-fg">
                      {s.title?.trim() || "—"}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex items-center text-xs px-2 py-0.5 rounded-full capitalize ${statusCls(s.status)}`}
                      >
                        {s.status || "idle"}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-fg-muted">
                      v{s.agent.version}
                    </td>
                    <td className="px-4 py-3 text-fg-muted">
                      {formatRelativeTime(s.created_at)}
                    </td>
                    <td
                      className="px-4 py-3 text-right"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <RowActionsMenu
                        label={`Actions for ${label}`}
                        actions={[
                          {
                            label: archived ? "Unarchive" : "Archive",
                            icon: <ArchiveIcon className="size-4" />,
                            disabled: archived,
                            onSelect: async () => {
                              try {
                                await api(`/v1/sessions/${s.id}/archive`, {
                                  method: "POST",
                                  body: "{}",
                                });
                                refreshSessions();
                              } catch {
                                // useApi toasts
                              }
                            },
                          },
                          {
                            label: "Delete",
                            icon: <TrashIcon className="size-4" />,
                            destructive: true,
                            onSelect: async () => {
                              if (
                                !confirm(
                                  `Delete session "${label}"? This can't be undone.`,
                                )
                              ) {
                                return;
                              }
                              try {
                                await api(`/v1/sessions/${s.id}`, {
                                  method: "DELETE",
                                });
                                refreshSessions();
                              } catch {
                                // useApi toasts
                              }
                            },
                          },
                        ]}
                      />
                    </td>
                  </tr>
                );
              })}
              {!displayed.length && (
                <tr>
                  <td
                    colSpan={6}
                    className="px-4 py-8 text-center text-fg-subtle"
                  >
                    {hasActiveFilter
                      ? "No matching sessions."
                      : "No sessions for this agent yet."}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {hasMore && (
        <div className="flex items-center gap-2 mt-3">
          <Button
            variant="outline"
            size="sm"
            onClick={loadMore}
            disabled={isLoadingMore}
            loading={isLoadingMore}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
