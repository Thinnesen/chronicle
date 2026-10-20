import type { Timestamp } from "@bufbuild/protobuf/wkt";

export function toDate(t?: Timestamp): Date | undefined {
  return t ? new Date(Number(t.seconds) * 1000 + Math.floor(t.nanos / 1e6)) : undefined;
}

// duration formats a time span like "1h 4m" or "12s".
export function duration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s % 60}s`;
  return `${s}s`;
}

// clock formats a time as "14:03" today and "Oct 17, 14:03" on other days.
export function clock(d: Date, now = new Date()): string {
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (d.toDateString() === now.toDateString()) return time;
  return `${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })}, ${time}`;
}

// ago formats a past time like "just now", "4 min ago" or "3 h ago", and older times like clock.
export function ago(d: Date, now = new Date()): string {
  const s = Math.round((now.getTime() - d.getTime()) / 1000);
  if (s < 3) return "just now";
  if (s < 60) return `${s} s ago`;
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86_400) return `${Math.floor(s / 3600)} h ago`;
  return clock(d, now);
}
