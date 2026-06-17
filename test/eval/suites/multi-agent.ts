import type { EvalTask } from "../types.js";
import { DEFAULT_TOOLS } from "../types.js";
import {
  assertToolUsed,
  assertIdleNoError,
  assertMinEvents,
  eventsOfType,
  allOf,
} from "../verify.js";
import {
  all,
  idleNoError,
  includes,
  threadCreated,
  teamCreated,
  teamMessage,
  toolUsed,
} from "@open-managed-agents/shared";

const TEAM_LEAD_SYSTEM = `You are a team lead coordinating agents via team tools:
team_create, spawn_teammate, send_team_message, read_team_messages.

Rules:
- When creating a team, include yourself as a member in team_create's members array with display_name "lead".
- Use the exact agent_id values from the user message — never invent IDs.
- After team_create, read the returned members list to find your from_member_id for send_team_message.
- Complete all steps in the user message before finishing.`;

const TEAM_CODER_SYSTEM =
  "You are a team coder teammate. When you receive mailbox instructions, follow them exactly " +
  "using your tools (write, bash). Write files with relative paths in the workspace root " +
  "(e.g. team-eval-marker.txt), never /workspace/... absolute paths. Complete file tasks " +
  "without asking questions.";

const TEAM_MARKER_SENDMSG = "TEAM-EVAL-SENDMSG-OK";
const TEAM_MARKER_FILE = "team-eval-marker.txt";
const TEAM_MARKER_SPAWN = "TEAM-SPAWN-OK";

// Note: Multi-agent evals require creating sub-agents at runtime.
// The `subAgents` field is used by the runner to create these agents
// and inject their IDs into the parent's `callable_agents` config.

export const multiAgentSuite: EvalTask[] = [
  // T5.1 — Simple Delegation (Medium)
  {
    id: "T5.1-simple-delegation",
    category: "multi-agent",
    difficulty: "medium",
    description: "Delegate research to sub-agent, then use result",
    agentConfig: {
      system:
        "You are a coordinator. When you need information, delegate to available sub-agents using the call_agent tools. Use their responses to complete your task.",
      tools: DEFAULT_TOOLS,
    },
    subAgents: [
      {
        name: "researcher",
        system:
          "You are a concise research assistant. Answer questions directly and briefly. Do not use tools unless necessary.",
        tools: DEFAULT_TOOLS,
      },
    ],
    turns: [
      {
        message:
          "Ask the researcher agent what the Fibonacci sequence is. Then use that information to write a Python function at /workspace/fib.py that returns the nth Fibonacci number. Run a quick test: python3 -c 'from fib import fibonacci; print(fibonacci(10))'",
        verify: (events) => {
          // Check for thread events (delegation happened)
          const threadCreated = eventsOfType(events, "session.thread_created");
          const hasBash = assertToolUsed(events, "bash");
          const noError = assertIdleNoError(events);

          if (threadCreated.length === 0) {
            return {
              status: "fail",
              message: "No sub-agent delegation occurred (no session.thread_created event)",
            };
          }

          return allOf(hasBash, noError);
        },
      },
    ],
    scorer: all(threadCreated(1), toolUsed("bash"), idleNoError()),
  },

  // T5.2 — Multi-Agent Coordination (Hard)
  {
    id: "T5.2-multi-agent-coordination",
    category: "multi-agent",
    difficulty: "hard",
    description: "Coordinate two sub-agents to build and test code",
    agentConfig: {
      system: `You are a project coordinator. You have two sub-agents available:
- code-writer: writes implementation code
- test-writer: writes test code
Delegate appropriately, then verify the work yourself by running the tests.`,
      tools: DEFAULT_TOOLS,
    },
    subAgents: [
      {
        name: "code-writer",
        system:
          "You are a code writer. Write clean Python code as requested. Save files to the paths specified.",
        tools: DEFAULT_TOOLS,
      },
      {
        name: "test-writer",
        system:
          "You are a test writer. Write comprehensive Python tests as requested. Save files to the paths specified. Use assert statements and print 'ALL_TESTS_PASSED' at the end.",
        tools: DEFAULT_TOOLS,
      },
    ],
    turns: [
      {
        message: `I need:
1. Ask the code-writer to create /workspace/sort.py with a function merge_sort(arr) that implements merge sort.
2. Ask the test-writer to create /workspace/test_sort.py with tests for merge_sort (empty list, single element, already sorted, reverse sorted, duplicates). Tests should print "ALL_TESTS_PASSED".
3. Run the tests yourself with bash.`,
        verify: (events) => {
          const threads = eventsOfType(events, "session.thread_created");
          const hasBash = assertToolUsed(events, "bash");
          const noError = assertIdleNoError(events);

          if (threads.length < 2) {
            return {
              status: "fail",
              message: `Expected at least 2 sub-agent delegations, got ${threads.length}`,
            };
          }

          return allOf(hasBash, noError);
        },
      },
    ],
    scorer: all(threadCreated(2), toolUsed("bash"), idleNoError()),
    timeoutMs: 600_000, // 10 min — multi-agent takes longer
  },

  // T5.3 — Delegation with Error Handling (Hard)
  {
    id: "T5.3-delegation-error-handling",
    category: "multi-agent",
    difficulty: "hard",
    description: "Handle sub-agent failure and recover",
    agentConfig: {
      system: `You are a coordinator with a helper sub-agent. The helper may encounter errors — when that happens, fix the issue yourself and try again.`,
      tools: DEFAULT_TOOLS,
    },
    subAgents: [
      {
        name: "helper",
        system:
          "You are a helper. Read files as requested and summarize their contents. If the file doesn't exist, report the error clearly.",
        tools: DEFAULT_TOOLS,
      },
    ],
    turns: [
      {
        message: `Ask the helper agent to read /workspace/data.json and summarize it.
The file doesn't exist yet, so the helper will report an error.
When that happens, create /workspace/data.json yourself with this content: {"users": [{"name": "Alice", "role": "admin"}, {"name": "Bob", "role": "user"}]}
Then ask the helper again to read and summarize it.`,
        verify: (events) => {
          const threads = eventsOfType(events, "session.thread_created");
          const noError = assertIdleNoError(events);

          // Should have at least 2 delegations (first fails, second succeeds)
          // OR coordinator may have written the file and delegated once after
          if (threads.length < 1) {
            return {
              status: "fail",
              message: "No sub-agent delegation occurred",
            };
          }

          return noError;
        },
      },
    ],
    scorer: all(threadCreated(1), idleNoError()),
    timeoutMs: 600_000,
  },

  // T5.4 — Parallel delegation (Medium)
  {
    id: "T5.4-parallel-delegation",
    category: "multi-agent",
    difficulty: "medium",
    description:
      "Delegate to two sub-agents in the same coordinator turn (parallel tool calls)",
    agentConfig: {
      system:
        "You are a coordinator with two sub-agents. When asked to delegate in parallel, " +
        "you MUST invoke both call_agent tools in a single model turn — do not wait for " +
        "the first result before calling the second. Use the exact tool names provided.",
      tools: DEFAULT_TOOLS,
    },
    subAgents: [
      {
        name: "alpha",
        system:
          "You answer math questions with a single number only. No explanation.",
        tools: DEFAULT_TOOLS,
      },
      {
        name: "beta",
        system:
          "You answer math questions with a single number only. No explanation.",
        tools: DEFAULT_TOOLS,
      },
    ],
    turns: [
      {
        message:
          "In ONE response, call BOTH sub-agents in parallel: ask alpha 'What is 2+2?' " +
          "and ask beta 'What is 3+3?'. After both return, write /workspace/parallel.txt " +
          "with one line per answer (format: alpha=<n>, beta=<n>).",
        verify: (events) => {
          const threads = eventsOfType(events, "session.thread_created");
          const noError = assertIdleNoError(events);

          if (threads.length < 2) {
            return {
              status: "fail",
              message: `Expected >=2 parallel thread_created events, got ${threads.length}`,
            };
          }

          const bash = assertToolUsed(events, "bash");
          return allOf(bash, noError);
        },
      },
    ],
    scorer: all(threadCreated(2), toolUsed("bash"), idleNoError()),
    timeoutMs: 600_000,
  },

  // T13.1 — Team create + spawn teammate (Medium)
  {
    id: "T13.1-team-spawn",
    category: "multi-agent",
    difficulty: "medium",
    description: "Lead uses team_create and spawn_teammate to add a named teammate",
    agentConfig: {
      system: TEAM_LEAD_SYSTEM,
      tools: DEFAULT_TOOLS,
      metadata: { enable_team_tools: true },
    },
    teamWorkers: [
      {
        name: "coder",
        system: TEAM_CODER_SYSTEM,
        tools: DEFAULT_TOOLS,
      },
    ],
    turns: [
      {
        message: (ctx) => {
          const workerId = ctx.teamWorkers.coder;
          const leadId = ctx.leadAgentId;
          return `Using team tools, do exactly this in order:
1. team_create with name "eval-alpha" and members: [{ "agent_id": "${leadId}", "display_name": "lead", "role": "lead" }]
2. spawn_teammate with team_id from step 1, agent_id "${workerId}", display_name "coder", start_poll_loop true
3. bash: echo ${TEAM_MARKER_SPAWN}`;
        },
        verify: (events) => {
          const teamEvt = assertMinEvents(events, "session.team_created", 1);
          const threads = eventsOfType(events, "session.thread_created");
          const createTool = assertToolUsed(events, "team_create");
          const spawnTool = assertToolUsed(events, "spawn_teammate");
          const noError = assertIdleNoError(events);

          if (threads.length < 1) {
            return {
              status: "fail",
              message: "No teammate thread created (expected session.thread_created)",
            };
          }

          return allOf(teamEvt, createTool, spawnTool, noError);
        },
      },
    ],
    scorer: all(
      teamCreated(1),
      threadCreated(1),
      toolUsed("team_create"),
      toolUsed("spawn_teammate"),
      idleNoError(),
    ),
    timeoutMs: 600_000,
  },

  // T13.2 — SendMessage mailbox collaboration (Hard)
  {
    id: "T13.2-team-send-message",
    category: "multi-agent",
    difficulty: "hard",
    description:
      "Lead spawns teammate and sends_team_message; teammate writes marker file",
    agentConfig: {
      system: TEAM_LEAD_SYSTEM,
      tools: DEFAULT_TOOLS,
      metadata: { enable_team_tools: true },
    },
    teamWorkers: [
      {
        name: "coder",
        system: TEAM_CODER_SYSTEM,
        tools: DEFAULT_TOOLS,
      },
    ],
    turns: [
      {
        message: (ctx) => {
          const workerId = ctx.teamWorkers.coder;
          const leadId = ctx.leadAgentId;
          return `Using team tools, do exactly this in order (do not verify the file yet):
1. team_create with name "eval-mailbox" and members: [{ "agent_id": "${leadId}", "display_name": "lead", "role": "lead" }]
2. spawn_teammate with team_id from step 1, agent_id "${workerId}", display_name "coder", start_poll_loop true
3. send_team_message with team_id, from_member_id = your lead member id from step 1, to "coder", body "Write ${TEAM_MARKER_FILE} with exactly the text ${TEAM_MARKER_SENDMSG} using the write tool."`;
        },
        verify: (events) => {
          const teamEvt = assertMinEvents(events, "session.team_created", 1);
          const mailbox = assertMinEvents(events, "team.message", 1);
          const sendTool = assertToolUsed(events, "send_team_message");
          const spawnTool = assertToolUsed(events, "spawn_teammate");
          const noError = assertIdleNoError(events);

          return allOf(teamEvt, mailbox, sendTool, spawnTool, noError);
        },
      },
      {
        message: `Run: cat ${TEAM_MARKER_FILE}`,
        verify: (events) => {
          const allText = events
            .map((e) => JSON.stringify(e))
            .join("\n");
          if (!allText.includes(TEAM_MARKER_SENDMSG)) {
            return {
              status: "fail",
              message: `Marker ${TEAM_MARKER_SENDMSG} not found in turn output`,
            };
          }
          return assertIdleNoError(events);
        },
      },
    ],
    scorer: all(
      teamCreated(1),
      teamMessage(1),
      threadCreated(1),
      toolUsed("team_create"),
      toolUsed("spawn_teammate"),
      toolUsed("send_team_message"),
      includes(TEAM_MARKER_SENDMSG),
      idleNoError(),
    ),
    timeoutMs: 600_000,
  },
];
