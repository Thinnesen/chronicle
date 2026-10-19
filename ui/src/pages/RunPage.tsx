import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Clock, FileCode2, GanttChart, Hand, Network } from "lucide-react";
import { lazy, Suspense, useMemo } from "react";

import { workflows } from "../api/client";
import { Status } from "../components/Lamp";
import { NodeDrawer } from "../components/NodeDrawer";
import { ProgressTrack } from "../components/ProgressTrack";
import { RunActions } from "../components/RunActions";
import { Timeline } from "../components/Timeline";
import { YamlView } from "../components/YamlView";
import { RefreshError } from "../components/RefreshError";
import { ago, duration, toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import { nodeLabel, runParameters, type NodeStatus, type Workflow } from "../lib/workflow";
import { runRoute, type RunSearch } from "../router";
import "./RunPage.css";

// The graph library is large, so it loads only on this page.
const RunGraph = lazy(() => import("../components/RunGraph").then((m) => ({ default: m.RunGraph })));

const finished = new Set(["Succeeded", "Failed", "Error"]);

const tabs: { id: RunSearch["tab"]; label: string; icon: typeof Network }[] = [
  { id: undefined, label: "Graph", icon: Network },
  { id: "timeline", label: "Timeline", icon: GanttChart },
  { id: "spec", label: "Spec", icon: FileCode2 },
];

// RunPage shows one run: the header with the actions, the graph, the timeline or the spec, and the selected node
// in a drawer. It updates while the run is active.
export function RunPage() {
  const { namespace, name } = runRoute.useParams();
  const { node: selected, tab } = runRoute.useSearch();
  const navigate = useNavigate({ from: runRoute.fullPath });
  const { data, error } = useQuery({
    queryKey: ["run", namespace, name],
    queryFn: () => workflows.getWorkflow({ namespace, name }),
    refetchInterval: (q) => (finished.has(q.state.data?.summary?.phase ?? "") ? false : 2000),
  });
  const wf = useMemo(() => (data?.workflow ? (data.workflow as unknown as Workflow) : undefined), [data]);
  const now = useNow(!finished.has(data?.summary?.phase ?? ""));
  if (error && !data) return <p role="alert">Could not load the run: {error.message}</p>;
  if (!data?.summary || !wf) return <p className="quiet">Loading the run…</p>;
  const s = data.summary;
  const phase = s.phase || "Pending";
  const started = toDate(s.startedAt);
  const nodes = wf.status?.nodes ?? {};
  const node = selected ? nodes[selected] : undefined;
  const select = (id?: string) => void navigate({ search: (q) => ({ ...q, node: id }), replace: true });
  return (
    <section className="run">
      <RefreshError error={error} />
      <header className="run-head">
        <div className="run-title">
          <div className="run-name">
            <h1>{s.name}</h1>
            <Status phase={phase} />
          </div>
          <RunActions wf={wf} phase={phase} archived={s.archived} />
        </div>
        <dl className="run-facts">
          {s.template && (
            <div>
              <dt>Template</dt>
              <dd>
                <Link to="/ns/$namespace/templates/$name" params={{ namespace, name: s.template }}>
                  {s.template}
                </Link>
              </dd>
            </div>
          )}
          {started && (
            <div>
              <dt>Started</dt>
              <dd>
                <time title={started.toLocaleString()}>{ago(started, new Date(now))}</time>
              </dd>
            </div>
          )}
          {started && (
            <div>
              <dt>Duration</dt>
              <dd>
                <Clock aria-hidden="true" />
                {duration((toDate(s.finishedAt)?.getTime() ?? now) - started.getTime())}
              </dd>
            </div>
          )}
          <div>
            <dt>Steps</dt>
            <dd>
              <ProgressTrack done={s.nodesDone} total={s.nodesTotal} phase={phase} />
              <span className="quiet">
                {s.nodesDone} of {s.nodesTotal}
              </span>
            </dd>
          </div>
        </dl>
        <Parameters wf={wf} />
        {s.message && (
          <p className="run-message" data-phase={phase}>
            {s.message}
          </p>
        )}
        {!finished.has(phase) && <WaitingApprovals nodes={nodes} onReview={select} />}
        {s.archived && <p className="run-notice">This run is in the archive. Its Workflow and Pods were deleted. Logs are available only when the namespace archives them.</p>}
      </header>
      <div className="run-tabs" role="tablist" aria-label="Run views">
        {tabs.map((t) => (
          <button
            key={t.label}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => void navigate({ search: (q) => ({ ...q, tab: t.id }), replace: true })}
          >
            <t.icon aria-hidden="true" />
            {t.label}
          </button>
        ))}
      </div>
      <div className="run-body" data-drawer={node ? "" : undefined}>
        <div className="run-view card" role="tabpanel" aria-label={tabs.find((t) => t.id === tab)?.label}>
          {tab === undefined && (
            <Suspense fallback={<div className="run-graph-placeholder" />}>
              <RunGraph wf={wf} now={now} selected={selected} onSelect={select} />
            </Suspense>
          )}
          {tab === "timeline" && <Timeline nodes={nodes} now={now} selected={selected} onSelect={select} />}
          {tab === "spec" && <SpecView wf={wf} />}
        </div>
        {node && <NodeDrawer wf={wf} node={node} nodes={nodes} namespace={namespace} workflow={name} onSelect={select} onClose={() => select(undefined)} />}
      </div>
      {!node && tab !== "spec" && <p className="run-hint quiet">Select a step to see its details and logs.</p>}
    </section>
  );
}

// Parameters shows the parameters of a run as name and value.
function Parameters({ wf }: { wf: Workflow }) {
  const params = runParameters(wf);
  if (params.length === 0) return null;
  return (
    <ul className="run-params" aria-label="Parameters">
      {params.map((p) => (
        <li key={p.name}>
          <span className="run-param-name">{p.name}</span>
          <span className="run-param-value" title={p.value}>
            {p.value}
          </span>
        </li>
      ))}
    </ul>
  );
}

// SpecView shows the run as its author wrote it and, for a run of a template, the spec that the run uses.
function SpecView({ wf }: { wf: Workflow }) {
  const stored = wf.spec.workflowTemplateRef ? wf.status?.storedSpec : undefined;
  return (
    <div className="spec-view">
      <YamlView value={wf} label="The run" />
      {stored && (
        <>
          <h2 className="spec-title">Spec from the template {wf.spec.workflowTemplateRef?.name}</h2>
          <YamlView value={stored} label="The spec that the run uses" />
        </>
      )}
    </div>
  );
}

// WaitingApprovals tells that steps wait for an approval, because the phase of such a run stays Running.
// Review opens the first waiting step, where the approval form is.
function WaitingApprovals({ nodes, onReview }: { nodes: Record<string, NodeStatus>; onReview: (id: string) => void }) {
  const waiting = Object.values(nodes).filter((n) => n.type === "Suspend" && n.phase === "Running");
  const first = waiting[0];
  if (!first) return null;
  return (
    <div className="approval-notice">
      <Hand aria-hidden="true" />
      <span>{waiting.length === 1 ? `The step ${nodeLabel(first)} waits for an approval.` : `${waiting.length} steps wait for an approval.`}</span>
      <button type="button" className="button" onClick={() => onReview(first.id)}>
        Review
      </button>
    </div>
  );
}
