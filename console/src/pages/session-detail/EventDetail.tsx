/**
 * EventDetail — full right-pane render for a single event.
 *
 * Uses ai-elements primitives (Message, Tool, Reasoning, Markdown).
 * Includes "View in Debug →" link for Transcript tab to cross-reference
 * the raw event in the Debug tab.
 */

import { Link2Icon } from "lucide-react";
import { Markdown } from "../../components/Markdown";
import {
  Message,
  MessageContent,
} from "../../components/ai-elements/message";
import {
  Reasoning,
  ReasoningContent,
  ReasoningTrigger,
} from "../../components/ai-elements/reasoning";
import {
  Tool,
  ToolContent,
  ToolHeader,
  ToolInput,
  ToolOutput,
} from "../../components/ai-elements/tool";
import type { Event } from "../../lib/events";

export interface EventDetailProps {
  event: Event;
  /**
   * Paired tool_result event for tool_use events. Caller pre-pairs by id
   * (tool_use_id / mcp_tool_use_id / custom_tool_use_id) and passes the
   * result here so the Tool card shows input + output in one collapsible
   * block instead of two disconnected bubbles.
   */
  pairedResult?: Event;
  /**
   * Upstream model error context for `session.error` events. The
   * SSE-delivered session.error payload only carries a generic
   * "No output generated. Check the stream for errors." message; the
   * actionable cause (rate limit, billing, model 4xx, etc.) lives on
   * the preceding `span.model_request_end` with `is_error=true`. Caller
   * walks the events array and pairs them, passing the looked-up cause
   * here so operators see the real reason inline without diving into
   * the timeline tab. Only meaningful when `event.type === "session.error"`.
   */
  modelErrorCause?: { error: string; model?: string };
  /**
   * Callback to switch to Debug tab and scroll to this event.
   * If provided, a "View in Debug →" link is shown in the detail header.
   */
  onViewInDebug?: () => void;
  /**
   * When this detail represents a group of merged consecutive events
   * (e.g., multiple agent.message events in a row), pass all events
   * here. The component will render each one as a separate Message
   * block, separated by subtle dividers, so the operator sees the
   * full conversation thread in the right pane.
   */
  mergedEvents?: Event[];
}

export function EventDetail({
  event,
  pairedResult,
  modelErrorCause,
  onViewInDebug,
  mergedEvents,
}: EventDetailProps) {
  // When multiple events are merged, render each as its own Message block
  // with a subtle divider, so the operator sees the full thread.
  const content =
    mergedEvents && mergedEvents.length > 1
      ? mergedEvents.map((e, idx) => (
          <div
            key={e.id ?? idx}
            className={idx > 0 ? "border-t border-border/50 pt-3" : ""}
          >
            {renderEventContent(e, undefined, undefined)}
          </div>
        ))
      : renderEventContent(event, pairedResult, modelErrorCause);

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {onViewInDebug && (
        <div className="flex items-center gap-2 border-b border-border pb-2 text-xs text-muted-foreground">
          <button
            type="button"
            onClick={onViewInDebug}
            className="flex items-center gap-1 hover:text-foreground transition-colors"
          >
            <Link2Icon className="h-3 w-3" />
            <span>View in Debug →</span>
          </button>
          {event.id && (
            <span className="font-mono text-[10px] opacity-60">{event.id}</span>
          )}
          {mergedEvents && mergedEvents.length > 1 && (
            <span className="ml-auto rounded-full bg-green-500/20 px-2 py-0.5 text-[10px] font-medium text-green-700">
              {mergedEvents.length} merged messages
            </span>
          )}
        </div>
      )}
      <div className="flex min-w-0 w-full flex-col gap-3">
        {content}
      </div>
    </div>
  );
}

/**
 * Render the actual event content using ai-elements primitives.
 */
function renderEventContent(
  event: Event,
  pairedResult?: Event,
  modelErrorCause?: { error: string; model?: string }
) {
  switch (event.type) {
    case "user.message": {
      const text = Array.isArray(event.content)
        ? event.content.map((b) => b.text).join("")
        : typeof event.content === "string"
          ? event.content
          : "";
      return (
        <Message from="user" className="ml-0 max-w-full">
          <MessageContent className="w-full">
            {text}
          </MessageContent>
        </Message>
      );
    }

    case "agent.message": {
      const text = (Array.isArray(event.content) ? event.content : [])
        .map((b) => b.text)
        .join("");
      return (
        <Message from="assistant" className="max-w-full">
          <MessageContent className="w-full">
            <Markdown>{text}</Markdown>
          </MessageContent>
        </Message>
      );
    }

    case "agent.thinking": {
      const text = (event as { text?: string }).text ?? "";
      if (!text) return null;
      return (
        <Reasoning isStreaming={false} defaultOpen={false}>
          <ReasoningTrigger />
          <ReasoningContent>{text}</ReasoningContent>
        </Reasoning>
      );
    }

    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use": {
      const mcpServerName =
        event.type === "agent.mcp_tool_use"
          ? (event as { mcp_server_name?: string }).mcp_server_name
          : undefined;
      const baseName = event.name ?? "tool";
      const title = mcpServerName
        ? `${baseName} (mcp · ${mcpServerName})`
        : baseName;

      const rawContent = pairedResult
        ? (pairedResult as { content?: unknown }).content
        : undefined;
      const output: unknown =
        rawContent === undefined
          ? undefined
          : typeof rawContent === "string"
            ? rawContent
            : JSON.stringify(rawContent, null, 2);

      const isError = pairedResult
        ? Boolean((pairedResult as { is_error?: boolean }).is_error)
        : false;
      const errorText = isError
        ? typeof output === "string"
          ? output
          : JSON.stringify(output ?? null)
        : undefined;
      const state = pairedResult
        ? isError
          ? "output-error"
          : "output-available"
        : "input-available";

      return (
        <Tool className="max-w-full">
          <ToolHeader type="dynamic-tool" toolName={title} state={state} />
          <ToolContent>
            <ToolInput input={event.input ?? {}} />
            <ToolOutput
              output={isError ? undefined : output}
              errorText={errorText}
            />
          </ToolContent>
        </Tool>
      );
    }

    case "agent.tool_result":
    case "agent.mcp_tool_result": {
      const rawContent = (event as { content?: unknown }).content;
      const output: unknown =
        rawContent === undefined
          ? undefined
          : typeof rawContent === "string"
            ? rawContent
            : JSON.stringify(rawContent, null, 2);
      return (
        <Tool className="max-w-full">
          <ToolHeader
            type="dynamic-tool"
            toolName="tool result (unpaired)"
            state="output-available"
          />
          <ToolContent>
            <ToolOutput output={output} errorText={undefined} />
          </ToolContent>
        </Tool>
      );
    }

    case "session.error":
      return (
        <div className="w-full bg-danger-subtle rounded-lg px-4 py-2.5 text-sm text-danger">
          <div>Error: {event.error}</div>
          {modelErrorCause && (
            <div className="mt-1.5 pt-1.5 text-[12px] opacity-90">
              <span className="font-medium">Cause</span>
              {modelErrorCause.model && (
                <span className="ml-1 font-mono opacity-75">
                  ({modelErrorCause.model})
                </span>
              )}
              : {modelErrorCause.error}
            </div>
          )}
        </div>
      );

    case "session.warning":
      return (
        <div className="w-full bg-warning-subtle rounded-lg px-4 py-2.5 text-sm text-warning">
          <div className="font-medium mb-0.5">
            Warning ({String(event.source ?? "")})
          </div>
          <div>{String(event.message ?? "")}</div>
        </div>
      );

    default:
      return (
        <div className="text-sm text-muted-foreground">
          <div className="font-mono text-xs opacity-60">{event.type}</div>
          <pre className="mt-2 overflow-x-auto rounded bg-muted p-2 text-xs">
            {JSON.stringify(event, null, 2)}
          </pre>
        </div>
      );
  }
}
