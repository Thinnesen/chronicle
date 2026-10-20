import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Command } from "cmdk";
import { CalendarClock, Cpu, FileCode2, Folder, Monitor, Moon, Sun, Workflow } from "lucide-react";
import type { ReactNode } from "react";

import { crons, namespaces, templates, workflows } from "../api/client";
import { applyTheme } from "../theme";
import { Lamp } from "./Lamp";
import "./CommandPalette.css";

// CommandPalette finds runs, templates, schedules, sections and namespaces with the keyboard. Cmd+K or Ctrl+K opens it.
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
  const tmpl = useQuery({
    queryKey: ["templates", namespace],
    queryFn: () => templates.listTemplates({ namespace: namespace ?? "" }),
    enabled: open && !!namespace,
  });
  const cron = useQuery({
    queryKey: ["crons", namespace],
    queryFn: () => crons.listCronWorkflows({ namespace: namespace ?? "" }),
    enabled: open && !!namespace,
  });
  const go = (fn: () => void) => {
    onOpenChange(false);
    fn();
  };
  return (
    <Command.Dialog open={open} onOpenChange={onOpenChange} label="Command palette" className="palette" overlayClassName="palette-overlay">
      <Command.Input className="palette-input" placeholder={namespace ? `Search runs, templates and schedules in ${namespace}` : "Search namespaces"} />
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
        {namespace && (tmpl.data?.templates.length ?? 0) + (cron.data?.cronWorkflows.length ?? 0) > 0 && (
          <Command.Group heading="Templates and schedules" className="palette-group">
            {tmpl.data?.templates.map((t) => (
              <Item
                key={`${t.clusterScope}/${t.name}`}
                value={`template ${t.name} ${t.description}`}
                icon={<FileCode2 />}
                hint={t.clusterScope ? "Cluster template" : "Template"}
                onSelect={() =>
                  go(() => void navigate({ to: "/ns/$namespace/templates/$name", params: { namespace, name: t.name }, search: { cluster: t.clusterScope || undefined } }))
                }
              >
                {t.name}
              </Item>
            ))}
            {cron.data?.cronWorkflows.map((c) => (
              <Item
                key={c.name}
                value={`schedule ${c.name} ${c.description}`}
                icon={<CalendarClock />}
                hint="Schedule"
                onSelect={() => go(() => void navigate({ to: "/ns/$namespace/cron/$name", params: { namespace, name: c.name } }))}
              >
                {c.name}
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
