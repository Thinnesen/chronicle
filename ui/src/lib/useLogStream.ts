import { useEffect, useState } from "react";

import { logs } from "../api/client";

// maxLines limits the lines that the browser keeps for one log.
export const maxLines = 5000;

// eslint-disable-next-line no-control-regex
const ansi = /\u001b\[[0-9;]*[A-Za-z]/g;

export interface LogLine {
  n: number;
  time?: string;
  text: string;
}

export interface LogState {
  lines: LogLine[];
  dropped: number;
  error?: string;
  done: boolean;
}

// useLogStream streams the log of one node. It follows new lines while follow is true.
export function useLogStream(namespace: string, workflow: string, node: string | undefined, follow: boolean): LogState {
  const [state, setState] = useState<LogState>({ lines: [], dropped: 0, done: false });
  useEffect(() => {
    if (!node) return;
    const abort = new AbortController();
    let count = 0;
    setState({ lines: [], dropped: 0, done: false });
    void (async () => {
      try {
        for await (const m of logs.streamLogs({ namespace, workflow, node, follow }, { signal: abort.signal })) {
          count++;
          const line: LogLine = { n: count, text: m.line.replace(ansi, ""), time: m.time ? new Date(Number(m.time.seconds) * 1000).toISOString() : undefined };
          setState((s) => {
            const lines = s.lines.length >= maxLines ? s.lines.slice(1) : s.lines.slice();
            lines.push(line);
            return { ...s, lines, dropped: s.dropped + (s.lines.length >= maxLines ? 1 : 0) };
          });
        }
        setState((s) => ({ ...s, done: true }));
      } catch (e) {
        if (!abort.signal.aborted) {
          setState((s) => ({ ...s, done: true, error: e instanceof Error ? e.message : String(e) }));
        }
      }
    })();
    return () => abort.abort();
  }, [namespace, workflow, node, follow]);
  return state;
}
