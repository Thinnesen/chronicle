import { useEffect, useRef, useState } from "react";

import { useLogStream } from "../lib/useLogStream";
import "./LogView.css";

// LogView shows the log of one node. While the node runs, new lines arrive live.
export function LogView({ namespace, workflow, node, live }: { namespace: string; workflow: string; node: string; live: boolean }) {
  const { lines, dropped, error, done } = useLogStream(namespace, workflow, node, live);
  const [filter, setFilter] = useState("");
  const box = useRef<HTMLPreElement>(null);
  const atBottom = useRef(true);
  useEffect(() => {
    const el = box.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [lines]);
  const shown = filter ? lines.filter((l) => l.text.toLowerCase().includes(filter.toLowerCase())) : lines;
  return (
    <div className="logview">
      <div className="logview-bar">
        <label className="logview-filter">
          <span className="visually-hidden">Filter the log</span>
          <input type="search" placeholder="Filter lines" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </label>
        <span className="quiet" aria-live="polite">
          {live && !done ? "Following new lines" : `${lines.length} lines`}
          {dropped > 0 && `, the first ${dropped} are not shown`}
        </span>
      </div>
      {error && <p role="alert">Could not read the log: {error}</p>}
      <pre
        ref={box}
        className="logview-lines"
        tabIndex={0}
        aria-label="Log"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
      >
        {shown.length === 0 && done && !error && <span className="quiet">The log is empty.</span>}
        {shown.map((l) => (
          <span key={l.n} className="logline">
            <span className="logline-n" aria-hidden="true">
              {l.n}
            </span>
            <span className="logline-text">{l.text}</span>
          </span>
        ))}
      </pre>
    </div>
  );
}
