/**
 * DebugTab — raw event stream view for debugging.
 *
 * Left panel: flat event list with raw type filter. Adjacent agent.message
 * events are merged into a single row (with a ×N count badge) to keep the
 * list compact — same merge behavior as TranscriptTab.
 * Right panel: EventDetail with Rendered/Raw toggle.
 * Scroll to selectedDebugEventId on mount/update.
 * Shows only canonical events (no streaming overlays).
 *
 * Default filter hides span.model_request_* (telemetry noise); chips stay
 * listed so operators can opt in. Tool use/result rows both remain (A2);
 * selecting either shows bidirectional Input/Output.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { isDefaultHiddenDebugType } from "../../lib/event-io";
import type { Event } from "../../lib/events";
import {
  pairSessionErrors,
  pairToolResults,
  resolveToolPair,
} from "../../lib/tool-pairing";
import { cn } from "../../lib/utils";
import { EventDetail } from "./EventDetail";
import {
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
export function getEventTypes(events: Event[]): string[] {
  const types = new Set<string>();
  for (const e of events) {
    types.add(e.type);
  }
  return Array.from(types).sort();
}

/**
 * Apply Debug type filter.
 * - selectedTypes empty → all events except default-hidden telemetry
 * - selectedTypes non-empty → only those types
 */
export function filterDebugEvents(
  events: Event[],
  selectedTypes: Set<string>
): Event[] {
  if (selectedTypes.size === 0) {
    return events.filter((e) => !isDefaultHiddenDebugType(e.type));
  }
  return events.filter((e) => selectedTypes.has(e.type));
}

/** Whether a type chip appears active under the current filter selection. */
export function isDebugTypeChipActive(
  type: string,
  selectedTypes: Set<string>
): boolean {
  if (selectedTypes.size === 0) {
    return !isDefaultHiddenDebugType(type);
  }
  return selectedTypes.has(type);
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

  const hasDefaultHidden = eventTypes.some((t) => isDefaultHiddenDebugType(t));

  // Filter by selected types (default: hide telemetry spans)
  const visibleEvents = useMemo(
    () => filterDebugEvents(filteredEvents, selectedTypes),
    [filteredEvents, selectedTypes]
  );

  // Merge consecutive agent.message events (reuses TranscriptTab logic)
  const displayEvents: DisplayEvent[] = useMemo(
    () => mergeConsecutiveAgentEvents(visibleEvents),
    [visibleEvents]
  );

  // Pair tool_use ↔ tool_result (bidirectional)
  const toolPairing = useMemo(
    () => pairToolResults(filteredEvents),
    [filteredEvents]
  );

  // Pair session.error ↔ upstream model error cause
  const sessionErrorCause = useMemo(
    () => pairSessionErrors(filteredEvents),
    [filteredEvents]
  );

  // Prefer the exact selected event inside a merged group for detail/pairing
  const selectedPrimaryEvent = useMemo(() => {
    if (!selectedDebugEventId) return null;
    const group = displayEvents.find((de) =>
      de.events.some((e) => e.id === selectedDebugEventId)
    );
    if (!group) return null;
    return (
      group.events.find((e) => e.id === selectedDebugEventId)
      ?? group.primaryEvent
    );
  }, [displayEvents, selectedDebugEventId]);

  const selectedDisplayEvent = useMemo(() => {
    if (!selectedDebugEventId) return null;
    return (
      displayEvents.find((de) =>
        de.events.some((e) => e.id === selectedDebugEventId)
      ) ?? null
    );
  }, [displayEvents, selectedDebugEventId]);

  const toolPair = useMemo(() => {
    if (!selectedPrimaryEvent) return undefined;
    return resolveToolPair(selectedPrimaryEvent, toolPairing);
  }, [selectedPrimaryEvent, toolPairing]);

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
      if (prev.size === 0) {
        // Leaving default view
        if (isDefaultHiddenDebugType(type)) {
          // Opt in telemetry while keeping default-visible types
          const next = new Set(
            eventTypes.filter((t) => !isDefaultHiddenDebugType(t))
          );
          next.add(type);
          return next;
        }
        // Narrow to a single type
        return new Set([type]);
      }
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
          <div className="mb-1 flex items-center justify-between gap-2">
            <div className="text-xs font-medium text-muted-foreground">
              Filter by type
            </div>
            {hasDefaultHidden && selectedTypes.size === 0 && (
              <span className="text-[10px] text-muted-foreground opacity-70">
                model spans hidden by default
              </span>
            )}
          </div>
          <div className="flex max-h-32 flex-wrap gap-1 overflow-y-auto">
            {eventTypes.map((type) => (
              <button
                key={type}
                type="button"
                onClick={() => toggleType(type)}
                className={cn(
                  "rounded bg-muted px-1.5 py-0.5 text-xs transition-colors",
                  isDebugTypeChipActive(type, selectedTypes)
                    ? "text-foreground"
                    : "text-muted-foreground opacity-50"
                )}
              >
                {type}
              </button>
            ))}
          </div>
          <div className="mt-1 flex flex-wrap gap-2">
            {selectedTypes.size > 0 && (
              <button
                type="button"
                onClick={() => setSelectedTypes(new Set())}
                className="text-xs text-brand hover:underline"
              >
                Reset filter
              </button>
            )}
            {hasDefaultHidden && (
              <button
                type="button"
                onClick={() => setSelectedTypes(new Set(eventTypes))}
                className="text-xs text-brand hover:underline"
              >
                Show all types
              </button>
            )}
          </div>
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
                const isSelected = de.events.some(
                  (e) => e.id === selectedDebugEventId
                );
                return (
                  <div
                    key={de.primaryEvent.id ?? `group-${idx}`}
                    ref={(el) => registerRef(de.primaryEvent.id ?? "", el)}
                  >
                    <EventRow
                      event={de.primaryEvent}
                      selected={isSelected}
                      onClick={() =>
                        onSelectDebugEvent(de.primaryEvent.id ?? null)
                      }
                      mergedCount={isMerged ? de.events.length : undefined}
                      mergedText={
                        isMerged ? getMergedEventText(de.events) : undefined
                      }
                      className={cn(isSelected && "ring-2 ring-brand")}
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

        {selectedDisplayEvent && selectedPrimaryEvent ? (
          viewMode === "rendered" ? (
            <EventDetail
              event={selectedPrimaryEvent}
              pairedResult={toolPair?.result}
              pairedUse={
                // Only pass pairedUse when viewing a result (avoid circular self)
                selectedPrimaryEvent.type === "agent.tool_result"
                || selectedPrimaryEvent.type === "agent.mcp_tool_result"
                || selectedPrimaryEvent.type === "user.custom_tool_result"
                  ? toolPair?.use
                  : undefined
              }
              modelErrorCause={
                selectedPrimaryEvent.id
                && selectedPrimaryEvent.type === "session.error"
                  ? sessionErrorCause.get(selectedPrimaryEvent.id)
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
                : JSON.stringify(selectedPrimaryEvent, null, 2)}
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
