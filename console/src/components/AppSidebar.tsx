import type { ComponentType } from "react";
import { NavLink, useLocation } from "react-router";

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";

import { TenantSwitcher } from "./TenantSwitcher";
import { Logo } from "./Logo";
import { UserProfile } from "./UserProfile";
import {
  AgentIcon,
  ApiKeysIcon,
  RuntimesIcon,
  DashboardIcon,
  EnvIcon,
  FilesIcon,
  GitHubIcon,
  LinearIcon,
  MemoryIcon,
  ModelCardsIcon,
  SessionsIcon,
  SkillsIcon,
  SlackIcon,
  VaultIcon,
} from "./icons";
import { consolePlugins } from "../plugins/registry";

interface NavItem {
  to: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  end?: boolean;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

/* ── Navigation groups — single source of truth for sidebar items ──
 * Each entry renders as its own labeled section in the sidebar. Order
 * here is the visual top-to-bottom order. Dashboard and API Keys are
 * pinned to the top per user request; Files and Skills live under a
 * dedicated "Build" section alongside "Managed Agents".             */
const navGroups: NavGroup[] = [
  {
    label: "Dashboard",
    items: [{ to: "/", label: "Dashboard", icon: DashboardIcon, end: true }],
  },
  {
    label: "API Keys",
    items: [{ to: "/api-keys", label: "API Keys", icon: ApiKeysIcon }],
  },
  {
    label: "Managed Agents",
    items: [
      { to: "/agents", label: "Agents", icon: AgentIcon },
      { to: "/sessions", label: "Sessions", icon: SessionsIcon },
      { to: "/environments", label: "Environments", icon: EnvIcon },
      { to: "/vaults", label: "Credential Vaults", icon: VaultIcon },
      { to: "/memory", label: "Memory Stores", icon: MemoryIcon },
    ],
  },
  {
    label: "Build",
    items: [
      { to: "/files", label: "Files", icon: FilesIcon },
      { to: "/skills", label: "Skills", icon: SkillsIcon },
      { to: "/evals", label: "Eval Runs", icon: SessionsIcon },
    ],
  },
  {
    label: "Configuration",
    items: [
      { to: "/model-cards", label: "Model Cards", icon: ModelCardsIcon },
      { to: "/runtimes", label: "Local Runtimes", icon: RuntimesIcon },
    ],
  },
  {
    label: "Integrations",
    items: [
      { to: "/integrations/linear", label: "Linear", icon: LinearIcon },
      { to: "/integrations/github", label: "GitHub", icon: GitHubIcon },
      { to: "/integrations/slack", label: "Slack", icon: SlackIcon },
    ],
  },
];

/**
 * Console sidebar — cloned from minimaxhub_benchmark/AppShell so the
 * brand-row recipe matches a known-good layout:
 *
 *   `<SidebarHeader className="bg-sidebar h-11 px-3 flex-row items-
 *   center gap-2">` directly hosts the brand row (no nested wrapper
 *   div). `flex-row` overrides shadcn's default `flex-col`, putting
 *   logo + name on one line aligned with the AppShell top toolbar.
 *
 *   `<Sidebar className="bg-sidebar border-0 group-data-[side=left]:
 *   border-r-0">` — bg-sidebar matches the AppShell outer wrapper so
 *   they read as one continuous stage; the border-0 + border-r-0
 *   pair strips shadcn's default right border which otherwise anti-
 *   aliases into a dark hairline against the rounded main panel.
 *
 * Layout from top to bottom:
 *
 *   1. SidebarHeader  — `[ logo ] openma` (h-11)
 *   2. TenantSwitcher — h-11, shares the brand-row recipe so it
 *                       collapses identically (icon at x=12, text
 *                       hides via group-data-[collapsible=icon]:hidden)
 *   3. SidebarContent — one labeled SidebarGroup per navGroups entry:
 *                       Dashboard / API Keys / Managed Agents
 *                       (Quickstart, Agents, Sessions, Environments,
 *                       Credential Vaults, Memory Stores)
 *                       / Build (Files, Skills, Eval Runs) /
 *                       Configuration (Model Cards, Local Runtimes) /
 *                       Integrations
 *   4. SidebarFooter  — UserProfile (alone — tenant lives at the top now)
 */
export function AppSidebar() {
  const { pathname } = useLocation();

  // Workflow plugin nav items (Quickstart) are surfaced inside the
  // "Managed Agents" section so they sit next to the agent-centric
  // entries rather than forming their own group. All other plugin
  // nav groups are appended after the built-in groups as before.
  const workflowsItems = consolePlugins
    .flatMap((p) => p.navGroups ?? [])
    .filter((g) => g.label === "Workflows")
    .flatMap((g) => g.items);

  const groups = [
    ...navGroups,
    ...consolePlugins.flatMap((p) =>
      (p.navGroups ?? []).filter((g) => g.label !== "Workflows"),
    ),
  ];

  const isItemActive = (to: string, end?: boolean) => {
    if (end) return pathname === to;
    return pathname === to || pathname.startsWith(`${to}/`);
  };

  const renderItem = (item: NavItem) => {
    const active = isItemActive(item.to, item.end);
    return (
      <SidebarMenuItem key={item.to}>
        <SidebarMenuButton
          asChild
          isActive={active}
          tooltip={item.label}
          // Decoration follows selection — same principle as the filter
          // chips: inactive rows are completely transparent (no pill,
          // no hover fill), only the active route gets the
          // bg-sidebar-accent pill. The `!` overrides are necessary
          // because Tailwind v4's `data-active:` variant matches the
          // attribute regardless of value (true/false both fire), so
          // shadcn's built-in `data-active:bg-sidebar-accent` would
          // otherwise paint every row.
          className={
            active
              ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
              : "!bg-transparent hover:!bg-transparent !text-sidebar-foreground hover:!text-sidebar-foreground"
          }
        >
          <NavLink to={item.to} end={item.end}>
            <item.icon className="size-4 opacity-80" />
            <span>{item.label}</span>
          </NavLink>
        </SidebarMenuButton>
      </SidebarMenuItem>
    );
  };

  return (
    <Sidebar
      collapsible="icon"
      className="bg-sidebar border-0 group-data-[side=left]:border-r-0"
    >
      <SidebarHeader className="bg-sidebar h-11 px-3 flex-row items-center gap-2">
        <Logo size="sm" />
        <span className="font-mono font-bold text-base text-brand group-data-[collapsible=icon]:hidden">
          openma
        </span>
      </SidebarHeader>

      {/* Tenant sits between brand row and nav content — same h-11 px-3
          recipe as the brand row so the collapse animation pins its
          icon at the same x=12 axis as the openma logo above. `mt-2`
          drops it 8 px below the brand row so its vertical center
          aligns with the toolbar chips on the right (PageHeader's
          py-3 pushes them down by the same amount). */}
      <div className="mt-2">
        <TenantSwitcher />
      </div>

      <SidebarContent className="bg-sidebar [&::-webkit-scrollbar]:hidden [scrollbar-width:none]">
        {/* Every group renders as its own labeled section. Quickstart
            (workflowsItems) is appended to the Managed Agents section
            since workflows are agent-orchestration primitives, not a
            standalone domain. */}
        {groups.map((g) => (
          <SidebarGroup key={g.label}>
            <SidebarGroupLabel>{g.label}</SidebarGroupLabel>
            <SidebarMenu>
              {/* Quickstart (from the workflows plugin) is rendered at the
                  top of the Managed Agents section so it sits right above
                  Agents, the first real managed entry. */}
              {g.label === "Managed Agents" &&
                workflowsItems.map((item) => renderItem(item as NavItem))}
              {g.items.map(renderItem)}
            </SidebarMenu>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter className="bg-sidebar p-0">
        <UserProfile />
      </SidebarFooter>
    </Sidebar>
  );
}
