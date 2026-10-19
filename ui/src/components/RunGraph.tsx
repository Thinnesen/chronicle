import "@xyflow/react/dist/style.css";
import "./RunGraph.css";

import { Background, Controls, Handle, Position, ReactFlow, useReactFlow, type BuiltInEdge, type FitViewOptions, type Node, type NodeProps } from "@xyflow/react";
import { Box, ChevronsDown, Cpu, Hand } from "lucide-react";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import { fold } from "../lib/fold";
import { place } from "../lib/layout";
import { duration } from "../lib/time";
import { buildGraph, isStation, nodeLabel, withPlan, type NodeStatus, type Workflow } from "../lib/workflow";
import { Lamp } from "./Lamp";

type NodeData = { status: NodeStatus; selected: boolean; now: number; vertical: boolean; width: number; hidden?: Record<string, number> };

function nodeDuration(n: NodeStatus, now: number): string {
  if (!n.startedAt) return "";
  const end = n.finishedAt ? Date.parse(n.finishedAt) : now;
  return duration(end - Date.parse(n.startedAt));
}

const kindIcon = { Pod: Box, Task: Cpu, Suspend: Hand } as const;

function Handles({ vertical }: { vertical: boolean }) {
  return (
    <>
      <Handle type="target" position={vertical ? Position.Top : Position.Left} />
      <Handle type="source" position={vertical ? Position.Bottom : Position.Right} />
    </>
  );
}

// StationNode is a card for a node that does work: a Pod, a task or a suspend.
function StationNode({ data }: NodeProps<Node<NodeData>>) {
  const n = data.status;
  const phase = n.phase || "Pending";
  const Icon = kindIcon[n.type as keyof typeof kindIcon] ?? Box;
  return (
    <div className={`station-node${data.selected ? " is-selected" : ""}${n.planned ? " is-planned" : ""}`} data-phase={phase}>
      <Handles vertical={data.vertical} />
      <Lamp phase={phase} size={16} />
      <span className="station-text">
        <span className="station-name" title={nodeLabel(n)}>
          {nodeLabel(n)}
        </span>
        <span className="station-meta">{n.planned ? "Not started" : nodeDuration(n, data.now) || phase}</span>
      </span>
      <Icon className="station-kind" aria-hidden="true" />
    </div>
  );
}

// JunctionNode is a small pill for a node that only groups others: a DAG, steps or a retry.
function JunctionNode({ data }: NodeProps<Node<NodeData>>) {
  const n = data.status;
  return (
    <div className={`junction-node${data.selected ? " is-selected" : ""}${n.planned ? " is-planned" : ""}`} data-phase={n.phase || "Pending"} style={{ width: data.width }}>
      <Handles vertical={data.vertical} />
      <Lamp phase={n.phase || "Pending"} size={11} />
      <span className="junction-name" title={nodeLabel(n)}>
        {nodeLabel(n)}
      </span>
    </div>
  );
}

// MoreNode stands for the hidden items of a large fan-out. A click shows them all.
function MoreNode({ data }: NodeProps<Node<NodeData>>) {
  const counts = Object.entries(data.hidden ?? {}).map(([phase, n]) => `${n} ${phase.toLowerCase()}`);
  return (
    <div className="station-node is-more" title="Show all items">
      <Handles vertical={data.vertical} />
      <ChevronsDown className="station-kind" aria-hidden="true" />
      <span className="station-text">
        <span className="station-name">{nodeLabel(data.status)}</span>
        <span className="station-meta">{counts.join(", ")}</span>
      </span>
    </div>
  );
}

const nodeTypes = { station: StationNode, junction: JunctionNode, more: MoreNode };

// FitOnResize fits the graph again when its box changes, for example when the node drawer opens.
function FitOnResize({ box, options }: { box: React.RefObject<HTMLDivElement | null>; options: FitViewOptions }) {
  const { fitView } = useReactFlow();
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const observer = new ResizeObserver(() => void fitView(options));
    observer.observe(el);
    return () => observer.disconnect();
  }, [box, fitView, options]);
  return null;
}

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

// RunGraph draws the nodes of a run. An edge is colored after its source succeeded, and moves while its target runs.
export function RunGraph({ wf, now, selected, onSelect }: { wf: Workflow; now: number; selected?: string; onSelect: (id: string) => void }) {
  const narrow = useNarrow();
  const box = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  // Below this zoom the names are too small to read. A wide graph then needs a pan.
  const fit = useMemo(() => ({ maxZoom: 1, minZoom: 0.5, padding: 0.08 }), []);
  const { nodes, edges, height } = useMemo(() => {
    // The root node is the run itself, which the header shows. Without it the graph is one column narrower.
    const root = wf.status?.nodes?.[wf.metadata.name];
    const hide = root && !isStation(root) ? root.id : undefined;
    const active = !["Succeeded", "Failed", "Error"].includes(wf.status?.phase ?? "");
    const source = active ? withPlan(wf) : (wf.status?.nodes ?? {});
    const visible = Object.fromEntries(Object.entries(source).filter(([id]) => id !== hide));
    const edges = buildGraph(wf, source).filter((e) => e.from !== hide && e.to !== hide);
    // A phone shows fewer items of a fan-out, because the items stand side by side.
    const folded = narrow ? fold(visible, edges, expanded, 3, 2) : fold(visible, edges, expanded);
    const all = folded.nodes;
    const graphEdges = folded.edges;
    const { placed, height } = place(all, graphEdges, narrow ? "TB" : "LR");
    const phase = new Map(all.map((n) => [n.id, n.phase]));
    const groups = new Set(all.filter((n) => !isStation(n)).map((n) => n.id));
    const planned = new Set(all.filter((n) => n.planned).map((n) => n.id));
    return {
      nodes: placed.map(
        (p): Node<NodeData> => ({
          id: p.node.id,
          type: folded.hidden[p.node.id] ? "more" : isStation(p.node) ? "station" : "junction",
          position: { x: p.x, y: p.y },
          width: p.width,
          height: p.height,
          data: { status: p.node, selected: p.node.id === selected, now, vertical: narrow, width: p.width, hidden: folded.hidden[p.node.id] },
          ariaLabel: `${nodeLabel(p.node)}, ${p.node.planned ? "not started" : p.node.phase || "Pending"}`,
        }),
      ),
      edges: graphEdges.map((e): BuiltInEdge => {
        const to = phase.get(e.to);
        // An edge out of a group shows the state of its target; an edge out of a step shows the state of the step.
        const from = groups.has(e.from) ? to : phase.get(e.from);
        const state = planned.has(e.to) ? " is-planned" : from === "Failed" || from === "Error" ? " is-failed" : from === "Succeeded" ? " is-lit" : "";
        return {
          id: `${e.from}-${e.to}`,
          source: e.from,
          target: e.to,
          type: "smoothstep",
          pathOptions: { borderRadius: 10 },
          animated: to === "Running",
          className: `track${state}`,
        };
      }),
      height,
    };
  }, [wf, selected, now, narrow, expanded]);
  return (
    <div className="run-graph" ref={box} style={{ height: `clamp(18rem, ${height + 48}px, 70vh)` }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={(_, n) => {
          if (n.data.hidden) setExpanded((e) => new Set([...e, n.id.replace(/\/more$/, "")]));
          else if (!n.data.status.planned) onSelect(n.id);
        }}
        nodesDraggable={false}
        nodesConnectable={false}
        fitView
        fitViewOptions={fit}
        preventScrolling={!narrow}
        minZoom={0.2}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={false} position="bottom-right" fitViewOptions={{ ...fit, minZoom: 0.2 }} />
        <FitOnResize box={box} options={fit} />
      </ReactFlow>
    </div>
  );
}
