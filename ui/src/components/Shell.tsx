import * as Menu from "@radix-ui/react-dropdown-menu";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
import { useState } from "react";

import { namespaces } from "../api/client";
import { logout } from "../api/session";
import { applyTheme, readTheme, type Theme } from "../theme";
import "./Shell.css";

// Shell is the frame of all pages: the brand, the namespace, the theme and the user.
export function Shell() {
  const { session } = useRouteContext({ from: "/app" });
  return (
    <div className="shell">
      <header className="shell-bar">
        <Link to="/" className="brand" aria-label="chronicle home">
          <BrandMark />
          <span>chronicle</span>
        </Link>
        <NamespacePicker />
        <div className="shell-end">
          <ThemeSwitch />
          <UserMenu user={session.user} groups={session.groups ?? []} />
        </div>
      </header>
      <SectionNav />
      <main className="shell-main">
        <Outlet />
      </main>
    </div>
  );
}

// SectionNav switches between the runs, the templates, the cron workflows and the tasks of the namespace.
function SectionNav() {
  const params = useParams({ strict: false }) as { namespace?: string };
  if (!params.namespace) return null;
  const namespace = params.namespace;
  const link = { className: "section-link", activeProps: { "aria-current": "page" as const } };
  return (
    <nav className="section-nav" aria-label="Sections">
      <Link to="/ns/$namespace" params={{ namespace }} activeOptions={{ exact: true }} {...link}>
        Runs
      </Link>
      <Link to="/ns/$namespace/templates" params={{ namespace }} {...link}>
        Templates
      </Link>
      <Link to="/ns/$namespace/cron" params={{ namespace }} {...link}>
        Cron
      </Link>
      <Link to="/ns/$namespace/tasks" params={{ namespace }} {...link}>
        Tasks
      </Link>
    </nav>
  );
}

function BrandMark() {
  return (
    <svg width="22" height="22" viewBox="0 0 32 32" aria-hidden="true">
      <path d="M4 16h24M10 16V8h12v8" fill="none" stroke="var(--track)" strokeWidth="3" />
      <circle cx="10" cy="16" r="4" fill="var(--lamp-proceed)" />
      <circle cx="22" cy="16" r="4" fill="var(--lamp-running)" />
    </svg>
  );
}

function NamespacePicker() {
  const params = useParams({ strict: false }) as { namespace?: string };
  const navigate = useNavigate();
  const { data } = useQuery({ queryKey: ["namespaces"], queryFn: () => namespaces.listNamespaces({}) });
  const list = data?.namespaces ?? [];
  if (list.length === 0) {
    return null;
  }
  return (
    <label className="ns-picker">
      <span className="visually-hidden">Namespace</span>
      <select
        value={params.namespace ?? ""}
        onChange={(e) => void navigate({ to: "/ns/$namespace", params: { namespace: e.target.value } })}
      >
        {!params.namespace && <option value="">Choose a namespace</option>}
        {list.map((n) => (
          <option key={n.name} value={n.name}>
            {n.name}
          </option>
        ))}
      </select>
    </label>
  );
}

const themes: { value: Theme; label: string }[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

function ThemeSwitch() {
  const [theme, setTheme] = useState(readTheme);
  return (
    <div className="theme-switch" role="radiogroup" aria-label="Theme">
      {themes.map((t) => (
        <button
          key={t.value}
          type="button"
          role="radio"
          aria-checked={theme === t.value}
          onClick={() => {
            applyTheme(t.value);
            setTheme(t.value);
          }}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}

function UserMenu({ user, groups }: { user: string; groups: string[] }) {
  return (
    <Menu.Root>
      <Menu.Trigger className="user-button">{user}</Menu.Trigger>
      <Menu.Portal>
        <Menu.Content className="menu" align="end" sideOffset={6}>
          <Menu.Label className="menu-note">Kubernetes user: {user}</Menu.Label>
          {groups.length > 0 && <Menu.Label className="menu-note">Groups: {groups.join(", ")}</Menu.Label>}
          <Menu.Separator className="menu-rule" />
          <Menu.Item
            className="menu-item"
            onSelect={() => {
              void logout().then(() => window.location.assign("/login"));
            }}
          >
            Log out
          </Menu.Item>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}
