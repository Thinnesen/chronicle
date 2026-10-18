import cronstrue from "cronstrue";

// readable turns a cron expression into a sentence, for example "At 03:00 AM". A bad expression stays as it is.
export function readable(expr: string): string {
  try {
    return cronstrue.toString(expr, { use24HourTimeFormat: true, verbose: false });
  } catch {
    return expr;
  }
}

// relative tells how far a time is from now, for example "in 3h 4m" or "5m ago".
export function relative(t: Date, now: number): string {
  const ms = t.getTime() - now;
  const abs = Math.abs(ms);
  const m = Math.round(abs / 60000);
  const text = m < 1 ? "less than a minute" : m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${Math.floor(m / 1440)}d ${Math.floor((m % 1440) / 60)}h`;
  return ms >= 0 ? `in ${text}` : `${text} ago`;
}
