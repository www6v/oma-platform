/**
 * DebugTab — raw event stream view for debugging.
 *
 * Left panel: flat event list with raw type filter. Adjacent agent.message
 * events are merged into a single row (with a ×N count badge) to keep the
 * list compact — same merge behavior as TranscriptTab.
 * Right panel: EventDetail with Rendered/Raw toggle.
 * Scroll to selectedDebugEventId on mount/update.
 * Shows only canonical events (no streaming overlays).
 */

import { useEffect, useMemo, useRef, useState } from "react";
import type { Event } from "../../lib/events";
import { pairSessionErrors, pairToolResults } from "../../lib/tool-pairing";
import { cn } from "../../lib/utils";
import { EventDetail } from "./EventDetail";
import {
  categorizeEvent,
  DisplayEvent,
  EventRow,
  getMergedEventText,
  mergeConsecutiveAgentEvents,
} from "./EventRow";

export interface DebugTabProps {
  events: Event[];
  activeThreadId: string;
  selectedDebugEventId: string | null;
  onSelectDebugEvent: (eventId: string | null) => void;
  /** Ref for scroll container */
  scrollRef?: React.RefObject<HTMLDivElement | null>;
}

/**
 * Get all unique event types from the events array.
 */
function getEventTypes(events: Event[]): string[] {
  const types = new Set<string>();
  for (const e of events) {
    types.add(e.type);
  }
  return Array.from(types).sort();
}

export function DebugTab({
  events,
  activeThreadId,
  selectedDebugEventId,
  onSelectDebugEvent,
  scrollRef,
}: DebugTabProps) {
  const [selectedTypes, setSelectedTypes] = useState<Set<string>>(new Set());
  const [viewMode, setViewMode] = useState<"rendered" | "raw">("rendered");
  const eventRefs = useRef<Map<string, HTMLDivElement>>(new Map());

  // Filter events by active thread
  const filteredEvents = events.filter((e) => {
    const tid = (e as { session_thread_id?: string }).session_thread_id;
    if (!tid) return activeThreadId === "sthr_primary";
    return tid === activeThreadId;
  });

  // Get all event types for filter
  const eventTypes = getEventTypes(filteredEvents);

  // Filter by selected types
  const visibleEvents =
    selectedTypes.size === 0
      ? filteredEvents
      : filteredEvents.filter((e) => selectedTypes.has(e.type));

  // Merge consecutive agent.message events (reuses TranscriptTab logic)
  const displayEvents: DisplayEvent[] = useMemo(
    () => mergeConsecutiveAgentEvents(visibleEvents),
    [visibleEvents]
  );

  // Pair tool_use ↔ tool_result
  const { resultByToolUseId } = pairToolResults(filteredEvents);

  // Pair session.error ↔ upstream model error cause
  const sessionErrorCause = pairSessionErrors(filteredEvents);

  // Find the display group containing the selected event (for scroll + detail)
  const selectedDisplayEvent = useMemo(() => {
    if (!selectedDebugEventId) return null;
    return displayEvents.find((de) => de.events.some((e) => e.id === selectedDebugEventId)) ?? null;
  }, [displayEvents, selectedDebugEventId]);

  // Scroll to selected event
  useEffect(() => {
    if (selectedDebugEventId && scrollRef?.current) {
      const el = eventRefs.current.get(selectedDebugEventId);
      if (el) {
        el.scrollIntoView({ behavior: "smooth", block: "center" });
      }
    }
  }, [selectedDebugEventId, scrollRef]);

  const toggleType = (type: string) => {
    setSelectedTypes((prev) => {
      const next = new Set(prev);
      if (next.has(type)) {
        next.delete(type);
      } else {
        next.add(type);
      }
      return next;
    });
  };

  const registerRef = (id: string, el: HTMLDivElement | null) => {
    if (el) {
      eventRefs.current.set(id, el);
    } else {
      eventRefs.current.delete(id);
    }
  };

  return (
    <div className="grid h-full min-h-0 w-full grid-cols-2 overflow-hidden">
      {/* Left: event list — exactly 50% (matches Transcript) */}
      <div className="flex min-h-0 min-w-0 flex-col border-r border-border">
        {/* Type filter */}
        <div className="border-b border-border p-2">
          <div className="mb-1 text-xs font-medium text-muted-foreground">
            Filter by type
          </div>
          <div className="flex max-h-32 flex-wrap gap-1 overflow-y-auto">
            {eventTypes.map((type) => (
              <button
                key={type}
                type="button"
                onClick={() => toggleType(type)}
                className={cn(
                  "rounded bg-muted px-1.5 py-0.5 text-xs transition-colors",
                  selectedTypes.size === 0 || selectedTypes.has(type)
                    ? "text-foreground"
                    : "text-muted-foreground opacity-50"
                )}
              >
                {type}
              </button>
            ))}
          </div>
          {selectedTypes.size > 0 && (
            <button
              type="button"
              onClick={() => setSelectedTypes(new Set())}
              className="mt-1 text-xs text-brand hover:underline"
            >
              Clear filters
            </button>
          )}
        </div>

        {/* Event list — merged display events */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto p-2">
          {displayEvents.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              No events
            </div>
          ) : (
            <div className="flex flex-col gap-0.5">
              {displayEvents.map((de, idx) => {
                const isMerged = de.events.length > 1;
                const isSelected = de.events.some((e) => e.id === selectedDebugEventId);
                return (
                  <div
                    key={de.primaryEvent.id ?? `group-${idx}`}
                    ref={(el) => registerRef(de.primaryEvent.id ?? "", el)}
                  >
                    <EventRow
                      event={de.primaryEvent}
                      selected={isSelected}
                      onClick={() => onSelectDebugEvent(de.primaryEvent.id ?? null)}
                      mergedCount={isMerged ? de.events.length : undefined}
                      mergedText={isMerged ? getMergedEventText(de.events) : undefined}
                      className={cn(
                        isSelected && "ring-2 ring-brand"
                      )}
                    />
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Right: detail pane flush to the content’s right edge */}
      <div className="flex min-h-0 min-w-0 flex-col overflow-y-auto bg-bg p-4">
        {/* View mode toggle */}
        <div className="mb-3 flex items-center gap-2 border-b border-border pb-2">
          <button
            type="button"
            onClick={() => setViewMode("rendered")}
            className={cn(
              "rounded px-2 py-1 text-sm transition-colors",
              viewMode === "rendered"
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-accent/50"
            )}
          >
            Rendered
          </button>
          <button
            type="button"
            onClick={() => setViewMode("raw")}
            className={cn(
              "rounded px-2 py-1 text-sm transition-colors",
              viewMode === "raw"
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-accent/50"
            )}
          >
            Raw
          </button>
        </div>

        {selectedDisplayEvent ? (
          viewMode === "rendered" ? (
            <EventDetail
              event={selectedDisplayEvent.primaryEvent}
              pairedResult={
                selectedDisplayEvent.primaryEvent.id &&
                resultByToolUseId.has(selectedDisplayEvent.primaryEvent.id)
                  ? resultByToolUseId.get(selectedDisplayEvent.primaryEvent.id)
                  : undefined
              }
              modelErrorCause={
                selectedDisplayEvent.primaryEvent.id &&
                selectedDisplayEvent.primaryEvent.type === "session.error"
                  ? sessionErrorCause.get(selectedDisplayEvent.primaryEvent.id)
                  : undefined
              }
              mergedEvents={
                selectedDisplayEvent.events.length > 1
                  ? selectedDisplayEvent.events
                  : undefined
              }
            />
          ) : (
            <pre className="overflow-x-auto rounded bg-muted p-3 text-xs">
              {selectedDisplayEvent.events.length > 1
                ? JSON.stringify(selectedDisplayEvent.events, null, 2)
                : JSON.stringify(selectedDisplayEvent.primaryEvent, null, 2)}
            </pre>
          )
        ) : (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            Select an event to view details
          </div>
        )}
      </div>
    </div>
  );
}
