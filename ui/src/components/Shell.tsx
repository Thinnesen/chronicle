import * as Menu from "@radix-ui/react-dropdown-menu";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useLocation, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
import { CalendarClock, ChevronRight, Cpu, FileCode2, LogOut, Menu as MenuIcon, Monitor, Moon, Search, Sun, Workflow, X } from "lucide-react";
import { useEffect, useState } from "react";

import { namespaces, workflows } from "../api/client";
import { logout } from "../api/session";
import { applyTheme, readTheme, type Theme } from "../theme";
import { BrandMark } from "./BrandMark";
import { CommandPalette } from "./CommandPalette";
import "./Shell.css";
import { at } from "../lib/base";

// Shell is the frame of all pages. A sidebar has the namespace and the sections. A top bar has the breadcrumbs, the
// search and the user. On a narrow screen the sidebar is a drawer.
export function Shell() {
  const { session } = useRouteContext({ from: "/app" });
  const [palette, setPalette] = useState(false);
  // The drawer stays open only on the page where the user opened it, so a link in it closes it.
  const { pathname } = useLocation();
  const [drawerAt, setDrawerAt] = useState<string | null>(null);
  const drawer = drawerAt === pathname;
  const setDrawer = (open: boolean) => setDrawerAt(open ? pathname : null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setPalette((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return (
    <div className="shell" data-drawer={drawer || undefined}>
      <aside className="sidebar" aria-label="Sidebar">
        <div className="sidebar-head">
          <Link to="/" className="brand" aria-label="chronicle home">
            <BrandMark />
            <span>chronicle</span>
          </Link>
          <button type="button" className="icon-button drawer-close" aria-label="Close the menu" onClick={() => setDrawer(false)}>
            <X />
          </button>
        </div>
        <NamespacePicker />
        <SectionNav />
        <div className="sidebar-foot">
          <ThemeSwitch />
        </div>
      </aside>
      <button type="button" className="scrim" aria-label="Close the menu" tabIndex={-1} onClick={() => setDrawer(false)} />
      <div className="workspace">
        <header className="topbar">
          <button type="button" className="icon-button drawer-open" aria-label="Open the menu" onClick={() => setDrawer(true)}>
            <MenuIcon />
          </button>
          <Breadcrumbs />
          <button type="button" className="search-button" aria-label="Search" aria-keyshortcuts="Control+K Meta+K" onClick={() => setPalette(true)}>
            <Search aria-hidden="true" />
            <span>Search</span>
            <kbd>⌘K</kbd>
          </button>
          <UserMenu user={session.user} groups={session.groups ?? []} />
        </header>
        <main className="shell-main">
          <Outlet />
        </main>
      </div>
      <CommandPalette open={palette} onOpenChange={setPalette} />
    </div>
  );
}

const sections = [
  { to: "/ns/$namespace", label: "Runs", icon: Workflow, exact: true },
  { to: "/ns/$namespace/templates", label: "Templates", icon: FileCode2, exact: false },
  { to: "/ns/$namespace/cron", label: "Cron", icon: CalendarClock, exact: false },
  { to: "/ns/$namespace/tasks", label: "Tasks", icon: Cpu, exact: false },
] as const;

// SectionNav switches between the runs, the templates, the cron workflows and the tasks of the namespace.
function SectionNav() {
  const params = useParams({ strict: false }) as { namespace?: string };
  const running = useRunningCount(params.namespace);
  if (!params.namespace) return null;
  const namespace = params.namespace;
  return (
    <nav className="section-nav" aria-label="Sections">
      {sections.map(({ to, label, icon: Icon, exact }) => (
        <Link
          key={to}
          to={to}
          params={{ namespace }}
          activeOptions={{ exact }}
          className="section-link"
          activeProps={{ "aria-current": "page" as const }}
        >
          <Icon aria-hidden="true" />
          <span>{label}</span>
          {label === "Runs" && running > 0 && (
            <span className="section-count" aria-label={`${running} running`}>
              {running}
            </span>
          )}
        </Link>
      ))}
    </nav>
  );
}

// useRunningCount returns the number of running runs of a namespace for the sidebar.
function useRunningCount(namespace?: string): number {
  const { data } = useQuery({
    queryKey: ["running", namespace],
    queryFn: () => workflows.listWorkflows({ namespace: namespace ?? "", phases: ["Running"], limit: 100 }),
    enabled: !!namespace,
    refetchInterval: 15_000,
  });
  return data?.workflows.length ?? 0;
}

// Breadcrumbs show where the user is: the namespace, the section and the object.
function Breadcrumbs() {
  const params = useParams({ strict: false }) as { namespace?: string; name?: string };
  const { pathname } = useLocation();
  const parts = pathname.split("/").filter(Boolean);
  const section = parts[2] === "runs" || parts.length === 2 ? "Runs" : (sections.find((s) => s.to.endsWith(`/${parts[2]}`))?.label ?? "");
  const crumbs =
    pathname === "/runs" ? ["All namespaces", "Runs"] : [params.namespace, params.namespace && section, params.name].filter((c): c is string => !!c);
  return (
    <nav className="breadcrumbs" aria-label="Breadcrumbs">
      {crumbs.map((c, i) => (
        <span key={`${i}-${c}`} className="crumb" aria-current={i === crumbs.length - 1 ? "page" : undefined}>
          {i > 0 && <ChevronRight aria-hidden="true" />}
          {c}
        </span>
      ))}
    </nav>
  );
}

// allNamespaces is the value of the picker for the runs of all namespaces.
const allNamespaces = "*";

function NamespacePicker() {
  const params = useParams({ strict: false }) as { namespace?: string };
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const { data } = useQuery({ queryKey: ["namespaces"], queryFn: () => namespaces.listNamespaces({}) });
  const list = data?.namespaces ?? [];
  if (list.length === 0) {
    return null;
  }
  return (
    <label className="ns-picker">
      <span className="ns-label">Namespace</span>
      <select
        value={pathname === "/runs" ? allNamespaces : (params.namespace ?? "")}
        onChange={(e) =>
          void (e.target.value === allNamespaces ? navigate({ to: "/runs" }) : navigate({ to: "/ns/$namespace", params: { namespace: e.target.value } }))
        }
      >
        {!params.namespace && pathname !== "/runs" && <option value="">Choose a namespace</option>}
        {(list.length > 1 || pathname === "/runs") && <option value={allNamespaces}>All namespaces</option>}
        {/* A namespace from a link that is not in the list still shows, so the picker never names another one. */}
        {params.namespace && !list.some((n) => n.name === params.namespace) && <option value={params.namespace}>{params.namespace}</option>}
        {list.map((n) => (
          <option key={n.name} value={n.name}>
            {n.name}
          </option>
        ))}
      </select>
    </label>
  );
}

const themes: { value: Theme; label: string; icon: typeof Sun }[] = [
  { value: "system", label: "System", icon: Monitor },
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
];

function ThemeSwitch() {
  const [theme, setTheme] = useState(readTheme);
  return (
    <div className="theme-switch" role="radiogroup" aria-label="Theme">
      {themes.map(({ value, label, icon: Icon }) => (
        <button
          key={value}
          type="button"
          role="radio"
          aria-checked={theme === value}
          aria-label={label}
          title={label}
          onClick={() => {
            applyTheme(value);
            setTheme(value);
          }}
        >
          <Icon aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}

function UserMenu({ user, groups }: { user: string; groups: string[] }) {
  const initial = (user.split(":").pop() ?? user).slice(0, 1).toUpperCase();
  return (
    <Menu.Root>
      <Menu.Trigger className="user-button" aria-label={`User ${user}`}>
        <span className="avatar" aria-hidden="true">
          {initial}
        </span>
        <span className="user-name">{user}</span>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content className="menu" align="end" sideOffset={6}>
          <Menu.Label className="menu-note">Kubernetes user: {user}</Menu.Label>
          {groups.length > 0 && <Menu.Label className="menu-note">Groups: {groups.join(", ")}</Menu.Label>}
          <Menu.Separator className="menu-rule" />
          <Menu.Item
            className="menu-item"
            onSelect={() => {
              void logout().then(() => window.location.assign(at("login")));
            }}
          >
            <LogOut aria-hidden="true" /> Log out
          </Menu.Item>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}
