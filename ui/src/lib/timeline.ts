import { isStation, type NodeStatus } from "./workflow";

export interface Segment {
  node: NodeStatus;
  start: number;
  end: number;
}

// A Row is one step of a run. A step with a retry strategy has one segment for each attempt.
export interface Row {
  id: string;
  label: NodeStatus;
  segments: Segment[];
  start: number;
  end: number;
}

// rows returns the timeline rows of a run, in the order of their start. Attempts of a retry share a row.
export function rows(nodes: Record<string, NodeStatus>, now: number): Row[] {
  const attemptOf = new Map<string, NodeStatus>();
  for (const n of Object.values(nodes)) {
    if (n.type === "Retry") for (const c of n.children ?? []) attemptOf.set(c, n);
  }
  const byRow = new Map<string, Row>();
  for (const n of Object.values(nodes)) {
    if (!isStation(n) || !n.startedAt) continue;
    const parent = attemptOf.get(n.id);
    const key = parent?.id ?? n.id;
    const seg = { node: n, start: Date.parse(n.startedAt), end: n.finishedAt ? Date.parse(n.finishedAt) : now };
    const row = byRow.get(key) ?? { id: key, label: parent ?? n, segments: [], start: seg.start, end: seg.end };
    row.segments.push(seg);
    row.start = Math.min(row.start, seg.start);
    row.end = Math.max(row.end, seg.end);
    byRow.set(key, row);
  }
  const out = [...byRow.values()];
  for (const r of out) r.segments.sort((a, b) => a.start - b.start);
  return out.sort((a, b) => a.start - b.start || a.id.localeCompare(b.id));
}

const steps = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14_400, 21_600, 43_200, 86_400].map((s) => s * 1000);

// ticks returns the times of the axis marks: at most max marks at a round interval.
export function ticks(span: number, max = 6): number[] {
  const step = steps.find((s) => span / s <= max) ?? steps[steps.length - 1] ?? span;
  const out: number[] = [];
  for (let t = 0; t <= span; t += step) out.push(t);
  return out;
}
