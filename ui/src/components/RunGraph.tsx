import "@xyflow/react/dist/style.css";
import "./RunGraph.css";

import { Background, Controls, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useMemo, useSyncExternalStore } from "react";

import { place } from "../lib/layout";
import { duration } from "../lib/time";
import { buildGraph, isStation, nodeLabel, type NodeStatus, type Workflow } from "../lib/workflow";
import { Lamp } from "./Lamp";

type StationData = { status: NodeStatus; selected: boolean; now: number; vertical: boolean };

function nodeDuration(n: NodeStatus, now: number): string {
  if (!n.startedAt) return "";
  const end = n.finishedAt ? Date.parse(n.finishedAt) : now;
  return duration(end - Date.parse(n.startedAt));
}

function StationNode({ data }: NodeProps<Node<StationData>>) {
  const n = data.status;
  const phase = n.phase || "Pending";
  return (
    <div className={`station-node${data.selected ? " is-selected" : ""}`} data-phase={phase}>
      <Handle type="target" position={data.vertical ? Position.Top : Position.Left} />
      <Lamp phase={phase} size={14} />
      <span className="station-text">
        <span className="station-name">{nodeLabel(n)}</span>
        <span className="station-meta">{nodeDuration(n, data.now) || phase}</span>
      </span>
      <Handle type="source" position={data.vertical ? Position.Bottom : Position.Right} />
    </div>
  );
}

function JunctionNode({ data }: NodeProps<Node<StationData>>) {
  const n = data.status;
  return (
    <div className={`junction-node${data.selected ? " is-selected" : ""}`} data-phase={n.phase || "Pending"}>
      <Handle type="target" position={data.vertical ? Position.Top : Position.Left} />
      <span className="junction-mark" aria-hidden="true" />
      <span className="junction-name">{nodeLabel(n)}</span>
      <Handle type="source" position={data.vertical ? Position.Bottom : Position.Right} />
    </div>
  );
}

const nodeTypes = { station: StationNode, junction: JunctionNode };

const narrowQuery = "(max-width: 40rem)";

// useNarrow tells if the screen is narrow, so that the graph runs from top to bottom.
function useNarrow(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const m = window.matchMedia(narrowQuery);
      m.addEventListener("change", onChange);
      return () => m.removeEventListener("change", onChange);
    },
    () => window.matchMedia(narrowQuery).matches,
  );
}

// RunGraph draws the nodes of a run as a track diagram. A track lights up after its node succeeded.
export function RunGraph({ wf, now, selected, onSelect }: { wf: Workflow; now: number; selected?: string; onSelect: (id: string) => void }) {
  const narrow = useNarrow();
  const { nodes, edges, height } = useMemo(() => {
    const all = Object.values(wf.status?.nodes ?? {});
    const graphEdges = buildGraph(wf);
    const { placed, height } = place(all, graphEdges, narrow ? "TB" : "LR");
    const phase = new Map(all.map((n) => [n.id, n.phase]));
    return {
      nodes: placed.map(
        (p): Node<StationData> => ({
          id: p.node.id,
          type: isStation(p.node) ? "station" : "junction",
          position: { x: p.x, y: p.y },
          data: { status: p.node, selected: p.node.id === selected, now, vertical: narrow },
          ariaLabel: `${nodeLabel(p.node)}, ${p.node.phase || "Pending"}`,
        }),
      ),
      edges: graphEdges.map(
        (e): Edge => ({
          id: `${e.from}-${e.to}`,
          source: e.from,
          target: e.to,
          type: "smoothstep",
          className: phase.get(e.from) === "Succeeded" ? "track is-lit" : "track",
        }),
      ),
      height,
    };
  }, [wf, selected, now, narrow]);
  // The box is as high as the graph, within the limits of the CSS.
  return (
    <div className="run-graph" style={{ height: `${height + 32}px` }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={(_, n) => onSelect(n.id)}
        nodesDraggable={false}
        nodesConnectable={false}
        fitView
        fitViewOptions={{ maxZoom: 1, minZoom: narrow ? 0.6 : 0.2 }}
        preventScrolling={!narrow}
        minZoom={0.2}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={24} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
