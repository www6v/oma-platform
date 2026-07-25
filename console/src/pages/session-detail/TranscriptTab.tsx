/**
 * TranscriptTab — Claude Console-style transcript view.
 *
 * Left panel: flat event list with category filter, HITL panel at bottom.
 * Right panel: EventDetailPane (type / time / Rendered|Raw) + "View in Debug →".
 * Streaming overlays render only in this tab (not in Debug tab).
 *
 * Events are color-coded by category:
 * - User: red/pink
 * - Agent: green (consecutive agent.message events are merged)
 * - Tool: blue/cyan
 * - Error: red/dark red
 * - System: gray
 */

import { Link2Icon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import type { Event } from "../../lib/events";
import { pairSessionErrors, pairToolResults } from "../../lib/tool-pairing";
import { cn } from "../../lib/utils";
import {
  PromptInput,
  PromptInputTextarea,
  PromptInputFooter,
  PromptInputSubmit,
} from "../../components/ai-elements/prompt-input";
import { EventDetail } from "./EventDetail";
import {
  EventDetailPane,
  formatEventRaw,
  type DetailViewMode,
} from "./EventDetailPane";
import {
  categorizeEvent,
  EventRow,
  getMergedEventText,
  mergeConsecutiveAgentEvents,
  type TranscriptCategory,
} from "./EventRow";

export interface TranscriptTabProps {
  events: Event[];
  activeThreadId: string;
  selectedEventId: string | null;
  onSelectEvent: (eventId: string | null) => void;
  onViewInDebug?: (eventId: string) => void;
  onSend?: (text: string) => void;
  sending?: boolean;
  /** Streaming overlays — rendered only in Transcript tab */
  streams?: Map<string, Event>;
  thinkingStreams?: Map<string, Event>;
  toolInputStreams?: Map<string, Event>;
}

const CATEGORY_LABELS: Record<TranscriptCategory, string> = {
  user: "User",
  agent: "Agent",
  tool: "Tool",
  error: "Error",
  system: "System",
};

const CATEGORY_COLORS: Record<TranscriptCategory, string> = {
  user: "bg-red-500",
  agent: "bg-green-500",
  tool: "bg-blue-500",
  error: "bg-red-700",
  system: "bg-gray-500",
};

const CATEGORY_BG: Record<TranscriptCategory, string> = {
  user: "bg-red-50 border-l-4 border-red-500",
  agent: "bg-green-50 border-l-4 border-green-500",
  tool: "bg-blue-50 border-l-4 border-blue-500",
  error: "bg-red-100 border-l-4 border-red-700",
  system: "bg-gray-50 border-l-4 border-gray-500",
};

export function TranscriptTab({
  events,
  activeThreadId,
  selectedEventId,
  onSelectEvent,
  onViewInDebug,
  onSend,
  sending = false,
}: TranscriptTabProps) {
  const [selectedCategories, setSelectedCategories] = useState<Set<TranscriptCategory>>(
    new Set(["user", "agent", "tool", "error"])
  );
  const [viewMode, setViewMode] = useState<DetailViewMode>("rendered");

  // Filter events by active thread
  const filteredEvents = useMemo(
    () =>
      events.filter((e) => {
        const tid = (e as { session_thread_id?: string }).session_thread_id;
        if (!tid) return activeThreadId === "sthr_primary";
        return tid === activeThreadId;
      }),
    [events, activeThreadId]
  );

  // Pair tool_use ↔ tool_result
  const { resultByToolUseId, pairedResultIds } = useMemo(
    () => pairToolResults(filteredEvents),
    [filteredEvents]
  );

  // Pair session.error ↔ upstream model error cause
  const sessionErrorCause = useMemo(
    () => pairSessionErrors(filteredEvents),
    [filteredEvents]
  );

  // Filter events by selected categories and skip paired tool_result
  const visibleEvents = useMemo(() => {
    return filteredEvents.filter((e) => {
      // Skip paired tool_result
      if (e.type === "agent.tool_result" || e.type === "agent.mcp_tool_result") {
        const id =
          e.type === "agent.tool_result" ? e.tool_use_id : e.mcp_tool_use_id;
        if (id && pairedResultIds.has(id)) return false;
      }
      // Skip status events
      if (e.type.startsWith("session.status_")) return false;
      // Filter by category
      return selectedCategories.has(categorizeEvent(e));
    });
  }, [filteredEvents, selectedCategories, pairedResultIds]);

  // Merge consecutive agent.message events
  const displayEvents = useMemo(
    () => mergeConsecutiveAgentEvents(visibleEvents),
    [visibleEvents]
  );

  // Check if selected event is part of a merged group
  const selectedDisplayEvent = useMemo(() => {
    if (!selectedEventId) return null;
    return displayEvents.find((de) =>
      de.events.some((e) => e.id === selectedEventId)
    ) ?? null;
  }, [displayEvents, selectedEventId]);

  const toggleCategory = (cat: TranscriptCategory) => {
    setSelectedCategories((prev) => {
      const next = new Set(prev);
      if (next.has(cat)) {
        next.delete(cat);
      } else {
        next.add(cat);
      }
      return next;
    });
  };

  return (
    <div className="grid h-full min-h-0 w-full grid-cols-2 overflow-hidden">
      {/* Left: event list — exactly 50% of the session content width */}
      <div className="flex min-h-0 min-w-0 flex-col border-r border-border">
        {/* Filter chips */}
        <div className="flex flex-wrap gap-1.5 border-b border-border p-2">
          {(Object.keys(CATEGORY_LABELS) as TranscriptCategory[]).map((cat) => (
            <button
              key={cat}
              type="button"
              onClick={() => toggleCategory(cat)}
              className={cn(
                "flex items-center gap-1 rounded-full px-2 py-0.5 text-xs transition-colors",
                selectedCategories.has(cat)
                  ? "bg-accent text-accent-foreground"
                  : "bg-muted text-muted-foreground opacity-50"
              )}
            >
              <span className={cn("h-2 w-2 rounded-full", CATEGORY_COLORS[cat])} />
              {CATEGORY_LABELS[cat]}
            </button>
          ))}
        </div>

        {/* Event list */}
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {displayEvents.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              No events
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              {displayEvents.map((de, idx) => {
                const isMerged = de.events.length > 1;
                const isSelected = de.events.some((e) => e.id === selectedEventId);

                return (
                  <div
                    key={de.primaryEvent.id ?? `group-${idx}`}
                    className={cn(
                      "rounded-md p-1",
                      CATEGORY_BG[de.category]
                    )}
                  >
                    <EventRow
                      event={de.primaryEvent}
                      selected={isSelected}
                      onClick={() => onSelectEvent(de.primaryEvent.id ?? null)}
                      mergedCount={isMerged ? de.events.length : undefined}
                      mergedText={isMerged ? getMergedEventText(de.events) : undefined}
                    />
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Query input at the bottom of the transcript */}
        {onSend && (
          <div className="border-t border-border bg-bg p-2">
            <PromptInput
              accept=""
              maxFiles={0}
              maxFileSize={0}
              onError={(err) => toast.error(err.message)}
              onSubmit={({ text }) => {
                if (text.trim()) onSend(text);
              }}
            >
              <PromptInputTextarea
                placeholder="Send a message to the agent…"
                disabled={sending}
              />
              <PromptInputFooter>
                <PromptInputSubmit disabled={sending} />
              </PromptInputFooter>
            </PromptInput>
          </div>
        )}
      </div>

      {/* Right: detail pane flush to the content’s right edge */}
      <div className="flex min-h-0 min-w-0 flex-col overflow-hidden bg-bg p-4">
        {selectedDisplayEvent ? (
          <EventDetailPane
            event={selectedDisplayEvent.primaryEvent}
            viewMode={viewMode}
            onViewModeChange={setViewMode}
            onClose={() => onSelectEvent(null)}
            badgeMode="category"
            mergedCount={selectedDisplayEvent.events.length}
            headerExtra={
              onViewInDebug && selectedDisplayEvent.primaryEvent.id ? (
                <button
                  type="button"
                  onClick={() =>
                    onViewInDebug(selectedDisplayEvent.primaryEvent.id!)
                  }
                  className="mb-2 flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
                >
                  <Link2Icon className="h-3 w-3" />
                  <span>View in Debug →</span>
                </button>
              ) : undefined
            }
            rendered={
              <EventDetail
                event={selectedDisplayEvent.primaryEvent}
                pairedResult={
                  selectedDisplayEvent.primaryEvent.id &&
                  resultByToolUseId.has(selectedDisplayEvent.primaryEvent.id)
                    ? resultByToolUseId.get(
                        selectedDisplayEvent.primaryEvent.id
                      )
                    : undefined
                }
                modelErrorCause={
                  selectedDisplayEvent.primaryEvent.id &&
                  selectedDisplayEvent.primaryEvent.type === "session.error"
                    ? sessionErrorCause.get(
                        selectedDisplayEvent.primaryEvent.id
                      )
                    : undefined
                }
                mergedEvents={
                  selectedDisplayEvent.events.length > 1
                    ? selectedDisplayEvent.events
                    : undefined
                }
              />
            }
            raw={formatEventRaw(
              selectedDisplayEvent.primaryEvent,
              selectedDisplayEvent.events.length > 1
                ? selectedDisplayEvent.events
                : undefined
            )}
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Select an event to view details
          </div>
        )}
      </div>
    </div>
  );
}
