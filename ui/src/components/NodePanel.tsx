import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { workflows } from "../api/client";
import { actionError } from "../lib/errors";
import { ConfirmDialog } from "./ConfirmDialog";
import { useCanEdit } from "./RunActions";
import { useToast } from "./Toast";

import { clock, duration } from "../lib/time";
import { useNow } from "../lib/useNow";
import { artifactURL, nodeLabel, type NodeStatus, type Parameter, type Workflow } from "../lib/workflow";
import { ApprovalForm } from "./ApprovalForm";
import { CopyButton } from "./CopyButton";
import { Lamp } from "./Lamp";
import { LogView } from "./LogView";
import { NodeEvents } from "./NodeEvents";
import "./NodePanel.css";

interface PanelProps {
  wf: Workflow;
  node: NodeStatus;
  nodes: Record<string, NodeStatus>;
  namespace: string;
  workflow: string;
  onSelect: (id: string) => void;
}

type Tab = "details" | "logs" | "events" | "attempts";

// retryOf returns the retry node of a node: the node itself, or its parent when the node is an attempt.
function retryOf(node: NodeStatus, nodes: Record<string, NodeStatus>): NodeStatus | undefined {
  if (node.type === "Retry") return node;
  return Object.values(nodes).find((n) => n.type === "Retry" && n.children?.includes(node.id));
}

// NodePanel shows one node: its facts, its log and its attempts. Each node opens with its own tabs.
export function NodePanel(props: PanelProps) {
  return <NodePanelInner key={props.node.id} {...props} />;
}

function NodePanelInner({ wf, node, nodes, namespace, workflow, onSelect }: PanelProps) {
  const phase = node.phase || "Pending";
  // A Pod that has not started has no log yet; its events tell why it waits, for example a missing image.
  const [tab, setTab] = useState<Tab>(!node.podName ? "details" : phase === "Pending" ? "events" : "logs");
  const retry = retryOf(node, nodes);
  const tabs: { id: Tab; label: string }[] = [
    { id: "details", label: "Summary" },
    ...(node.podName ? [{ id: "logs" as const, label: "Logs" }, { id: "events" as const, label: "Events" }] : []),
    ...(retry ? [{ id: "attempts" as const, label: `Attempts (${retry.children?.length ?? 0})` }] : []),
  ];
  return (
    <div className="node-panel">
      {node.message && (
        <p className="node-message" data-phase={phase}>
          {node.message}
        </p>
      )}
      <ApprovalForm wf={wf} node={node} />
      <div className="tabs" role="tablist" aria-label="Node views">
        {tabs.map((t) => (
          <button key={t.id} type="button" role="tab" id={`tab-${t.id}`} aria-selected={tab === t.id} aria-controls={`view-${t.id}`} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`view-${tab}`} aria-labelledby={`tab-${tab}`} className="tab-view">
        {tab === "details" && (
          <>
            <RetryFromNode wf={wf} node={node} />
            <Details node={node} />
            <Artifacts node={node} namespace={namespace} workflow={workflow} />
          </>
        )}
        {tab === "logs" &&
          (phase === "Pending" ? (
            <p className="quiet">The step has not started yet. Its log appears here when the container starts.</p>
          ) : (
            <LogView namespace={namespace} workflow={workflow} node={node.id} live={phase === "Running"} />
          ))}
        {tab === "events" && <NodeEvents namespace={namespace} workflow={workflow} node={node.id} live={phase === "Running" || phase === "Pending"} />}
        {tab === "attempts" && retry && <Attempts retry={retry} nodes={nodes} current={node.id} onSelect={onSelect} />}
      </div>
    </div>
  );
}

// RetryFromNode runs a node and all nodes after it again, in a finished run.
function RetryFromNode({ wf, node }: { wf: Workflow; node: NodeStatus }) {
  const { name, namespace } = wf.metadata;
  const canEdit = useCanEdit(namespace);
  const toast = useToast();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const retry = useMutation({
    mutationFn: () => workflows.retryWorkflow({ namespace, name, node: node.id }),
    onSuccess: () => {
      toast(`Retrying ${name} from ${nodeLabel(node)}`);
      void client.invalidateQueries({ queryKey: ["run", namespace, name] });
    },
    onError: (err) => toast(actionError("retry runs", namespace, err), "error"),
  });
  const done = ["Succeeded", "Failed", "Error"].includes(wf.status?.phase ?? "");
  if (!canEdit || !done) return null;
  return (
    <>
      <button type="button" className="button" onClick={() => setOpen(true)} disabled={retry.isPending}>
        Retry from this node
      </button>
      <ConfirmDialog open={open} onOpenChange={setOpen} title={`Retry from ${nodeLabel(node)}?`} confirm="Retry" onConfirm={() => retry.mutate()}>
        This node and all nodes after it run again. The nodes before it keep their results.
      </ConfirmDialog>
    </>
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
  const now = useNow(!!started && !finished);
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
            <dd>{duration((finished?.getTime() ?? now) - started.getTime())}</dd>
          </>
        )}
        {node.outputs?.exitCode !== undefined && (
          <>
            <dt>Exit code</dt>
            <dd>{node.outputs.exitCode}</dd>
          </>
        )}
        {node.podName && (
          <>
            <dt>Pod</dt>
            <dd className="copyable">
              <code>{node.podName}</code>
              <CopyButton text={node.podName} label="Copy the Pod name" />
            </dd>
          </>
        )}
        <dt>ID</dt>
        <dd className="copyable">
          <code>{node.id}</code>
          <CopyButton text={node.id} label="Copy the node ID" />
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

// Artifacts lists the files of a node. Output artifacts download as tar.gz archives.
function Artifacts({ node, namespace, workflow }: { node: NodeStatus; namespace: string; workflow: string }) {
  const inputs = node.inputs?.artifacts ?? [];
  const outputs = node.outputs?.artifacts ?? [];
  if (inputs.length === 0 && outputs.length === 0) return null;
  return (
    <section className="params">
      <h3>Artifacts</h3>
      <ul className="artifacts">
        {inputs.map((a) => (
          <li key={`in-${a.name}`}>
            <span>{a.name}</span>
            <span className="quiet">input at <code>{a.path}</code></span>
          </li>
        ))}
        {outputs.map((a) => (
          <li key={`out-${a.name}`}>
            <a href={artifactURL(namespace, workflow, node.id, a.name)} download={`${a.name}.tgz`}>
              {a.name}.tgz
            </a>
            <span className="quiet">output</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
