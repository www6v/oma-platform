import { useCallback, useEffect, useMemo, useState } from "react";

import { useApi } from "../../lib/api";
import { formatRelative } from "../../lib/format";
import { Button } from "@/components/ui/button";

export interface TeamMemberRow {
  id: string;
  team_id: string;
  agent_id: string;
  display_name: string;
  color?: string | null;
  thread_id?: string | null;
  role?: string | null;
  backend_type: string;
  status: string;
  joined_at?: string;
}

export interface TeamRow {
  id: string;
  session_id: string;
  name: string;
  description?: string | null;
  lead_thread_id: string;
  lead_agent_id: string;
  status: string;
  created_at?: string;
  members: TeamMemberRow[];
}

export interface TeamMessageRow {
  id: string;
  team_id: string;
  from_member_id: string;
  to_member_id?: string | null;
  message_type: string;
  body: string;
  summary?: string | null;
  created_at: string;
  read_at?: string | null;
}

function memberStatusTone(status: string): string {
  switch (status) {
    case "active":
    case "running":
      return "text-info bg-info-subtle";
    case "listening":
      return "text-accent-violet bg-accent-violet-subtle";
    case "shutdown":
      return "text-fg-subtle bg-bg-surface";
    default:
      return "text-fg-muted bg-bg-surface/60";
  }
}

function messageTypeLabel(messageType: string): string {
  if (!messageType || messageType === "text") {
    return "text";
  }
  return messageType.replace(/_/g, " ");
}

export function TeamPanel({
  sessionId,
  teams,
  messagesByTeamId,
  onTeamsChange,
  onMessagesChange,
  onSelectThread,
}: {
  sessionId: string;
  teams: TeamRow[];
  messagesByTeamId: Record<string, TeamMessageRow[]>;
  onTeamsChange: (teams: TeamRow[]) => void;
  onMessagesChange: (
    teamId: string,
    messages: TeamMessageRow[],
  ) => void;
  onSelectThread?: (threadId: string) => void;
}) {
  const { api } = useApi();
  const [selectedTeamId, setSelectedTeamId] = useState<string | null>(null);
  const [loadingMessages, setLoadingMessages] = useState(false);
  const [shutdownBusy, setShutdownBusy] = useState<string | null>(null);

  const selectedTeam = useMemo(
    () => teams.find((t) => t.id === selectedTeamId) ?? teams[0] ?? null,
    [teams, selectedTeamId],
  );

  useEffect(() => {
    if (teams.length === 0) {
      setSelectedTeamId(null);
      return;
    }
    if (!selectedTeamId || !teams.some((t) => t.id === selectedTeamId)) {
      setSelectedTeamId(teams[0].id);
    }
  }, [teams, selectedTeamId]);

  const loadMessages = useCallback(
    async (teamId: string) => {
      setLoadingMessages(true);
      try {
        const res = await api<{ data: TeamMessageRow[] }>(
          `/v1/sessions/${sessionId}/teams/${teamId}/messages?limit=200`,
        );
        onMessagesChange(teamId, res.data ?? []);
      } catch {
        onMessagesChange(teamId, []);
      } finally {
        setLoadingMessages(false);
      }
    },
    [api, sessionId, onMessagesChange],
  );

  useEffect(() => {
    if (!selectedTeam) {
      return;
    }
    if (!messagesByTeamId[selectedTeam.id]) {
      void loadMessages(selectedTeam.id);
    }
  }, [selectedTeam, messagesByTeamId, loadMessages]);

  const memberNames = useMemo(() => {
    const map = new Map<string, string>();
    if (!selectedTeam) {
      return map;
    }
    for (const m of selectedTeam.members) {
      map.set(m.id, m.display_name);
    }
    return map;
  }, [selectedTeam]);

  const messages = selectedTeam
    ? messagesByTeamId[selectedTeam.id] ?? []
    : [];

  const refreshTeams = useCallback(async () => {
    try {
      const res = await api<{ data: TeamRow[] }>(
        `/v1/sessions/${sessionId}/teams`,
      );
      onTeamsChange(res.data ?? []);
    } catch {
      onTeamsChange([]);
    }
  }, [api, sessionId, onTeamsChange]);

  const shutdownMember = async (teamId: string, memberId: string) => {
    setShutdownBusy(memberId);
    try {
      await api(
        `/v1/sessions/${sessionId}/teams/${teamId}/members/${memberId}/shutdown`,
        { method: "POST" },
      );
      await refreshTeams();
      await loadMessages(teamId);
    } finally {
      setShutdownBusy(null);
    }
  };

  if (teams.length === 0) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center text-sm text-fg-muted px-6 text-center max-w-md mx-auto gap-3">
        <p>No teams in this session yet.</p>
        <p className="text-xs leading-relaxed">
          Use an agent with{" "}
          <span className="font-medium text-fg">Enable team tools</span>
          {" "}
          (Agents → Create agent → Agents tab), then in the{" "}
          <span className="font-medium text-fg">Conversation</span>
          {" "}
          tab ask the lead to create a team or spawn teammates — for example:
          {" "}
          <span className="italic">
            &quot;Create a team called alpha and spawn a coder teammate.&quot;
          </span>
          {" "}
          Results show up here with members, mailbox, and Shutdown controls.
        </p>
      </div>
    );
  }

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      {teams.length > 1 && (
        <div className="pl-3 pr-4 py-2 flex gap-1 shrink-0 overflow-x-auto">
          {teams.map((team) => (
            <button
              key={team.id}
              type="button"
              onClick={() => setSelectedTeamId(team.id)}
              className={`px-3 py-1.5 min-h-11 sm:min-h-0 text-xs rounded-md whitespace-nowrap transition-colors duration-[var(--dur-quick)] ease-[var(--ease-soft)] ${
                selectedTeam?.id === team.id
                  ? "bg-bg-surface text-brand font-semibold"
                  : "text-fg-subtle hover:text-fg-muted hover:bg-bg-surface/60"
              }`}
            >
              {team.name}
            </button>
          ))}
        </div>
      )}

      {selectedTeam && (
        <div className="flex-1 min-h-0 flex flex-col lg:flex-row gap-0">
          <section className="lg:w-[320px] shrink-0 pl-3 pr-4 py-4 lg:border-r border-border/40">
            <div className="mb-3">
              <h2 className="text-sm font-semibold text-fg">{selectedTeam.name}</h2>
              {selectedTeam.description && (
                <p className="text-xs text-fg-muted mt-1">{selectedTeam.description}</p>
              )}
              <p className="text-[10px] font-mono text-fg-subtle mt-1 truncate">
                {selectedTeam.id}
              </p>
            </div>

            <h3 className="text-[10px] uppercase tracking-wide text-fg-subtle font-mono mb-2">
              Members
            </h3>
            <ul className="space-y-2">
              {selectedTeam.members.map((member) => {
                const canShutdown =
                  member.backend_type === "in_process" &&
                  member.status !== "shutdown";
                return (
                  <li
                    key={member.id}
                    className="rounded-lg bg-bg-surface/50 px-3 py-2.5"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="text-sm font-medium text-fg truncate">
                          {member.display_name}
                        </div>
                        {member.role && (
                          <div className="text-[10px] text-fg-subtle uppercase tracking-wide">
                            {member.role}
                          </div>
                        )}
                        {member.thread_id && onSelectThread && (
                          <button
                            type="button"
                            onClick={() => onSelectThread(member.thread_id!)}
                            className="text-[10px] font-mono text-info hover:underline mt-1 truncate block max-w-full text-left"
                            title="Open thread in Conversation view"
                          >
                            {member.thread_id}
                          </button>
                        )}
                      </div>
                      <span
                        className={`shrink-0 text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded ${memberStatusTone(member.status)}`}
                      >
                        {member.status}
                      </span>
                    </div>
                    {canShutdown && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="mt-2 h-7 text-xs"
                        disabled={shutdownBusy === member.id}
                        onClick={() => void shutdownMember(selectedTeam.id, member.id)}
                      >
                        {shutdownBusy === member.id ? "Stopping…" : "Shutdown"}
                      </Button>
                    )}
                  </li>
                );
              })}
              {selectedTeam.members.length === 0 && (
                <li className="text-xs text-fg-muted">No teammates spawned yet.</li>
              )}
            </ul>
          </section>

          <section className="flex-1 min-h-0 flex flex-col pl-3 pr-4 py-4">
            <div className="flex items-center justify-between gap-2 mb-3 shrink-0">
              <h3 className="text-[10px] uppercase tracking-wide text-fg-subtle font-mono">
                Mailbox
              </h3>
              <button
                type="button"
                onClick={() => void loadMessages(selectedTeam.id)}
                disabled={loadingMessages}
                className="text-xs text-fg-muted hover:text-fg disabled:opacity-50"
              >
                {loadingMessages ? "Refreshing…" : "Refresh"}
              </button>
            </div>

            <div className="flex-1 min-h-0 overflow-y-auto space-y-3 pr-1">
              {messages.length === 0 && !loadingMessages && (
                <p className="text-sm text-fg-muted">No mailbox messages yet.</p>
              )}
              {messages.map((msg) => {
                const fromName =
                  memberNames.get(msg.from_member_id) ?? msg.from_member_id;
                const toName = msg.to_member_id
                  ? memberNames.get(msg.to_member_id) ?? msg.to_member_id
                  : "broadcast";
                const createdMs = Date.parse(msg.created_at);
                const rel = Number.isFinite(createdMs)
                  ? formatRelative(Date.now() - createdMs)
                  : msg.created_at;
                return (
                  <article
                    key={msg.id}
                    className="rounded-lg border border-border/30 bg-bg-surface/40 px-3 py-2.5"
                  >
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
                      <span className="font-medium text-fg">{fromName}</span>
                      <span className="text-fg-subtle">→</span>
                      <span className="text-fg-muted">{toName}</span>
                      <span
                        className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-bg-surface text-fg-subtle"
                      >
                        {messageTypeLabel(msg.message_type)}
                      </span>
                      <span className="ml-auto text-[10px] text-fg-subtle font-mono">
                        {rel}
                      </span>
                    </div>
                    {msg.summary && (
                      <p className="text-xs font-medium text-fg-muted mt-1.5">
                        {msg.summary}
                      </p>
                    )}
                    <p className="text-sm text-fg mt-1 whitespace-pre-wrap break-words">
                      {msg.body}
                    </p>
                    {!msg.read_at && msg.message_type !== "shutdown_response" && (
                      <p className="text-[10px] text-accent-violet mt-1.5">Unread</p>
                    )}
                  </article>
                );
              })}
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
