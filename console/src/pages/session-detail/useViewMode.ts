/**
 * useViewMode — cross-tab state management for SessionDetail.
 *
 * Manages:
 * - activeTab: which tab is currently active
 * - selectedEventId: selected event in Transcript tab
 * - selectedDebugEventId: selected event in Debug tab (for cross-tab navigation)
 * - debugScrollRef: ref for Debug tab scroll container
 * - setActiveTab: switch tabs
 * - scrollToDebugEvent: switch to Debug tab and scroll to event
 */

import { useCallback, useRef, useState } from "react";

export type TabId = "transcript" | "debug" | "timeline" | "team";

export interface ViewModeState {
  activeTab: TabId;
  selectedEventId: string | null;
  selectedDebugEventId: string | null;
  debugScrollRef: React.RefObject<HTMLDivElement | null>;
  setActiveTab: (tab: TabId) => void;
  setSelectedEventId: (eventId: string | null) => void;
  setSelectedDebugEventId: (eventId: string | null) => void;
  scrollToDebugEvent: (eventId: string) => void;
}

export function useViewMode(initialTab: TabId = "transcript"): ViewModeState {
  const [activeTab, setActiveTab] = useState<TabId>(initialTab);
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null);
  const [selectedDebugEventId, setSelectedDebugEventId] = useState<string | null>(null);
  const debugScrollRef = useRef<HTMLDivElement | null>(null);

  const scrollToDebugEvent = useCallback(
    (eventId: string) => {
      setSelectedDebugEventId(eventId);
      setActiveTab("debug");
      // Scroll is handled by DebugTab's useEffect watching selectedDebugEventId
    },
    []
  );

  return {
    activeTab,
    selectedEventId,
    selectedDebugEventId,
    debugScrollRef,
    setActiveTab,
    setSelectedEventId,
    setSelectedDebugEventId,
    scrollToDebugEvent,
  };
}
