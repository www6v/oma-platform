import { render, screen, fireEvent, act } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { server } from "../../mocks/server";
import { TeamPanel, type TeamRow } from "./TeamPanel";

const SESS = "sess_poll_test";
const TEAM_ID = "team_poll_test";

const mockTeam: TeamRow = {
  id: TEAM_ID,
  session_id: SESS,
  name: "alpha",
  lead_thread_id: "sthr_primary",
  lead_agent_id: "agt_lead",
  status: "active",
  members: [],
};

describe("TeamPanel task polling", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("registers a 5 s interval when Tasks tab is active and clears it on unmount", async () => {
    server.use(
      http.get(`/v1/sessions/${SESS}/teams/${TEAM_ID}/tasks`, () =>
        HttpResponse.json({ data: [] }),
      ),
    );

    const setIntervalSpy = vi.spyOn(global, "setInterval");
    const clearIntervalSpy = vi.spyOn(global, "clearInterval");

    const { unmount } = render(
      <TeamPanel
        sessionId={SESS}
        teams={[mockTeam]}
        messagesByTeamId={{ [TEAM_ID]: [] }}
        onTeamsChange={vi.fn()}
        onMessagesChange={vi.fn()}
      />,
    );

    // Switch to Tasks tab — triggers the polling useEffect
    fireEvent.click(screen.getByRole("button", { name: "tasks" }));

    // Flush the immediate async fetch and React state update
    await act(async () => {
      await Promise.resolve();
    });

    // The polling effect must have registered a 5 s interval
    const taskIntervalCallIdx = setIntervalSpy.mock.calls.findIndex(
      ([, delay]) => delay === 5000,
    );
    expect(taskIntervalCallIdx).toBeGreaterThanOrEqual(0);

    const intervalId =
      setIntervalSpy.mock.results[taskIntervalCallIdx].value;

    // Unmount — cleanup function must call clearInterval with the right id
    unmount();

    expect(clearIntervalSpy).toHaveBeenCalledWith(intervalId);
  });

  it("does not register a task interval while on the Mailbox tab", async () => {
    // No task route handler needed — should never be called
    const setIntervalSpy = vi.spyOn(global, "setInterval");

    render(
      <TeamPanel
        sessionId={SESS}
        teams={[mockTeam]}
        messagesByTeamId={{ [TEAM_ID]: [] }}
        onTeamsChange={vi.fn()}
        onMessagesChange={vi.fn()}
      />,
    );

    // Default view is mailbox — no 5 s interval for tasks
    await act(async () => {
      await Promise.resolve();
    });

    const taskIntervalCalls = setIntervalSpy.mock.calls.filter(
      ([, delay]) => delay === 5000,
    );
    expect(taskIntervalCalls.length).toBe(0);
  });

  it("stops polling when switching back to Mailbox tab", async () => {
    server.use(
      http.get(`/v1/sessions/${SESS}/teams/${TEAM_ID}/tasks`, () =>
        HttpResponse.json({ data: [] }),
      ),
    );

    const clearIntervalSpy = vi.spyOn(global, "clearInterval");

    const { unmount } = render(
      <TeamPanel
        sessionId={SESS}
        teams={[mockTeam]}
        messagesByTeamId={{ [TEAM_ID]: [] }}
        onTeamsChange={vi.fn()}
        onMessagesChange={vi.fn()}
      />,
    );

    // Switch to Tasks
    fireEvent.click(screen.getByRole("button", { name: "tasks" }));
    await act(async () => { await Promise.resolve(); });

    // Switch back to Mailbox — cleanup fires, interval cleared
    fireEvent.click(screen.getByRole("button", { name: "mailbox" }));
    await act(async () => { await Promise.resolve(); });

    expect(clearIntervalSpy).toHaveBeenCalled();

    unmount();
  });
});
