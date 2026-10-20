import { Code, ConnectError } from "@connectrpc/connect";
import { useEffect, useState } from "react";

import { logs } from "../api/client";
import { errorText } from "./errors";

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
  // archived is true when the lines come from the log archive.
  archived?: boolean;
  // reconnecting is true while a broken stream waits to connect again.
  reconnecting?: boolean;
}

// useLogStream streams the log of one node. It follows new lines while follow is true.
export function useLogStream(namespace: string, workflow: string, node: string | undefined, follow: boolean): LogState {
  const [state, setState] = useState<LogState>({ lines: [], dropped: 0, done: false });
  // attempt counts the connections. A followed stream that breaks, for example during a server rollout, connects
  // again after a short wait and reads the log from the start.
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!node) return;
    const abort = new AbortController();
    let retry: ReturnType<typeof setTimeout> | undefined;
    let count = 0;
    setState({ lines: [], dropped: 0, done: false });
    void (async () => {
      try {
        for await (const m of logs.streamLogs({ namespace, workflow, node, follow }, { signal: abort.signal })) {
          // After a cleanup, a buffered line of the old stream must not reach the state of the new stream.
          if (abort.signal.aborted) return;
          count++;
          const line: LogLine = { n: count, text: m.line.replace(ansi, ""), time: m.time ? new Date(Number(m.time.seconds) * 1000).toISOString() : undefined };
          setState((s) => {
            const lines = s.lines.length >= maxLines ? s.lines.slice(1) : s.lines.slice();
            lines.push(line);
            return { ...s, lines, dropped: s.dropped + (s.lines.length >= maxLines ? 1 : 0), archived: s.archived || m.archived };
          });
        }
        if (!abort.signal.aborted) setState((s) => ({ ...s, done: true }));
      } catch (e) {
        if (abort.signal.aborted) return;
        if (follow && healable(e)) {
          setState((s) => ({ ...s, reconnecting: true }));
          retry = setTimeout(() => setAttempt((a) => a + 1), 2000);
          return;
        }
        setState((s) => ({ ...s, done: true, error: errorText(e) }));
      }
    })();
    return () => {
      abort.abort();
      clearTimeout(retry);
    };
  }, [namespace, workflow, node, follow, attempt]);
  return state;
}

// healable tells if a stream error can go away by itself, such as a lost connection or a server that restarts.
function healable(e: unknown): boolean {
  const code = ConnectError.from(e).code;
  return code === Code.Unavailable || code === Code.Unknown || code === Code.Internal || code === Code.DeadlineExceeded;
}
