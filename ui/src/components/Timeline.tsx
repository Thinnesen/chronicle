import { duration } from "../lib/time";
import { isStation, nodeLabel, type NodeStatus } from "../lib/workflow";
import "./Timeline.css";

// Timeline shows when each station ran, on one time axis. It shares the selection with the graph.
interface Row {
  node: NodeStatus;
  start: number;
  end: number;
}

export function Timeline(props: { nodes: NodeStatus[]; now: number; selected?: string; onSelect: (id: string) => void }) {
  const { nodes, now, selected, onSelect } = props;
  const rows: Row[] = nodes
    .filter((n) => isStation(n) && n.startedAt)
    .map((n) => ({ node: n, start: Date.parse(n.startedAt ?? ""), end: n.finishedAt ? Date.parse(n.finishedAt) : now }))
    .sort((a, b) => a.start - b.start);
  if (rows.length === 0) {
    return <p className="quiet">No node has started yet.</p>;
  }
  const start = Math.min(...rows.map((r) => r.start));
  const end = Math.max(...rows.map((r) => r.end), start + 1000);
  const span = end - start;
  return (
    <div className="timeline" role="list" aria-label="Timeline of the nodes">
      <div className="timeline-axis" aria-hidden="true">
        <span>0s</span>
        <span>{duration(span / 2)}</span>
        <span>{duration(span)}</span>
      </div>
      {rows.map(({ node: n, start: s, end: e }) => {
        const phase = n.phase || "Pending";
        return (
          <button
            key={n.id}
            type="button"
            role="listitem"
            className={`timeline-row${n.id === selected ? " is-selected" : ""}`}
            onClick={() => onSelect(n.id)}
            aria-label={`${nodeLabel(n)}, ${phase}, ${duration(e - s)}`}
          >
            <span className="timeline-label">{nodeLabel(n)}</span>
            <span className="timeline-lane">
              <span
                className="timeline-bar"
                data-phase={phase}
                style={{ left: `${(100 * (s - start)) / span}%`, width: `max(3px, ${(100 * (e - s)) / span}%)` }}
              />
            </span>
            <span className="timeline-duration">{duration(e - s)}</span>
          </button>
        );
      })}
    </div>
  );
}
