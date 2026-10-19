import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Command } from "cmdk";
import { CalendarClock, Cpu, FileCode2, Folder, Monitor, Moon, Sun, Workflow } from "lucide-react";
import type { ReactNode } from "react";

import { namespaces, workflows } from "../api/client";
import { applyTheme } from "../theme";
import { Lamp } from "./Lamp";
import "./CommandPalette.css";

// CommandPalette finds runs, sections and namespaces with the keyboard. Cmd+K or Ctrl+K opens it.
export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const params = useParams({ strict: false }) as { namespace?: string };
  const namespace = params.namespace;
  const navigate = useNavigate();
  const runs = useQuery({
    queryKey: ["palette-runs", namespace],
    queryFn: () => workflows.listWorkflows({ namespace: namespace ?? "", limit: 50 }),
    enabled: open && !!namespace,
  });
  const spaces = useQuery({ queryKey: ["namespaces"], queryFn: () => namespaces.listNamespaces({}), enabled: open });
  const go = (fn: () => void) => {
    onOpenChange(false);
    fn();
  };
  return (
    <Command.Dialog open={open} onOpenChange={onOpenChange} label="Command palette" className="palette" overlayClassName="palette-overlay">
      <Command.Input className="palette-input" placeholder={namespace ? `Search runs, sections and namespaces in ${namespace}` : "Search namespaces"} />
      <Command.List className="palette-list">
        <Command.Empty className="palette-empty">No result.</Command.Empty>
        {namespace && (
          <Command.Group heading="Go to" className="palette-group">
            <Item icon={<Workflow />} onSelect={() => go(() => void navigate({ to: "/ns/$namespace", params: { namespace } }))}>
              Runs
            </Item>
            <Item icon={<FileCode2 />} onSelect={() => go(() => void navigate({ to: "/ns/$namespace/templates", params: { namespace } }))}>
              Templates
            </Item>
            <Item icon={<CalendarClock />} onSelect={() => go(() => void navigate({ to: "/ns/$namespace/cron", params: { namespace } }))}>
              Cron
            </Item>
            <Item icon={<Cpu />} onSelect={() => go(() => void navigate({ to: "/ns/$namespace/tasks", params: { namespace } }))}>
              Tasks
            </Item>
          </Command.Group>
        )}
        {namespace && (runs.data?.workflows.length ?? 0) > 0 && (
          <Command.Group heading="Recent runs" className="palette-group">
            {runs.data?.workflows.map((w) => (
              <Item
                key={w.uid}
                value={`run ${w.name} ${w.template}`}
                icon={<Lamp phase={w.phase || "Pending"} />}
                hint={w.template}
                onSelect={() => go(() => void navigate({ to: "/ns/$namespace/runs/$name", params: { namespace, name: w.name } }))}
              >
                {w.name}
              </Item>
            ))}
          </Command.Group>
        )}
        {(spaces.data?.namespaces.length ?? 0) > 0 && (
          <Command.Group heading="Namespaces" className="palette-group">
            {spaces.data?.namespaces.map((n) => (
              <Item
                key={n.name}
                value={`namespace ${n.name}`}
                icon={<Folder />}
                hint={n.name === namespace ? "Current" : undefined}
                onSelect={() => go(() => void navigate({ to: "/ns/$namespace", params: { namespace: n.name } }))}
              >
                {n.name}
              </Item>
            ))}
          </Command.Group>
        )}
        <Command.Group heading="Theme" className="palette-group">
          <Item icon={<Monitor />} onSelect={() => go(() => applyTheme("system"))}>
            Use the system theme
          </Item>
          <Item icon={<Sun />} onSelect={() => go(() => applyTheme("light"))}>
            Use the light theme
          </Item>
          <Item icon={<Moon />} onSelect={() => go(() => applyTheme("dark"))}>
            Use the dark theme
          </Item>
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  );
}

function Item({ icon, hint, value, onSelect, children }: { icon: ReactNode; hint?: string; value?: string; onSelect: () => void; children: ReactNode }) {
  return (
    <Command.Item className="palette-item" value={value} onSelect={onSelect}>
      <span className="palette-icon" aria-hidden="true">
        {icon}
      </span>
      <span className="palette-text">{children}</span>
      {hint && <span className="palette-hint">{hint}</span>}
    </Command.Item>
  );
}
