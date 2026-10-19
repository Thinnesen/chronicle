import { duration } from "../lib/time";
import { rows, ticks } from "../lib/timeline";
import { nodeLabel, type NodeStatus } from "../lib/workflow";
import { Lamp } from "./Lamp";
import "./Timeline.css";

// Timeline shows when each step ran, on one time axis. A bar is an attempt; a dashed line is the wait before
// the next attempt. It shares the selection with the graph.
export function Timeline(props: { nodes: Record<string, NodeStatus>; now: number; selected?: string; onSelect: (id: string) => void }) {
  const { nodes, now, selected, onSelect } = props;
  const all = rows(nodes, now);
  if (all.length === 0) {
    return (
      <div className="empty">
        <p>No step has started yet.</p>
      </div>
    );
  }
  const start = Math.min(...all.map((r) => r.start));
  const end = Math.max(...all.map((r) => r.end), start + 1000);
  const span = end - start;
  const pos = (t: number) => `${(100 * (t - start)) / span}%`;
  return (
    <div className="timeline" role="list" aria-label="Timeline of the steps">
      <div className="timeline-head" aria-hidden="true">
        <span className="timeline-label">Step</span>
        <span className="timeline-lane">
          {ticks(span).map((t) => (
            <span key={t} className="timeline-tick" style={{ left: pos(start + t) }}>
              {t === 0 ? "0s" : `+${duration(t)}`}
            </span>
          ))}
        </span>
        <span className="timeline-duration">Duration</span>
      </div>
      {all.map((r) => {
        const last = r.segments[r.segments.length - 1];
        const phase = last?.node.phase || "Pending";
        const attempts = r.segments.length;
        const isSelected = r.id === selected || r.segments.some((s) => s.node.id === selected);
        return (
          <div key={r.id} role="listitem" className={`timeline-row${isSelected ? " is-selected" : ""}`}>
            <button
              type="button"
              className="timeline-label"
              onClick={() => onSelect(last?.node.id ?? r.id)}
              aria-label={`${nodeLabel(r.label)}, ${phase}, ${duration(r.end - r.start)}${attempts > 1 ? `, ${attempts} attempts` : ""}`}
            >
              <Lamp phase={phase} size={13} />
              <span className="timeline-name">{nodeLabel(r.label)}</span>
              {attempts > 1 && <span className="timeline-attempts">{attempts} attempts</span>}
            </button>
            <span className="timeline-lane" aria-hidden="true">
              {ticks(span).map((t) => (
                <span key={t} className="timeline-grid" style={{ left: pos(start + t) }} />
              ))}
              {r.segments.slice(1).map((s, i) => {
                const prev = r.segments[i];
                return prev ? <span key={`wait-${s.node.id}`} className="timeline-wait" style={{ left: pos(prev.end), width: `calc(${pos(s.start)} - ${pos(prev.end)})` }} /> : null;
              })}
              {r.segments.map((s) => (
                <button
                  key={s.node.id}
                  type="button"
                  tabIndex={-1}
                  className={`timeline-bar${s.node.id === selected ? " is-selected" : ""}`}
                  data-phase={s.node.phase || "Pending"}
                  onClick={() => onSelect(s.node.id)}
                  title={`${nodeLabel(s.node)}: ${duration(s.end - s.start)}`}
                  style={{ left: pos(s.start), width: `max(4px, calc(${pos(s.end)} - ${pos(s.start)}))` }}
                />
              ))}
            </span>
            <span className="timeline-duration">{duration(r.end - r.start)}</span>
          </div>
        );
      })}
    </div>
  );
}
