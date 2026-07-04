import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { CodeBlock } from "../../components/ai-elements/code-block";
import type {
  HitlActionType,
  HitlPendingItem,
} from "./hitl";
import { defaultCustomToolResultText } from "./hitl";

interface HitlActionPanelProps {
  actionType: HitlActionType;
  items: HitlPendingItem[];
  submittingId: string | null;
  onSubmitCustomResult: (
    toolCallId: string,
    text: string,
    isError: boolean,
  ) => Promise<void>;
  onSubmitToolConfirmation: (
    toolCallId: string,
    result: "allow" | "deny",
    denyMessage?: string,
  ) => Promise<void>;
}

export function HitlActionPanel({
  actionType,
  items,
  submittingId,
  onSubmitCustomResult,
  onSubmitToolConfirmation,
}: HitlActionPanelProps) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [rejectFlags, setRejectFlags] = useState<Record<string, boolean>>({});
  const [denyMessages, setDenyMessages] = useState<Record<string, string>>({});

  const draftFor = (item: HitlPendingItem) => {
    const existing = drafts[item.toolCallId];
    if (existing !== undefined) {
      return existing;
    }
    return defaultCustomToolResultText(item);
  };

  const title = actionType === "custom_tool_result"
    ? "Human input required"
    : "Tool approval required";

  const subtitle = actionType === "custom_tool_result"
    ? "Reply to each custom tool call below to resume the session turn."
    : "Allow or deny each pending tool call to resume the session turn.";

  return (
    <div className="mb-3 rounded-lg border border-warning/40 bg-warning-subtle/40 p-4 space-y-4">
      <div>
        <div className="text-sm font-medium text-fg">{title}</div>
        <div className="text-xs text-fg-subtle mt-1">{subtitle}</div>
      </div>
      {items.map((item) => {
        const busy = submittingId === item.toolCallId;
        return (
          <div
            key={item.toolCallId}
            className="rounded-md border border-border bg-bg p-3 space-y-3"
          >
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-sm font-medium text-fg">
                  {item.toolName}
                </div>
                <div className="text-[11px] font-mono text-fg-subtle mt-0.5">
                  {item.toolCallId}
                </div>
              </div>
              <span className="text-[10px] uppercase tracking-wide text-warning font-medium">
                {item.kind === "custom_tool" ? "custom tool" : "tool"}
              </span>
            </div>
            <div>
              <div className="text-[11px] text-fg-subtle mb-1">Input</div>
              <CodeBlock code={JSON.stringify(item.args, null, 2)} language="json" />
            </div>
            {actionType === "custom_tool_result" ? (
              <>
                <div>
                  <div className="text-[11px] text-fg-subtle mb-1">Result</div>
                  <Textarea
                    value={draftFor(item)}
                    rows={5}
                    className="font-mono text-xs"
                    disabled={busy}
                    onChange={(event) => {
                      setDrafts((prev) => ({
                        ...prev,
                        [item.toolCallId]: event.target.value,
                      }));
                    }}
                  />
                </div>
                <label className="flex items-center gap-2 text-xs text-fg-subtle">
                  <input
                    type="checkbox"
                    checked={Boolean(rejectFlags[item.toolCallId])}
                    disabled={busy}
                    onChange={(event) => {
                      setRejectFlags((prev) => ({
                        ...prev,
                        [item.toolCallId]: event.target.checked,
                      }));
                    }}
                  />
                  Mark result as error (reviewer rejected)
                </label>
                <Button
                  type="button"
                  size="sm"
                  disabled={busy}
                  onClick={() => onSubmitCustomResult(
                    item.toolCallId,
                    draftFor(item),
                    Boolean(rejectFlags[item.toolCallId]),
                  )}
                >
                  {busy ? "Submitting…" : "Submit result"}
                </Button>
              </>
            ) : (
              <>
                <div>
                  <div className="text-[11px] text-fg-subtle mb-1">
                    Deny message (optional)
                  </div>
                  <Textarea
                    value={denyMessages[item.toolCallId] ?? ""}
                    rows={2}
                    className="text-xs"
                    disabled={busy}
                    onChange={(event) => {
                      setDenyMessages((prev) => ({
                        ...prev,
                        [item.toolCallId]: event.target.value,
                      }));
                    }}
                  />
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    disabled={busy}
                    onClick={() => onSubmitToolConfirmation(item.toolCallId, "allow")}
                  >
                    Allow
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => onSubmitToolConfirmation(
                      item.toolCallId,
                      "deny",
                      denyMessages[item.toolCallId] || "Denied by reviewer",
                    )}
                  >
                    Deny
                  </Button>
                </div>
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}
