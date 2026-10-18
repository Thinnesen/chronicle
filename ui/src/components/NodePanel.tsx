import { clock, duration } from "../lib/time";
import { nodeLabel, type NodeStatus, type Parameter } from "../lib/workflow";
import { Lamp } from "./Lamp";
import "./NodePanel.css";

// NodePanel shows the facts of one node: its phase, times, inputs and outputs.
export function NodePanel({ node }: { node?: NodeStatus }) {
  if (!node) {
    return (
      <aside className="node-panel">
        <p className="quiet">Select a node in the graph or the timeline to see its details.</p>
      </aside>
    );
  }
  const phase = node.phase || "Pending";
  const started = node.startedAt ? new Date(node.startedAt) : undefined;
  const finished = node.finishedAt ? new Date(node.finishedAt) : undefined;
  return (
    <aside className="node-panel" aria-label={`Node ${nodeLabel(node)}`}>
      <h2>
        <Lamp phase={phase} size={14} /> {nodeLabel(node)}
      </h2>
      {node.message && <p className="node-message">{node.message}</p>}
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
            <dd>{duration((finished ?? new Date()).getTime() - started.getTime())}</dd>
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
    </aside>
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
