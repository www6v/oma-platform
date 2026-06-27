// Dynamic Workflows plugin - Registration file
import React, { lazy, Suspense } from "react";
import type { ConsolePlugin } from "../registry";
import "./styles.css";

// Lazy load components for code splitting
const WorkflowList = lazy(() => import("./WorkflowList"));
const WorkflowEditor = lazy(() => import("./WorkflowEditor"));
const TraceViewer = lazy(() => import("./TraceViewer"));
const WorkflowQuickstart = lazy(() => import("./WorkflowQuickstart"));

// Wrapper component with Suspense for lazy-loaded components
function LazyWorkflowList() {
  return (
    <Suspense fallback={<div>Loading workflows...</div>}>
      <WorkflowList />
    </Suspense>
  );
}

function LazyWorkflowEditor() {
  return (
    <Suspense fallback={<div>Loading editor...</div>}>
      <WorkflowEditor />
    </Suspense>
  );
}

function LazyTraceViewer() {
  return (
    <Suspense fallback={<div>Loading traces...</div>}>
      <TraceViewer />
    </Suspense>
  );
}

function LazyWorkflowQuickstart() {
  return (
    <Suspense fallback={<div>Loading quickstart...</div>}>
      <WorkflowQuickstart />
    </Suspense>
  );
}

// Simple icon component for nav
function WorkflowIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      width="20"
      height="20"
      viewBox="0 0 20 20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M3 4h14M3 10h14M3 16h14"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  );
}

// Plugin definition
const dynamicWorkflowsPlugin: ConsolePlugin = {
  id: "dynamic-workflows",

  routes: [
    {
      path: "workflows",
      element: <LazyWorkflowQuickstart />,
    },
    {
      path: "workflows/all",
      element: <LazyWorkflowList />,
    },
    {
      path: "workflows/new",
      element: <LazyWorkflowEditor />,
    },
    {
      path: "workflows/:id",
      element: <LazyWorkflowEditor />,
    },
    {
      path: "workflows/:id/traces/:executionId",
      element: <LazyTraceViewer />,
    },
  ],

  navGroups: [
    {
      label: "Workflows",
      items: [
        {
          to: "/workflows",
          label: "Quickstart",
          icon: WorkflowIcon,
          end: true,
        },
        {
          to: "/workflows/all",
          label: "All Workflows",
          icon: WorkflowIcon,
        },
      ],
    },
  ],
};

export default dynamicWorkflowsPlugin;
