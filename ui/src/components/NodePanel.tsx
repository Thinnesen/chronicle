import { useState } from "react";

import { clock, duration } from "../lib/time";
import { nodeLabel, type NodeStatus, type Parameter } from "../lib/workflow";
import { Lamp } from "./Lamp";
import { LogView } from "./LogView";
import "./NodePanel.css";

interface PanelProps {
  node?: NodeStatus;
  nodes: Record<string, NodeStatus>;
  namespace: string;
  workflow: string;
  onSelect: (id: string) => void;
}

type Tab = "details" | "logs" | "attempts";

// retryOf returns the retry node of a node: the node itself, or its parent when the node is an attempt.
function retryOf(node: NodeStatus, nodes: Record<string, NodeStatus>): NodeStatus | undefined {
  if (node.type === "Retry") return node;
  return Object.values(nodes).find((n) => n.type === "Retry" && n.children?.includes(node.id));
}

// NodePanel shows one node: its facts, its log and its attempts. Each node opens with its own tabs.
export function NodePanel(props: PanelProps) {
  return <NodePanelInner key={props.node?.id ?? "none"} {...props} />;
}

function NodePanelInner({ node, nodes, namespace, workflow, onSelect }: PanelProps) {
  const [tab, setTab] = useState<Tab>(node?.podName ? "logs" : "details");
  if (!node) {
    return (
      <aside className="node-panel">
        <p className="quiet">Select a node in the graph or the timeline to see its details.</p>
      </aside>
    );
  }
  const phase = node.phase || "Pending";
  const retry = retryOf(node, nodes);
  const tabs: { id: Tab; label: string }[] = [
    { id: "details", label: "Details" },
    ...(node.podName ? [{ id: "logs" as const, label: "Logs" }] : []),
    ...(retry ? [{ id: "attempts" as const, label: `Attempts (${retry.children?.length ?? 0})` }] : []),
  ];
  return (
    <aside className="node-panel" aria-label={`Node ${nodeLabel(node)}`}>
      <h2>
        <Lamp phase={phase} size={14} /> {nodeLabel(node)}
      </h2>
      {node.message && <p className="node-message">{node.message}</p>}
      <div className="tabs" role="tablist" aria-label="Node views">
        {tabs.map((t) => (
          <button key={t.id} type="button" role="tab" id={`tab-${t.id}`} aria-selected={tab === t.id} aria-controls={`view-${t.id}`} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`view-${tab}`} aria-labelledby={`tab-${tab}`} className="tab-view">
        {tab === "details" && <Details node={node} />}
        {tab === "logs" && <LogView namespace={namespace} workflow={workflow} node={node.id} live={phase === "Running" || phase === "Pending"} />}
        {tab === "attempts" && retry && <Attempts retry={retry} nodes={nodes} current={node.id} onSelect={onSelect} />}
      </div>
    </aside>
  );
}

function Attempts({ retry, nodes, current, onSelect }: { retry: NodeStatus; nodes: Record<string, NodeStatus>; current: string; onSelect: (id: string) => void }) {
  return (
    <ol className="attempts">
      {(retry.children ?? []).map((id, i) => {
        const a = nodes[id];
        if (!a) return null;
        const took = a.startedAt && a.finishedAt ? duration(Date.parse(a.finishedAt) - Date.parse(a.startedAt)) : "";
        return (
          <li key={id}>
            <button type="button" className={id === current ? "attempt is-current" : "attempt"} onClick={() => onSelect(id)}>
              <Lamp phase={a.phase || "Pending"} />
              <span>Attempt {i + 1}</span>
              <span className="quiet">{a.phase}</span>
              <span className="quiet">{took}</span>
            </button>
            {a.message && <p className="attempt-message">{a.message}</p>}
          </li>
        );
      })}
    </ol>
  );
}

function Details({ node }: { node: NodeStatus }) {
  const phase = node.phase || "Pending";
  const started = node.startedAt ? new Date(node.startedAt) : undefined;
  const finished = node.finishedAt ? new Date(node.finishedAt) : undefined;
  return (
    <>
      <dl className="facts">
        <dt>Phase</dt>
        <dd>{phase}</dd>
        <dt>Type</dt>
        <dd>{node.type}</dd>
        {node.templateName && (
          <>
            <dt>Template</dt>
            <dd>{node.templateName}</dd>
          </>
        )}
        {started && (
          <>
            <dt>Started</dt>
            <dd>{clock(started)}</dd>
            <dt>Duration</dt>
            <dd>{finished ? duration(finished.getTime() - started.getTime()) : "Running"}</dd>
          </>
        )}
        {node.podName && (
          <>
            <dt>Pod</dt>
            <dd>
              <code>{node.podName}</code>
            </dd>
          </>
        )}
        <dt>ID</dt>
        <dd>
          <code>{node.id}</code>
        </dd>
      </dl>
      <Params title="Inputs" params={node.inputs?.parameters} />
      <Params title="Outputs" params={node.outputs?.parameters} result={node.outputs?.result} />
    </>
  );
}

function Params({ title, params, result }: { title: string; params?: Parameter[]; result?: string }) {
  if (!params?.length && result === undefined) return null;
  return (
    <section className="params">
      <h3>{title}</h3>
      <dl className="facts">
        {params?.map((p) => (
          <div key={p.name} className="fact">
            <dt>{p.name}</dt>
            <dd>
              <code>{p.value}</code>
            </dd>
          </div>
        ))}
        {result !== undefined && (
          <div className="fact">
            <dt>result</dt>
            <dd>
              <code>{result}</code>
            </dd>
          </div>
        )}
      </dl>
    </section>
  );
}
